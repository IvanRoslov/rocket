package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func TestLaunchCommandMinimal(t *testing.T) {
	cc := New()
	spec := agent.LaunchSpec{
		SessionID:      "sess-123",
		Kind:           "feature",
		ParentID:       "parent-456",
		ProjectID:      "proj-789",
		RepoID:         "repo-abc",
		Feature:        "feature-xyz",
		WorktreePath:   "/tmp/wt",
		SocketPath:     "/tmp/socket.sock",
		SystemPrompt:   "",
		FirstMessage:   "",
		Model:          "",
		PermissionMode: "",
	}

	cmd := cc.LaunchCommand(spec)
	if len(cmd) != 2 {
		t.Errorf("expected 2 args, got %d: %v", len(cmd), cmd)
	}
	if cmd[0] != "claude" || cmd[1] != "--dangerously-skip-permissions" {
		t.Errorf("expected [claude --dangerously-skip-permissions], got %v", cmd)
	}
}

func TestLaunchCommandFull(t *testing.T) {
	cc := New()
	spec := agent.LaunchSpec{
		SessionID:      "sess-123",
		Kind:           "feature",
		ParentID:       "parent-456",
		ProjectID:      "proj-789",
		RepoID:         "repo-abc",
		Feature:        "feature-xyz",
		WorktreePath:   "/tmp/wt",
		SocketPath:     "/tmp/socket.sock",
		SystemPrompt:   "You are a code assistant.",
		FirstMessage:   "Help me write a function.",
		Model:          "claude-opus",
		PermissionMode: "strict",
	}

	cmd := cc.LaunchCommand(spec)

	// Expected: ["claude", "--dangerously-skip-permissions", "--append-system-prompt", "You are a code assistant.", "--model", "claude-opus", "--permission-mode", "strict", "--", "Help me write a function."]
	expectedLen := 10
	if len(cmd) != expectedLen {
		t.Errorf("expected %d args, got %d: %v", expectedLen, len(cmd), cmd)
	}

	if cmd[0] != "claude" || cmd[1] != "--dangerously-skip-permissions" {
		t.Errorf("first two args wrong: %v", cmd[:2])
	}

	if cmd[2] != "--append-system-prompt" || cmd[3] != "You are a code assistant." {
		t.Errorf("system prompt args wrong: [%s, %s]", cmd[2], cmd[3])
	}

	if cmd[4] != "--model" || cmd[5] != "claude-opus" {
		t.Errorf("model args wrong: [%s, %s]", cmd[4], cmd[5])
	}

	if cmd[6] != "--permission-mode" || cmd[7] != "strict" {
		t.Errorf("permission-mode args wrong: [%s, %s]", cmd[6], cmd[7])
	}

	if cmd[8] != "--" || cmd[9] != "Help me write a function." {
		t.Errorf("first message args wrong: [%s, %s]", cmd[8], cmd[9])
	}
}

func TestEnvCompleteness(t *testing.T) {
	cc := New()
	spec := agent.LaunchSpec{
		SessionID:      "sess-123",
		Kind:           "feature",
		ParentID:       "parent-456",
		ProjectID:      "proj-789",
		RepoID:         "repo-abc",
		Feature:        "feature-xyz",
		WorktreePath:   "/tmp/wt",
		SocketPath:     "/tmp/socket.sock",
		SystemPrompt:   "prompt",
		FirstMessage:   "msg",
		Model:          "model",
		PermissionMode: "mode",
	}

	env := cc.Env(spec)

	tests := map[string]string{
		"CLAUDECODE":        "",
		"ROCKET_SESSION_ID": "sess-123",
		"ROCKET_KIND":       "feature",
		"ROCKET_PARENT_ID":  "parent-456",
		"ROCKET_PROJECT_ID": "proj-789",
		"ROCKET_REPO_ID":    "repo-abc",
		"ROCKET_FEATURE":    "feature-xyz",
		"ROCKET_SOCKET":     "/tmp/socket.sock",
	}

	for key, expectedVal := range tests {
		val, ok := env[key]
		if !ok {
			t.Errorf("missing env var: %s", key)
		}
		if val != expectedVal {
			t.Errorf("env[%s] = %q, expected %q", key, val, expectedVal)
		}
	}
}

