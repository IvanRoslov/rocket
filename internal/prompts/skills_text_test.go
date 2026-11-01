package prompts

import (
	"strings"
	"testing"
)

// Version 1.1 of the skill (task #5027): fact tree before questions,
// scenarios before the spec.
func TestOrchestratorSkillV11Text(t *testing.T) {
	b, err := orchestratorSkillFS.ReadFile(orchestratorSkillRoot + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"\nname: orchestrator-brainstorming\n",
		"## Context Before Questions",
		"`INDEX.md`",
		"lepsto/platform",
		"what already exists on",
		"**Run scenarios**",
		`"Scenarios" section`,
		`"Run scenarios" [shape=box];`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md lacks %q", want)
		}
	}
	// The section sits between shared understanding and the hard gate.
	ctx := strings.Index(s, "## Context Before Questions")
	if ctx < strings.Index(s, "## Establish Shared Understanding") || ctx > strings.Index(s, "<HARD-GATE>") {
		t.Error("Context Before Questions is not between Establish Shared Understanding and <HARD-GATE>")
	}
	// Run scenarios comes after Present design and before Write design doc.
	arch := s[strings.Index(s, "**Architectural:**"):]
	if !(strings.Index(arch, "**Present design**") < strings.Index(arch, "**Run scenarios**") &&
		strings.Index(arch, "**Run scenarios**") < strings.Index(arch, "**Write design doc**")) {
		t.Error("Run scenarios is not between Present design and Write design doc")
	}
}

// Version 1.2 (review of #5894): the orchestrator brainstorms one level above
// the code, never hands the spec to an agent as a builder reviewer, and hands
// off to orchestrator-planning rather than writing-plans.
func TestOrchestratorSkillV12Text(t *testing.T) {
	b, err := orchestratorSkillFS.ReadFile(orchestratorSkillRoot + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"## Your Altitude",
		"**Reading code is allowed and often useful**",
		"**Altitude check:**",
		`read "as the`,
		"invoke the orchestrator-planning skill",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md lacks %q", want)
		}
	}
	if strings.Contains(s, "Invoke the writing-plans skill") || strings.Contains(s, "invoke writing-plans skill") {
		t.Error("SKILL.md still sends the orchestrator to writing-plans")
	}
}

// The planning skill is the orchestrator's own decomposition guide.
func TestOrchestratorPlanningSkillText(t *testing.T) {
	b, err := orchestratorPlanningSkillFS.ReadFile("skills/" + OrchestratorPlanningSkill + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"\nname: orchestrator-planning\n",
		"It is NOT an implementation plan",
		"**Mitigation first.**",
		"## From plan to worker briefs",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("planning SKILL.md lacks %q", want)
		}
	}
}
