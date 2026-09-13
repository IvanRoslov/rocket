// Package mirror keeps the shared repo mirrors under ~/.rocket/repos/ fresh
// and reports how fresh they are.
//
// The bug this package exists for: `git fetch` moves only remote-tracking
// refs. The mirror's own working tree — the files an agent actually reads
// with Read/Grep/Glob — stays on whatever commit it was cloned at, forever.
// So a mirror can have a perfectly current origin/main and still hand out
// weeks-old file content. Sync closes that gap by advancing the working tree
// too.
//
// The iron rule here is the mirror of workspace's: we NEVER clobber. The
// advance is strictly `git merge --ff-only`, guarded by a clean-tree check
// and a HEAD-on-default-branch check. There is no reset --hard, no
// checkout -f, no clean anywhere in this package. A mirror that cannot be
// advanced safely is left exactly as it is — and the reason is observable
// through Check().Blocked, so a stale mirror can never be read silently.
package mirror

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// Reasons why Sync cannot advance a mirror's working tree. They are shown to
// humans verbatim by the CLI, hence Russian.
const (
	// BlockedDirty: the mirror has uncommitted local changes. Nobody is
	// supposed to edit a mirror, but if someone did we would rather report
	// it than silently overwrite their work.
	BlockedDirty = "локальные изменения в зеркале"
	// BlockedNoFF: HEAD is not an ancestor of origin/<default>, so the
	// update would not be a fast-forward.
	BlockedNoFF = "fast-forward невозможен"
)

// BlockedNotOnDefault builds the Blocked reason for a mirror whose HEAD is
// not on its default branch (including a detached HEAD).
func BlockedNotOnDefault(defaultBranch string) string {
	return fmt.Sprintf("HEAD не на ветке %s", defaultBranch)
}

// Freshness describes one mirror's state. It is computed entirely from local
// data — Check never touches the network.
type Freshness struct {
	// RepoID is the store repo this mirror belongs to.
	RepoID string
	// BehindCommits is how many commits origin/<default_branch> is ahead of
	// the mirror's HEAD.
	BehindCommits int
	// LastFetch is the mtime of the mirror's FETCH_HEAD, i.e. when it last
	// talked to origin. Zero when the mirror has never been fetched.
	LastFetch time.Time
	// Blocked explains why Sync cannot advance the working tree; empty when
	// nothing is in the way.
	Blocked string
	// Stale is true when the mirror must not be trusted as a view of
	// origin: it is behind, blocked, or has not been fetched recently
	// enough.
	Stale bool
	// Head is the commit the mirror's working tree is actually on — the
	// content an agent reading this mirror gets.
	Head string
	// Upstream is the commit origin/<default_branch> points at.
	Upstream string
	// Branch is the checked-out branch, empty for a detached HEAD. The
	// package does not invent a name for a detached HEAD; naming it is the
	// caller's job.
	Branch string
	// Dirty is true when the mirror has uncommitted changes or untracked
	// files.
	Dirty bool
}

// ErrEmptyRemote marks a mirror whose remote has no branches at all — a
// repository created and never pushed to. It is a correct state, not a
// broken one, so callers can say so plainly instead of showing the raw
// "origin/<branch> not found" git error it would otherwise produce.
var ErrEmptyRemote = errors.New("remote has no branches")

// Check computes a mirror's freshness. It makes no network calls and no
// changes: it runs the same guards as Sync in read-only form. staleAfter is
// the age of LastFetch beyond which the mirror counts as stale (callers pass
// roughly twice their sync interval); now is injected so callers — and
// tests — control the clock.
func Check(ctx context.Context, repo store.Repo, staleAfter time.Duration, now time.Time) (Freshness, error) {
	if err := validate(repo); err != nil {
		return Freshness{}, err
	}

	fr := Freshness{RepoID: repo.ID}

	if _, err := runGit(ctx, repo.Path, "rev-parse", "--git-dir"); err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: not a git repository at %s: %w", repo.ID, repo.Path, err)
	}

	upstream := "origin/" + repo.DefaultBranch
	upstreamSHA, err := runGit(ctx, repo.Path, "rev-parse", "--verify", "--quiet", upstream+"^{commit}")
	if err != nil {
		// A missing origin/<branch> has two very different causes, and
		// they need different actions from the human. If the mirror has
		// no remote-tracking refs at all, the remote itself is empty —
		// correct, if useless. Anything else is a real defect. The
		// distinction is drawn from local refs on purpose: asking the
		// remote (git ls-remote) would make Check a network call.
		if empty, emptyErr := hasNoRemoteRefs(ctx, repo.Path); emptyErr == nil && empty {
			return Freshness{}, fmt.Errorf("mirror %s: %w", repo.ID, ErrEmptyRemote)
		}
		return Freshness{}, fmt.Errorf("mirror %s: %s not found: %w", repo.ID, upstream, err)
	}
	fr.Upstream = strings.TrimSpace(upstreamSHA)

	headSHA, err := runGit(ctx, repo.Path, "rev-parse", "HEAD")
	if err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: resolve HEAD: %w", repo.ID, err)
	}
	fr.Head = strings.TrimSpace(headSHA)

	behindOut, err := runGit(ctx, repo.Path, "rev-list", "--count", "HEAD.."+upstream)
	if err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: count commits behind %s: %w", repo.ID, upstream, err)
	}
	behind, err := strconv.Atoi(strings.TrimSpace(behindOut))
	if err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: parse behind count %q: %w", repo.ID, strings.TrimSpace(behindOut), err)
	}
	fr.BehindCommits = behind

	lastFetch, err := lastFetchTime(ctx, repo.Path)
	if err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: %w", repo.ID, err)
	}
	fr.LastFetch = lastFetch

	state, err := inspect(ctx, repo, upstream)
	if err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: %w", repo.ID, err)
	}
	fr.Blocked, fr.Branch, fr.Dirty = state.Blocked, state.Branch, state.Dirty

	fr.Stale = fr.BehindCommits > 0 ||
		fr.Blocked != "" ||
		fr.LastFetch.IsZero() ||
		now.Sub(fr.LastFetch) > staleAfter

	return fr, nil
}