func TestSetupWorkspaceWritesPromptFile(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{
		WorktreePath: tmpDir,
		SystemPrompt: "Test system prompt content.",
	}

	err := cc.SetupWorkspace(spec)
	if err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	// Check file exists
	promptPath := filepath.Join(tmpDir, ".rocket-prompt.md")
	content, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("failed to read prompt file: %v", err)
	}

	if string(content) != "Test system prompt content." {
		t.Errorf("prompt content mismatch: got %q", string(content))
	}

	// Check permissions (0600)
	info, err := os.Stat(promptPath)
	if err != nil {
		t.Fatalf("failed to stat prompt file: %v", err)
	}

	mode := info.Mode().Perm()
	expectedMode := os.FileMode(0o600)
	if mode != expectedMode {
		t.Errorf("prompt file permissions: got %o, expected %o", mode, expectedMode)
	}
}

func TestSetupWorkspaceIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{
		WorktreePath: tmpDir,
		SystemPrompt: "First prompt.",
	}

	err := cc.SetupWorkspace(spec)
	if err != nil {
		t.Fatalf("first SetupWorkspace failed: %v", err)
	}

	// Call again with different prompt
	spec.SystemPrompt = "Second prompt."
	err = cc.SetupWorkspace(spec)
	if err != nil {
		t.Fatalf("second SetupWorkspace failed: %v", err)
	}

	// File should have new content
	promptPath := filepath.Join(tmpDir, ".rocket-prompt.md")
	content, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("failed to read prompt file: %v", err)
	}

	if string(content) != "Second prompt." {
		t.Errorf("prompt content not overwritten: got %q", string(content))
	}
}

func TestSetupWorkspaceEmptyPrompt(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{
		WorktreePath: tmpDir,
		SystemPrompt: "",
	}

	err := cc.SetupWorkspace(spec)
	if err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	// File should not exist
	promptPath := filepath.Join(tmpDir, ".rocket-prompt.md")
	_, err = os.Stat(promptPath)
	if err == nil {
		t.Errorf("prompt file should not exist when SystemPrompt is empty")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetupWorkspaceWritesActivityHookScript(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}

	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	scriptPath := filepath.Join(tmpDir, ".rocket", "activity-hook.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat hook script: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("hook script mode = %o, want 0700", mode)
	}

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read hook script: %v", err)
	}
	if !strings.HasPrefix(string(content), "#!/bin/sh") {
		t.Errorf("hook script does not start with shebang: %q", string(content))
	}
	if !strings.Contains(string(content), "ROCKET_SOCKET") || !strings.Contains(string(content), "ROCKET_SESSION_ID") {
		t.Errorf("hook script missing expected env var references: %q", string(content))
	}
	if !strings.Contains(string(content), "/v1/internal/activity") {
		t.Errorf("hook script missing internal activity endpoint: %q", string(content))
	}
}

// TestSetupWorkspaceWritesQuizHookScript asserts that SetupWorkspace writes
// an executable .rocket/quiz-hook.sh with the expected shebang, env var
// references, and internal quiz endpoint.
func TestSetupWorkspaceWritesQuizHookScript(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}

	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	scriptPath := filepath.Join(tmpDir, ".rocket", "quiz-hook.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat quiz hook script: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("quiz hook script mode = %o, want 0700", mode)
	}

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read quiz hook script: %v", err)
	}
	if !strings.HasPrefix(string(content), "#!/bin/sh") {
		t.Errorf("quiz hook script does not start with shebang: %q", string(content))
	}
	if !strings.Contains(string(content), "ROCKET_SOCKET") || !strings.Contains(string(content), "ROCKET_SESSION_ID") {
		t.Errorf("quiz hook script missing expected env var references: %q", string(content))
	}
	if !strings.Contains(string(content), "/v1/internal/quiz") {
		t.Errorf("quiz hook script missing internal quiz endpoint: %q", string(content))
	}
}

