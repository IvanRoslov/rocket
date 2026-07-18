package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
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
	renderStatus("demo-feature", sessions, &buf, now)
	out := buf.String()

	if !strings.Contains(out, "feature: demo-feature") {
		t.Errorf("expected feature header, got: %q", out)
	}
	if !strings.Contains(out, "orchestrator: demo-orch [running] planning (2h ago)") {
		t.Errorf("expected orchestrator line, got: %q", out)
	}
	for _, col := range []string{"SESSION", "ACTIVITY", "AGE"} {
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

// TestRenderStatusNoOrchestrator tests rendering when no orchestrator is
// present in the session list.
func TestRenderStatusNoOrchestrator(t *testing.T) {
	now := time.Now()
	sessions := []sessionRow{
		{ID: "demo-worker-1", Kind: "worker", State: "running", CreatedAt: now.Unix()},
	}

	var buf bytes.Buffer
	renderStatus("demo-feature", sessions, &buf, now)
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
	renderStatus("demo-feature", sessions, &buf, now)
	out := buf.String()

	if strings.Contains(out, "SESSION") {
		t.Errorf("expected no worker table when there are no workers, got: %q", out)
	}
}
