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
	want := []string{"REPO", "HEAD", "ORIGIN", "BEHIND", "BRANCH", "DIRTY", "FETCHED"}
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
