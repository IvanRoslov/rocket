package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/mirror"
	"github.com/IvanRoslov/rocket/internal/store"
)

// The shared mirrors under ~/.rocket/repos/ are what agents actually read
// when they look at another repo's files, and a mirror can be weeks behind
// origin while looking perfectly normal (task #795). So every command that
// shows repos says out loud how fresh each mirror is: a stale mirror must
// never be read silently.
//
// Freshness is a derived value — it is computed from the git repository
// itself and never stored in rocket.db — so the CLI computes it locally with
// mirror.Check. Check makes no network calls; this file must never call
// mirror.Sync, which does.

// mirrorRow is one mirror as the CLI renders it: either a computed
// Freshness, or the error that prevented computing it. A mirror we cannot
// check is reported, not dropped — that is exactly when the user most needs
// to be told something is wrong.
type mirrorRow struct {
	RepoID string
	Fresh  mirror.Freshness
	Err    error
	// Sync is the last recorded sync of this mirror, read from the sidecar
	// the syncing callers leave under <repos_dir>/.state. Its zero value
	// means no recording caller has synced this mirror yet — which is not
	// the same as "synced fine", and is rendered differently.
	Sync mirror.SyncState
	// SyncErr is a sidecar we could not read at all. Reported rather than
	// swallowed: the file exists precisely for the mirrors whose last sync
	// went wrong.
	SyncErr error
}

// loadSyncStates fills in each row's last recorded sync. It is separate from
// the freshness sweep because the two answer different questions — Check
// measures the mirror as it is now, the sidecar says what the last writer of
// it managed to do — and only `rocket repo status` shows both.
func loadSyncStates(rows []mirrorRow, reposDir string) []mirrorRow {
	stateDir := mirror.StateDir(reposDir)
	if stateDir == "" {
		return rows
	}
	for i := range rows {
		rows[i].Sync, rows[i].SyncErr = mirror.ReadState(stateDir, rows[i].RepoID)
	}
	return rows
}

// syncErrorText is the full sentence describing what the last sync could not
// do, empty when it did everything it was asked. A failed merge leads: it is
// the failure that used to be invisible, and a fetch that failed afterwards
// does not explain a mirror that is still behind.
func syncErrorText(row mirrorRow) string {
	switch {
	case row.SyncErr != nil:
		return row.SyncErr.Error()
	case row.Sync.MergeErr != "" && row.Sync.FetchErr != "":
		return row.Sync.MergeErr + "; " + row.Sync.FetchErr
	case row.Sync.MergeErr != "":
		return row.Sync.MergeErr
	case row.Sync.FetchErr != "":
		return row.Sync.FetchErr
	default:
		return ""
	}
}

// repoRow is the subset of a GET /v1/repos row needed to check a mirror.
type repoRow struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch"`
}

// mirrorCheckTimeout bounds ONE mirror's freshness check. Check runs about
// five local git commands, so five seconds is already generous; the point is
// that a wedged mirror costs its own row and nothing else.
//
// It used to be 15s covering the entire sweep, written when the sweep was a
// courtesy line under `rocket status`. On a real fleet of 83 mirrors that
// budget ran out mid-pass and every remaining row rendered as "resolve HEAD:
// signal: killed" — a broken-repository message for what was only impatience.
const mirrorCheckTimeout = 5 * time.Second

// mirrorSweepBase and mirrorSweepPerMirror shape the overall ceiling. A
// wedged filesystem must still not hang the CLI, but the ceiling has to grow
// with the fleet or it re-creates the bug above: the base covers process
// startup and the registry round trip, and each mirror adds its own share.
// The share is deliberately smaller than mirrorCheckTimeout — a fleet where
// every mirror needs its full budget is a broken host, not a slow one.
const (
	mirrorSweepBase      = 10 * time.Second
	mirrorSweepPerMirror = 2 * time.Second
)

// mirrorSweepTimeout is the whole sweep's ceiling for a fleet of n mirrors.
func mirrorSweepTimeout(n int) time.Duration {
	if n < 0 {
		n = 0
	}
	return mirrorSweepBase + time.Duration(n)*mirrorSweepPerMirror
}

// errMirrorCheckTimeout is the error a mirror gets when we ran out of time
// rather than when the repository is broken. The distinction is the whole
// point: "signal: killed" reads as a corrupt mirror and sends a human
// digging into a repository that is perfectly fine.
var errMirrorCheckTimeout = errors.New("превышено время проверки")

// mirrorChecker is mirror.Check's shape, injected so the timeout behaviour
// can be tested against a checker that blocks instead of a real sweep that
// would have to actually take seconds.
type mirrorChecker func(ctx context.Context, repo store.Repo, staleAfter time.Duration, now time.Time) (mirror.Freshness, error)

// mirrorStaleFallback is the staleness threshold used when the daemon's
// mirror sync interval is 0, i.e. background sync is disabled.
const mirrorStaleFallback = 10 * time.Minute

