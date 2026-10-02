package session

import (
	"context"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"

	"github.com/IvanRoslov/rocket/internal/prompts"
	"github.com/IvanRoslov/rocket/internal/store"
)

func TestSpawnOrchestratorUsesTaskBrainstormSkill(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	proj, err := st.GetProject("proj1")
	if err != nil {
		t.Fatal(err)
	}
	// The task's own value wins over the current setting.
	if err := st.SetSetting(store.SettingOrchestratorBrainstormCustom, "false"); err != nil {
		t.Fatal(err)
	}
	task := store.Task{ID: 42, Title: "Add login page", ProjectID: "proj1", BrainstormSkill: prompts.CustomBrainstormSkill}

	if _, err := m.SpawnOrchestrator(context.Background(), task, proj, "fake", LaunchProfile{}); err != nil {
		t.Fatalf("SpawnOrchestrator: %v", err)
	}
	spec := testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1]
	if spec.BrainstormSkill != prompts.CustomBrainstormSkill {
		t.Errorf("spec.BrainstormSkill = %q, want %q", spec.BrainstormSkill, prompts.CustomBrainstormSkill)
	}
}

func TestSpawnOrchestratorFallsBackToSetting(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	proj, err := st.GetProject("proj1")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(store.SettingOrchestratorBrainstormCustom, "true"); err != nil {
		t.Fatal(err)
	}
	testFakeAgent.shipsSkill = true
	task := store.Task{ID: 42, Title: "Add login page", ProjectID: "proj1"}

	if _, err := m.SpawnOrchestrator(context.Background(), task, proj, "fake", LaunchProfile{}); err != nil {
		t.Fatalf("SpawnOrchestrator: %v", err)
	}
	spec := testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1]
	if spec.BrainstormSkill != prompts.CustomBrainstormSkill {
		t.Errorf("spec.BrainstormSkill = %q, want %q", spec.BrainstormSkill, prompts.CustomBrainstormSkill)
	}
}

func restoreOrchestratorWithTaskSkill(t *testing.T, taskSkill, setting string, shipsSkill bool) string {
	t.Helper()
	spec, _, _ := restoreOrchestrator(t, taskSkill, setting, shipsSkill)
	return spec.BrainstormSkill
}

// restoreOrchestrator restores an errored orchestrator whose task stored
// taskSkill and returns the launch spec, the store and the task id.
func restoreOrchestrator(t *testing.T, taskSkill, setting string, shipsSkill bool) (agent.LaunchSpec, *store.Store, int64) {
	t.Helper()
	m, st, _, _, _ := testManager(t)
	testFakeAgent.shipsSkill = shipsSkill
	seedProjectRepo(t, st, "proj1", "repo1")
	sess := seedRunningSession(t, st, "orch1")
	sess.Kind = "orchestrator"
	sess.State = "errored"
	if err := st.UpdateSession(sess); err != nil {
		t.Fatal(err)
	}
	id, err := st.AddTask(store.Task{Title: "Add ping docs", ProjectID: "proj1", SessionID: "orch1"})
	if err != nil {
		t.Fatal(err)
	}
	if taskSkill != "" {
		if err := st.SetTaskBrainstormSkill(id, taskSkill); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetSetting(store.SettingOrchestratorBrainstormCustom, setting); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(context.Background(), "orch1"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	return testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1], st, id
}

// Flipping the toggle mid-storm must not switch a running task's skill.
func TestRestoreKeepsTaskBrainstormSkill(t *testing.T) {
	if got := restoreOrchestratorWithTaskSkill(t, prompts.StockBrainstormSkill, "true", true); got != prompts.StockBrainstormSkill {
		t.Errorf("restored BrainstormSkill = %q, want stored %q", got, prompts.StockBrainstormSkill)
	}
}

func TestRestoreOldTaskFallsBackToSetting(t *testing.T) {
	if got := restoreOrchestratorWithTaskSkill(t, "", "true", true); got != prompts.CustomBrainstormSkill {
		t.Errorf("restored BrainstormSkill = %q, want setting's %q", got, prompts.CustomBrainstormSkill)
	}
}

// An agent that does not lay out the custom skill (codex) never gets it named,
// whatever the setting says.
func TestBrainstormSkillFallbackIgnoresToggleForNonShippingAgent(t *testing.T) {
	if got := restoreOrchestratorWithTaskSkill(t, "", "true", false); got != prompts.StockBrainstormSkill {
		t.Errorf("restored BrainstormSkill = %q, want %q", got, prompts.StockBrainstormSkill)
	}

	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	proj, err := st.GetProject("proj1")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(store.SettingOrchestratorBrainstormCustom, "true"); err != nil {
		t.Fatal(err)
	}
	task := store.Task{ID: 42, Title: "Add login page", ProjectID: "proj1"}
	if _, err := m.SpawnOrchestrator(context.Background(), task, proj, "fake", LaunchProfile{}); err != nil {
		t.Fatalf("SpawnOrchestrator: %v", err)
	}
	if got := testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1].BrainstormSkill; got != prompts.StockBrainstormSkill {
		t.Errorf("spawned BrainstormSkill = %q, want %q", got, prompts.StockBrainstormSkill)
	}
}

