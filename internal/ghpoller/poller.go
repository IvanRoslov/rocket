// Package ghpoller periodically polls GitHub for pull-request and
// check-run status on behalf of live worker sessions, persisting changes to
// the store and publishing events/notifications when a PR is opened, its CI
// status changes, a reviewer requests changes, or it is merged.
package ghpoller

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/store"
)

// PRNotifier receives notifications about pull-request lifecycle events
// worth surfacing to a human or orchestrator. Implementations are expected
// to be cheap/non-blocking; Task 6 provides the real reactions
// implementation, this package only depends on the interface.
type PRNotifier interface {
	PROpened(sess store.Session, pr *github.PR)
	CIFailing(sess store.Session, pr *github.PR, failingSummary string)
	ChangesRequested(sess store.Session, pr *github.PR)
	Merged(sess store.Session, pr *github.PR)
}

// NopNotifier is a PRNotifier that does nothing. It is a placeholder for
// daemon wiring until Task 6's reactions implementation is available.
type NopNotifier struct{}

func (NopNotifier) PROpened(store.Session, *github.PR)          {}
func (NopNotifier) CIFailing(store.Session, *github.PR, string) {}
func (NopNotifier) ChangesRequested(store.Session, *github.PR)  {}
func (NopNotifier) Merged(store.Session, *github.PR)            {}

// Poller periodically polls GitHub for PR/CI status on live worker sessions.
type Poller struct {
	st     *store.Store
	bus    *bus.Bus
	gh     func() (*github.Client, error)
	cfg    *config.Config
	notify PRNotifier

	// runGit executes `git -C dir remote get-url origin`; overridable by
	// tests.
	runGit func(ctx context.Context, dir string) (string, error)

	mu              sync.Mutex
	lastReviewState map[string]string // sessionID -> last observed ReviewDecision
}

// New builds a Poller.
func New(st *store.Store, b *bus.Bus, gh func() (*github.Client, error), cfg *config.Config, notify PRNotifier) *Poller {
	return &Poller{
		st:              st,
		bus:             b,
		gh:              gh,
		cfg:             cfg,
		notify:          notify,
		runGit:          runGitRemoteOrigin,
		lastReviewState: make(map[string]string),
	}
}

func runGitRemoteOrigin(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Run runs Tick at cfg.GithubPollInterval, blocking until ctx is cancelled.
// It performs one immediate Tick before the first tick.
func (p *Poller) Run(ctx context.Context) {
	if err := p.Tick(ctx); err != nil {
		slog.Error("ghpoller: tick", "error", err)
	}

	ticker := time.NewTicker(p.cfg.GithubPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Tick(ctx); err != nil {
				slog.Error("ghpoller: tick", "error", err)
			}
		}
	}
}

// repoInfo caches the owner/repo resolution for a repo_id, along with
// whether resolution succeeded (a false ok means "skip sessions in this
// repo for the rest of this tick").
type repoInfo struct {
	owner, repo string
	ok          bool
}

// Tick runs a single polling pass over every live worker session.
func (p *Poller) Tick(ctx context.Context) error {
	client, err := p.gh()
	if err != nil {
		slog.Debug("ghpoller: skipping tick", "error", err)
		return nil
	}

	sessions, err := p.st.ListSessions(store.SessionFilter{Kind: "worker"})
	if err != nil {
		return err
	}

	repoCache := make(map[string]repoInfo)

	for _, sess := range sessions {
		owner, repo, ok := p.resolveOwnerRepo(ctx, sess.RepoID, repoCache)
		if !ok {
			continue
		}

		err := p.tickSession(ctx, client, sess, owner, repo)
		if errors.Is(err, github.ErrBackoff) {
			slog.Warn("ghpoller: backoff, aborting tick early", "error", err)
			return nil
		}
		if err != nil {
			slog.Error("ghpoller: tick session", "session", sess.ID, "error", err)
			continue
		}
	}
	return nil
}

// resolveOwnerRepo resolves the GitHub owner/repo for a repo_id, caching
// the result (including failures) for the remainder of the current tick.
func (p *Poller) resolveOwnerRepo(ctx context.Context, repoID string, cache map[string]repoInfo) (owner, repo string, ok bool) {
	if info, cached := cache[repoID]; cached {
		return info.owner, info.repo, info.ok
	}

	fail := func() (string, string, bool) {
		cache[repoID] = repoInfo{}
		return "", "", false
	}

	r, err := p.st.GetRepo(repoID)
	if err != nil {
		return fail()
	}

	remoteURL, err := p.runGit(ctx, r.Path)
	if err != nil {
		return fail()
	}

	owner, repo, ok = github.ParseRemote(remoteURL)
	if !ok {
		return fail()
	}

	cache[repoID] = repoInfo{owner: owner, repo: repo, ok: true}
	return owner, repo, true
}