// mirrorStaleAfter converts the daemon's mirror sync interval into the age
// of the last fetch beyond which a mirror counts as stale. Twice the
// interval: one missed tick is normal jitter, two means sync is not running.
func mirrorStaleAfter(syncInterval time.Duration) time.Duration {
	if syncInterval <= 0 {
		return mirrorStaleFallback
	}
	return 2 * syncInterval
}

// mirrorSyncInterval reports how often the daemon fast-forwards the mirrors,
// which is what makes a given fetch age normal or alarming. "0s" in the
// config disables background syncing, and mirrorStaleAfter treats that as
// the loudest case rather than the quietest.
func mirrorSyncInterval(cfg *config.Config) time.Duration {
	if cfg == nil {
		return 0
	}
	return cfg.MirrorSyncInterval
}

// checkMirrors computes freshness for every repo, in the order given. A repo
// whose check fails — or takes too long — yields a row carrying the error, so
// one broken or wedged mirror never hides the others.
func checkMirrors(ctx context.Context, repos []repoRow, staleAfter time.Duration, now time.Time) []mirrorRow {
	return checkMirrorsWith(ctx, mirror.Check, repos,
		mirrorCheckTimeout, mirrorSweepTimeout(len(repos)), staleAfter, now)
}

// checkMirrorsWith is checkMirrors with the checker and both budgets handed
// in. Every mirror is checked under its own perMirror budget, nested inside
// one overall ceiling for the sweep.
func checkMirrorsWith(ctx context.Context, check mirrorChecker, repos []repoRow,
	perMirror, overall, staleAfter time.Duration, now time.Time) []mirrorRow {
	sweepCtx, cancelSweep := context.WithTimeout(ctx, overall)
	defer cancelSweep()

	rows := make([]mirrorRow, 0, len(repos))
	for _, r := range repos {
		row := mirrorRow{RepoID: r.ID}
		row.Fresh, row.Err = checkOneMirror(sweepCtx, check, r, perMirror, staleAfter, now)
		rows = append(rows, row)
	}
	return rows
}

// checkOneMirror runs a single check under its own deadline and translates a
// deadline that fired into errMirrorCheckTimeout: the git error left behind
// by a killed command describes the symptom, not the cause.
func checkOneMirror(ctx context.Context, check mirrorChecker, r repoRow,
	perMirror, staleAfter time.Duration, now time.Time) (mirror.Freshness, error) {
	ctx, cancel := context.WithTimeout(ctx, perMirror)
	defer cancel()

	fr, err := check(ctx, store.Repo{
		ID:            r.ID,
		Path:          r.Path,
		DefaultBranch: r.DefaultBranch,
	}, staleAfter, now)
	if err != nil && ctx.Err() != nil {
		return mirror.Freshness{}, errMirrorCheckTimeout
	}
	return fr, err
}

// mirrorFreshness fetches the repo registry and computes freshness for every
// registered mirror. It is deliberately unfiltered by feature or project —
// the misreads this exists to prevent came from agents reading repos their
// own feature did not own — but it is filtered to actual mirrors, see
// mirrorsOnly.
//
// A failure to reach the registry is not fatal: it returns no rows and no
// error, because a command must still print what it does know. Per-mirror
// failures are carried in the rows themselves.
func mirrorFreshness(ctx context.Context, c *client.Client, cfg *config.Config, now time.Time) []mirrorRow {
	var repos []repoRow
	if err := c.Get("/v1/repos", nil, &repos); err != nil {
		slog.Debug("cli: cannot list repos for mirror freshness", "error", err)
		return nil
	}
	return checkMirrorsWithTimeout(ctx, mirrorsOnly(repos, cfg.ReposDir), mirrorSyncInterval(cfg), now)
}

