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
