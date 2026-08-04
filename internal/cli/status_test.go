package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirror"
)

// TestStatusUsage tests usage violations for status.
func TestStatusUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"too many args", []string{"feature-a", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newStatusCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Errorf("expected error for args %v", tt.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

// TestRenderStatusOrchestratorAndWorkers tests rendering an orchestrator
// with two workers.
func TestRenderStatusOrchestratorAndWorkers(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	sessions := []sessionRow{
		{ID: "demo-orch", Kind: "orchestrator", State: "running", Activity: "planning", CreatedAt: now.Add(-2 * time.Hour).Unix()},
		{ID: "demo-worker-1", Kind: "worker", State: "running", Activity: "editing foo.go", CreatedAt: now.Add(-5 * time.Minute).Unix()},
		{ID: "demo-worker-2", Kind: "worker", State: "running", Activity: "", CreatedAt: now.Add(-30 * time.Second).Unix()},
	}

	var buf bytes.Buffer
	renderStatus("demo-feature", sessions, nil, &buf, now)
	out := buf.String()

	if !strings.Contains(out, "feature: demo-feature") {
		t.Errorf("expected feature header, got: %q", out)
	}
	if !strings.Contains(out, "orchestrator: demo-orch [running] planning (2h ago)") {
		t.Errorf("expected orchestrator line, got: %q", out)
	}
	for _, col := range []string{"SESSION", "ACTIVITY", "PR", "CI", "AGE"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected worker table column %q, got: %q", col, out)
		}
	}
	if !strings.Contains(out, "demo-worker-1") || !strings.Contains(out, "editing foo.go") || !strings.Contains(out, "5m") {
		t.Errorf("expected worker 1 row, got: %q", out)
	}
	if !strings.Contains(out, "demo-worker-2") || !strings.Contains(out, "30s") {
		t.Errorf("expected worker 2 row, got: %q", out)
	}
}

// TestRenderStatusWorkerWithPRAndCI tests rendering workers with PR and CI info.
func TestRenderStatusWorkerWithPRAndCI(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	sessions := []sessionRow{
		{ID: "demo-orch", Kind: "orchestrator", State: "running", CreatedAt: now.Unix()},
		{ID: "worker-with-pr", Kind: "worker", State: "running", Activity: "testing", PRNumber: 42, PRState: "open", CIState: "failing", CreatedAt: now.Add(-5 * time.Minute).Unix()},
		{ID: "worker-no-pr", Kind: "worker", State: "running", Activity: "idle", PRNumber: 0, PRState: "", CIState: "", CreatedAt: now.Add(-10 * time.Minute).Unix()},
	}

	var buf bytes.Buffer
	renderStatus("demo-feature", sessions, nil, &buf, now)
	out := buf.String()

	if !strings.Contains(out, "#42") {
		t.Errorf("expected PR #42 in output; got:\n%s", out)
	}
	if !strings.Contains(out, "failing") {
		t.Errorf("expected CI state failing in output; got:\n%s", out)
	}
}

// TestRenderStatusNoOrchestrator tests rendering when no orchestrator is
// present in the session list.
func TestRenderStatusNoOrchestrator(t *testing.T) {
	now := time.Now()
	sessions := []sessionRow{
		{ID: "demo-worker-1", Kind: "worker", State: "running", CreatedAt: now.Unix()},
	}

	var buf bytes.Buffer
	renderStatus("demo-feature", sessions, nil, &buf, now)
	out := buf.String()

	if !strings.Contains(out, "orchestrator: -") {
		t.Errorf("expected 'orchestrator: -' line, got: %q", out)
	}
}

// TestRenderStatusNoWorkersNoTable tests that the worker table is omitted
// when there are no workers.
func TestRenderStatusNoWorkersNoTable(t *testing.T) {
	now := time.Now()
	sessions := []sessionRow{
		{ID: "demo-orch", Kind: "orchestrator", State: "running", CreatedAt: now.Unix()},
	}

	var buf bytes.Buffer
	renderStatus("demo-feature", sessions, nil, &buf, now)
	out := buf.String()

	if strings.Contains(out, "SESSION") {
		t.Errorf("expected no worker table when there are no workers, got: %q", out)
	}
}

// TestRenderStatusMirrorBlock checks that the mirror freshness block is
// printed after the worker table, covers every registered mirror (not just
// the feature's own repos — the incidents behind task #795 were agents
// reading repos their feature did not own), and that a mirror whose
// freshness could not be computed does not swallow the rest of the output.
func TestRenderStatusMirrorBlock(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	sessions := []sessionRow{
		{ID: "demo-orch", Kind: "orchestrator", State: "running", Activity: "planning", CreatedAt: now.Unix()},
		{ID: "demo-worker-1", Kind: "worker", State: "running", Activity: "editing foo.go", CreatedAt: now.Unix()},
	}
	mirrors := []mirrorRow{
		{RepoID: "rocket", Fresh: mirror.Freshness{LastFetch: now.Add(-2 * time.Minute)}},
		{RepoID: "docs-source", Fresh: mirror.Freshness{BehindCommits: 37, LastFetch: now.Add(-72 * time.Hour), Stale: true}},
		{RepoID: "app", Fresh: mirror.Freshness{Blocked: mirror.BlockedDirty, Stale: true}},
		{RepoID: "broken", Err: errors.New("not a git repository")},
	}

	var buf bytes.Buffer
	renderStatus("demo-feature", sessions, mirrors, &buf, now)
	out := buf.String()

	for _, want := range []string{
		"mirror rocket: свежее (последний fetch 2 мин назад)",
		"mirror docs-source: ПРОТУХЛО — рабочее дерево отстаёт на 37 коммитов, последний fetch 3 дня назад",
		"mirror app: ПРОТУХЛО — синхронизация не может обновить дерево: локальные изменения в зеркале",
		"mirror broken: свежесть неизвестна (not a git repository)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected line %q, got:\n%s", want, out)
		}
	}

	// The sessions must still be there — a broken mirror is exactly when the
	// rest of the output matters most.
	if !strings.Contains(out, "demo-worker-1") {
		t.Errorf("expected the worker table to survive alongside mirrors, got:\n%s", out)
	}
	if strings.Index(out, "SESSION") > strings.Index(out, "mirror rocket") {
		t.Errorf("expected the mirror block after the session table, got:\n%s", out)
	}
}
