package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/IvanRoslov/rocket/internal/agent/pi"
)

func TestSpawnPiScrubsUnrelatedProviderCredentials(t *testing.T) {
	m, st, _, rt, ws := testManager(t)
	ws.createMkdir = true
	ws.createResult.Path = filepath.Join(t.TempDir(), "worktree")
	seedProjectRepo(t, st, "proj1", "repo1")

	repo, err := st.GetRepo("repo1")
	if err != nil {
		t.Fatal(err)
	}
	repo.Env = map[string]string{
		"DEEPSEEK_API_KEY": "repo-deepseek",
		"OPENAI_API_KEY":   "repo-openai",
	}
	if err := st.UpdateRepo(repo); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "daemon-deepseek")
	t.Setenv("OPENAI_API_KEY", "daemon-openai")
	t.Setenv("ANTHROPIC_API_KEY", "daemon-anthropic")

	binDir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '1.0.0'\n"
	if err := os.WriteFile(filepath.Join(binDir, "pi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err = m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat",
		AgentName: "pi", Profile: LaunchProfile{Model: "deepseek/deepseek-flash"},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(rt.created) != 1 {
		t.Fatalf("runtime.Create calls = %d, want 1", len(rt.created))
	}
	created := rt.created[0]
	if got := created.Env["DEEPSEEK_API_KEY"]; got != "daemon-deepseek" {
		t.Errorf("selected provider key = %q, want daemon-deepseek", got)
	}
	for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		if value, ok := created.Env[key]; ok {
			t.Errorf("unrelated credential %s survived merge: %q", key, value)
		}
		if !containsString(created.UnsetEnv, key) {
			t.Errorf("inherited credential %s will not be unset", key)
		}
	}
	if containsString(created.UnsetEnv, "DEEPSEEK_API_KEY") {
		t.Error("selected provider key would be unset")
	}
}