// A task stores the custom skill with its version (task #5027); the prompt,
// the kickoff and the agent get the bare name.
func TestSpawnOrchestratorStripsSkillVersion(t *testing.T) {
	for _, rec := range []string{"orchestrator-brainstorming@1.0", "orchestrator-brainstorming@1.1"} {
		t.Run(rec, func(t *testing.T) {
			m, st, _, _, _ := testManager(t)
			seedProjectRepo(t, st, "proj1", "repo1")
			proj, err := st.GetProject("proj1")
			if err != nil {
				t.Fatal(err)
			}
			task := store.Task{ID: 42, Title: "Add login page", ProjectID: "proj1", BrainstormSkill: rec}
			if _, err := m.SpawnOrchestrator(context.Background(), task, proj, "fake", LaunchProfile{}); err != nil {
				t.Fatalf("SpawnOrchestrator: %v", err)
			}
			spec := testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1]
			if spec.BrainstormSkill != prompts.CustomBrainstormSkill {
				t.Errorf("spec.BrainstormSkill = %q, want %q", spec.BrainstormSkill, prompts.CustomBrainstormSkill)
			}
			for name, text := range map[string]string{"system prompt": spec.SystemPrompt, "kickoff": spec.FirstMessage} {
				if !strings.Contains(text, prompts.CustomBrainstormSkill) {
					t.Errorf("%s does not name %s", name, prompts.CustomBrainstormSkill)
				}
				if strings.Contains(text, prompts.CustomBrainstormSkill+"@") {
					t.Errorf("%s names the skill with its version", name)
				}
			}
		})
	}
}

func TestRestoreStripsSkillVersion(t *testing.T) {
	spec, _, _ := restoreOrchestrator(t, "orchestrator-brainstorming@1.0", "false", true)
	if spec.BrainstormSkill != prompts.CustomBrainstormSkill {
		t.Errorf("restored BrainstormSkill = %q, want %q", spec.BrainstormSkill, prompts.CustomBrainstormSkill)
	}
	if !strings.Contains(spec.SystemPrompt, prompts.CustomBrainstormSkill) || strings.Contains(spec.SystemPrompt, prompts.CustomBrainstormSkill+"@") {
		t.Errorf("restored system prompt does not name the bare skill")
	}
}

// Restoring a task started on an older version of the custom skill lays the
// current text into the worktree; the task log says so once.
func TestRestoreOlderSkillVersionWritesNote(t *testing.T) {
	_, st, id := restoreOrchestrator(t, "orchestrator-brainstorming@1.0", "true", true)
	notes, err := st.ListTaskLog(id, "note")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 {
		t.Fatalf("notes = %+v, want exactly one", notes)
	}
	if !strings.Contains(notes[0].Body, "1.0→"+prompts.CustomBrainstormSkillVersion) {
		t.Errorf("note = %q, want it to name 1.0→%s", notes[0].Body, prompts.CustomBrainstormSkillVersion)
	}
	got, err := st.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.BrainstormSkill != "orchestrator-brainstorming@1.0" {
		t.Errorf("task label changed to %q on restore", got.BrainstormSkill)
	}
}

func TestRestoreCurrentOrOtherSkillWritesNoNote(t *testing.T) {
	for _, rec := range []string{"orchestrator-brainstorming@" + prompts.CustomBrainstormSkillVersion, prompts.StockBrainstormSkill, ""} {
		t.Run(rec, func(t *testing.T) {
			_, st, id := restoreOrchestrator(t, rec, "true", true)
			notes, err := st.ListTaskLog(id, "note")
			if err != nil {
				t.Fatal(err)
			}
			if len(notes) != 0 {
				t.Errorf("notes = %+v, want none", notes)
			}
		})
	}
}
