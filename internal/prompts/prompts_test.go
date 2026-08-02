package prompts

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// completeVars returns a complete set of Vars for testing.
func completeVars() Vars {
	return Vars{
		"feature_slug":     "test-feature",
		"task_id":          "123",
		"project_name":     "rocket",
		"session_id":       "sess-001",
		"worktree_path":    "/tmp/worktree",
		"main_repo":        "rocket",
		"main_repo_path":   "/path/to/rocket",
		"allowed_repos":    "rocket, agent-lib",
		"project_rules":    "", // Empty is OK for project_rules
		"task_title":       "Implement feature X",
		"task_description": "This is a test feature",
		"task_name":        "Implement feature X",
		"subtask_id":       "456",
		"repo_id":          "agent-lib",
		"branch":           "feature/test-feature/task",
		"parent_id":        "sess-002",
		"role_id":          "sre",
		"project_id":       "platform",
		"memory_dir":       "/home/agents/sre/memory",
		"role_prompt":      "ROLE POLICY BODY",
	}
}

func TestRenderWithCompleteVars(t *testing.T) {
	vars := completeVars()

	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			result, err := Render("", name, vars)
			if err != nil {
				t.Fatalf("Render failed: %v", err)
			}

			// Check non-empty
			if result == "" {
				t.Error("rendered template is empty")
			}

			// Check contains known substitutions (different for each template)
			var expectedContent string
			switch name {
			case "kickoff":
				expectedContent = "Implement feature X" // task_title
			case "orchestrator":
				expectedContent = "test-feature" // feature_slug
			case "worker":
				expectedContent = "sess-001" // session_id
			case "agent":
				expectedContent = "ROLE POLICY BODY" // role_prompt
			}
			if !strings.Contains(result, expectedContent) {
				t.Errorf("rendered template does not contain expected substitution: %s", expectedContent)
			}

			// Check no {{ remains
			if strings.Contains(result, "{{") {
				t.Error("rendered template contains unresolved placeholders")
			}
		})
	}
}

func TestRenderMissingVar(t *testing.T) {
	vars := completeVars()
	delete(vars, "task_id") // Remove required var

	_, err := Render("", "orchestrator", vars)
	if err == nil {
		t.Error("expected error for missing variable")
	}

	// Error should name the unresolved placeholder
	if !strings.Contains(err.Error(), "{{task_id}}") {
		t.Errorf("error should name the missing placeholder, got: %v", err)
	}
}

func TestRenderMultipleMissingVars(t *testing.T) {
	vars := completeVars()
	delete(vars, "feature_slug")
	delete(vars, "session_id")

	_, err := Render("", "orchestrator", vars)
	if err == nil {
		t.Error("expected error for missing variables")
	}

	// Error should name all missing placeholders
	errMsg := err.Error()
	if !strings.Contains(errMsg, "{{feature_slug}}") {
		t.Errorf("error should name feature_slug, got: %v", err)
	}
	if !strings.Contains(errMsg, "{{session_id}}") {
		t.Errorf("error should name session_id, got: %v", err)
	}
}

func TestRenderOverrideFile(t *testing.T) {
	tempDir := t.TempDir()

	// Create override directory
	promptsDir := filepath.Join(tempDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatalf("failed to create prompts dir: %v", err)
	}

	// Create override file
	overrideContent := "Override: {{feature_slug}}"
	overridePath := filepath.Join(promptsDir, "orchestrator.md")
	if err := os.WriteFile(overridePath, []byte(overrideContent), 0644); err != nil {
		t.Fatalf("failed to write override file: %v", err)
	}

	vars := Vars{"feature_slug": "test-feature"}

	result, err := Render(tempDir, "orchestrator", vars)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Should use override content
	if !strings.Contains(result, "Override:") {
		t.Error("override file was not used")
	}

	// Should NOT contain content from embedded template
	if strings.Contains(result, "FEATURE ORCHESTRATOR") {
		t.Error("embedded template was used instead of override")
	}
}

func TestRenderEmptyHome(t *testing.T) {
	vars := completeVars()

	result, err := Render("", "orchestrator", vars)
	if err != nil {
		t.Fatalf("Render with empty home failed: %v", err)
	}

	if result == "" {
		t.Error("rendered template is empty")
	}

	// Should contain content from embedded template
	if !strings.Contains(result, "FEATURE ORCHESTRATOR") {
		t.Error("embedded template was not used")
	}
}