// TestSetupWorkspaceWiresQuizHookMatchers asserts that settings.local.json
// after SetupWorkspace contains both the activity hook ("*" matcher) and
// the quiz hook ("AskUserQuestion" matcher) for PreToolUse and PostToolUse.
func TestSetupWorkspaceWiresQuizHookMatchers(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}
	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	settings := readSettings(t, tmpDir)
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("settings.json missing hooks object: %+v", settings)
	}

	wantEventPhase := map[string]string{
		"PreToolUse":  "pending",
		"PostToolUse": "resolved",
	}
	for event, phase := range wantEventPhase {
		groupsRaw, ok := hooks[event]
		if !ok {
			t.Fatalf("hooks missing event %q: %+v", event, hooks)
		}
		groups, ok := groupsRaw.([]any)
		if !ok {
			t.Fatalf("hooks[%s] is not an array: %+v", event, groupsRaw)
		}

		var found bool
		want := "sh .rocket/quiz-hook.sh " + phase
		for _, raw := range groups {
			group, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			matcher, _ := group["matcher"].(string)
			if matcher != "AskUserQuestion" {
				continue
			}
			hookList, _ := group["hooks"].([]any)
			for _, hRaw := range hookList {
				h, ok := hRaw.(map[string]any)
				if !ok {
					continue
				}
				if cmd, _ := h["command"].(string); cmd == want {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("hooks[%s] missing AskUserQuestion matcher with command %q: %+v", event, want, groups)
		}

		// The activity hook's "*" matcher must still be present alongside
		// the quiz hook's "AskUserQuestion" matcher (both matcher-hooks
		// coexist per the task brief).
		var foundActivity bool
		for _, raw := range groups {
			group, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if matcher, _ := group["matcher"].(string); matcher == "*" {
				foundActivity = true
			}
		}
		if !foundActivity {
			t.Errorf("hooks[%s] missing activity hook's \"*\" matcher: %+v", event, groups)
		}
	}
}

func TestSetupWorkspaceWritesFreshSettingsJSON(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}

	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	settings := readSettings(t, tmpDir)
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("settings.json missing hooks object: %+v", settings)
	}

	wantEventState := map[string]string{
		"PreToolUse":   "active",
		"PostToolUse":  "active",
		"Stop":         "ready",
		"Notification": "waiting_input",
		"SessionEnd":   "exited",
	}
	for event, state := range wantEventState {
		cmd := findHookCommand(t, hooks, event)
		want := "sh .rocket/activity-hook.sh " + state
		if cmd != want {
			t.Errorf("hooks[%s] command = %q, want %q", event, cmd, want)
		}
	}
}

func TestSetupWorkspacePreservesForeignSettingsAndAppendsHooks(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}

	existing := `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{"type": "command", "command": "echo custom"}]
      }
    ]
  }
}`
	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if err := os.WriteFile(settingsPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("write existing settings.local.json: %v", err)
	}

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}
	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	settings := readSettings(t, tmpDir)
	if settings["model"] != "opus" {
		t.Errorf("model = %v, want opus (foreign key preserved)", settings["model"])
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("settings.json missing hooks object: %+v", settings)
	}

	preToolUse, ok := hooks["PreToolUse"].([]any)
	if !ok {
		t.Fatalf("hooks.PreToolUse is not an array: %+v", hooks["PreToolUse"])
	}

	var foundCustom, foundOurs bool
	for _, raw := range preToolUse {
		group, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		matcher, _ := group["matcher"].(string)
		hookList, _ := group["hooks"].([]any)
		for _, hRaw := range hookList {
			h, ok := hRaw.(map[string]any)
			if !ok {
				continue
			}
			cmd, _ := h["command"].(string)
			if matcher == "Bash" && cmd == "echo custom" {
				foundCustom = true
			}
			if matcher == "*" && cmd == "sh .rocket/activity-hook.sh active" {
				foundOurs = true
			}
		}
	}
	if !foundCustom {
		t.Errorf("existing custom PreToolUse hook was dropped: %+v", preToolUse)
	}
	if !foundOurs {
		t.Errorf("our PreToolUse hook was not appended: %+v", preToolUse)
	}
}

func TestSetupWorkspaceHooksIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}

	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("first SetupWorkspace failed: %v", err)
	}
	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("second SetupWorkspace failed: %v", err)
	}

	settings := readSettings(t, tmpDir)
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("settings.json missing hooks object: %+v", settings)
	}

	// PreToolUse now carries two matcher groups: "*" (activity hook) and
	// "AskUserQuestion" (quiz hook). Running SetupWorkspace twice must not
	// duplicate either group or the command within it.
	preToolUse, ok := hooks["PreToolUse"].([]any)
	if !ok {
		t.Fatalf("hooks.PreToolUse is not an array: %+v", hooks["PreToolUse"])
	}
	if len(preToolUse) != 2 {
		t.Fatalf("len(hooks.PreToolUse) = %d, want 2 (no duplicate groups): %+v", len(preToolUse), preToolUse)
	}
	seenMatchers := map[string]bool{}
	for _, raw := range preToolUse {
		group, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("hooks.PreToolUse entry is not an object: %+v", raw)
		}
		matcher, _ := group["matcher"].(string)
		if seenMatchers[matcher] {
			t.Fatalf("duplicate matcher group %q in hooks.PreToolUse: %+v", matcher, preToolUse)
		}
		seenMatchers[matcher] = true

		hookList, ok := group["hooks"].([]any)
		if !ok || len(hookList) != 1 {
			t.Fatalf("hooks.PreToolUse matcher %q has %d hooks, want 1 (no duplicate command): %+v", matcher, len(hookList), group["hooks"])
		}
	}
	if !seenMatchers["*"] || !seenMatchers["AskUserQuestion"] {
		t.Fatalf("hooks.PreToolUse missing expected matchers, got: %+v", seenMatchers)
	}
}

// readSettings reads and JSON-decodes
// <worktreePath>/.claude/settings.local.json.
func readSettings(t *testing.T, worktreePath string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktreePath, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("decode settings.local.json: %v", err)
	}
	return settings
}

// findHookCommand returns the single command string wired for event in
// hooks (as decoded from settings.json), failing the test if it's not
// found in exactly the expected shape.
func findHookCommand(t *testing.T, hooks map[string]any, event string) string {
	t.Helper()
	groupsRaw, ok := hooks[event]
	if !ok {
		t.Fatalf("hooks missing event %q: %+v", event, hooks)
	}
	groups, ok := groupsRaw.([]any)
	if !ok || len(groups) == 0 {
		t.Fatalf("hooks[%s] is not a non-empty array: %+v", event, groupsRaw)
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("hooks[%s][0] is not an object: %+v", event, groups[0])
	}
	hookList, ok := group["hooks"].([]any)
	if !ok || len(hookList) == 0 {
		t.Fatalf("hooks[%s][0].hooks is not a non-empty array: %+v", event, group["hooks"])
	}
	h, ok := hookList[0].(map[string]any)
	if !ok {
		t.Fatalf("hooks[%s][0].hooks[0] is not an object: %+v", event, hookList[0])
	}
	cmd, _ := h["command"].(string)
	return cmd
}

// TestSetupWorkspaceDoesNotCreateSettingsJSON asserts that a fresh
// SetupWorkspace call never creates .claude/settings.json — rocket only
// ever writes hooks into settings.local.json, so a worker's PR never gets
// a settings.json diff.
func TestSetupWorkspaceDoesNotCreateSettingsJSON(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}
	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Errorf(".claude/settings.json should not exist after SetupWorkspace (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, ".claude", "settings.local.json")); err != nil {
		t.Errorf(".claude/settings.local.json should exist after SetupWorkspace: %v", err)
	}
}

// initGitRepo runs `git init` in dir so it behaves like a real worktree for
// git-plumbing-based tests.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
}