// treeState is everything Sync's guards observe about a mirror's working
// tree. The guards have to look at the tree and the branch anyway, so the
// observations are handed back rather than thrown away: Check reports them
// as fields and would otherwise have to run the same two git commands twice.
type treeState struct {
	Dirty   bool
	Branch  string
	Blocked string
}

// inspect runs Sync's three guards read-only, in the same order Sync applies
// them, and reports the first reason that would stop the fast-forward along
// with what it saw on the way.
func inspect(ctx context.Context, repo store.Repo, upstream string) (treeState, error) {
	var st treeState

	dirty, err := isDirty(ctx, repo.Path)
	if err != nil {
		return st, err
	}
	st.Dirty = dirty

	branch, err := currentBranch(ctx, repo.Path)
	if err != nil {
		return st, err
	}
	st.Branch = branch

	switch {
	case dirty:
		st.Blocked = BlockedDirty
	case branch != repo.DefaultBranch:
		st.Blocked = BlockedNotOnDefault(repo.DefaultBranch)
	default:
		// `merge-base --is-ancestor` exits 1 (without a message) when HEAD
		// is not an ancestor of upstream, i.e. when the merge would not be
		// a fast-forward.
		if _, err := runGit(ctx, repo.Path, "merge-base", "--is-ancestor", "HEAD", upstream); err != nil {
			st.Blocked = BlockedNoFF
		}
	}

	return st, nil
}

// blockedReason is inspect for the callers that only want the reason.
func blockedReason(ctx context.Context, repo store.Repo, upstream string) (string, error) {
	st, err := inspect(ctx, repo, upstream)
	return st.Blocked, err
}

// SyncResult is everything one pass over a mirror observed. It exists
// because the merge error used to be logged and dropped (#3576): a mirror
// that could not fast-forward reported the same nil as one that did, and the
// only trace was a warn line nobody was reading.
//
// Blocked and the errors are deliberately different things. Blocked is Sync
// refusing to clobber — a correct, expected outcome with nothing to fix on
// our side. FetchErr and MergeErr are git failing at something it was asked
// to do, and they travel up as an error.
type SyncResult struct {
	// Advanced is how many commits the working tree moved forward by.
	Advanced int
	// Blocked is why the fast-forward was not attempted, verbatim from the
	// reasons Check reports; empty when nothing was in the way.
	Blocked string
	// FetchErr is a failed `git fetch origin --prune`. Sync still tries to
	// fast-forward from the refs already on disk afterwards.
	FetchErr error
	// MergeErr is a failed `git merge --ff-only` — the swallowed error this
	// type exists for. It is set only when none of the guards fired, i.e.
	// when the merge genuinely should have worked.
	MergeErr error
	// LockWaited is how long the caller waited for the mirror's lock. Filled
	// by the locked entry points; zero here.
	LockWaited time.Duration
	// IndexLockRemoved records that an abandoned .git/index.lock was reaped
	// before the sync. Filled by the locked entry points; false here.
	IndexLockRemoved bool
}

// Err joins the failures the caller has to know about. A blocked mirror is
// not among them: refusing to clobber is the design, not a fault.
func (r SyncResult) Err() error {
	return errors.Join(r.FetchErr, r.MergeErr)
}

