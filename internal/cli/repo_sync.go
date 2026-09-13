package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirror"
	"github.com/IvanRoslov/rocket/internal/store"
)

// selectMirrors narrows the mirrors to the ids the user named, in the order
// they named them, and hands back the ids that matched nothing.
//
// An id that is not a mirror is returned rather than dropped: `rocket repo
// sync app` that silently syncs nothing because the registry calls it
// "myapp" is exactly the kind of quiet no-op this command exists to replace.
// With no ids given every mirror is selected.
func selectMirrors(mirrors []repoRow, ids []string) (selected []repoRow, unknown []string) {
	if len(ids) == 0 {
		return mirrors, nil
	}

	byID := make(map[string]repoRow, len(mirrors))
	for _, m := range mirrors {
		byID[m.ID] = m
	}

	for _, id := range ids {
		m, ok := byID[id]
		if !ok {
			unknown = append(unknown, id)
			continue
		}
		selected = append(selected, m)
	}
	return selected, unknown
}

// syncOutcome is what one pass over one mirror did. A blocked mirror is a
// perfectly normal outcome — Sync refuses to clobber, by design — so Blocked
// is a report, not an error. Err is reserved for the mirrors that could not
// be examined at all.
type syncOutcome struct {
	RepoID string
	// Advanced is how many commits the working tree moved forward by.
	Advanced int
	// Blocked is why the mirror still could not be advanced, verbatim from
	// the mirror package; empty when it could.
	Blocked string
	// Repaired is true when --repair actually changed something.
	Repaired bool
	// RescueBranch names the branch the mirror's uncommitted changes were
	// committed to, empty when there was nothing to rescue.
	RescueBranch string
	// Err is what stopped us from examining the mirror at all.
	Err error
	// FetchErr is a failed `git fetch`. Sync fast-forwards from the refs
	// already on disk anyway, so this is reported alongside the outcome, not
	// instead of it.
	FetchErr error
}

// renderSync writes one line per mirror, then the ids that matched nothing.
func renderSync(outcomes []syncOutcome, unknown []string, w io.Writer) {
	for _, o := range outcomes {
		fmt.Fprintln(w, syncLine(o))
	}
	for _, id := range unknown {
		fmt.Fprintf(w, "неизвестное зеркало: %s\n", id)
	}
}

// syncLine renders one mirror's outcome. Blocked reasons come from the
// mirror package verbatim — do not reword them here.
//
// When a repair happened the rescue branch leads the line: where someone's
// uncommitted work went is the one thing they must never have to go looking
// for.
func syncLine(o syncOutcome) string {
	if o.Err != nil {
		return fmt.Sprintf("mirror %s: ошибка — %v", o.RepoID, o.Err)
	}

	prefix := fmt.Sprintf("mirror %s: ", o.RepoID)
	if o.Repaired {
		prefix += "починено"
		if o.RescueBranch != "" {
			prefix += fmt.Sprintf(" (изменения сохранены в ветке %s)", o.RescueBranch)
		}
		prefix += ", "
		if o.Blocked != "" {
			prefix += "но "
		}
	}

	var line string
	switch {
	case o.Blocked != "":
		line = prefix + "заблокировано: " + o.Blocked
	case o.Advanced > 0:
		line = prefix + "обновлено на " + pluralCommits(o.Advanced)
	default:
		line = prefix + "уже актуально"
	}

	// A mirror that could not fetch may still have advanced — to a stale
	// origin. Saying only "обновлено" would be true and misleading.
	if o.FetchErr != nil {
		line += fmt.Sprintf(" (fetch не удался: %v)", o.FetchErr)
	}
	return line
}

// syncOps is the git and mirror work syncMirrors does, behind function
// values so the flow — sync, then blocked-detection, then repair — can be
// tested without a git repository. The real implementations are in
// realSyncOps.
type syncOps struct {
	head    func(ctx context.Context, path string) (string, error)
	count   func(ctx context.Context, path, from, to string) (int, error)
	sync    func(ctx context.Context, repo store.Repo) error
	blocked func(ctx context.Context, repo store.Repo) (string, error)
	repair  func(ctx context.Context, repo store.Repo, now time.Time) (mirror.RepairResult, error)
}

