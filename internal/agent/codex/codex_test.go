package codex

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// withCodexHome points CODEX_HOME at a fresh temp dir for the duration of
// the test and restores the previous value afterward.
func withCodexHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, hadOld := os.LookupEnv("CODEX_HOME")
	if err := os.Setenv("CODEX_HOME", dir); err != nil {
		t.Fatalf("setenv CODEX_HOME: %v", err)
	}
	t.Cleanup(func() {
		if hadOld {
			os.Setenv("CODEX_HOME", old)
		} else {
			os.Unsetenv("CODEX_HOME")
		}
	})
	return dir
}

func TestLaunchCommandMinimal(t *testing.T) {
	c := New()
	spec := agent.LaunchSpec{
		SessionID:    "sess-123",
		WorktreePath: "/tmp/wt",
		SocketPath:   "/tmp/socket.sock",
	}

	cmd := c.LaunchCommand(spec)
	want := []string{"codex", "--sandbox", "workspace-write", "--ask-for-approval", "never"}
	if len(cmd) != len(want) {
		t.Fatalf("expected %d args, got %d: %v", len(want), len(cmd), cmd)
	}
	for i := range want {
		if cmd[i] != want[i] {
			t.Errorf("cmd[%d] = %q, want %q (full: %v)", i, cmd[i], want[i], cmd)
		}
	}
}

func TestLaunchCommandFull(t *testing.T) {
	c := New()
	spec := agent.LaunchSpec{
		WorktreePath: "/tmp/wt",
		SystemPrompt: "You are a code assistant.",
		FirstMessage: "Help me write a function.",
		Model:        "gpt-5-codex",
	}

	cmd := c.LaunchCommand(spec)
	want := []string{
		"codex", "--sandbox", "workspace-write", "--ask-for-approval", "never",
		"-m", "gpt-5-codex",
		"--", "Help me write a function.",
	}
	if len(cmd) != len(want) {
		t.Fatalf("expected %d args, got %d: %v", len(want), len(cmd), cmd)
	}
	for i := range want {
		if cmd[i] != want[i] {
			t.Errorf("cmd[%d] = %q, want %q (full: %v)", i, cmd[i], want[i], cmd)
		}
	}
}

func TestLaunchCommandFirstMessageStartingWithDashIsSeparated(t *testing.T) {
	// A brief starting with "-" must not be parsed as a flag by codex's
	// clap-based CLI; LaunchCommand must insert a `--` separator before it.
	c := New()
	spec := agent.LaunchSpec{
		WorktreePath: "/tmp/wt",
		FirstMessage: "-fix the login bug",
	}

	cmd := c.LaunchCommand(spec)
	want := []string{
		"codex", "--sandbox", "workspace-write", "--ask-for-approval", "never",
		"--", "-fix the login bug",
	}
	if len(cmd) != len(want) {
		t.Fatalf("expected %d args, got %d: %v", len(want), len(cmd), cmd)
	}
	for i := range want {
		if cmd[i] != want[i] {
			t.Errorf("cmd[%d] = %q, want %q (full: %v)", i, cmd[i], want[i], cmd)
		}
	}
}

func TestLaunchCommandNoSystemPromptFlag(t *testing.T) {
	// codex's interactive mode has no flag equivalent to
	// --append-system-prompt; the system prompt is delivered via
	// AGENTS.md (SetupWorkspace), not the launch command.
	c := New()
	spec := agent.LaunchSpec{SystemPrompt: "should not appear on the command line"}
	cmd := c.LaunchCommand(spec)
	for _, arg := range cmd {
		if strings.Contains(arg, "should not appear") {
			t.Errorf("LaunchCommand leaked SystemPrompt into args: %v", cmd)
		}
	}
}

