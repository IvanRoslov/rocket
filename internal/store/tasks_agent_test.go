package store

import (
	"encoding/json"
	"testing"
)

// taskWithDossierRole creates a task and attaches it to the given roles'
// dossiers, returning the task id.
func taskWithDossierRole(t *testing.T, s *Store, roles ...string) int64 {
	t.Helper()

	taskID, err := s.AddTask(Task{Title: "fix the pager", ProjectID: "platform"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	for _, role := range roles {
		if _, err := s.UpsertAgentItem(AgentItem{
			RoleID: role, Kind: "issue", ExternalRef: "o/r#1", State: "taken", TaskID: taskID,
		}); err != nil {
			t.Fatalf("UpsertAgentItem: %v", err)
		}
	}
	return taskID
}

func TestTaskUpdateInboxEventOnStatusChange(t *testing.T) {
	st, role := setupAgentGH(t)
	taskID := taskWithDossierRole(t, st, role)

	if err := st.UpdateTaskStatus(taskID, "review"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	events, err := st.QueuedInboxEvents(role)
	if err != nil {
		t.Fatalf("QueuedInboxEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "task_update" {
		t.Fatalf("expected one task_update event, got %+v", events)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(events[0].Payload), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["task_id"] != float64(taskID) || payload["title"] != "fix the pager" ||
		payload["from"] != "backlog" || payload["to"] != "review" {
		t.Fatalf("unexpected payload: %v", payload)
	}
}

func TestTaskUpdateInboxNoEventWithoutTransition(t *testing.T) {
	st, role := setupAgentGH(t)
	taskID := taskWithDossierRole(t, st, role)

	// backlog → backlog is not a transition: writing an event here would wake
	// the role for nothing on every no-op save.
	if err := st.UpdateTaskStatus(taskID, "backlog"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	events, _ := st.QueuedInboxEvents(role)
	if len(events) != 0 {
		t.Fatalf("expected no events for a no-op transition, got %+v", events)
	}
}

func TestTaskUpdateInboxIgnoresUnreferencedTasks(t *testing.T) {
	st, role := setupAgentGH(t)

	taskID, err := st.AddTask(Task{Title: "nobody's task", ProjectID: "platform"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := st.UpdateTaskStatus(taskID, "done"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	events, _ := st.QueuedInboxEvents(role)
	if len(events) != 0 {
		t.Fatalf("expected no events for a task no dossier references, got %+v", events)
	}
}

func TestTaskUpdateInboxOneEventPerRole(t *testing.T) {
	st, role := setupAgentGH(t)
	if err := st.AddAgent(Agent{ID: "triage", ProjectID: "platform", PromptPath: "role.md", Enabled: true}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	taskID := taskWithDossierRole(t, st, role, "triage")

	if err := st.UpdateTaskStatus(taskID, "in_progress"); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	for _, r := range []string{role, "triage"} {
		events, _ := st.QueuedInboxEvents(r)
		if len(events) != 1 {
			t.Fatalf("role %s: expected exactly one event, got %d", r, len(events))
		}
	}
}
