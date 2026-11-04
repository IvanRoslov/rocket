package prompts

import (
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
