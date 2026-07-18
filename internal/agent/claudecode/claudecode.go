package claudecode

import (
	"encoding/json"
	"fmt"
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

// SetupWorkspace prepares the worktree for a Claude Code launch: it writes
// the push-channel activity hook script, wires it into .claude/settings.json
// hooks, and writes the system prompt to .rocket-prompt.md if provided.
func (c *ClaudeCode) SetupWorkspace(spec agent.LaunchSpec) error {
	if err := writeActivityHookScript(spec.WorktreePath); err != nil {
		return fmt.Errorf("write activity hook script: %w", err)
	}
	if err := upsertClaudeSettings(spec.WorktreePath); err != nil {
		return fmt.Errorf("upsert claude settings: %w", err)
	}

	if spec.SystemPrompt == "" {
		return nil
	}

	promptPath := filepath.Join(spec.WorktreePath, ".rocket-prompt.md")
	return os.WriteFile(promptPath, []byte(spec.SystemPrompt), 0o600)
}

// activityHookRelPath is the hook script's path relative to the worktree
// root. Claude Code hooks run with cwd set to the project directory, so
// commands reference it relatively.
const activityHookRelPath = ".rocket/activity-hook.sh"

// activityHookScript is a POSIX shell script that reports an activity state
// to rocket's push endpoint over the unix socket. It never fails the
// invoking hook: missing env vars or an unreachable daemon are silently
// ignored (`|| true`, output discarded).
const activityHookScript = `#!/bin/sh
# rocket activity push hook. Invoked by Claude Code as:
#   ` + activityHookRelPath + ` <state>
# Reports the given state for this session to the rocket daemon over its
# unix socket. Never blocks or fails the agent: if the env vars needed to
# reach the daemon aren't set, or the daemon is unreachable, this exits 0.
if [ -z "$ROCKET_SOCKET" ] || [ -z "$ROCKET_SESSION_ID" ]; then
  exit 0
fi

curl -s --unix-socket "$ROCKET_SOCKET" \
  -X POST -H 'Content-Type: application/json' \
  -d "{\"session\":\"$ROCKET_SESSION_ID\",\"state\":\"$1\",\"ts\":$(date +%s)}" \
  http://rocket/v1/internal/activity >/dev/null 2>&1 || true
`

// writeActivityHookScript writes the push-channel hook script to
// <worktreePath>/.rocket/activity-hook.sh, creating the .rocket directory
// (0755) if needed. The script is written 0700 (owner rwx only).
func writeActivityHookScript(worktreePath string) error {
	dir := filepath.Join(worktreePath, ".rocket")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "activity-hook.sh")
	return os.WriteFile(path, []byte(activityHookScript), 0o700)
}

// activityHookEvents maps the Claude Code hook events rocket wires up to the
// activity.State the hook script should report when that event fires.
var activityHookEvents = map[string]string{
	"PreToolUse":   "active",
	"PostToolUse":  "active",
	"Stop":         "ready",
	"Notification": "waiting_input",
	"SessionEnd":   "exited",
}

// hookMatcherFor returns the "matcher" value used for event's wired hook
// entry. Tool-use events match every tool; lifecycle events (Stop,
// Notification, SessionEnd) have no matcher concept and use "".
func hookMatcherFor(event string) string {
	switch event {
	case "PreToolUse", "PostToolUse":
		return "*"
	default:
		return ""
	}
}

// activityHookCommand builds the shell command rocket wires into
// settings.json hooks for the given state. It uses the project-relative
// form ("sh .rocket/activity-hook.sh <state>") since Claude Code runs hook
// commands with cwd set to the project root.
func activityHookCommand(state string) string {
	return "sh " + activityHookRelPath + " " + state
}

// hookCommand is the {"type":"command","command":"..."} shape of a single
// hook within a settings.json hooks[event][].hooks array.
type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hookMatcherGroup is the {"matcher":"...","hooks":[...]} shape of a single
// entry within settings.json hooks[event].
type hookMatcherGroup struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

// upsertClaudeSettings idempotently merges rocket's activity-hook wiring
// into <worktreePath>/.claude/settings.json. It preserves any existing
// content: unknown top-level keys are round-tripped untouched, and existing
// hook entries for other events/matchers are kept. Re-running against a
// file that already has our entries is a no-op (no duplicates).
func upsertClaudeSettings(worktreePath string) error {
	dir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "settings.json")

	settings := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if len(data) > 0 {
			if err := json.Unmarshal(data, &settings); err != nil {
				return fmt.Errorf("parse existing %s: %w", path, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	hooks := map[string][]hookMatcherGroup{}
	if raw, ok := settings["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return fmt.Errorf("parse existing hooks in %s: %w", path, err)
		}
	}

	for event, state := range activityHookEvents {
		matcher := hookMatcherFor(event)
		cmd := activityHookCommand(state)

		groups := hooks[event]

		alreadyPresent := false
		groupIdx := -1
		for i, g := range groups {
			if g.Matcher != matcher {
				continue
			}
			groupIdx = i
			for _, h := range g.Hooks {
				if h.Type == "command" && h.Command == cmd {
					alreadyPresent = true
					break
				}
			}
			break
		}
		if alreadyPresent {
			continue
		}

		entry := hookCommand{Type: "command", Command: cmd}
		if groupIdx >= 0 {
			groups[groupIdx].Hooks = append(groups[groupIdx].Hooks, entry)
		} else {
			groups = append(groups, hookMatcherGroup{Matcher: matcher, Hooks: []hookCommand{entry}})
		}
		hooks[event] = groups
	}

	hooksRaw, err := json.Marshal(hooks)
	if err != nil {
		return err
	}
	settings["hooks"] = hooksRaw

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
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