// mirrorsOnly keeps the repos that are actually shared mirrors: the service
// clones the daemon made under repos_dir.
//
// The registry holds two different kinds of thing. A clone under repos_dir
// is a mirror the daemon owns and keeps fast-forwarded. But `rocket repo add
// <path>` registers the user's own working copy, which rocket promises to
// leave exactly as it is (docs/05-state.md) — and a working copy parked on a
// feature branch is perfectly healthy. Reporting it as ПРОТУХЛО would be a
// false alarm, and a warning nobody trusts is a warning nobody reads, which
// would defeat the point of printing these lines at all.
//
// With no repos_dir configured there is nothing to tell the two apart, so
// nothing is reported: saying nothing beats crying wolf over someone's own
// checkout.
func mirrorsOnly(repos []repoRow, reposDir string) []repoRow {
	if reposDir == "" {
		return nil
	}
	base := filepath.Clean(reposDir)

	out := make([]repoRow, 0, len(repos))
	for _, r := range repos {
		rel, err := filepath.Rel(base, filepath.Clean(r.Path))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// checkMirrorsWithTimeout runs checkMirrors with the staleness threshold
// derived from the daemon's sync interval. The timeouts themselves live in
// checkMirrors, which is the only place that knows the fleet size.
func checkMirrorsWithTimeout(ctx context.Context, repos []repoRow, syncInterval time.Duration, now time.Time) []mirrorRow {
	return checkMirrors(ctx, repos, mirrorStaleAfter(syncInterval), now)
}

// taskMirrorJSON is one mirror's freshness as `rocket task show --json`
// emits it: the same mirrorJSON shape `rocket repo ls --json` already
// carries, plus the repo it belongs to. Reusing the shape is the point —
// a second vocabulary for the same fact is a second thing to get wrong.
type taskMirrorJSON struct {
	RepoID string `json:"repo_id"`
	mirrorJSON
}

// taskMirrorJSONRows converts freshness rows for --json. It always returns
// an array, never nil: `jq '.mirrors[]'` must work on a host with no
// mirrors registered at all.
func taskMirrorJSONRows(rows []mirrorRow) []taskMirrorJSON {
	out := make([]taskMirrorJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, taskMirrorJSON{RepoID: row.RepoID, mirrorJSON: toMirrorJSON(row)})
	}
	return out
}

// unfreshMirrors keeps the rows worth a reader's attention: stale mirrors,
// and mirrors whose freshness could not be computed at all. A mirror we
// could not check is the loudest case, not the quietest — "unknown" must
// never be dropped into the same silence as "fine".
func unfreshMirrors(rows []mirrorRow) []mirrorRow {
	out := make([]mirrorRow, 0, len(rows))
	for _, row := range rows {
		if row.Err != nil || row.Fresh.Stale {
			out = append(out, row)
		}
	}
	return out
}

// renderMirrors writes one freshness line per mirror, in the order given.
// Nothing at all is written when there are no mirrors.
func renderMirrors(rows []mirrorRow, w io.Writer, now time.Time) {
	for _, row := range rows {
		fmt.Fprintln(w, mirrorLine(row, now))
	}
}

// mirrorLine renders one mirror's freshness. The wording is frozen by the
// approved spec (docs/04-cli.md, docs/05-state.md) and the Blocked reasons
// come from the mirror package verbatim — do not reword either here.
//
// When several reasons apply, the most actionable one wins: a blocked sync
// (which no amount of waiting will fix) over a lagging working tree over a
// merely old fetch.
func mirrorLine(row mirrorRow, now time.Time) string {
	if errors.Is(row.Err, errMirrorCheckTimeout) {
		return fmt.Sprintf("mirror %s: свежесть неизвестна — %v", row.RepoID, errMirrorCheckTimeout)
	}
	// An empty remote is a correct state, not a failure: there is simply
	// nothing to mirror yet. Naming it in words keeps a human from digging
	// into a repository that is fine — but it stays out of the "свежее"
	// branch, because the mirror is still no view of origin.
	if errors.Is(row.Err, mirror.ErrEmptyRemote) {
		return fmt.Sprintf("mirror %s: в remote нет веток", row.RepoID)
	}
	if row.Err != nil {
		return fmt.Sprintf("mirror %s: свежесть неизвестна (%v)", row.RepoID, row.Err)
	}
	if !row.Fresh.Stale {
		return fmt.Sprintf("mirror %s: свежее (%s)", row.RepoID, lastFetchPhrase(row.Fresh.LastFetch, now))
	}
	switch {
	case row.Fresh.Blocked != "":
		return fmt.Sprintf("mirror %s: ПРОТУХЛО — синхронизация не может обновить дерево: %s",
			row.RepoID, row.Fresh.Blocked)
	case row.Fresh.BehindCommits > 0:
		return fmt.Sprintf("mirror %s: ПРОТУХЛО — рабочее дерево отстаёт на %s, %s",
			row.RepoID, pluralCommits(row.Fresh.BehindCommits), lastFetchPhrase(row.Fresh.LastFetch, now))
	default:
		return fmt.Sprintf("mirror %s: ПРОТУХЛО — %s", row.RepoID, lastFetchPhrase(row.Fresh.LastFetch, now))
	}
}

// lastFetchPhrase describes when the mirror last talked to origin.
func lastFetchPhrase(lastFetch, now time.Time) string {
	if lastFetch.IsZero() {
		return "fetch ни разу не выполнялся"
	}
	return "последний fetch " + humanAgeRU(now.Sub(lastFetch)) + " назад"
}

// humanAgeRU renders a duration the way the spec's example lines do ("2 мин",
// "3 дня"). It mirrors humanAge's thresholds; humanAge itself stays as it is
// because the session tables it feeds are column-width sensitive.
func humanAgeRU(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "меньше минуты"
	case d < time.Hour:
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "час", "часа", "часов")
	default:
		return plural(int(d.Hours()/24), "день", "дня", "дней")
	}
}

// pluralCommits renders a commit count with the right Russian form.
func pluralCommits(n int) string {
	return plural(n, "коммит", "коммита", "коммитов")
}

// plural renders n with the Russian form matching it: one for 1, few for
// 2-4, many otherwise — with the 11-14 exception that takes the many form
// despite ending in 1-4.
func plural(n int, one, few, many string) string {
	form := many
	switch mod100 := n % 100; {
	case mod100 >= 11 && mod100 <= 14:
	default:
		switch n % 10 {
		case 1:
			form = one
		case 2, 3, 4:
			form = few
		}
	}
	return fmt.Sprintf("%d %s", n, form)
}