func TestNames(t *testing.T) {
	names := Names()

	expected := []string{"kickoff", "orchestrator", "worker"}
	if len(names) != len(expected) {
		t.Errorf("expected %d names, got %d", len(expected), len(names))
	}

	// Check all expected names are present
	for i, name := range expected {
		if i >= len(names) || names[i] != name {
			t.Errorf("expected names to be %v, got %v", expected, names)
		}
	}

	// Verify they are sorted
	sorted := append([]string{}, names...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if len(names) != len(sorted) || names[0] != sorted[0] || names[len(names)-1] != sorted[len(sorted)-1] {
		// Simple check: if names are not in lexicographic order, this would fail
		for i := 0; i < len(names)-1; i++ {
			if names[i] > names[i+1] {
				t.Errorf("Names() should return sorted list, got: %v", names)
				break
			}
		}
	}
}

func TestRenderProjectRulesOptional(t *testing.T) {
	vars := completeVars()
	// project_rules is already "" in completeVars

	result, err := Render("", "orchestrator", vars)
	if err != nil {
		t.Fatalf("Render failed with empty project_rules: %v", err)
	}

	if result == "" {
		t.Error("rendered template is empty")
	}

	// Should not contain {{project_rules}}
	if strings.Contains(result, "{{project_rules}}") {
		t.Error("project_rules placeholder was not replaced with empty string")
	}
}

func TestRenderKickoffTemplate(t *testing.T) {
	vars := completeVars()

	result, err := Render("", "kickoff", vars)
	if err != nil {
		t.Fatalf("Render kickoff failed: %v", err)
	}

	// Check specific content from kickoff template
	if !strings.Contains(result, "Feature request from the human") {
		t.Error("kickoff template missing expected content")
	}

	if !strings.Contains(result, "superpowers:brainstorming") {
		t.Error("kickoff template missing superpowers reference")
	}

	// Verify task_title and task_description are replaced
	if !strings.Contains(result, "Implement feature X") {
		t.Error("task_title not properly substituted")
	}

	// Spec confirmation gate: reopens on any spec edit.
	if !strings.Contains(result, "reopens this gate") {
		t.Error("kickoff template missing spec re-confirmation gate phrase")
	}
	if !strings.Contains(result, `rocket task ask 123 "Confirm spec v<N>`) {
		t.Error("kickoff template missing rocket task ask confirmation command")
	}
}

func TestRenderWorkerTemplate(t *testing.T) {
	vars := completeVars()

	result, err := Render("", "worker", vars)
	if err != nil {
		t.Fatalf("Render worker failed: %v", err)
	}

	// Check specific content from worker template
	if !strings.Contains(result, "WORKER for task") {
		t.Error("worker template missing expected content")
	}

	if !strings.Contains(result, "Superpowers skills plugin") {
		t.Error("worker template missing superpowers reference")
	}

	// Verify substitutions
	if !strings.Contains(result, "sess-001") {
		t.Error("session_id not properly substituted")
	}

	// Evidence-based pushback: brief may be wrong, code wins.
	if !strings.Contains(result, "the code wins") {
		t.Error("worker template missing 'code wins' evidence-based pushback phrase")
	}

	// Inbox pointer note for large incoming messages.
	if !strings.Contains(result, "inbox/msg-") {
		t.Error("worker template missing inbox/msg- pointer note")
	}
}

func TestRenderOrchestratorFieldFixes(t *testing.T) {
	vars := completeVars()

	result, err := Render("", "orchestrator", vars)
	if err != nil {
		t.Fatalf("Render orchestrator failed: %v", err)
	}

	// Merge verification by content, not commit lists.
	if !strings.Contains(result, "squash merges") {
		t.Error("orchestrator template missing squash-merge verification note")
	}

	// Finishing sequence: per-worker done+kill as soon as merged/verified.
	if !strings.Contains(result, "task move <subtask-id> done") {
		t.Error("orchestrator template missing field-tested finishing sequence")
	}

	// Spec re-confirmation on edit.
	if !strings.Contains(result, "re-confirmation via") {
		t.Error("orchestrator template missing spec re-confirmation tracking bullet")
	}
}

func TestRenderInvalidTemplate(t *testing.T) {
	vars := completeVars()

	_, err := Render("", "nonexistent", vars)
	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}

func TestRenderValueWithLiteralPlaceholder(t *testing.T) {
	tempDir := t.TempDir()

	// Create override directory and file with literal {{ }} in a variable value
	promptsDir := filepath.Join(tempDir, "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatalf("failed to create prompts dir: %v", err)
	}

	// Template with task_description containing literal {{ }}
	overrideContent := "Task: {{task_title}}\nDescription: {{task_description}}"
	overridePath := filepath.Join(promptsDir, "test.md")
	if err := os.WriteFile(overridePath, []byte(overrideContent), 0644); err != nil {
		t.Fatalf("failed to write override file: %v", err)
	}

	// Value containing literal {{ }} should not cause error
	vars := Vars{
		"task_title":       "Implement feature",
		"task_description": "wrap vars in {{variable}} syntax",
	}

	result, err := Render(tempDir, "test", vars)
	if err != nil {
		t.Fatalf("Render failed for value with literal {{...}}: %v", err)
	}

	// Check the output contains the literal {{ }} from the value
	if !strings.Contains(result, "{{variable}}") {
		t.Error("rendered output should preserve literal {{}} from variable value")
	}

	// Check substitutions worked
	if !strings.Contains(result, "Implement feature") {
		t.Error("task_title not properly substituted")
	}
}

func TestRenderContainsNoMarkerLines(t *testing.T) {
	vars := completeVars()

	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			result, err := Render("", name, vars)
			if err != nil {
				t.Fatalf("Render failed: %v", err)
			}
			if strings.Contains(result, "<!-- skills:start -->") || strings.Contains(result, "<!-- skills:end -->") {
				t.Errorf("rendered %q output contains skills marker lines:\n%s", name, result)
			}
		})
	}
}

