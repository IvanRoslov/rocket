package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestTaskAddMilestoneWithProjectIsUsageError(t *testing.T) {
	cmd := newTaskAddCmd()
	cmd.SetArgs([]string{"Agents UX", "--milestone", "--project", "rocket"})
	err := cmd.Execute()
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("err = %v (%T), want usageError", err, err)
	}
	if !strings.Contains(usageErr.message, "--milestone") {
		t.Errorf("message = %q, want it to mention --milestone", usageErr.message)
	}
}

func TestNeedsProjectDefaultMilestone(t *testing.T) {
	if needsProjectDefault("", 0, true) {
		t.Error("a milestone must not resolve a default project")
	}
	if !needsProjectDefault("", 0, false) {
		t.Error("a plain task with no project/parent must resolve a default project")
	}
}

func TestTaskAddRequestBodyMilestone(t *testing.T) {
	body := taskAddRequestBody("Agents UX", "", "", 0, true)
	if body["milestone"] != true {
		t.Errorf("milestone = %v, want true", body["milestone"])
	}
	if _, ok := body["project"]; ok {
		t.Errorf("milestone body carries a project: %v", body["project"])
	}

	plain := taskAddRequestBody("T", "", "rocket", 0, false)
	if _, ok := plain["milestone"]; ok {
		t.Errorf("plain body carries milestone: %v", plain["milestone"])
	}
	if plain["project"] != "rocket" {
		t.Errorf("project = %v, want rocket", plain["project"])
	}
}

func TestTaskLsQuery(t *testing.T) {
	if got := taskLsQuery("", "", true); !strings.Contains(got, "milestones=true") {
		t.Errorf("query = %q, want milestones=true", got)
	}
	if got := taskLsQuery("review", "rocket", false); strings.Contains(got, "milestones") {
		t.Errorf("query = %q, want no milestones key", got)
	}
}

func TestAssignRequestBody(t *testing.T) {
	body, err := assignRequestBody("cto", false)
	if err != nil {
		t.Fatalf("assignRequestBody: %v", err)
	}
	if body["agent_id"] != "cto" {
		t.Errorf("agent_id = %v, want cto", body["agent_id"])
	}

	body, err = assignRequestBody("", true)
	if err != nil {
		t.Fatalf("assignRequestBody --none: %v", err)
	}
	if body["none"] != true {
		t.Errorf("none = %v, want true", body["none"])
	}

	if _, err := assignRequestBody("", false); err == nil {
		t.Error("assign without an agent and without --none: want usage error")
	}
	if _, err := assignRequestBody("cto", true); err == nil {
		t.Error("assign with both an agent and --none: want usage error")
	}
}

func TestTaskTakeAndAssignUsage(t *testing.T) {
	for _, tt := range []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{"take without id", newTaskTakeCmd, []string{}},
		{"take with bad id", newTaskTakeCmd, []string{"abc"}},
		{"assign without id", newTaskAssignCmd, []string{}},
		{"assign with bad id", newTaskAssignCmd, []string{"abc", "cto"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("err = %v (%T), want usageError", err, err)
			}
		})
	}
}

func TestRenderTaskCardMilestone(t *testing.T) {
	var buf bytes.Buffer
	renderTaskCard(taskDetailRow{
		ID: 42, Title: "Agents UX", Status: "in_progress",
		Milestone: true, AssignedRole: "cto",
	}, nil, nil, nil, &buf, time.Now())

	out := buf.String()
	for _, want := range []string{"Milestone: yes", "Agent: cto", "rocket agent attach cto"} {
		if !strings.Contains(out, want) {
			t.Errorf("card missing %q:\n%s", want, out)
		}
	}

	buf.Reset()
	renderTaskCard(taskDetailRow{ID: 43, Title: "Agents UX", Status: "backlog", Milestone: true},
		nil, nil, nil, &buf, time.Now())
	if !strings.Contains(buf.String(), "Agent: не взят") {
		t.Errorf("untaken milestone card:\n%s", buf.String())
	}

	buf.Reset()
	renderTaskCard(taskDetailRow{ID: 44, Title: "regular", Status: "backlog", ProjectID: "rocket"},
		nil, nil, nil, &buf, time.Now())
	if strings.Contains(buf.String(), "Milestone") {
		t.Errorf("regular task card mentions milestones:\n%s", buf.String())
	}
}

func TestRenderAgentMilestones(t *testing.T) {
	var buf bytes.Buffer
	renderAgentMilestones(&buf, []any{
		map[string]any{"id": float64(42), "title": "Agents UX", "status": "in_progress"},
	})
	out := buf.String()
	if !strings.Contains(out, "#42") || !strings.Contains(out, "Agents UX") || !strings.Contains(out, "in_progress") {
		t.Errorf("milestones block = %q", out)
	}

	buf.Reset()
	renderAgentMilestones(&buf, nil)
	if !strings.Contains(buf.String(), "milestones: 0") {
		t.Errorf("empty milestones block = %q", buf.String())
	}
}
