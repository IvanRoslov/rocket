package claudecode

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/prompts"
)

func skillFile(worktree string) string {
	return filepath.Join(worktree, ".claude", "skills", prompts.CustomBrainstormSkill, "SKILL.md")
}

func TestSetupWorkspaceLaysCustomSkillForOrchestrator(t *testing.T) {
	wt := t.TempDir()
	spec := agent.LaunchSpec{WorktreePath: wt, Kind: "orchestrator", BrainstormSkill: prompts.CustomBrainstormSkill}
	if err := New().SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace: %v", err)
	}
	data, err := os.ReadFile(skillFile(wt))
	if err != nil {
		t.Fatalf("skill not laid into worktree: %v", err)
	}
	if !strings.Contains(string(data), "name: orchestrator-brainstorming") {
		t.Error("laid SKILL.md is not the orchestrator-brainstorming skill")
	}
	info, err := os.Stat(filepath.Join(wt, ".claude", "skills", prompts.CustomBrainstormSkill, "scripts", "start-server.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("start-server.sh not executable: %v", info.Mode())
	}
}

// With the stock skill the custom copy must not be visible: both carry the
// same description, and a stray copy would let Claude pick it and blur the
// metric's per-skill series.
func TestSetupWorkspaceRemovesCustomSkillForStockOrchestrator(t *testing.T) {
	wt := t.TempDir()
	cc := New()
	if err := cc.SetupWorkspace(agent.LaunchSpec{WorktreePath: wt, Kind: "orchestrator", BrainstormSkill: prompts.CustomBrainstormSkill}); err != nil {
		t.Fatal(err)
	}
	if err := cc.SetupWorkspace(agent.LaunchSpec{WorktreePath: wt, Kind: "orchestrator", BrainstormSkill: prompts.StockBrainstormSkill}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(skillFile(wt))); !os.IsNotExist(err) {
		t.Errorf("custom skill dir still present for a stock-skill orchestrator (err=%v)", err)
	}
}

func TestSetupWorkspaceNoSkillForWorker(t *testing.T) {
	wt := t.TempDir()
	spec := agent.LaunchSpec{WorktreePath: wt, Kind: "worker", BrainstormSkill: prompts.CustomBrainstormSkill}
	if err := New().SetupWorkspace(spec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".claude", "skills")); !os.IsNotExist(err) {
		t.Errorf("worker worktree got a skills dir (err=%v)", err)
	}
}

func TestSetupWorkspaceCustomSkillIsGitIgnored(t *testing.T) {
	wt := t.TempDir()
	initGitRepo(t, wt)
	spec := agent.LaunchSpec{WorktreePath: wt, Kind: "orchestrator", BrainstormSkill: prompts.CustomBrainstormSkill}
	if err := New().SetupWorkspace(spec); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", wt, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "skills") {
		t.Errorf("skill files show up in git status:\n%s", out)
	}
}