func TestRenderWorkerAndOrchestratorStillReferenceSuperpowers(t *testing.T) {
	// Sanity check: the (non-stripped, claude-code) render still carries the
	// Superpowers content between the markers -- only the marker lines themselves
	// are removed by Render.
	vars := completeVars()

	for _, name := range []string{"orchestrator", "worker"} {
		result, err := Render("", name, vars)
		if err != nil {
			t.Fatalf("Render(%q) failed: %v", name, err)
		}
		if !strings.Contains(result, "Superpowers") {
			t.Errorf("Render(%q) should still contain Superpowers content (markers only strip for codex via StripSkills)", name)
		}
	}
}

// rawTemplate reads the embedded, unrendered template source for name, so tests
// can exercise StripSkills against the marker-delimited source text directly
// (Render itself only removes bare marker lines, never whole skills blocks).
func rawTemplate(t *testing.T, name string) string {
	t.Helper()
	data, err := templates.ReadFile(filepath.Join("templates", name+".md"))
	if err != nil {
		t.Fatalf("failed to read embedded template %q: %v", name, err)
	}
	return string(data)
}

func TestStripSkillsRemovesOrchestratorBlock(t *testing.T) {
	raw := rawTemplate(t, "orchestrator")
	stripped := StripSkills(raw)

	if strings.Contains(stripped, "<!-- skills:start -->") || strings.Contains(stripped, "<!-- skills:end -->") {
		t.Error("StripSkills left marker lines behind")
	}
	if strings.Contains(stripped, "Superpowers") || strings.Contains(stripped, "superpowers:") {
		t.Errorf("StripSkills left Superpowers content behind:\n%s", stripped)
	}
	// The rest of the template must survive intact and coherent.
	if !strings.Contains(stripped, "## Rules") {
		t.Error("StripSkills should not remove content outside the skills block")
	}
	if !strings.Contains(stripped, "## Finishing") {
		t.Error("StripSkills should not remove content outside the skills block")
	}
	if strings.Contains(stripped, "\n\n\n") {
		t.Error("StripSkills should collapse blank lines left behind by a removed block")
	}
}

