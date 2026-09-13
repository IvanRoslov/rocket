package cli

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/mirror"
)

// TestSelectMirrorsAll: with no ids given, every mirror is selected and
// nothing is reported as unknown.
func TestSelectMirrorsAll(t *testing.T) {
	mirrors := []repoRow{{ID: "rocket"}, {ID: "app"}}

	got, unknown := selectMirrors(mirrors, nil)

	if !reflect.DeepEqual(got, mirrors) {
		t.Fatalf("selected = %v, want %v", got, mirrors)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
}

// TestSelectMirrorsByID keeps the requested mirrors in the order the user
// named them, so the report reads back in the order it was asked for.
func TestSelectMirrorsByID(t *testing.T) {
	mirrors := []repoRow{{ID: "rocket"}, {ID: "app"}, {ID: "web"}}

	got, unknown := selectMirrors(mirrors, []string{"web", "rocket"})

	want := []repoRow{{ID: "web"}, {ID: "rocket"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected = %v, want %v", got, want)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
}

// TestSelectMirrorsUnknownID: an id that is not a mirror is reported back
// rather than silently skipped — a typo that syncs nothing must say so.
func TestSelectMirrorsUnknownID(t *testing.T) {
	mirrors := []repoRow{{ID: "rocket"}}

	got, unknown := selectMirrors(mirrors, []string{"rocket", "nope"})

	if len(got) != 1 || got[0].ID != "rocket" {
		t.Fatalf("selected = %v, want [rocket]", got)
	}
	if !reflect.DeepEqual(unknown, []string{"nope"}) {
		t.Fatalf("unknown = %v, want [nope]", unknown)
	}
}

// TestSyncLineAdvanced: the ordinary outcome names how far the working tree
// moved, because "synced" alone does not tell a reader whether the mirror
// they were about to read was days behind.
func TestSyncLineAdvanced(t *testing.T) {
	got := syncLine(syncOutcome{RepoID: "rocket", Advanced: 5})
	want := "mirror rocket: обновлено на 5 коммитов"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

func TestSyncLineAlreadyCurrent(t *testing.T) {
	got := syncLine(syncOutcome{RepoID: "rocket"})
	want := "mirror rocket: уже актуально"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

// TestSyncLineBlocked carries the mirror package's reason verbatim: a
// blocked mirror is a normal, reportable outcome, not a failure.
func TestSyncLineBlocked(t *testing.T) {
	got := syncLine(syncOutcome{RepoID: "app", Blocked: mirror.BlockedDirty})
	want := "mirror app: не обновлено — локальные изменения в зеркале"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

// TestSyncLineRepairedNamesRescueBranch: the one thing a user must never
// have to go looking for is where their uncommitted work went.
func TestSyncLineRepairedNamesRescueBranch(t *testing.T) {
	got := syncLine(syncOutcome{
		RepoID: "app", Advanced: 3, Repaired: true, RescueBranch: "rescue/2026-09-13-120000",
	})
	want := "mirror app: починено (изменения сохранены в ветке rescue/2026-09-13-120000), обновлено на 3 коммита"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

// TestSyncLineRepairedButStillBlocked: Repair deliberately does not fix a
// diverged default branch, so say so instead of implying success.
func TestSyncLineRepairedButStillBlocked(t *testing.T) {
	got := syncLine(syncOutcome{
		RepoID: "web", Repaired: true, RescueBranch: "rescue/2026-09-13-120000", Blocked: mirror.BlockedNoFF,
	})
	want := "mirror web: починено (изменения сохранены в ветке rescue/2026-09-13-120000), но не обновлено — fast-forward невозможен"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

func TestSyncLineError(t *testing.T) {
	got := syncLine(syncOutcome{RepoID: "landing", Err: errors.New("origin/main not found")})
	want := "mirror landing: ошибка — origin/main not found"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

// TestRenderSyncReportsUnknownIDs: an id that matched no mirror is printed,
// never swallowed.
func TestRenderSyncReportsUnknownIDs(t *testing.T) {
	var buf bytes.Buffer
	renderSync([]syncOutcome{{RepoID: "rocket", Advanced: 1}}, []string{"nope"}, &buf)
	out := buf.String()

	for _, want := range []string{
		"mirror rocket: обновлено на 1 коммит",
		"неизвестное зеркало: nope",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderSync output missing %q:\n%s", want, out)
		}
	}
}