// excludeLines reads <worktreePath>/.git/info/exclude and returns its
// trimmed, non-empty lines.
func excludeLines(t *testing.T, worktreePath string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(worktreePath, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read info/exclude: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	return lines
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

// TestSetupWorkspaceExcludesRocketPlumbingFromGit runs SetupWorkspace
// against a real git repo (via `git init`) and checks that all four rocket
// plumbing paths land in .git/info/exclude exactly once, including after a
// second, idempotent run.
func TestSetupWorkspaceExcludesRocketPlumbingFromGit(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir, SystemPrompt: "hello"}
	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("first SetupWorkspace failed: %v", err)
	}

	want := []string{".claude/settings.local.json", ".rocket/", ".rocket-prompt.md", ".rocket-launch.sh"}
	lines := excludeLines(t, tmpDir)
	for _, w := range want {
		count := 0
		for _, l := range lines {
			if l == w {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected exactly one %q entry in info/exclude, got %d: %v", w, count, lines)
		}
	}

	// Second run: idempotent, no duplicate entries.
	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("second SetupWorkspace failed: %v", err)
	}
	lines2 := excludeLines(t, tmpDir)
	for _, w := range want {
		count := 0
		for _, l := range lines2 {
			if l == w {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected exactly one %q entry after second run, got %d: %v", w, count, lines2)
		}
	}
}

// TestSetupWorkspaceGitExcludeSkipsTrackedPath seeds a tracked
// .rocket-launch.sh (an unrealistic setup, but it exercises the
// tracked-path skip path of agent.ExcludeFromGit) and verifies it is not
// added to info/exclude, while the other rocket plumbing paths still are.
func TestSetupWorkspaceGitExcludeSkipsTrackedPath(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	trackedPath := filepath.Join(tmpDir, ".rocket-launch.sh")
	if err := os.WriteFile(trackedPath, []byte("#!/bin/sh\necho tracked\n"), 0o755); err != nil {
		t.Fatalf("seed .rocket-launch.sh: %v", err)
	}
	addCmd := exec.Command("git", "add", ".rocket-launch.sh")
	addCmd.Dir = tmpDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir, SystemPrompt: "hello"}
	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	lines := excludeLines(t, tmpDir)
	if containsLine(lines, ".rocket-launch.sh") {
		t.Errorf("tracked .rocket-launch.sh should not be added to info/exclude: %v", lines)
	}
	for _, w := range []string{".claude/settings.local.json", ".rocket/", ".rocket-prompt.md"} {
		if !containsLine(lines, w) {
			t.Errorf("expected %q in info/exclude: %v", w, lines)
		}
	}
}

// TestSetupWorkspaceNonGitDirNoError asserts that SetupWorkspace still
// succeeds (git-exclude plumbing is best-effort and silently skipped) when
// the worktree isn't a git repository at all.
func TestSetupWorkspaceNonGitDirNoError(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir, SystemPrompt: "hello"}
	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace should succeed in a non-git dir, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, ".git")); !os.IsNotExist(err) {
		t.Errorf("no .git directory should have been created (err=%v)", err)
	}
}

func TestName(t *testing.T) {
	cc := New()
	if cc.Name() != "claude-code" {
		t.Errorf("Name() = %q, expected claude-code", cc.Name())
	}
}

func TestRegistryGetKnown(t *testing.T) {
	ag, err := agent.Get("claude-code")
	if err != nil {
		t.Fatalf("Get(claude-code) failed: %v", err)
	}
	if ag.Name() != "claude-code" {
		t.Errorf("got agent with Name() = %q", ag.Name())
	}
}

func TestRegistryGetUnknown(t *testing.T) {
	_, err := agent.Get("unknown-agent")
	if err == nil {
		t.Errorf("Get(unknown-agent) should return error, got nil")
	}
}

func TestAvailable(t *testing.T) {
	cc := New()
	// claude command should be available on most systems
	err := cc.Available()
	if err != nil {
		t.Logf("claude command not available (ok for testing): %v", err)
	}
}

