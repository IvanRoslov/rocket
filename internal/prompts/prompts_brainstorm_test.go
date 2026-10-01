package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func renderWithSkill(t *testing.T, name, skill string) string {
	t.Helper()
	vars := completeVars()
	vars["brainstorm_skill"] = skill
	out, err := Render("", name, vars)
	if err != nil {
		t.Fatalf("Render %s: %v", name, err)
	}
	return out
}

// The setting picks which brainstorm skill the orchestrator is told to use;
// with the custom skill the stock one must not be named at all, or the
// orchestrator could still reach for it and blur the per-skill metric.
func TestOrchestratorPromptsNameBrainstormSkill(t *testing.T) {
	for _, name := range []string{"orchestrator", "kickoff"} {
		t.Run(name, func(t *testing.T) {
			custom := renderWithSkill(t, name, CustomBrainstormSkill)
			if !strings.Contains(custom, "invoke "+CustomBrainstormSkill) {
				t.Errorf("custom render does not tell the orchestrator to invoke %s", CustomBrainstormSkill)
			}
			if strings.Contains(custom, StockBrainstormSkill) {
				t.Errorf("custom render still names %s", StockBrainstormSkill)
			}

			stock := renderWithSkill(t, name, StockBrainstormSkill)
			if !strings.Contains(stock, "invoke "+StockBrainstormSkill) {
				t.Errorf("stock render does not tell the orchestrator to invoke %s", StockBrainstormSkill)
			}
			if strings.Contains(stock, CustomBrainstormSkill) {
				t.Errorf("stock render names %s", CustomBrainstormSkill)
			}
		})
	}
}

func TestOrchestratorPromptsRequireBrainstormSkillVar(t *testing.T) {
	for _, name := range []string{"orchestrator", "kickoff"} {
		vars := completeVars()
		delete(vars, "brainstorm_skill")
		if _, err := Render("", name, vars); err == nil || !strings.Contains(err.Error(), "{{brainstorm_skill}}") {
			t.Errorf("%s: Render without brainstorm_skill err = %v, want unresolved {{brainstorm_skill}}", name, err)
		}
	}
}

// The storm mechanism (task #4901 spec §2.4) is taught regardless of the
// toggle and of the agent: it lives outside the skills markers, so a codex
// orchestrator (skills stripped) gets it too.
func TestPromptsTeachStormMechanism(t *testing.T) {
	must := []string{
		"rocket task doc put 123 --kind problem",
		"--brainstorm",
		"--recommend",
		"one decision per question",
		"rocket task brainstorm record 123/Q",
		"verbatim",
		"rocket task gate request 123",
		"[rocket gate] Go",
		"[rocket gate] Нужны правки",
	}
	mustNot := []string{
		"Confirm spec",
		"rocket task move 123 in_progress",
		`--option "go" --option "needs changes"`,
	}

	texts := map[string]string{
		"kickoff":               renderWithSkill(t, "kickoff", StockBrainstormSkill),
		"orchestrator":          renderWithSkill(t, "orchestrator", StockBrainstormSkill),
		"orchestrator-stripped": renderStripped(t, "orchestrator"),
	}
	for name, text := range texts {
		for _, s := range must {
			if !strings.Contains(text, s) {
				t.Errorf("%s: missing %q", name, s)
			}
		}
		for _, s := range mustNot {
			if strings.Contains(text, s) {
				t.Errorf("%s: still contains %q", name, s)
			}
		}
	}
}

// renderStripped renders name from a template whose skills blocks were cut
// out first — the text an agent without skills support is meant to get.
func renderStripped(t *testing.T, name string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	stripped := StripSkills(rawTemplate(t, name))
	if err := os.WriteFile(filepath.Join(home, "prompts", name+".md"), []byte(stripped), 0o644); err != nil {
		t.Fatal(err)
	}
	vars := completeVars()
	vars["brainstorm_skill"] = CustomBrainstormSkill
	out, err := Render(home, name, vars)
	if err != nil {
		t.Fatalf("Render stripped %s: %v", name, err)
	}
	return out
}

// Workers always use the stock skill; their prompt has no knob.
func TestWorkerPromptHasNoBrainstormSkillVar(t *testing.T) {
	if strings.Contains(rawTemplate(t, "worker"), "brainstorm_skill") {
		t.Error("worker template references brainstorm_skill")
	}
	vars := completeVars()
	delete(vars, "brainstorm_skill")
	if _, err := Render("", "worker", vars); err != nil {
		t.Errorf("worker render needs brainstorm_skill: %v", err)
	}
}