func TestEnvKeys(t *testing.T) {
	c := New()
	spec := agent.LaunchSpec{
		SessionID:  "sess-123",
		Kind:       "feature",
		ParentID:   "parent-456",
		ProjectID:  "proj-789",
		RepoID:     "repo-abc",
		Feature:    "feature-xyz",
		SocketPath: "/tmp/socket.sock",
	}

	env := c.Env(spec)

	want := map[string]string{
		"ROCKET_SESSION_ID": "sess-123",
		"ROCKET_KIND":       "feature",
		"ROCKET_PARENT_ID":  "parent-456",
		"ROCKET_PROJECT_ID": "proj-789",
		"ROCKET_REPO_ID":    "repo-abc",
		"ROCKET_FEATURE":    "feature-xyz",
		"ROCKET_SOCKET":     "/tmp/socket.sock",
	}
	for k, v := range want {
		got, ok := env[k]
		if !ok {
			t.Errorf("missing env var %s", k)
			continue
		}
		if got != v {
			t.Errorf("env[%s] = %q, want %q", k, got, v)
		}
	}
	if _, ok := env["CLAUDECODE"]; ok {
		t.Errorf("codex Env should not set CLAUDECODE")
	}
}

func TestSetupWorkspaceCreatesAgentsMD(t *testing.T) {
	withCodexHome(t)
	tmpDir := t.TempDir()

	c := New()
	spec := agent.LaunchSpec{
		WorktreePath: tmpDir,
		SystemPrompt: "Be a helpful worker.\n\n<!-- skills:start -->\nsuperpowers:writing-plans stuff.\n<!-- skills:end -->\nDo the task.",
	}

	if err := c.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	path := filepath.Join(tmpDir, "AGENTS.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "<!-- rocket:start -->") || !strings.Contains(s, "<!-- rocket:end -->") {
		t.Errorf("AGENTS.md missing rocket markers: %q", s)
	}
	if !strings.Contains(s, "Be a helpful worker.") {
		t.Errorf("AGENTS.md missing stripped prompt content: %q", s)
	}
	if strings.Contains(s, "superpowers:") {
		t.Errorf("AGENTS.md contains un-stripped skills content: %q", s)
	}

	// No temp files left behind.
	assertNoTempFiles(t, tmpDir)
}

func TestSetupWorkspaceEmptySystemPromptNoAgentsMD(t *testing.T) {
	withCodexHome(t)
	tmpDir := t.TempDir()

	c := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}

	if err := c.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.md should not be created when SystemPrompt is empty (err=%v)", err)
	}
}

func TestSetupWorkspacePrependsToExistingAgentsMD(t *testing.T) {
	withCodexHome(t)
	tmpDir := t.TempDir()

	existing := "# Project notes\n\nSome pre-existing human-written content.\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte(existing), 0o644); err != nil {
		t.Fatalf("seed AGENTS.md: %v", err)
	}

	c := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir, SystemPrompt: "Rocket prompt content."}
	if err := c.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "Rocket prompt content.") {
		t.Errorf("missing new rocket block content: %q", s)
	}
	if !strings.Contains(s, "Some pre-existing human-written content.") {
		t.Errorf("existing content was dropped: %q", s)
	}
	if strings.Index(s, "<!-- rocket:start -->") > strings.Index(s, "# Project notes") {
		t.Errorf("rocket block should be prepended before existing content: %q", s)
	}
}

