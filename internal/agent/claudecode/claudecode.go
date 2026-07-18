package claudecode

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/IvanRoslov/rocket/internal/agent"
)

type ClaudeCode struct{}

// New creates a new claudecode agent.
func New() agent.Agent {
	return &ClaudeCode{}
}

func (c *ClaudeCode) Name() string {
	return "claude-code"
}

// Available checks if the claude command is available in PATH.
func (c *ClaudeCode) Available() error {
	_, err := exec.LookPath("claude")
	return err
}

// SetupWorkspace writes the system prompt to .rocket-prompt.md if provided.
func (c *ClaudeCode) SetupWorkspace(spec agent.LaunchSpec) error {
	if spec.SystemPrompt == "" {
		return nil
	}

	promptPath := filepath.Join(spec.WorktreePath, ".rocket-prompt.md")
	return os.WriteFile(promptPath, []byte(spec.SystemPrompt), 0o600)
}

// LaunchCommand builds the command to launch claude with the given spec.
func (c *ClaudeCode) LaunchCommand(spec agent.LaunchSpec) []string {
	cmd := []string{"claude", "--dangerously-skip-permissions"}

	if spec.SystemPrompt != "" {
		cmd = append(cmd, "--append-system-prompt", spec.SystemPrompt)
	}

	if spec.Model != "" {
		cmd = append(cmd, "--model", spec.Model)
	}

	if spec.PermissionMode != "" {
		cmd = append(cmd, "--permission-mode", spec.PermissionMode)
	}

	if spec.FirstMessage != "" {
		cmd = append(cmd, "--", spec.FirstMessage)
	}

	return cmd
}

// Env returns environment variables for the agent.
func (c *ClaudeCode) Env(spec agent.LaunchSpec) map[string]string {
	return map[string]string{
		"CLAUDECODE":        "",
		"ROCKET_SESSION_ID": spec.SessionID,
		"ROCKET_KIND":       spec.Kind,
		"ROCKET_PARENT_ID":  spec.ParentID,
		"ROCKET_PROJECT_ID": spec.ProjectID,
		"ROCKET_REPO_ID":    spec.RepoID,
		"ROCKET_FEATURE":    spec.Feature,
		"ROCKET_SOCKET":     spec.SocketPath,
	}
}

func init() {
	agent.Register("claude-code", New)
}
