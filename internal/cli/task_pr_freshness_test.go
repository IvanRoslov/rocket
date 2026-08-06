package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func renderCardWithSubtask(now time.Time, sub taskRow) string {
	task := taskDetailRow{
		ID: 1, Title: "My Task", Status: "in_progress", ProjectID: "proj-1",
		Subtasks: []taskRow{sub},
	}
	w := &bytes.Buffer{}
	renderTaskCard(task, []taskDocRow{}, []taskLogRow{}, nil, w, now)
	return w.String()
}

func TestRenderTaskCard_PRShowsStatusAge(t *testing.T) {
	now := time.Now()
	out := renderCardWithSubtask(now, taskRow{
		ID: 10, Title: "Sub", Status: "in_progress",
		PRNumber: 42, PRState: "open", CIState: "passing",
		PRCheckedAt: now.Add(-5 * time.Minute).Unix(),
	})

	if !strings.Contains(out, "#42 (open, 5m назад)") {
		t.Errorf("expected PR cell with status age, got:\n%s", out)
	}
	if strings.Contains(out, prStaleMark) {
		t.Errorf("a fresh PR must not carry the staleness mark, got:\n%s", out)
	}
}

func TestRenderTaskCard_StalePRIsMarked(t *testing.T) {
	now := time.Now()
	out := renderCardWithSubtask(now, taskRow{
		ID: 10, Title: "Sub", Status: "in_progress",
		PRNumber: 42, PRState: "open", CIState: "passing",
		PRCheckedAt: now.Add(-2 * time.Hour).Unix(),
		PRStale:     true,
	})

	if !strings.Contains(out, prStaleMark) {
		t.Errorf("expected staleness mark for a stale PR, got:\n%s", out)
	}
	if !strings.Contains(out, "2h назад") {
		t.Errorf("expected the age alongside the mark, got:\n%s", out)
	}
}

// A PR the poller never reached must render without an age — the zero
// timestamp would otherwise print as "56 лет назад" — while still carrying the
// staleness mark.
func TestRenderTaskCard_NeverCheckedPRHasNoAge(t *testing.T) {
	now := time.Now()
	out := renderCardWithSubtask(now, taskRow{
		ID: 10, Title: "Sub", Status: "in_progress",
		PRNumber: 42, PRState: "open",
		PRStale: true,
	})

	if !strings.Contains(out, "#42 (open, "+prStaleMark+")") {
		t.Errorf("expected the state and the stale mark without an age, got:\n%s", out)
	}
	if strings.Contains(out, "назад") {
		t.Errorf("a never-checked PR must render no age at all, got:\n%s", out)
	}
}

// A PR that was checked but is not stale renders the age alone; one that was
// never checked and is not stale (a state the daemon does not currently
// produce, but the renderer must not garble) renders the bare state.
func TestRenderTaskCard_NeverCheckedNotStaleIsBareState(t *testing.T) {
	now := time.Now()
	out := renderCardWithSubtask(now, taskRow{
		ID: 10, Title: "Sub", Status: "in_progress",
		PRNumber: 42, PRState: "merged",
	})

	if !strings.Contains(out, "#42 (merged)") {
		t.Errorf("expected a bare state cell, got:\n%s", out)
	}
}

func TestRenderTaskCard_NoPRUnchanged(t *testing.T) {
	now := time.Now()
	out := renderCardWithSubtask(now, taskRow{ID: 10, Title: "Sub", Status: "backlog"})

	if strings.Contains(out, prStaleMark) || strings.Contains(out, "назад") {
		t.Errorf("a subtask without a PR must render no freshness info, got:\n%s", out)
	}
}
