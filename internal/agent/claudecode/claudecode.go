package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

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
// the push-channel activity hook script, wires it into
// .claude/settings.local.json hooks, writes the system prompt to
// .rocket-prompt.md if provided, and excludes rocket's own plumbing files
// from the worktree's git status.
func (c *ClaudeCode) SetupWorkspace(spec agent.LaunchSpec) error {
	if err := writeActivityHookScript(spec.WorktreePath); err != nil {
		return fmt.Errorf("write activity hook script: %w", err)
	}
	if err := writeQuizHookScript(spec.WorktreePath); err != nil {
		return fmt.Errorf("write quiz hook script: %w", err)
	}
	if err := upsertClaudeSettings(spec.WorktreePath); err != nil {
		return fmt.Errorf("upsert claude settings: %w", err)
	}
	if err := trustWorktree(spec.WorktreePath); err != nil {
		return fmt.Errorf("trust worktree: %w", err)
	}

	if spec.SystemPrompt != "" {
		promptPath := filepath.Join(spec.WorktreePath, ".rocket-prompt.md")
		if err := os.WriteFile(promptPath, []byte(spec.SystemPrompt), 0o600); err != nil {
			return err
		}
	}

	// Keep rocket's own harness plumbing out of the worker's git status: a
	// worker running `git status`/`git diff` (or a reviewer looking at its
	// PR) should never see .claude/settings.local.json, the .rocket/
	// directory, or the launch/prompt scaffolding files rocket writes.
	// Best-effort and a no-op for already-tracked paths or non-git dirs.
	agent.ExcludeFromGit(spec.WorktreePath, []string{
		".claude/settings.local.json",
		".rocket/",
		".rocket-prompt.md",
		".rocket-launch.sh",
	})

	return nil
}

// activityHookRelPath is the hook script's path relative to the worktree
// root. Claude Code hooks run with cwd set to the project directory, so
// commands reference it relatively.
const activityHookRelPath = ".rocket/activity-hook.sh"

// activityHookScript is a POSIX shell script that reports an activity state
// to rocket's push endpoint over the unix socket. It never fails the
// invoking hook: missing env vars or an unreachable daemon are silently
// ignored (`|| true`, output discarded). Curl includes timeouts to prevent
// the daemon hook from hanging.
const activityHookScript = `#!/bin/sh
# rocket activity push hook. Invoked by Claude Code as:
#   ` + activityHookRelPath + ` <state>
# Reports the given state for this session to the rocket daemon over its
# unix socket. Never blocks or fails the agent: if the env vars needed to
# reach the daemon aren't set, or the daemon is unreachable, this exits 0.
if [ -z "$ROCKET_SOCKET" ] || [ -z "$ROCKET_SESSION_ID" ]; then
  exit 0
fi

curl -s --max-time 2 --connect-timeout 1 --unix-socket "$ROCKET_SOCKET" \
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

// quizHookRelPath is the quiz hook script's path relative to the worktree
// root, mirroring activityHookRelPath.
const quizHookRelPath = ".rocket/quiz-hook.sh"

// quizHookScript is a POSIX shell script that reports the raw stdin payload
// of an AskUserQuestion PreToolUse/PostToolUse hook invocation to rocket's
// internal quiz endpoint over the unix socket. The transcript stays silent
// until a TUI quiz is answered, so this push channel is how the daemon sees
// a pending quiz in real time. Like activity-hook.sh, it never fails the
// invoking hook: it always exits 0, regardless of whether the env vars are
// set or the daemon is reachable.
const quizHookScript = `#!/bin/sh
# rocket quiz push hook. Invoked by Claude Code as:
#   ` + quizHookRelPath + ` <phase>
# <phase> is "pending" for PreToolUse (quiz about to be shown) or
# "resolved" for PostToolUse (quiz answered). Reports this session's
# AskUserQuestion hook stdin payload to the rocket daemon over its unix
# socket. Never blocks or fails the agent: if the env vars needed to reach
# the daemon aren't set, or the daemon is unreachable, this exits 0.
payload=$(cat)

if [ -z "$ROCKET_SOCKET" ] || [ -z "$ROCKET_SESSION_ID" ]; then
  exit 0
fi

