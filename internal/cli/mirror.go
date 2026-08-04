package cli

import (
	"context"
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
}

// repoRow is the subset of a GET /v1/repos row needed to check a mirror.
type repoRow struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch"`
}

// mirrorCheckTimeout bounds the whole freshness pass. Check only runs local
// git commands, but a wedged filesystem must not hang the CLI.
const mirrorCheckTimeout = 15 * time.Second

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
// whose check fails yields a row carrying the error, so one broken mirror
// never hides the others.
func checkMirrors(ctx context.Context, repos []repoRow, staleAfter time.Duration, now time.Time) []mirrorRow {
	rows := make([]mirrorRow, 0, len(repos))
	for _, r := range repos {
		row := mirrorRow{RepoID: r.ID}
		row.Fresh, row.Err = mirror.Check(ctx, store.Repo{
			ID:            r.ID,
			Path:          r.Path,
			DefaultBranch: r.DefaultBranch,
		}, staleAfter, now)
		rows = append(rows, row)
	}
	return rows
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

// checkMirrorsWithTimeout runs checkMirrors under mirrorCheckTimeout.
func checkMirrorsWithTimeout(ctx context.Context, repos []repoRow, syncInterval time.Duration, now time.Time) []mirrorRow {
	ctx, cancel := context.WithTimeout(ctx, mirrorCheckTimeout)
	defer cancel()
	return checkMirrors(ctx, repos, mirrorStaleAfter(syncInterval), now)
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