// realSyncOps wires syncOps to git and to the mirror package.
func realSyncOps() *syncOps {
	return &syncOps{
		head:  gitHead,
		count: gitCountCommits,
		sync:  mirror.Sync,
		blocked: func(ctx context.Context, repo store.Repo) (string, error) {
			// staleAfter and now only feed Freshness.Stale, which this
			// command does not use: it reports what it just did, not how old
			// the mirror looks.
			fr, err := mirror.Check(ctx, repo, mirrorStaleFallback, time.Now())
			return fr.Blocked, err
		},
		repair: mirror.Repair,
	}
}

// syncMirrors syncs each mirror in turn and reports what happened to it.
//
// The advance is measured as HEAD before against HEAD after rather than from
// the pre-sync behind count: Sync fetches and fast-forwards in one step, so
// a count taken beforehand misses exactly the commits that fetch brought in.
//
// A mirror that cannot be examined at all is recorded and the pass moves on.
// One broken mirror hiding the state of the other 76 is the failure mode
// this command was built to end.
func syncMirrors(ctx context.Context, mirrors []repoRow, ops *syncOps, repair bool, now time.Time) []syncOutcome {
	outcomes := make([]syncOutcome, 0, len(mirrors))
	for _, m := range mirrors {
		outcomes = append(outcomes, syncMirror(ctx, m, ops, repair, now))
	}
	return outcomes
}

func syncMirror(ctx context.Context, m repoRow, ops *syncOps, repair bool, now time.Time) syncOutcome {
	out := syncOutcome{RepoID: m.ID}
	repo := store.Repo{ID: m.ID, Path: m.Path, DefaultBranch: m.DefaultBranch}

	before, err := ops.head(ctx, m.Path)
	if err != nil {
		out.Err = err
		return out
	}

	// Sync's own error is the fetch's: it still fast-forwards from the refs
	// already on disk afterwards, so it is reported alongside the outcome
	// rather than instead of it.
	fetchErr := ops.sync(ctx, repo)

	blocked, err := ops.blocked(ctx, repo)
	if err != nil {
		out.Err = err
		return out
	}
	out.Blocked = blocked

	if blocked != "" && repair {
		res, err := ops.repair(ctx, repo, now)
		if err != nil {
			out.Err = err
			return out
		}
		out.Repaired = res.Repaired
		out.RescueBranch = res.RescueBranch
		out.Blocked = res.Blocked
	}

	advanced, err := countAdvance(ctx, ops, m.Path, before)
	if err != nil {
		out.Err = err
		return out
	}
	out.Advanced = advanced
	out.FetchErr = fetchErr

	return out
}

// countAdvance resolves HEAD again and counts how far it moved.
func countAdvance(ctx context.Context, ops *syncOps, path, before string) (int, error) {
	after, err := ops.head(ctx, path)
	if err != nil {
		return 0, err
	}
	if after == before {
		return 0, nil
	}
	return ops.count(ctx, path, before, after)
}

// syncExitError decides the command's exit code. An individual mirror that
// Sync refuses to advance is a normal, reportable outcome — refusing to
// clobber is the whole design of the mirror package — so only a pass that
// could not process a single mirror is a failure.
func syncExitError(outcomes []syncOutcome) error {
	if len(outcomes) == 0 {
		return errors.New("нет зеркал для синхронизации")
	}
	for _, o := range outcomes {
		if o.Err == nil {
			return nil
		}
	}
	return errors.New("ни одно зеркало не удалось обработать")
}

// gitHead resolves a mirror's HEAD.
func gitHead(ctx context.Context, path string) (string, error) {
	out, err := runGitLocal(ctx, path, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return out, nil
}

// gitCountCommits counts the commits in from..to.
func gitCountCommits(ctx context.Context, path, from, to string) (int, error) {
	out, err := runGitLocal(ctx, path, "rev-list", "--count", from+".."+to)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("parse commit count %q: %w", out, err)
	}
	return n, nil
}

// runGitLocal runs a read-only git command in a mirror. No shell involved,
// and nothing here touches the network — the one command that does is
// mirror.Sync.
func runGitLocal(ctx context.Context, path string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
