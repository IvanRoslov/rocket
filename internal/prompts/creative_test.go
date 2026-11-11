package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreativePromptsReadLiveRules(t *testing.T) {
	for _, role := range []string{"creative-orchestrator", "creative-worker"} {
		for _, home := range []string{"/custom/rocket", "/custom/rocket home"} {
			t.Run(role+home, func(t *testing.T) {
				vars := Vars{"session_id": "creative-1", "parent_id": "orch", "project_name": "Project", "worktree_path": "/tmp/work", "allowed_repos": "main", "creative_context": "run=4", "run_snapshot": "snapshot", "tools_home": home, "brief": "Make scene"}
				prompt, err := Render("", role, vars)
				if err != nil {
					t.Fatal(err)
				}
				// Ignore wrapping so formatting edits do not change the policy contract.
				prompt = strings.Join(strings.Fields(prompt), " ")
				for _, want := range []string{
					"`" + home + "/tools/rules.md`",
					"same Rocket home as the tools catalog",
					"user-owned, free-form text",
					"Read the current file at the start of work, after a restore, and again before every choice of tool, provider or model",
					"writing a worker brief that names a tool or model",
					"preparing a paid-generation proposal",
					"Do not rely on a copy captured at launch",
					"A missing or empty file means there are no rules; proceed as before, without a blocker",
					"the human's direct instruction in this run comes first, then the rules file, then your own judgement",
					"A one-off instruction does not change the file",
					"Rules never replace paid-generation Go or stage approval",
					"If a rule cannot be followed (tool not registered, no authorization, model unavailable), report a blocker or ask",
					"Never silently substitute another tool or model",
					"Any agent may edit the file, but only when the human directly asks to change a rule",
					"Never edit it on your own initiative",
					"If it is unclear whether the human wants a one-off exception or a new rule, ask",
					"Preserve the other rules and tell the human that the change applies to all runs",
					"Rocket neither ships nor creates this file",
					"Rocket does not enforce these rules; this is an agent rule",
				} {
					if !strings.Contains(prompt, want) {
						t.Errorf("missing live-rules instruction %q", want)
					}
				}
			})
		}
	}
}

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

func TestCreativeOrchestratorAttachesPublishedResults(t *testing.T) {
	vars := Vars{"session_id": "creative-1", "parent_id": "orch", "project_name": "Project", "worktree_path": "/tmp/work", "allowed_repos": "main", "creative_context": "run=4", "run_snapshot": "snapshot", "tools_home": "/tmp/rocket", "brief": "Report published results"}
	prompt, err := Render("", "creative-orchestrator", vars)
	if err != nil {
		t.Fatal(err)
	}
	prompt = strings.Join(strings.Fields(prompt), " ")
	for _, want := range []string{
		"When reporting results (new versions, fixes or options to choose from), attach every published version you discuss to that same feed message with --version",
		"State briefly what each attachment is and which item it belongs to",
		"A result message without attachments violates this rule",
		"Attach intermediate/service files only when the human should look at them",
		"Report published results with attachments in the same feed message; follow the result-attachment rule below",
		"Repeat --version for multiple immutable versions",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing result attachment guidance %q", want)
		}
	}
}