func TestSetupWorkspaceReplacesExistingRocketBlockIdempotently(t *testing.T) {
	withCodexHome(t)
	tmpDir := t.TempDir()

	c := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir, SystemPrompt: "First prompt."}
	if err := c.SetupWorkspace(spec); err != nil {
		t.Fatalf("first SetupWorkspace failed: %v", err)
	}

	// Simulate a human adding their own content around our block.
	path := filepath.Join(tmpDir, "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	augmented := string(data) + "\n## Human section\n\nDon't touch this.\n"
	if err := os.WriteFile(path, []byte(augmented), 0o644); err != nil {
		t.Fatalf("write augmented AGENTS.md: %v", err)
	}

	spec.SystemPrompt = "Second prompt, replacing the first."
	if err := c.SetupWorkspace(spec); err != nil {
		t.Fatalf("second SetupWorkspace failed: %v", err)
	}

	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	s := string(final)

	if strings.Contains(s, "First prompt.") {
		t.Errorf("old rocket block content should have been replaced: %q", s)
	}
	if !strings.Contains(s, "Second prompt, replacing the first.") {
		t.Errorf("new rocket block content missing: %q", s)
	}
	if !strings.Contains(s, "## Human section") || !strings.Contains(s, "Don't touch this.") {
		t.Errorf("human-added section outside the block should survive: %q", s)
	}
	if strings.Count(s, "<!-- rocket:start -->") != 1 {
		t.Errorf("expected exactly one rocket block, got content: %q", s)
	}

	// Re-run once more with the same prompt: should be a stable fixed point.
	if err := c.SetupWorkspace(spec); err != nil {
		t.Fatalf("third SetupWorkspace failed: %v", err)
	}
	final2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(final2) != s {
		t.Errorf("SetupWorkspace not idempotent on repeated identical calls:\nfirst:  %q\nsecond: %q", s, string(final2))
	}

	assertNoTempFiles(t, tmpDir)
}

func TestSeedCodexTrustWritesConfigToml(t *testing.T) {
	home := withCodexHome(t)
	tmpDir := t.TempDir()

	c := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}
	if err := c.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	configPath := filepath.Join(home, "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	s := string(data)

	resolved := tmpDir
	if r, err := filepath.EvalSymlinks(tmpDir); err == nil {
		resolved = r
	}

	wantHeader := "[projects." + `"` + resolved + `"` + "]"
	if !strings.Contains(s, wantHeader) {
		t.Errorf("config.toml missing project trust header %q, got: %q", wantHeader, s)
	}
	if !strings.Contains(s, `trust_level = "trusted"`) {
		t.Errorf("config.toml missing trust_level=trusted, got: %q", s)
	}
}

func TestSeedCodexTrustPreservesExistingConfigAndIsIdempotent(t *testing.T) {
	home := withCodexHome(t)
	tmpDir := t.TempDir()

	preexisting := "[marketplaces.foo]\nsource = \"bar\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(preexisting), 0o644); err != nil {
		t.Fatalf("seed config.toml: %v", err)
	}

	c := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}
	if err := c.SetupWorkspace(spec); err != nil {
		t.Fatalf("first SetupWorkspace failed: %v", err)
	}
	if err := c.SetupWorkspace(spec); err != nil {
		t.Fatalf("second SetupWorkspace failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	s := string(data)

	if !strings.Contains(s, "[marketplaces.foo]") {
		t.Errorf("pre-existing config content was dropped: %q", s)
	}

	resolved := tmpDir
	if r, err := filepath.EvalSymlinks(tmpDir); err == nil {
		resolved = r
	}
	wantHeader := "[projects." + `"` + resolved + `"` + "]"
	if n := strings.Count(s, wantHeader); n != 1 {
		t.Errorf("expected exactly one trust table for %s, found %d in: %q", tmpDir, n, s)
	}

	assertNoTempFiles(t, home)
}

func TestSeedCodexTrustConcurrent(t *testing.T) {
	home := withCodexHome(t)

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := New()
			spec := agent.LaunchSpec{WorktreePath: filepath.Join(os.TempDir(), "wt-concurrent")}
			_ = i
			if err := c.SetupWorkspace(spec); err != nil {
				t.Errorf("SetupWorkspace failed: %v", err)
			}
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	s := string(data)

	resolved := filepath.Join(os.TempDir(), "wt-concurrent")
	if r, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = r
	}
	wantHeader := "[projects." + `"` + resolved + `"` + "]"
	if got := strings.Count(s, wantHeader); got != 1 {
		t.Errorf("expected exactly one trust table after concurrent SetupWorkspace calls, got %d in: %q", got, s)
	}
}

// assertNoTempFiles fails the test if any rocket atomic-write temp files
// (".rocket-tmp-*") were left behind in dir.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".rocket-tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}
