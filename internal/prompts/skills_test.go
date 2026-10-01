package prompts

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrainstormSkillName(t *testing.T) {
	if got := BrainstormSkill(true); got != "orchestrator-brainstorming" {
		t.Errorf("BrainstormSkill(true) = %q", got)
	}
	if got := BrainstormSkill(false); got != "superpowers:brainstorming" {
		t.Errorf("BrainstormSkill(false) = %q", got)
	}
}

// The shipped copy is the measurement baseline: renamed, nothing else.
func TestOrchestratorSkillIsRenamedCopy(t *testing.T) {
	skill, err := orchestratorSkillFS.ReadFile(orchestratorSkillRoot + "/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded SKILL.md: %v", err)
	}
	if !strings.Contains(string(skill), "\nname: orchestrator-brainstorming\n") {
		t.Error("SKILL.md frontmatter does not name orchestrator-brainstorming")
	}
	if strings.Contains(string(skill), "skills/brainstorming/") {
		t.Error("SKILL.md still points at superpowers' skills/brainstorming/ path")
	}
	for _, f := range []string{"visual-companion.md", "spec-document-reviewer-prompt.md",
		"scripts/start-server.sh", "scripts/stop-server.sh", "scripts/server.cjs",
		"scripts/helper.js", "scripts/frame-template.html", "LICENSE", "README.md"} {
		if _, err := orchestratorSkillFS.ReadFile(orchestratorSkillRoot + "/" + f); err != nil {
			t.Errorf("embedded skill missing %s: %v", f, err)
		}
	}
}

func TestWriteOrchestratorSkill(t *testing.T) {
	skillsDir := t.TempDir()
	dest := filepath.Join(skillsDir, "orchestrator-brainstorming")

	// A stale file from an older rocket must not survive a rewrite.
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dest, "stale.md")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteOrchestratorSkill(skillsDir); err != nil {
		t.Fatalf("WriteOrchestratorSkill: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale file survived the rewrite")
	}

	err := fs.WalkDir(orchestratorSkillFS, orchestratorSkillRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		want, _ := orchestratorSkillFS.ReadFile(p)
		rel := strings.TrimPrefix(p, orchestratorSkillRoot+"/")
		got, readErr := os.ReadFile(filepath.Join(dest, rel))
		if readErr != nil {
			t.Errorf("%s not written: %v", rel, readErr)
			return nil
		}
		if string(got) != string(want) {
			t.Errorf("%s content differs from embedded copy", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, script := range []string{"start-server.sh", "stop-server.sh"} {
		info, err := os.Stat(filepath.Join(dest, "scripts", script))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o100 == 0 {
			t.Errorf("scripts/%s is not executable: %v", script, info.Mode())
		}
	}
	info, err := os.Stat(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Errorf("SKILL.md should not be executable: %v", info.Mode())
	}

	// Idempotent: a second run over the written tree succeeds.
	if err := WriteOrchestratorSkill(skillsDir); err != nil {
		t.Fatalf("second WriteOrchestratorSkill: %v", err)
	}
}
