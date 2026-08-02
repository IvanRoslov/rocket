package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// addAgentFixtures registers the project every agent row references.
func addAgentFixtures(t *testing.T, s *Store) {
	t.Helper()
	if err := s.AddRepo(Repo{ID: "platform-repo", Path: "/tmp/platform"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := s.AddProject(Project{ID: "platform", Name: "Platform", MainRepo: "platform-repo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
}

func testAgent(id string) Agent {
	return Agent{
		ID:          id,
		Description: "agent " + id,
		ProjectID:   "platform",
		Dir:         "/tmp/agents/" + id,
		Command:     "claude --dangerously-skip-permissions",
		Enabled:     true,
	}
}

func TestSessionKindAgentAllowed(t *testing.T) {
	s := openTestStore(t)

	sess := Session{
		ID:        "sre",
		Kind:      "agent",
		ProjectID: "platform",
		RepoID:    "platform-repo",
		Agent:     "claude-code",
		Branch:    "",
		TmuxName:  "sre",
		State:     "running",
	}
	if err := s.AddSession(sess); err != nil {
		t.Fatalf("AddSession kind=agent: %v", err)
	}

	got, err := s.GetSession("sre")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Kind != "agent" {
		t.Errorf("Kind = %q, want agent", got.Kind)
	}
}

// TestMigrationPreservesSessionsAndTasks guards the sessions table rebuild in
// migration 5: rows and the tasks→sessions reference must survive a reopen.
func TestMigrationPreservesSessionsAndTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rocket.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.AddRepo(Repo{ID: "api", Path: "/tmp/api"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := s1.AddProject(Project{ID: "billing", Name: "Billing", MainRepo: "api"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := s1.AddSession(Session{
		ID: "billing-orch", Kind: "orchestrator", ProjectID: "billing", RepoID: "api",
		Agent: "claude-code", Branch: "feature/x", TmuxName: "billing-orch", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	taskID, err := s1.AddTask(Task{Title: "t", ProjectID: "billing", SessionID: "billing-orch"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	sess, err := s2.GetSession("billing-orch")
	if err != nil {
		t.Fatalf("GetSession after reopen: %v", err)
	}
	if sess.Branch != "feature/x" {
		t.Errorf("Branch = %q, want feature/x", sess.Branch)
	}
	got, err := s2.GetTask(taskID)
	if err != nil {
		t.Fatalf("GetTask after reopen: %v", err)
	}
	if got.SessionID != "billing-orch" {
		t.Errorf("SessionID = %q, want billing-orch", got.SessionID)
	}
}

func TestAddGetAgent(t *testing.T) {
	s := openTestStore(t)
	addAgentFixtures(t, s)

	if err := s.AddAgent(testAgent("sre")); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	got, err := s.GetAgent("sre")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.ProjectID != "platform" || got.Description != "agent sre" ||
		got.Dir != "/tmp/agents/sre" || got.Command != "claude --dangerously-skip-permissions" {
		t.Errorf("agent = %+v", got)
	}
	if !got.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if got.CreatedAt == 0 || got.UpdatedAt == 0 {
		t.Errorf("timestamps not set: %+v", got)
	}
}

func TestAddAgentDuplicate(t *testing.T) {
	s := openTestStore(t)
	addAgentFixtures(t, s)

	if err := s.AddAgent(testAgent("sre")); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	if err := s.AddAgent(testAgent("sre")); !errors.Is(err, ErrExists) {
		t.Fatalf("second AddAgent = %v, want ErrExists", err)
	}
}

func TestGetAgentNotFound(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetAgent("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAgent = %v, want ErrNotFound", err)
	}
}

func TestListAgentsFiltersByProject(t *testing.T) {
	s := openTestStore(t)
	addAgentFixtures(t, s)
	if err := s.AddRepo(Repo{ID: "web-repo", Path: "/tmp/web"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := s.AddProject(Project{ID: "web", Name: "Web", MainRepo: "web-repo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	if err := s.AddAgent(testAgent("sre")); err != nil {
		t.Fatalf("AddAgent sre: %v", err)
	}
	triage := testAgent("triage")
	triage.ProjectID = "web"
	if err := s.AddAgent(triage); err != nil {
		t.Fatalf("AddAgent triage: %v", err)
	}

	all, err := s.ListAgents("")
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(all) != 2 || all[0].ID != "sre" || all[1].ID != "triage" {
		t.Fatalf("ListAgents(all) = %+v, want sre,triage ordered by id", all)
	}

	web, err := s.ListAgents("web")
	if err != nil {
		t.Fatalf("ListAgents(web): %v", err)
	}
	if len(web) != 1 || web[0].ID != "triage" {
		t.Fatalf("ListAgents(web) = %+v", web)
	}
}

func TestUpdateAgent(t *testing.T) {
	s := openTestStore(t)
	addAgentFixtures(t, s)
	if err := s.AddAgent(testAgent("sre")); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	a, err := s.GetAgent("sre")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	a.Enabled = false
	a.Description = "on call"
	a.Command = ""
	if err := s.UpdateAgent(a); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}

	got, err := s.GetAgent("sre")
	if err != nil {
		t.Fatalf("GetAgent after update: %v", err)
	}
	if got.Enabled {
		t.Errorf("Enabled = true, want false")
	}
	if got.Command != "" {
		t.Errorf("Command = %q, want empty", got.Command)
	}
	if got.Description != "on call" {
		t.Errorf("Description = %q, want \"on call\"", got.Description)
	}
	if got.UpdatedAt < a.CreatedAt {
		t.Errorf("UpdatedAt = %d, want >= CreatedAt %d", got.UpdatedAt, a.CreatedAt)
	}
}

func TestUpdateAgentNotFound(t *testing.T) {
	s := openTestStore(t)
	addAgentFixtures(t, s)
	if err := s.UpdateAgent(testAgent("ghost")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateAgent = %v, want ErrNotFound", err)
	}
}

func TestDeleteAgentCascades(t *testing.T) {
	s := openTestStore(t)
	addAgentFixtures(t, s)
	if err := s.AddAgent(testAgent("sre")); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	if _, err := s.AddInboxMessage(InboxMessage{AgentID: "sre", Body: "hi"}); err != nil {
		t.Fatalf("AddInboxMessage: %v", err)
	}

	if err := s.DeleteAgent("sre"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	if _, err := s.GetAgent("sre"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAgent after delete = %v, want ErrNotFound", err)
	}
	msgs, err := s.ListInboxMessages("sre", "", 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("inbox messages after delete = %d, want 0", len(msgs))
	}
}

func TestDeleteAgentNotFound(t *testing.T) {
	s := openTestStore(t)
	if err := s.DeleteAgent("ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteAgent = %v, want ErrNotFound", err)
	}
}