func TestStripSkillsRemovesWorkerBlock(t *testing.T) {
	raw := rawTemplate(t, "worker")
	stripped := StripSkills(raw)

	if strings.Contains(stripped, "<!-- skills:start -->") || strings.Contains(stripped, "<!-- skills:end -->") {
		t.Error("StripSkills left marker lines behind")
	}
	// The skill-only lines (marked individually) must be gone.
	if strings.Contains(stripped, "You have the Superpowers skills plugin") {
		t.Error("StripSkills left the Superpowers intro sentence behind")
	}
	if strings.Contains(stripped, "superpowers:writing-plans") {
		t.Error("StripSkills left the writing-plans invocation behind")
	}
	if strings.Contains(stripped, "superpowers:systematic-debugging") {
		t.Error("StripSkills left the systematic-debugging invocation behind")
	}
	// NOTE: steps 3 and 5 mention "superpowers:test-driven-development",
	// "superpowers:subagent-driven-development" and
	// "superpowers:verification-before-completion" mid-line, sharing the line
	// with non-skill delivery text (commit/verify instructions). Those clauses
	// are not line-separable without editing the surrounding prose, so per the
	// scoping rule they are intentionally left unmarked and survive stripping
	// as a documented, minor cosmetic leftover.
	// Surrounding sections must remain, and remain coherent (headings intact).
	if !strings.Contains(stripped, "## Ground rules") {
		t.Error("StripSkills should not remove content outside the skills block")
	}
	if !strings.Contains(stripped, "## Tracking (required)") {
		t.Error("StripSkills should not remove content outside the skills block")
	}
	if !strings.Contains(stripped, "## Reporting") {
		t.Error("StripSkills should not remove content outside the skills block")
	}
	if strings.Contains(stripped, "\n\n\n") {
		t.Error("StripSkills should collapse blank lines left behind by a removed block")
	}
}

// TestStripSkillsWorkerKeepsDeliverySteps guards the critical regression this
// fix addresses: markers used to wrap the entire "Workflow" section, so
// StripSkills (used for non-claude-code runtimes like codex) deleted the PR
// open / CI react / merged-done steps along with the skill invocations. The
// markers now scope only the skill-specific lines, so these delivery steps
// must survive StripSkills intact.
func TestStripSkillsWorkerKeepsDeliverySteps(t *testing.T) {
	raw := rawTemplate(t, "worker")
	stripped := StripSkills(raw)

	for _, want := range []string{"gh pr create", "CI", "merged", "rocket task log", "rocket send"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("stripped worker render missing required phrase %q:\n%s", want, stripped)
		}
	}
}

// TestStripSkillsFullRenderUnchanged pins down the hard requirement that the
// non-stripped (claude-code) render is unaffected by scoping the markers more
// tightly: every original workflow step's key phrase must still be present,
// in its original relative order, in the full render.
func TestStripSkillsFullRenderUnchanged(t *testing.T) {
	vars := completeVars()
	result, err := Render("", "worker", vars)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	steps := []string{
		"Read the brief (your first message) carefully",
		"Plan: invoke superpowers:writing-plans for the implementation plan.",
		"Implement: superpowers:test-driven-development",
		"Any bug or failing test",
		"Before declaring done",
		"Open the PR: gh pr create",
		"After the PR: react to CI failures",
		"When the PR is merged your job is done",
	}
	last := -1
	for _, phrase := range steps {
		idx := strings.Index(result, phrase)
		if idx == -1 {
			t.Errorf("full render missing original step phrase %q", phrase)
			continue
		}
		if idx < last {
			t.Errorf("full render step phrase %q is out of original order", phrase)
		}
		last = idx
	}
}

func TestStripSkillsNoOpWithoutMarkers(t *testing.T) {
	raw := rawTemplate(t, "kickoff")
	stripped := StripSkills(raw)

	if stripped != raw {
		t.Error("StripSkills should be a no-op on text without any skills markers")
	}
}

func TestStripMarkersOnlyRemovesMarkerLines(t *testing.T) {
	text := "before\n<!-- skills:start -->\nkept content\n<!-- skills:end -->\nafter\n"
	got := StripMarkers(text)
	want := "before\nkept content\nafter\n"
	if got != want {
		t.Errorf("StripMarkers() = %q, want %q", got, want)
	}
}

