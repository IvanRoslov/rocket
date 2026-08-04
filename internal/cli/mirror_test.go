package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/mirror"
)

// TestMirrorLine covers every freshness line the spec freezes
// (docs/04-cli.md), including the precedence of reasons: Blocked wins over a
// non-zero behind count, which in turn wins over plain fetch age.
func TestMirrorLine(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		row  mirrorRow
		want string
	}{
		{
			name: "fresh",
			row:  mirrorRow{RepoID: "rocket", Fresh: mirror.Freshness{LastFetch: now.Add(-2 * time.Minute)}},
			want: "mirror rocket: свежее (последний fetch 2 мин назад)",
		},
		{
			name: "behind",
			row: mirrorRow{RepoID: "docs-source", Fresh: mirror.Freshness{
				BehindCommits: 37, LastFetch: now.Add(-72 * time.Hour), Stale: true,
			}},
			want: "mirror docs-source: ПРОТУХЛО — рабочее дерево отстаёт на 37 коммитов, последний fetch 3 дня назад",
		},
		{
			name: "blocked beats behind",
			row: mirrorRow{RepoID: "app", Fresh: mirror.Freshness{
				BehindCommits: 5, Blocked: mirror.BlockedDirty, LastFetch: now, Stale: true,
			}},
			want: "mirror app: ПРОТУХЛО — синхронизация не может обновить дерево: локальные изменения в зеркале",
		},
		{
			name: "blocked not on default branch is passed through verbatim",
			row: mirrorRow{RepoID: "app", Fresh: mirror.Freshness{
				Blocked: mirror.BlockedNotOnDefault("main"), LastFetch: now, Stale: true,
			}},
			want: "mirror app: ПРОТУХЛО — синхронизация не может обновить дерево: HEAD не на ветке main",
		},
		{
			name: "stale by fetch age only",
			row:  mirrorRow{RepoID: "old", Fresh: mirror.Freshness{LastFetch: now.Add(-3 * time.Hour), Stale: true}},
			want: "mirror old: ПРОТУХЛО — последний fetch 3 часа назад",
		},
		{
			name: "never fetched",
			row:  mirrorRow{RepoID: "nofetch", Fresh: mirror.Freshness{Stale: true}},
			want: "mirror nofetch: ПРОТУХЛО — fetch ни разу не выполнялся",
		},
		{
			name: "check error",
			row:  mirrorRow{RepoID: "broken", Err: errors.New("not a git repository")},
			want: "mirror broken: свежесть неизвестна (not a git repository)",
		},
		{
			name: "behind by one commit",
			row: mirrorRow{RepoID: "one", Fresh: mirror.Freshness{
				BehindCommits: 1, LastFetch: now.Add(-90 * time.Second), Stale: true,
			}},
			want: "mirror one: ПРОТУХЛО — рабочее дерево отстаёт на 1 коммит, последний fetch 1 мин назад",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mirrorLine(tt.row, now); got != tt.want {
				t.Errorf("mirrorLine() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

// TestHumanAgeRU pins the Russian plural forms, which are the part of the
// wording most likely to break silently.
func TestHumanAgeRU(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "меньше минуты"},
		{time.Minute, "1 мин"},
		{2 * time.Minute, "2 мин"},
		{59 * time.Minute, "59 мин"},
		{time.Hour, "1 час"},
		{2 * time.Hour, "2 часа"},
		{5 * time.Hour, "5 часов"},
		{25 * time.Hour, "1 день"},
		{72 * time.Hour, "3 дня"},
		{24 * 7 * time.Hour, "7 дней"},
		{24 * 11 * time.Hour, "11 дней"},
		{24 * 21 * time.Hour, "21 день"},
	}
	for _, tt := range tests {
		if got := humanAgeRU(tt.d); got != tt.want {
			t.Errorf("humanAgeRU(%s) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// TestPluralCommits pins the commit plural forms used in the behind line.
func TestPluralCommits(t *testing.T) {
	tests := map[int]string{
		1: "1 коммит", 2: "2 коммита", 4: "4 коммита", 5: "5 коммитов",
		11: "11 коммитов", 14: "14 коммитов", 21: "21 коммит", 22: "22 коммита",
		37: "37 коммитов", 101: "101 коммит", 111: "111 коммитов",
	}
	for n, want := range tests {
		if got := pluralCommits(n); got != want {
			t.Errorf("pluralCommits(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestRenderMirrors checks the block as a whole: a header, one line per
// mirror in the order given, and nothing at all when there are no mirrors.
func TestRenderMirrors(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	rows := []mirrorRow{
		{RepoID: "rocket", Fresh: mirror.Freshness{LastFetch: now.Add(-2 * time.Minute)}},
		{RepoID: "broken", Err: errors.New("boom")},
	}

	var buf bytes.Buffer
	renderMirrors(rows, &buf, now)
	out := buf.String()

	if !strings.Contains(out, "mirror rocket: свежее") {
		t.Errorf("expected the fresh line, got:\n%s", out)
	}
	if !strings.Contains(out, "mirror broken: свежесть неизвестна (boom)") {
		t.Errorf("expected the unknown-freshness line, got:\n%s", out)
	}
	if strings.Index(out, "mirror rocket") > strings.Index(out, "mirror broken") {
		t.Errorf("expected mirrors in the order given, got:\n%s", out)
	}

	var empty bytes.Buffer
	renderMirrors(nil, &empty, now)
	if empty.String() != "" {
		t.Errorf("expected no output for no mirrors, got: %q", empty.String())
	}
}

// TestCheckMirrorsReportsErrorsPerRepo verifies that a mirror that cannot be
// checked yields a row carrying the error instead of aborting the whole run:
// the other mirrors must still be reported.
func TestCheckMirrorsReportsErrorsPerRepo(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	repos := []repoRow{
		{ID: "missing-a", Path: t.TempDir(), DefaultBranch: "main"},
		{ID: "missing-b", Path: t.TempDir(), DefaultBranch: "main"},
	}

	rows := checkMirrors(t.Context(), repos, 10*time.Minute, now)

	if len(rows) != len(repos) {
		t.Fatalf("expected %d rows, got %d", len(repos), len(rows))
	}
	for i, row := range rows {
		if row.RepoID != repos[i].ID {
			t.Errorf("row %d: RepoID = %q, want %q", i, row.RepoID, repos[i].ID)
		}
		if row.Err == nil {
			t.Errorf("row %d (%s): expected an error for a non-git path", i, row.RepoID)
		}
	}
}

// TestMirrorStaleAfterFallback pins the fallback used while the daemon's
// sync interval is unset (0 means the background sync is disabled).
func TestMirrorStaleAfterFallback(t *testing.T) {
	if got := mirrorStaleAfter(0); got != 10*time.Minute {
		t.Errorf("mirrorStaleAfter(0) = %s, want 10m", got)
	}
	if got := mirrorStaleAfter(5 * time.Minute); got != 10*time.Minute {
		t.Errorf("mirrorStaleAfter(5m) = %s, want 10m", got)
	}
	if got := mirrorStaleAfter(time.Hour); got != 2*time.Hour {
		t.Errorf("mirrorStaleAfter(1h) = %s, want 2h", got)
	}
}

// TestMirrorsOnly keeps the user's own checkouts out of the freshness
// report. The repo registry holds two different things: service clones the
// daemon made under repos_dir, which are shared mirrors, and working copies
// the user registered with `rocket repo add <path>`, which rocket promises
// to leave alone (docs/05-state.md). A user checkout parked on a feature
// branch is perfectly healthy, and calling it ПРОТУХЛО would be a false
// alarm — and a warning system that cries wolf is one nobody reads.
func TestMirrorsOnly(t *testing.T) {
	reposDir := t.TempDir()
	repos := []repoRow{
		{ID: "mirror-a", Path: filepath.Join(reposDir, "mirror-a")},
		{ID: "user-checkout", Path: filepath.Join(t.TempDir(), "work", "app")},
		{ID: "mirror-b", Path: filepath.Join(reposDir, "nested", "mirror-b")},
		{ID: "lookalike", Path: reposDir + "-elsewhere/app"},
		{ID: "escape", Path: filepath.Join(reposDir, "..", "outside")},
	}

	got := mirrorsOnly(repos, reposDir)

	var ids []string
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	want := []string{"mirror-a", "mirror-b"}
	if len(ids) != len(want) {
		t.Fatalf("mirrorsOnly() = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("mirrorsOnly() = %v, want %v", ids, want)
			break
		}
	}
}

// TestMirrorsOnlyWithoutReposDir: with no repos_dir configured there is no
// way to tell a mirror from a user checkout, so nothing is reported rather
// than everything — a false ПРОТУХЛО on someone's own working copy is worse
// than saying nothing.
func TestMirrorsOnlyWithoutReposDir(t *testing.T) {
	repos := []repoRow{{ID: "a", Path: "/anywhere/a"}}
	if got := mirrorsOnly(repos, ""); len(got) != 0 {
		t.Errorf("mirrorsOnly(_, \"\") = %v, want none", got)
	}
}

// TestMirrorSyncIntervalFromConfig pins the seam to the daemon's configured
// sweep interval, including the disabled case: "0s" means nothing is keeping
// the mirrors fresh, which mirrorStaleAfter turns into the fixed fallback
// rather than into silence.
func TestMirrorSyncIntervalFromConfig(t *testing.T) {
	if got := mirrorSyncInterval(&config.Config{MirrorSyncInterval: 5 * time.Minute}); got != 5*time.Minute {
		t.Errorf("mirrorSyncInterval(5m) = %s, want 5m", got)
	}
	if got := mirrorStaleAfter(mirrorSyncInterval(&config.Config{MirrorSyncInterval: 30 * time.Minute})); got != time.Hour {
		t.Errorf("staleAfter for a 30m sweep = %s, want 1h", got)
	}
	if got := mirrorStaleAfter(mirrorSyncInterval(&config.Config{MirrorSyncInterval: 0})); got != mirrorStaleFallback {
		t.Errorf("staleAfter with syncing disabled = %s, want %s", got, mirrorStaleFallback)
	}
}