// Sync brings a mirror up to date with origin: it fetches (the only network
// call in this package) and then strictly fast-forwards the working tree to
// origin/<default_branch>.
//
// A mirror that is dirty, off its default branch, or not fast-forwardable is
// left untouched: the reason lands in SyncResult.Blocked and the returned
// error stays nil, because there is nothing the caller can do about it and
// the state is separately observable through Check. A failing fetch is not
// fatal either: Sync warns, still attempts the fast-forward with the refs
// already on disk (so an offline mirror can at least catch up to its last
// fetch), and reports the fetch error afterwards.
//
// A `merge --ff-only` that fails after every guard passed is a different
// animal — git refusing a merge it should have been able to do, most often
// because another process holds the index — and it is returned, not logged
// and forgotten.
func Sync(ctx context.Context, repo store.Repo) (SyncResult, error) {
	var res SyncResult

	if err := validate(repo); err != nil {
		return res, err
	}

	if out, err := runGit(ctx, repo.Path, "fetch", "origin", "--prune"); err != nil {
		res.FetchErr = fmt.Errorf("mirror %s: fetch origin --prune: %w", repo.ID, err)
		slog.Warn("mirror: fetch failed, continuing with local refs",
			"repo", repo.ID, "path", repo.Path, "error", err, "output", strings.TrimSpace(out))
	}

	upstream := "origin/" + repo.DefaultBranch
	st, err := inspect(ctx, repo, upstream)
	if err != nil {
		slog.Warn("mirror: cannot determine worktree state, leaving mirror untouched",
			"repo", repo.ID, "path", repo.Path, "error", err)
		return res, res.Err()
	}
	if st.Blocked != "" {
		res.Blocked = st.Blocked
		slog.Warn("mirror: skipping fast-forward",
			"repo", repo.ID, "path", repo.Path, "branch", st.Branch, "blocked", st.Blocked)
		return res, res.Err()
	}

	// HEAD before the merge is the only way to say how far the mirror moved:
	// the pre-sync behind count was taken before the fetch and misses exactly
	// the commits the fetch brought in.
	before, err := revParseHead(ctx, repo.Path)
	if err != nil {
		slog.Warn("mirror: cannot resolve HEAD before fast-forward",
			"repo", repo.ID, "path", repo.Path, "error", err)
	}

	if out, err := runGit(ctx, repo.Path, "merge", "--ff-only", upstream); err != nil {
		res.MergeErr = fmt.Errorf("mirror %s: merge --ff-only %s: %w", repo.ID, upstream, err)
		slog.Warn("mirror: fast-forward failed",
			"repo", repo.ID, "path", repo.Path, "error", err, "output", strings.TrimSpace(out))
		return res, res.Err()
	}

	res.Advanced = advanceFrom(ctx, repo.Path, before)

	return res, res.Err()
}

// advanceFrom counts how far HEAD moved from before. It never fails the
// sync: the fast-forward has already happened by the time it runs, and a
// count we could not take is worth a log line, not an error.
func advanceFrom(ctx context.Context, path, before string) int {
	if before == "" {
		return 0
	}
	after, err := revParseHead(ctx, path)
	if err != nil || after == before {
		return 0
	}
	out, err := runGit(ctx, path, "rev-list", "--count", before+".."+after)
	if err != nil {
		slog.Warn("mirror: cannot count the advance", "path", path, "error", err)
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0
	}
	return n
}

func validate(repo store.Repo) error {
	if repo.Path == "" {
		return errors.New("mirror: repo.Path must not be empty")
	}
	if repo.DefaultBranch == "" {
		return fmt.Errorf("mirror %s: repo.DefaultBranch must not be empty", repo.ID)
	}
	return nil
}

// isDirty reports whether the mirror has uncommitted changes or untracked
// files.
func isDirty(ctx context.Context, path string) (bool, error) {
	out, err := runGit(ctx, path, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status --porcelain: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

// currentBranch returns the checked-out branch name, or "" for a detached
// HEAD (which `symbolic-ref` reports as an error, not a failure of ours).
func currentBranch(ctx context.Context, path string) (string, error) {
	out, err := runGit(ctx, path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// lastFetchTime returns the mtime of the mirror's FETCH_HEAD, or the zero
// time when the mirror has never been fetched.
func lastFetchTime(ctx context.Context, path string) (time.Time, error) {
	gitDir, err := runGit(ctx, path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return time.Time{}, fmt.Errorf("resolve git dir: %w", err)
	}
	info, err := os.Stat(filepath.Join(strings.TrimSpace(gitDir), "FETCH_HEAD"))
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("stat FETCH_HEAD: %w", err)
	}
	return info.ModTime(), nil
}

// runGit runs `git -C path <args...>`, no shell involved. It returns the
// combined output either way, so callers can log what git actually said.
func runGit(ctx context.Context, path string, args ...string) (string, error) {
	return runGitEnv(ctx, path, nil, args...)
}

// runGitEnv is runGit with extra environment variables appended to the
// daemon's own, so a later duplicate wins.
func runGitEnv(ctx context.Context, path string, env []string, args ...string) (string, error) {
	fullArgs := append([]string{"-C", path}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// hasNoRemoteRefs reports whether the mirror carries no remote-tracking refs
// under refs/remotes/origin, i.e. the remote had nothing to offer the last
// time we fetched it. Purely local: it reads the mirror's own ref store.
func hasNoRemoteRefs(ctx context.Context, path string) (bool, error) {
	out, err := runGit(ctx, path, "for-each-ref", "--count=1", "refs/remotes/origin")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}
