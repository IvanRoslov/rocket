package claudecode

import (
	"os"
	"path/filepath"
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
