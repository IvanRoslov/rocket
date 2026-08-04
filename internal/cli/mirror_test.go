package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

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
