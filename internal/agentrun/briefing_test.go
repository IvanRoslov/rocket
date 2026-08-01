package agentrun

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/roles"
	"github.com/IvanRoslov/rocket/internal/store"
)

// briefingFixture builds a store with a project, a role and a dossier that
// covers every branch of the selection rules.
func briefingFixture(t *testing.T) (*store.Store, string, store.Agent) {
	t.Helper()
	st, home := openTestStore(t)

	if err := st.AddRepo(store.Repo{ID: "repo1", Path: "/tmp/repo1", DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: "platform", Name: "Platform", MainRepo: "repo1"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	promptPath, err := roles.Ensure(home, "sre", "POLICY", true)
	if err != nil {
		t.Fatalf("roles.Ensure: %v", err)
	}
	role := store.Agent{ID: "sre", ProjectID: "platform", PromptPath: promptPath, Enabled: true}
	if err := st.AddAgent(role); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}

	writeMemory(t, home, "sre", "- [db-migrations](db.md) — платформа мигрирует ночью\n")
	return st, home, role
}

func writeMemory(t *testing.T, home, roleID, body string) {
	t.Helper()
	if err := writeFile(filepath.Join(roles.MemoryDir(home, roleID), "MEMORY.md"), body); err != nil {
		t.Fatalf("write memory: %v", err)
	}
}

func TestBuildBriefingSections(t *testing.T) {
	st, home, role := briefingFixture(t)

	taskID, err := st.AddTask(store.Task{Title: "Fix the deploy", ProjectID: "platform", Status: "in_progress"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	mustUpsertItem(t, st, store.AgentItem{
		RoleID: "sre", Kind: "issue", ExternalRef: "acme/platform#1",
		State: "deferred", Note: "ждём миграцию",
	})
	mustUpsertItem(t, st, store.AgentItem{
		RoleID: "sre", Kind: "issue", ExternalRef: "acme/platform#2",
		State: "waiting_team", Note: "нужен ответ команды",
	})
	mustUpsertItem(t, st, store.AgentItem{
		RoleID: "sre", Kind: "task", ExternalRef: "task:" + itoa(taskID),
		State: "in_work", TaskID: taskID,
	})
	mustUpsertItem(t, st, store.AgentItem{
		RoleID: "sre", Kind: "issue", ExternalRef: "acme/platform#7",
		State: "triaged", Note: "referenced by an event",
	})
	mustUpsertItem(t, st, store.AgentItem{
		RoleID: "sre", Kind: "issue", ExternalRef: "acme/platform#99",
		State: "closed", Note: "старое",
	})

	events := []store.AgentInboxEvent{
		{ID: 11, Kind: "message", Payload: `{"text":"blocked by X","from":"feat-orch"}`},
		{ID: 12, Kind: "issue_comment", Payload: `{"ref":"acme/platform#7","body":"any update?"}`},
	}

	out, err := BuildBriefing(st, home, role, events)
	if err != nil {
		t.Fatalf("BuildBriefing: %v", err)
	}

	for _, want := range []string{
		"## Inbox",
		"#11 message",
		"blocked by X",
		"feat-orch",
		"#12 issue_comment",
		"## Dossier",
		"issue:acme/platform#1 [deferred]",
		"ждём миграцию",
		"issue:acme/platform#2 [waiting_team]",
		"issue:acme/platform#7 [triaged]",
		"task:" + itoa(taskID) + " [in_work]",
		"Fix the deploy",
		"in_progress",
		"## Memory",
		"платформа мигрирует ночью",
		"rocket agent done",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("briefing is missing %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, "acme/platform#99") {
		t.Errorf("closed, unreferenced item leaked into the briefing:\n%s", out)
	}
	if n := strings.Count(out, "acme/platform#7"); n != 2 {
		t.Errorf("referenced item should appear once in the inbox and once in the dossier, got %d occurrences", n)
	}
}

func TestBuildBriefingWithEmptyDossierAndMemory(t *testing.T) {
	st, home, role := briefingFixture(t)
	writeMemory(t, home, "sre", "")

	out, err := BuildBriefing(st, home, role, []store.AgentInboxEvent{
		{ID: 1, Kind: "cron", Payload: `{}`},
	})
	if err != nil {
		t.Fatalf("BuildBriefing: %v", err)
	}

	for _, want := range []string{"#1 cron", "## Dossier", "(empty)", "rocket agent state set"} {
		if !strings.Contains(out, want) {
			t.Errorf("briefing is missing %q:\n%s", want, out)
		}
	}
}

func TestBuildBriefingRendersUnknownPayloadAsJSON(t *testing.T) {
	st, home, role := briefingFixture(t)

	out, err := BuildBriefing(st, home, role, []store.AgentInboxEvent{
		{ID: 3, Kind: "task_update", Payload: `{"task_id":45,"status":"review"}`},
	})
	if err != nil {
		t.Fatalf("BuildBriefing: %v", err)
	}
	if !strings.Contains(out, `"status":"review"`) && !strings.Contains(out, `"status": "review"`) {
		t.Errorf("payload not rendered:\n%s", out)
	}
}

func mustUpsertItem(t *testing.T, st *store.Store, it store.AgentItem) {
	t.Helper()
	if _, err := st.UpsertAgentItem(it); err != nil {
		t.Fatalf("UpsertAgentItem %s: %v", it.ExternalRef, err)
	}
}