// tickSession polls GitHub for a single session's PR/CI status and persists
// any changes.
func (p *Poller) tickSession(ctx context.Context, client *github.Client, sess store.Session, owner, repo string) error {
	// A stored PRState of "closed" (i.e. closed-unmerged) means the PR we
	// were tracking is dead; go back to branch discovery so a NEW PR pushed
	// to the same branch is picked up. discoverPR itself guards against
	// re-processing the same closed-unmerged PR forever.
	if sess.PRNumber == 0 || sess.PRState == "closed" {
		return p.discoverPR(ctx, client, sess, owner, repo)
	}
	return p.updatePR(ctx, client, sess, owner, repo)
}

func (p *Poller) discoverPR(ctx context.Context, client *github.Client, sess store.Session, owner, repo string) error {
	prStub, err := client.FindPRByBranch(ctx, owner, repo, sess.Branch)
	if err != nil {
		return err
	}
	if prStub == nil {
		return nil
	}

	// FindPRByBranch returns minimal data without ReviewDecision; fetch the
	// full PR to get authoritative review state. This one extra API call per
	// discovery (a rare event) is acceptable to avoid seeding incorrect
	// notification state.
	pr, err := client.GetPR(ctx, owner, repo, prStub.Number)
	if err != nil {
		return err
	}

	prState := mapPRState(pr)

	// Guard against re-processing the same closed-unmerged PR: FindPRByBranch
	// queries state=all, so once we've recorded a PR as closed-unmerged it
	// keeps being returned on every subsequent tick. Without this guard
	// we'd re-fire pr.opened/PROpened (and any alert notifications) forever
	// for a PR that hasn't actually changed. If a NEW PR was opened on the
	// branch (different number) or this same PR has since merged, we fall
	// through and record it normally.
	if sess.PRNumber != 0 && pr.Number == sess.PRNumber && sess.PRState == "closed" && prState != "merged" {
		return nil
	}

	ci, err := client.CheckRollup(ctx, owner, repo, pr.HeadSHA)
	if err != nil {
		return err
	}

	if err := p.st.UpdateSessionPR(sess.ID, pr.Number, prState, ci); err != nil {
		return err
	}

	p.bus.Publish("pr.opened", sess.ID, map[string]any{"number": pr.Number})
	p.notify.PROpened(sess, pr)

	if prState == "merged" {
		// Stale discovery: the PR was already merged by the time we found it.
		p.bus.Publish("pr.merged", sess.ID, map[string]any{"number": pr.Number})
		p.notify.Merged(sess, pr)
	}

	// Fire notifications for alert states at discovery time.
	if ci == "failing" {
		p.notify.CIFailing(sess, pr, "checks failing")
	}
	if pr.ReviewDecision == "changes_requested" {
		p.notify.ChangesRequested(sess, pr)
	}

	p.setLastReviewState(sess.ID, pr.ReviewDecision)
	return nil
}

func (p *Poller) updatePR(ctx context.Context, client *github.Client, sess store.Session, owner, repo string) error {
	pr, err := client.GetPR(ctx, owner, repo, sess.PRNumber)
	if err != nil {
		return err
	}

	ci, err := client.CheckRollup(ctx, owner, repo, pr.HeadSHA)
	if err != nil {
		return err
	}

	newPRState := mapPRState(pr)

	// Capture the previously-stored state BEFORE persisting: the once-only
	// notification semantics below (merged fires once, CI-changed fires only
	// on an actual transition) must derive from what was on disk before this
	// tick, not from the value we're about to write.
	prevPRState := sess.PRState
	prevCIState := sess.CIState

	// Persist before notifying: if the store write fails, return the error
	// and skip all events/notifications rather than risk notifying about a
	// state change that never made it to disk (which would desync from a
	// future tick's dedup logic keyed on stored state).
	if newPRState != prevPRState || ci != prevCIState {
		if err := p.st.UpdateSessionPR(sess.ID, pr.Number, newPRState, ci); err != nil {
			return err
		}
	}

	if newPRState == "merged" && prevPRState != "merged" {
		p.bus.Publish("pr.merged", sess.ID, map[string]any{"number": pr.Number})
		p.notify.Merged(sess, pr)
	}

	if ci != prevCIState {
		p.bus.Publish("pr.ci_changed", sess.ID, map[string]any{
			"number": pr.Number,
			"from":   prevCIState,
			"to":     ci,
		})
		if ci == "failing" {
			p.notify.CIFailing(sess, pr, "checks failing")
		}
	}

	last := p.getLastReviewState(sess.ID)
	if pr.ReviewDecision == "changes_requested" && last != "changes_requested" {
		p.notify.ChangesRequested(sess, pr)
	}
	p.setLastReviewState(sess.ID, pr.ReviewDecision)

	return nil
}

func (p *Poller) getLastReviewState(sessionID string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastReviewState[sessionID]
}

func (p *Poller) setLastReviewState(sessionID, state string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastReviewState[sessionID] = state
}

// mapPRState maps a GitHub PR's raw state/merged fields to rocket's
// pr_state vocabulary: "open", "closed" or "merged".
func mapPRState(pr *github.PR) string {
	if pr.State == "closed" && pr.Merged {
		return "merged"
	}
	if pr.State == "closed" {
		return "closed"
	}
	return "open"
}
