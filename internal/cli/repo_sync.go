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
	"github.com/spf13/cobra"
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
	// Behind is how many commits the mirror is still behind origin after
	// the pass. mirror.Sync reports only the fetch error and logs a failed
	// `merge --ff-only`, so a mirror that is still behind with nothing
	// blocked is the only signal that the fast-forward did not happen.
	Behind int
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
	// MergeErr is a failed `git merge --ff-only` — git refusing a
	// fast-forward that none of the mirror package's guards objected to,
	// most often because another process holds the index. It used to be a
	// log line only (#3576), which is how mirrors stayed behind silently.
	MergeErr error
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
	case o.Behind > 0:
		line = prefix + "не обновлено, отстаёт на " + pluralCommits(o.Behind)
	default:
		line = prefix + "уже актуально"
	}

	// A mirror that could not fetch may still have advanced — to a stale
	// origin. Saying only "обновлено" would be true and misleading.
	if o.FetchErr != nil {
		line += fmt.Sprintf(" (fetch не удался: %v)", o.FetchErr)
	}
	if o.MergeErr != nil {
		line += fmt.Sprintf(" (merge не удался: %v)", o.MergeErr)
	}
	return line
}

// syncOps is the git and mirror work syncMirrors does, behind function
// values so the flow — sync, then blocked-detection, then repair — can be
// tested without a git repository. The real implementations are in
// realSyncOps.
type syncOps struct {
	head   func(ctx context.Context, path string) (string, error)
	count  func(ctx context.Context, path, from, to string) (int, error)
	sync   func(ctx context.Context, repo store.Repo) (mirror.SyncResult, error)
	check  func(ctx context.Context, repo store.Repo) (mirror.Freshness, error)
	repair func(ctx context.Context, repo store.Repo, now time.Time) (mirror.RepairResult, error)
}

// realSyncOps wires syncOps to git and to the mirror package.
func realSyncOps() *syncOps {
	return &syncOps{
		head:  gitHead,
		count: gitCountCommits,
		sync:  mirror.Sync,
		check: func(ctx context.Context, repo store.Repo) (mirror.Freshness, error) {
			// staleAfter and now only feed Freshness.Stale, which this
			// command does not use: it reports what it just did, not how old
			// the mirror looks.
			return mirror.Check(ctx, repo, mirrorStaleFallback, time.Now())
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

	// Neither of Sync's errors stops the report: it fast-forwards from the
	// refs already on disk after a failed fetch, and a failed merge leaves
	// the mirror exactly as it was. Both are shown alongside the outcome
	// rather than instead of it.
	syncRes, _ := ops.sync(ctx, repo)

	fr, err := ops.check(ctx, repo)
	if err != nil {
		out.Err = err
		return out
	}
	out.Blocked, out.Behind = fr.Blocked, fr.BehindCommits

	if fr.Blocked != "" && repair {
		res, err := ops.repair(ctx, repo, now)
		if err != nil {
			out.Err = err
			return out
		}
		out.Repaired = res.Repaired
		out.RescueBranch = res.RescueBranch
		out.Blocked = res.Blocked

		// Repair Syncs on its way out, so the behind count from before it
		// ran is stale. Ask again rather than report a number we know is out
		// of date.
		if after, err := ops.check(ctx, repo); err == nil {
			out.Behind = after.BehindCommits
		}
	}

	advanced, err := countAdvance(ctx, ops, m.Path, before)
	if err != nil {
		out.Err = err
		return out
	}
	out.Advanced = advanced
	out.FetchErr = syncRes.FetchErr
	out.MergeErr = syncRes.MergeErr

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

// newRepoSyncCmd fast-forwards the mirrors and says what happened to each.
//
// Individual blocks are reported, not fatal: mirror.Sync refuses to clobber
// by design, and a command that exited non-zero every time one mirror sat on
// someone's feature branch would be a command nobody could put in a script.
func newRepoSyncCmd() *cobra.Command {
	var repair bool
	cmd := &cobra.Command{
		Use:   "sync [id...]",
		Short: "Обновить зеркала (все или указанные)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, cfg, err := connect(true)
			if err != nil {
				return err
			}

			var repos []repoRow
			if err := c.Get("/v1/repos", nil, &repos); err != nil {
				return err
			}

			selected, unknown := selectMirrors(mirrorsOnly(repos, cfg.ReposDir), args)
			outcomes := syncMirrors(cmd.Context(), selected, realSyncOps(), repair, time.Now())

			if flags.JSON {
				if err := printJSON(cmd, syncJSON(outcomes, unknown)); err != nil {
					return err
				}
			} else {
				renderSync(outcomes, unknown, cmd.OutOrStdout())
			}

			return syncExitError(outcomes)
		},
	}
	cmd.Flags().BoolVar(&repair, "repair", false,
		"чинить заблокированные зеркала: незакоммиченные изменения — в ветку rescue/<время>, HEAD — на ветку по умолчанию")
	return cmd
}

// syncRow is one mirror's outcome in --json.
type syncRow struct {
	Repo         string `json:"repo"`
	Advanced     int    `json:"advanced"`
	Behind       int    `json:"behind"`
	Blocked      string `json:"blocked,omitempty"`
	Repaired     bool   `json:"repaired,omitempty"`
	RescueBranch string `json:"rescue_branch,omitempty"`
	FetchError   string `json:"fetch_error,omitempty"`
	MergeError   string `json:"merge_error,omitempty"`
	Error        string `json:"error,omitempty"`
}

// syncJSON is the machine view of the same report, unknown ids included:
// an agent parsing --json must not be the one reader left thinking it synced
// a mirror that does not exist.
func syncJSON(outcomes []syncOutcome, unknown []string) map[string]any {
	rows := make([]syncRow, 0, len(outcomes))
	for _, o := range outcomes {
		row := syncRow{
			Repo:         o.RepoID,
			Advanced:     o.Advanced,
			Behind:       o.Behind,
			Blocked:      o.Blocked,
			Repaired:     o.Repaired,
			RescueBranch: o.RescueBranch,
		}
		if o.Err != nil {
			row.Error = o.Err.Error()
		}
		if o.FetchErr != nil {
			row.FetchError = o.FetchErr.Error()
		}
		if o.MergeErr != nil {
			row.MergeError = o.MergeErr.Error()
		}
		rows = append(rows, row)
	}
	out := map[string]any{"mirrors": rows}
	if len(unknown) > 0 {
		out["unknown"] = unknown
	}
	return out
}
