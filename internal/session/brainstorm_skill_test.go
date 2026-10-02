package session

import (
	"context"
	"testing"

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
	return testFakeAgent.setupCalls[len(testFakeAgent.setupCalls)-1].BrainstormSkill
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
