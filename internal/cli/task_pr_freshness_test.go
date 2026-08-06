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

	if !strings.Contains(out, "#42 (open, 5м назад)") {
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
	if !strings.Contains(out, "2ч назад") {
		t.Errorf("expected the age alongside the mark, got:\n%s", out)
	}
}

func TestRenderTaskCard_NeverCheckedPRSaysSo(t *testing.T) {
	now := time.Now()
	out := renderCardWithSubtask(now, taskRow{
		ID: 10, Title: "Sub", Status: "in_progress",
		PRNumber: 42, PRState: "open",
		PRStale: true,
	})

	if !strings.Contains(out, prNeverChecked) {
		t.Errorf("expected an explicit never-checked wording, got:\n%s", out)
	}
	if !strings.Contains(out, prStaleMark) {
		t.Errorf("a never-checked PR must be marked stale, got:\n%s", out)
	}
}

func TestRenderTaskCard_NoPRUnchanged(t *testing.T) {
	now := time.Now()
	out := renderCardWithSubtask(now, taskRow{ID: 10, Title: "Sub", Status: "backlog"})

	if strings.Contains(out, prStaleMark) || strings.Contains(out, "назад") {
		t.Errorf("a subtask without a PR must render no freshness info, got:\n%s", out)
	}
}
