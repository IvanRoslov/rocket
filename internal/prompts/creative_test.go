package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreativePromptsSeparateLifecycleAndPaidGo(t *testing.T) {
	vars := Vars{"session_id": "creative-1", "parent_id": "creative-orch", "project_name": "Project", "worktree_path": "/tmp/work", "allowed_repos": "main and linked", "creative_context": "run=4 stage=5 item=6", "run_snapshot": "earlier results and pipeline snapshot", "tools_home": "/tmp/rocket", "brief": "Make scene"}
	for _, role := range []string{"creative-orchestrator", "creative-worker"} {
		prompt, err := Render("", role, vars)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"run=4 stage=5 item=6", "earlier results and pipeline snapshot", "explicit human Go", "paid retries", "Stage approval", "rocket tools", "artifact publish", "rework", "creative-preview setup"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s missing %q", role, want)
			}
		}
		for _, forbidden := range []string{"one PR", "spec-gate", "orchestrator-brainstorming", "rocket task start"} {
			if strings.Contains(prompt, forbidden) {
				t.Errorf("%s contains development policy %q", role, forbidden)
			}
		}
	}
}

func TestCreativePromptsAddressRolesAndDelivery(t *testing.T) {
	vars := Vars{"session_id": "creative-1", "parent_id": "creative-orch", "project_name": "Project", "worktree_path": "/tmp/work", "allowed_repos": "main", "creative_context": "run=4 stage=5 item=6", "run_snapshot": "snapshot", "tools_home": "/tmp/rocket", "brief": "Make scene"}
	for _, role := range []string{"creative-orchestrator", "creative-worker"} {
		rendered, err := Render("", role, vars)
		if err != nil {
			t.Fatal(err)
		}
		reportCommand := "rocket creative agent send RUN --to WORKER_SESSION"
		if role == "creative-worker" {
			reportCommand = "rocket creative agent send RUN --stage STAGE"
		}
		for _, want := range []string{reportCommand, "--wait", "accepted", "delivered", "failed", "rocket creative agent sessions RUN", "Never use tmux directly", "readiness is unknown", "--file", "explicit human Go"} {
			if !strings.Contains(rendered, want) {
				t.Errorf("%s missing operational instruction %q", role, want)
			}
		}
		if strings.Contains(rendered, "{{") {
			t.Errorf("unresolved fragment in %s", role)
		}
	}
}

func TestCreativeSharedRulesRenderingAndOverrides(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "prompts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "creative-worker.md")
	if err := os.WriteFile(path, []byte("custom {{session_id}}\n{{creative_rules}}"), 0644); err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(home, "creative-worker", Vars{"session_id": "worker", "tools_home": "/tool/home"})
	if err != nil {
		t.Fatalf("shared fragment variables were not resolved: %v", err)
	}
	if !strings.HasPrefix(rendered, "custom worker\n") || !strings.Contains(rendered, `rocket tools --home "/tool/home" list`) || strings.Contains(rendered, "{{") {
		t.Fatalf("bad expanded override: %s", rendered)
	}
	_, err = Render(home, "creative-worker", Vars{"session_id": "worker"})
	if err == nil || !strings.Contains(err.Error(), "{{tools_home}}") {
		t.Fatalf("missing shared variable: %v", err)
	}
	if err := os.WriteFile(path, []byte("custom {{session_id}} only"), 0644); err != nil {
		t.Fatal(err)
	}
	rendered, err = Render(home, "creative-worker", Vars{"session_id": "worker"})
	if err != nil || rendered != "custom worker only" {
		t.Fatalf("override changed: %q %v", rendered, err)
	}
}

func TestSharedTransportPreservesOrdinaryPrompt(t *testing.T) {
	data, err := templates.ReadFile("templates/orchestrator.md")
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the pre-extraction source, then compare complete rendered output.
	legacy := strings.ReplaceAll(string(data), "{{message_transport_rule}}", "Never use tmux directly —\n  rocket handles delivery, queueing and retries.")
	home := t.TempDir()
	dir := filepath.Join(home, "prompts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "orchestrator.md"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	before, err := Render(home, "orchestrator", completeVars())
	if err != nil {
		t.Fatal(err)
	}
	after, err := Render("", "orchestrator", completeVars())
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("ordinary orchestrator rendered instructions changed")
	}
}

func TestCreativePromptsUseSingleFormalReview(t *testing.T) {
	vars := Vars{"session_id": "creative-1", "parent_id": "orch", "project_name": "Project", "worktree_path": "/tmp/work", "allowed_repos": "main", "creative_context": "run=4", "run_snapshot": "snapshot", "tools_home": "/tmp/rocket", "brief": "Make scene"}
	for _, role := range []string{"creative-orchestrator", "creative-worker"} {
		prompt, err := Render("", role, vars)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Do not ask a separate question to approve", "review card", "exact immutable version IDs", "Return feedback"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s missing formal review contract: %s", role, want)
			}
		}
	}
}