curl -s --max-time 3 --connect-timeout 1 --unix-socket "$ROCKET_SOCKET" \
  -X POST -H 'Content-Type: application/json' \
  -d "{\"session\":\"$ROCKET_SESSION_ID\",\"phase\":\"$1\",\"payload\":$payload}" \
  http://rocket/v1/internal/quiz >/dev/null 2>&1

exit 0
`

// writeQuizHookScript writes the quiz push-channel hook script to
// <worktreePath>/.rocket/quiz-hook.sh, creating the .rocket directory
// (0755) if needed. The script is written 0700 (owner rwx only).
func writeQuizHookScript(worktreePath string) error {
	dir := filepath.Join(worktreePath, ".rocket")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "quiz-hook.sh")
	return os.WriteFile(path, []byte(quizHookScript), 0o700)
}

// quizHookEvents maps the Claude Code hook events rocket wires up for the
// quiz channel to the phase value quiz-hook.sh should report for that
// event: PreToolUse fires as the quiz is about to be shown ("pending"),
// PostToolUse fires once it's been answered ("resolved"), and Stop is the
// cancellation backstop — PostToolUse does NOT fire for a REJECTED tool
// call (Esc / declined quiz, verified live 2026-07-19), but rejecting the
// quiz ends the agent's turn, which fires Stop. A Stop while a quiz is
// pending therefore means the widget is gone either way; with no pending
// quiz the resolved report is a cheap no-op (no event published).
var quizHookEvents = map[string]string{
	"PreToolUse":  "pending",
	"PostToolUse": "resolved",
	"Stop":        "resolved",
}

// quizHookMatcher is the hook matcher rocket wires the quiz hook to: it
// only fires for the AskUserQuestion tool, unlike the activity hook's "*"
// (every tool).
const quizHookMatcher = "AskUserQuestion"

// quizHookCommand builds the shell command rocket wires into
// settings.local.json hooks for the given phase.
func quizHookCommand(phase string) string {
	return "sh " + quizHookRelPath + " " + phase
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
// settings.local.json hooks for the given state. It uses the project-relative
// form ("sh .rocket/activity-hook.sh <state>") since Claude Code runs hook
// commands with cwd set to the project root.
func activityHookCommand(state string) string {
	return "sh " + activityHookRelPath + " " + state
}

// hookCommand is the {"type":"command","command":"..."} shape of a single
// hook within a settings.local.json hooks[event][].hooks array.
type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// hookMatcherGroup is the {"matcher":"...","hooks":[...]} shape of a single
// entry within settings.local.json hooks[event].
type hookMatcherGroup struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

// mergeHookEntry idempotently merges a single hook command into hooks[event]
// under the given matcher: if a matcher group for event already exists it
// appends cmd to it (unless cmd is already present, in which case this is a
// no-op), otherwise it creates a new matcher group. Shared by the activity
// hook and quiz hook wiring in upsertClaudeSettings, since both idempotently
// upsert a {event, matcher, command} triple into the same hooks map.
func mergeHookEntry(hooks map[string][]hookMatcherGroup, event, matcher, cmd string) {
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
		return
	}

	entry := hookCommand{Type: "command", Command: cmd}
	if groupIdx >= 0 {
		groups[groupIdx].Hooks = append(groups[groupIdx].Hooks, entry)
	} else {
		groups = append(groups, hookMatcherGroup{Matcher: matcher, Hooks: []hookCommand{entry}})
	}
	hooks[event] = groups
}

// upsertClaudeSettings idempotently merges rocket's activity-hook wiring
// into <worktreePath>/.claude/settings.local.json. settings.local.json (not
// settings.json) is used deliberately: it is Claude Code's per-user/local
// override file, conventionally untracked by git (unlike settings.json,
// which projects commit), so rocket's harness plumbing does not pollute a
// worker's PR diff. It preserves any existing content: unknown top-level
// keys are round-tripped untouched, and existing hook entries for other
// events/matchers are kept. Re-running against a file that already has our
// entries is a no-op (no duplicates). .claude/settings.json is never
// touched or created by rocket.
func upsertClaudeSettings(worktreePath string) error {
	dir := filepath.Join(worktreePath, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "settings.local.json")

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
		mergeHookEntry(hooks, event, hookMatcherFor(event), activityHookCommand(state))
	}
	for event, phase := range quizHookEvents {
		// Tool-use events are scoped to AskUserQuestion; Stop is a
		// lifecycle event with no matcher concept (same rule as
		// hookMatcherFor).
		matcher := quizHookMatcher
		if event == "Stop" {
			matcher = ""
		}
		mergeHookEntry(hooks, event, matcher, quizHookCommand(phase))
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

	// Write atomically: create temp file in same directory, then rename.
	// This prevents corruption if write is interrupted mid-flight.
	f, err := os.CreateTemp(dir, ".settings-*.json")
	if err != nil {
		return fmt.Errorf("create temp settings file: %w", err)
	}
	tempPath := f.Name()
	if _, err := f.Write(out); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("write temp settings file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("close temp settings file: %w", err)
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("chmod temp settings file: %w", err)
	}
	return os.Rename(tempPath, path)
}

// trustWorktree marks worktreePath as trusted in the user's global
// ~/.claude.json so Claude Code's one-time "trust this folder" workspace
// dialog does not block a headless launch. That dialog runs even with
// --dangerously-skip-permissions (which only bypasses per-tool permission
// checks, not the initial folder-trust prompt), and it is not auto-skipped
// when rocket launches claude inside a real tmux TTY (only -p/non-TTY
// invocations skip it). Since every spawned worktree is a brand-new path
// Claude Code has never seen, without this the agent hangs forever on the
// dialog and never starts.
//
// It preserves the rest of ~/.claude.json untouched, including any existing
// entry for other projects or other fields already set for this path.
func trustWorktree(worktreePath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}

	// Claude Code keys ~/.claude.json's projects map by the fully resolved
	// path (e.g. on macOS /tmp is a symlink to /private/tmp), so resolve
	// symlinks before using the path as a key. Fall back to the given path
	// if it can't be resolved (e.g. it doesn't exist yet in some edge case)
	// rather than failing the whole setup over this best-effort step.
	resolved := worktreePath
	if r, err := filepath.EvalSymlinks(worktreePath); err == nil {
		resolved = r
	}

	path := filepath.Join(home, ".claude.json")

	// Guard the read-modify-write against concurrent trustWorktree calls
	// (e.g. multiple sessions spawning at once): without this, two
	// goroutines can both read the file, each add their own project entry
	// in memory, and the second writer's rename clobbers the first
	// goroutine's entry entirely.
	unlock, err := lockClaudeJSON(path)
	if err != nil {
		return err
	}
	defer unlock()

	// Preserve the existing file's permission bits; default to 0644 for a
	// brand-new file.
	perm := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}

	doc := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		if len(data) > 0 {
			if err := json.Unmarshal(data, &doc); err != nil {
				return fmt.Errorf("parse existing %s: %w", path, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	projects := map[string]json.RawMessage{}
	if raw, ok := doc["projects"]; ok {
		if err := json.Unmarshal(raw, &projects); err != nil {
			return fmt.Errorf("parse existing projects in %s: %w", path, err)
		}
	}

	entry := map[string]json.RawMessage{}
	if raw, ok := projects[resolved]; ok {
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("parse existing project entry for %s in %s: %w", resolved, path, err)
		}
	}
	entry["hasTrustDialogAccepted"] = json.RawMessage("true")

	entryRaw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	projects[resolved] = entryRaw

	projectsRaw, err := json.Marshal(projects)
	if err != nil {
		return err
	}
	doc["projects"] = projectsRaw

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	// Write atomically: create temp file in same directory, then rename.
	f, err := os.CreateTemp(home, ".claude-*.json")
	if err != nil {
		return fmt.Errorf("create temp claude.json file: %w", err)
	}
	tempPath := f.Name()
	if _, err := f.Write(out); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("write temp claude.json file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("close temp claude.json file: %w", err)
	}
	if err := os.Chmod(tempPath, perm); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("chmod temp claude.json file: %w", err)
	}
	return os.Rename(tempPath, path)
}

// lockClaudeJSON acquires an exclusive flock on a sidecar lock file next to
// path (path + ".rocket-lock"), serializing concurrent read-modify-write
// cycles against ~/.claude.json across goroutines/processes. The caller
// must invoke the returned func (typically via defer) to release the lock
// and close the file descriptor.
func lockClaudeJSON(path string) (func(), error) {
	lockPath := path + ".rocket-lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", lockPath, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("flock %s: %w", lockPath, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
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
