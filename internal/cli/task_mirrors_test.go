package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirror"
)

// taskMirrorJSONRows must always produce an array, never nil: `jq
// '.mirrors[]'` has to work on a host with no mirrors registered at all.
func TestTaskMirrorJSONRowsEmptyIsArray(t *testing.T) {
	got := taskMirrorJSONRows(nil)
	if got == nil {
		t.Fatal("taskMirrorJSONRows(nil) = nil, want empty slice")
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != "[]" {
		t.Errorf("marshalled = %s, want []", b)
	}
}

// The rows carry the same mirrorJSON shape `rocket repo ls --json` already
// emits — one vocabulary for mirror freshness across the CLI — keyed by the
// repo they belong to.
func TestTaskMirrorJSONRowsReusesRepoShape(t *testing.T) {
	lastFetch := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)

	rows := []mirrorRow{
		{
			RepoID: "blocked",
			Fresh: mirror.Freshness{
				RepoID: "blocked", Stale: true, BehindCommits: 12,
				LastFetch: lastFetch, Blocked: mirror.BlockedDirty,
			},
		},
		{RepoID: "broken", Err: errors.New("not a git repository")},
	}

	got := taskMirrorJSONRows(rows)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	if got[0].RepoID != "blocked" {
		t.Errorf("repo_id = %q, want blocked", got[0].RepoID)
	}
	if got[0].BehindCommits == nil || *got[0].BehindCommits != 12 {
		t.Errorf("behind_commits = %v, want 12", got[0].BehindCommits)
	}
	if got[0].Stale == nil || !*got[0].Stale {
		t.Errorf("stale = %v, want true", got[0].Stale)
	}
	if got[0].Blocked != mirror.BlockedDirty {
		t.Errorf("blocked = %q, want %q", got[0].Blocked, mirror.BlockedDirty)
	}
	if got[0].LastFetch != lastFetch.Format(time.RFC3339) {
		t.Errorf("last_fetch = %q, want %q", got[0].LastFetch, lastFetch.Format(time.RFC3339))
	}

	// A mirror we could not check is reported, not dropped — and carries
	// nothing but the error, so a machine reader cannot mistake it for
	// "checked, and fine".
	if got[1].RepoID != "broken" {
		t.Errorf("repo_id = %q, want broken", got[1].RepoID)
	}
	if got[1].Error != "not a git repository" {
		t.Errorf("error = %q, want %q", got[1].Error, "not a git repository")
	}
	if got[1].Stale != nil || got[1].BehindCommits != nil {
		t.Errorf("errored row carries measured fields: %+v", got[1])
	}
}

// The card's mirror block reuses mirrorLine verbatim — the same wording
// `rocket status` prints, so an agent reading either sees one vocabulary.
func TestRenderTaskCardRendersMirrors(t *testing.T) {
	now := time.Now()
	row := mirrorRow{
		RepoID: "rocket",
		Fresh: mirror.Freshness{
			RepoID: "rocket", Stale: true, BehindCommits: 12,
			LastFetch: now.Add(-3 * time.Hour),
		},
	}

	var w strings.Builder
	renderTaskCard(taskDetailRow{ID: 7, Title: "t", Status: "todo", ProjectID: "rocket"},
		[]taskDocRow{}, []taskLogRow{}, nil, []mirrorRow{row}, &w, now)

	out := w.String()
	if !strings.Contains(out, "## Mirrors") {
		t.Errorf("card is missing the Mirrors section:\n%s", out)
	}
	if !strings.Contains(out, mirrorLine(row, now)) {
		t.Errorf("card does not contain %q:\n%s", mirrorLine(row, now), out)
	}
}

func TestRenderTaskCardOmitsEmptyMirrors(t *testing.T) {
	var w strings.Builder
	renderTaskCard(taskDetailRow{ID: 7, Title: "t", Status: "todo", ProjectID: "rocket"},
		[]taskDocRow{}, []taskLogRow{}, nil, nil, &w, time.Now())

	if strings.Contains(w.String(), "Mirrors") {
		t.Errorf("card has a Mirrors section with no mirrors:\n%s", w.String())
	}
}

// A fresh mirror is not news. The card only spends lines on mirrors that
// must not be read as-is — otherwise the block becomes noise on every card
// and stops being read at all.
func TestRenderTaskCardOmitsFreshMirrors(t *testing.T) {
	now := time.Now()
	fresh := mirrorRow{
		RepoID: "rocket",
		Fresh:  mirror.Freshness{RepoID: "rocket", LastFetch: now.Add(-2 * time.Minute)},
	}

	var w strings.Builder
	renderTaskCard(taskDetailRow{ID: 7, Title: "t", Status: "todo", ProjectID: "rocket"},
		[]taskDocRow{}, []taskLogRow{}, nil, []mirrorRow{fresh}, &w, now)

	if strings.Contains(w.String(), "Mirrors") || strings.Contains(w.String(), "rocket: свежее") {
		t.Errorf("card printed a fresh mirror:\n%s", w.String())
	}
}

// A mirror whose freshness could not be computed is exactly when the reader
// most needs telling, so it is reported alongside the stale ones.
func TestRenderTaskCardRendersUncheckableMirrors(t *testing.T) {
	now := time.Now()
	broken := mirrorRow{RepoID: "docs", Err: errors.New("not a git repository")}

	var w strings.Builder
	renderTaskCard(taskDetailRow{ID: 7, Title: "t", Status: "todo", ProjectID: "rocket"},
		[]taskDocRow{}, []taskLogRow{}, nil, []mirrorRow{broken}, &w, now)

	if !strings.Contains(w.String(), mirrorLine(broken, now)) {
		t.Errorf("card dropped an uncheckable mirror:\n%s", w.String())
	}
}

// --json may only gain keys. `mirrors` must be present and an array even
// when nothing is registered.
func TestTaskShowJSONHasMirrorsKey(t *testing.T) {
	b, err := json.Marshal(taskShowJSON{
		taskDetailRow: taskDetailRow{ID: 7, Title: "t", Status: "todo"},
		Docs:          []taskDocRow{},
		Log:           []taskLogRow{},
		Questions:     []questionRow{},
		Mirrors:       taskMirrorJSONRows(nil),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	raw, ok := got["mirrors"]
	if !ok {
		t.Fatalf("task show --json is missing key \"mirrors\"; got keys %v", keysOf(got))
	}
	arr, ok := raw.([]any)
	if !ok || len(arr) != 0 {
		t.Errorf("mirrors = %#v, want an empty array", raw)
	}
}
