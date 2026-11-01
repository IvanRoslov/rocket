package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/prompts"
)

// codex lays rocket's orchestrator skills under .agents/skills, so it claims
// the custom brainstorm skill like the other skill-capable agents.
func TestCodexShipsBrainstormSkill(t *testing.T) {
	if !agent.ShipsBrainstormSkill(New()) {
		t.Error("codex must report shipping the orchestrator brainstorm skill")
	}
}

func TestCodexSetupWorkspaceLaysOrchestratorSkills(t *testing.T) {
	withCodexHome(t)
	wt := t.TempDir()
	spec := agent.LaunchSpec{WorktreePath: wt, Kind: "orchestrator", BrainstormSkill: prompts.BrainstormSkillRecord(true)}
	if err := New().SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace: %v", err)
	}
	for _, name := range prompts.OrchestratorSkillNames() {
		if _, err := os.Stat(filepath.Join(wt, ".agents", "skills", name, "SKILL.md")); err != nil {
			t.Errorf("%s not laid out: %v", name, err)
		}
	}

	// A stock-skill orchestrator keeps the planning skill but loses the
	// custom brainstorm copy.
	spec.BrainstormSkill = prompts.StockBrainstormSkill
	if err := New().SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace (stock): %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".agents", "skills", prompts.CustomBrainstormSkill)); !os.IsNotExist(err) {
		t.Error("custom brainstorm skill lingers for a stock-skill orchestrator")
	}
	if _, err := os.Stat(filepath.Join(wt, ".agents", "skills", prompts.OrchestratorPlanningSkill, "SKILL.md")); err != nil {
		t.Errorf("planning skill missing for a stock-skill orchestrator: %v", err)
	}
}

func TestCodexSetupWorkspaceWorkerGetsNoOrchestratorSkills(t *testing.T) {
	withCodexHome(t)
	wt := t.TempDir()
	if err := New().SetupWorkspace(agent.LaunchSpec{WorktreePath: wt, Kind: "worker"}); err != nil {
		t.Fatalf("SetupWorkspace: %v", err)
	}
	for _, name := range prompts.OrchestratorSkillNames() {
		if _, err := os.Stat(filepath.Join(wt, ".agents", "skills", name)); !os.IsNotExist(err) {
			t.Errorf("worker got orchestrator skill %s", name)
		}
	}
}
