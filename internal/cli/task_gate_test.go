package cli

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTaskGateUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"request no args", []string{"request"}},
		{"request bad id", []string{"request", "x"}},
		{"ls no args", []string{"ls"}},
		{"ls bad id", []string{"ls", "x"}},
		{"go no args", []string{"go"}},
		{"go bad id", []string{"go", "x"}},
		{"go extra", []string{"go", "1", "extra"}},
		{"changes no comment", []string{"changes", "1"}},
		{"changes comment and file", []string{"changes", "1", "text", "--file", "f.md"}},
		{"changes bad id", []string{"changes", "x", "text"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newTaskGateCmd()
			cmd.SetArgs(tt.args)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			err := cmd.Execute()
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Errorf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

func TestTaskCmdHasGate(t *testing.T) {
	for _, c := range newTaskCmd().Commands() {
		if c.Name() == "gate" {
			return
		}
	}
	t.Fatal("rocket task has no gate subcommand")
}

func TestRenderGates(t *testing.T) {
	plan := int64(1)
	decided := time.Date(2026, 10, 1, 15, 0, 0, 0, time.Local).Unix()
	gates := []gateRow{
		{ID: 3, TaskID: 7, SpecVersion: 2, PlanVersion: &plan, Status: "go", DecidedBy: "user", DecidedAt: &decided,
			RequestedBy: "task-7-orch", RequestedAt: decided - 60},
		{ID: 2, TaskID: 7, SpecVersion: 1, Status: "changes", Comment: "добавь ошибки", RequestedAt: decided - 3600},
		{ID: 1, TaskID: 7, SpecVersion: 1, Status: "superseded", RequestedAt: decided - 7200},
	}
	out := renderGates(7, gates)
	for _, want := range []string{
		"#3  спека v2 · план v1  go",
		"#2  спека v1 · план —  changes: добавь ошибки",
		"#1  спека v1 · план —  superseded",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if got := renderGates(7, nil); !strings.Contains(got, "гейтов нет") {
		t.Errorf("empty output = %q", got)
	}
}

func TestGateDecisionLine(t *testing.T) {
	plan := int64(2)
	if got := gateDecisionLine(gateRow{ID: 4, SpecVersion: 3, PlanVersion: &plan, Status: "go"}); got != "gate #4: go (спека v3 · план v2)\n" {
		t.Errorf("go line = %q", got)
	}
	if got := gateDecisionLine(gateRow{ID: 4, SpecVersion: 3, Status: "changes", Comment: "c"}); got != "gate #4: changes (спека v3 · план —): c\n" {
		t.Errorf("changes line = %q", got)
	}
}
