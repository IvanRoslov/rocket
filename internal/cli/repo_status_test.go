package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirror"
)

func statusFixture(now time.Time) []mirrorRow {
	return []mirrorRow{
		{RepoID: "rocket", Fresh: mirror.Freshness{
			Head:     "1111111111111111111111111111111111111111",
			Upstream: "1111111111111111111111111111111111111111",
			Branch:   "main", LastFetch: now.Add(-2 * time.Minute),
		}},
		{RepoID: "app", Fresh: mirror.Freshness{
			Head:     "2222222222222222222222222222222222222222",
			Upstream: "3333333333333333333333333333333333333333",
			Branch:   "feature/x", BehindCommits: 78, Dirty: true,
			Blocked: mirror.BlockedDirty, Stale: true,
			LastFetch: now.Add(-72 * time.Hour),
		}},
		{RepoID: "web", Fresh: mirror.Freshness{
			Head:     "4444444444444444444444444444444444444444",
			Upstream: "4444444444444444444444444444444444444444",
			Branch:   "", Stale: true,
		}},
		{RepoID: "landing", Err: errors.New("mirror landing: origin/main not found")},
	}
}

// TestRenderRepoStatusTable pins the columns the spec froze, in order.
func TestRenderRepoStatusTable(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	var buf bytes.Buffer
	renderRepoStatus(statusFixture(now), &buf, now)
	out := buf.String()

	header := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	want := []string{"REPO", "HEAD", "ORIGIN", "BEHIND", "BRANCH", "DIRTY", "FETCHED", "SYNC"}
	if strings.Join(header, " ") != strings.Join(want, " ") {
		t.Fatalf("header = %v, want %v", header, want)
	}

	for _, want := range []string{
		"1111111", "78", "feature/x", "да", "нет", "3 дня назад", "2 мин назад",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// TestRenderRepoStatusDetachedHead: the package leaves Branch empty, the CLI
// is the one that names it.
func TestRenderRepoStatusDetachedHead(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	var buf bytes.Buffer
	renderRepoStatus(statusFixture(now), &buf, now)

	if !strings.Contains(buf.String(), "detached") {
		t.Fatalf("output does not name a detached HEAD:\n%s", buf.String())
	}
}

// TestRenderRepoStatusNeverFetched says so in words rather than printing an
// age computed from the zero time.
func TestRenderRepoStatusNeverFetched(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	var buf bytes.Buffer
	renderRepoStatus(statusFixture(now), &buf, now)

	if !strings.Contains(buf.String(), "никогда") {
		t.Fatalf("output does not report a never-fetched mirror:\n%s", buf.String())
	}
}

// TestRenderRepoStatusKeepsUncheckableMirror: a mirror Check cannot examine
// is exactly when the human most needs telling, so it gets a row of its own
// and its error spelled out — never a silent drop.
func TestRenderRepoStatusKeepsUncheckableMirror(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	var buf bytes.Buffer
	renderRepoStatus(statusFixture(now), &buf, now)
	out := buf.String()

	if !strings.Contains(out, "landing") {
		t.Fatalf("uncheckable mirror dropped from the table:\n%s", out)
	}
	if !strings.Contains(out, "mirror landing: свежесть неизвестна (mirror landing: origin/main not found)") {
		t.Fatalf("uncheckable mirror's error not reported:\n%s", out)
	}
}

// TestRepoStatusJSONCarriesTheSameFields: an agent parsing --json must not
// be the one reader left unaware.
func TestRepoStatusJSONCarriesTheSameFields(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	b, err := json.Marshal(repoStatusJSON(statusFixture(now)))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d rows, want 4", len(got))
	}

	app := got[1]
	if app["repo"] != "app" || app["branch"] != "feature/x" || app["dirty"] != true {
		t.Fatalf("app row = %v", app)
	}
	if app["behind"] != float64(78) || app["blocked"] != mirror.BlockedDirty {
		t.Fatalf("app row = %v", app)
	}
	if app["head"] != "2222222222222222222222222222222222222222" {
		t.Fatalf("head must be the full sha in json, got %v", app["head"])
	}

	landing := got[3]
	if landing["repo"] != "landing" || landing["error"] != "mirror landing: origin/main not found" {
		t.Fatalf("landing row = %v", landing)
	}
	if _, ok := landing["behind"]; ok {
		t.Fatalf("uncheckable mirror must not report a measured behind count: %v", landing)
	}
}

// --- SYNC column --------------------------------------------------------

// longMergeErr is longer than the column, so the table has to truncate it
// while the block below keeps it whole.
const longMergeErr = "mirror rocket: merge --ff-only origin/main: git merge: exit status 128 (fatal: Unable to create '/x/.git/index.lock': File exists.)"

func syncStateFixture(now time.Time) []mirrorRow {
	rows := statusFixture(now)
	// "app" is the mirror that is behind: give it the merge failure that
	// explains why, which is precisely what used to be invisible.
	rows[1].Sync = mirror.SyncState{
		RepoID: "app", At: now.Add(-3 * time.Minute), By: mirror.OpSyncer, MergeErr: longMergeErr,
	}
	rows[0].Sync = mirror.SyncState{RepoID: "rocket", At: now.Add(-2 * time.Minute), By: mirror.OpSyncer}
	return rows
}

// The column exists so a failing sync cannot be scrolled past, and the block
// below the table so the reason is readable in full.
func TestRenderRepoStatusShowsSyncError(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	var buf bytes.Buffer
	renderRepoStatus(syncStateFixture(now), &buf, now)
	out := buf.String()

	if !strings.Contains(out, "index.lock") {
		t.Errorf("full sync error is not printed below the table:\n%s", out)
	}
	if !strings.Contains(out, "последняя синхронизация не удалась") {
		t.Errorf("output does not say the last sync failed:\n%s", out)
	}
	if !strings.Contains(out, mirror.OpSyncer) {
		t.Errorf("output does not name who synced:\n%s", out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("the SYNC column does not truncate a long error:\n%s", out)
	}
	if strings.Contains(strings.SplitN(out, "\n\n", 2)[0], "File exists") {
		t.Errorf("the table cell carries the untruncated error:\n%s", out)
	}
}

// A clean sync reads "ok"; a mirror nobody has synced yet reads "—". The two
// must not collapse into one cell: "nothing has tried" is not "fine".
func TestSyncCell(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		row  mirrorRow
		want string
	}{
		{"never synced", mirrorRow{RepoID: "a"}, "—"},
		{"clean sync", mirrorRow{RepoID: "a", Sync: mirror.SyncState{At: now, By: mirror.OpSyncer}}, "ok"},
		{"blocked is not an error", mirrorRow{RepoID: "a", Sync: mirror.SyncState{
			At: now, By: mirror.OpSyncer, Blocked: mirror.BlockedDirty}}, "ok"},
		{"merge error", mirrorRow{RepoID: "a", Sync: mirror.SyncState{At: now, MergeErr: "boom"}}, "boom"},
		{"fetch error", mirrorRow{RepoID: "a", Sync: mirror.SyncState{At: now, FetchErr: "no route"}}, "no route"},
		{"unreadable sidecar", mirrorRow{RepoID: "a", SyncErr: errors.New("parse")}, "parse"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := syncCell(tt.row); got != tt.want {
				t.Errorf("syncCell = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRepoStatusJSONCarriesTheSyncState(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

	rows := repoStatusJSON(syncStateFixture(now))
	byRepo := map[string]repoStatusRow{}
	for _, r := range rows {
		byRepo[r.Repo] = r
	}

	app := byRepo["app"]
	if app.SyncError != longMergeErr {
		t.Errorf("sync_error = %q, want the full merge error", app.SyncError)
	}
	if app.SyncBy != mirror.OpSyncer {
		t.Errorf("sync_by = %q, want %q", app.SyncBy, mirror.OpSyncer)
	}
	if app.SyncAt == "" {
		t.Error("sync_at is empty")
	}
	if got := byRepo["web"]; got.SyncAt != "" || got.SyncError != "" {
		t.Errorf("a never-synced mirror carries sync fields: %+v", got)
	}
}

// A mirror that could not be checked at all is exactly the one whose last
// sync a reader needs, so its row keeps the sidecar fields.
func TestRepoStatusJSONKeepsSyncStateOnUncheckableMirror(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	rows := syncStateFixture(now)
	rows[3].Sync = mirror.SyncState{RepoID: "landing", At: now, By: mirror.OpRepoSync, FetchErr: "no route to host"}

	for _, r := range repoStatusJSON(rows) {
		if r.Repo != "landing" {
			continue
		}
		if r.Error == "" {
			t.Error("the check error was dropped")
		}
		if r.SyncError != "no route to host" {
			t.Errorf("sync_error = %q, want the recorded fetch error", r.SyncError)
		}
		return
	}
	t.Fatal("landing row missing")
}