func TestSetupWorkspaceTrustsWorktreeInClaudeJSON(t *testing.T) {
	tmpDir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}
	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	resolved, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(fakeHome, ".claude.json"))
	if err != nil {
		t.Fatalf("read ~/.claude.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode ~/.claude.json: %v", err)
	}
	projects, ok := doc["projects"].(map[string]any)
	if !ok {
		t.Fatalf("~/.claude.json missing projects object: %+v", doc)
	}
	entry, ok := projects[resolved].(map[string]any)
	if !ok {
		t.Fatalf("projects missing entry for %q: %+v", resolved, projects)
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Errorf("hasTrustDialogAccepted = %v, want true", entry["hasTrustDialogAccepted"])
	}
}

func TestSetupWorkspaceTrustPreservesExistingClaudeJSON(t *testing.T) {
	tmpDir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	existing := `{
  "numStartups": 42,
  "projects": {
    "/some/other/path": {"hasTrustDialogAccepted": true, "allowedTools": ["Bash"]}
  }
}`
	if err := os.WriteFile(filepath.Join(fakeHome, ".claude.json"), []byte(existing), 0o644); err != nil {
		t.Fatalf("write existing ~/.claude.json: %v", err)
	}

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}
	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(fakeHome, ".claude.json"))
	if err != nil {
		t.Fatalf("read ~/.claude.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode ~/.claude.json: %v", err)
	}
	if doc["numStartups"] != float64(42) {
		t.Errorf("numStartups = %v, want 42 (foreign key preserved)", doc["numStartups"])
	}
	projects := doc["projects"].(map[string]any)
	other := projects["/some/other/path"].(map[string]any)
	if tools, _ := other["allowedTools"].([]any); len(tools) != 1 || tools[0] != "Bash" {
		t.Errorf("existing project entry was clobbered: %+v", other)
	}
}

// TestTrustWorktreeConcurrentCallsAllEntriesPresent races 10 concurrent
// trustWorktree calls, each for a distinct worktree, against the same
// ~/.claude.json. Without a lock serializing the read-modify-write, later
// writers can clobber earlier ones' project entries entirely (last-writer-
// wins on the whole file, not just the one project key). With the flock
// guard, every entry must survive.
func TestTrustWorktreeConcurrentCallsAllEntriesPresent(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	const n = 10
	worktrees := make([]string, n)
	for i := 0; i < n; i++ {
		wt := filepath.Join(t.TempDir(), fmt.Sprintf("wt-%d", i))
		if err := os.MkdirAll(wt, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		worktrees[i] = wt
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i, wt := range worktrees {
		wg.Add(1)
		go func(i int, wt string) {
			defer wg.Done()
			errs[i] = trustWorktree(wt)
		}(i, wt)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("trustWorktree(%d) failed: %v", i, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(fakeHome, ".claude.json"))
	if err != nil {
		t.Fatalf("read ~/.claude.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode ~/.claude.json: %v", err)
	}
	projects, ok := doc["projects"].(map[string]any)
	if !ok {
		t.Fatalf("~/.claude.json missing projects object: %+v", doc)
	}

	for _, wt := range worktrees {
		resolved, err := filepath.EvalSymlinks(wt)
		if err != nil {
			t.Fatalf("EvalSymlinks(%s): %v", wt, err)
		}
		entry, ok := projects[resolved].(map[string]any)
		if !ok {
			t.Errorf("projects missing entry for %q (lost to a concurrent write race)", resolved)
			continue
		}
		if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
			t.Errorf("entry for %q: hasTrustDialogAccepted = %v, want true", resolved, entry["hasTrustDialogAccepted"])
		}
	}
}

func TestSetupWorkspaceNoLeftoverTempFiles(t *testing.T) {
	tmpDir := t.TempDir()

	cc := New()
	spec := agent.LaunchSpec{WorktreePath: tmpDir}

	if err := cc.SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace failed: %v", err)
	}

	claudeDir := filepath.Join(tmpDir, ".claude")
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		t.Fatalf("read .claude directory: %v", err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".settings-") && strings.HasSuffix(entry.Name(), ".json") {
			t.Errorf("leftover temp file in .claude: %s", entry.Name())
		}
	}
}
