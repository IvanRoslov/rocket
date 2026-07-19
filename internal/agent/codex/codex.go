// Package codex implements the agent.Agent adapter for the codex CLI
// (https://github.com/openai/codex), following the same shape as
// internal/agent/claudecode.
//
// Facts this file relies on come from the Task 2 recon
// (.superpowers/sdd/task-2-report.md, "## Codex facts" section), not from
// codex's --help alone:
//
//   - codex accepts a positional PROMPT that becomes the first user message
//     in interactive mode (confirmed live).
//   - `--sandbox workspace-write --ask-for-approval never` gives full
//     command auto-approval in interactive mode.
//   - A brand-new worktree directory triggers a blocking "Do you trust the
//     contents of this directory?" TUI prompt on first launch, which is NOT
//     suppressed by the approval/sandbox flags above. It is a
//     directory-trust gate persisted in $CODEX_HOME/config.toml as
//     `[projects."<path>"] trust_level = "trusted"` — the exact table shape
//     was confirmed present in this machine's own config.toml, so
//     SetupWorkspace pre-seeds it (see seedCodexTrust) rather than relying
//     on an extra keystroke.
//   - codex does not appear to auto-inject AGENTS.md into context on a
//     plain new-directory session (recon's live test found no evidence of
//     it), but the adapter still writes a rocket-managed block into
//     AGENTS.md per the binding decision — worker prompts instruct the
//     agent to check AGENTS.md explicitly, and this keeps codex consistent
//     with claude-code's system-prompt delivery contract. If that turns out
//     to be a no-op for codex, Task 4's live check should flag it.
package codex

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/prompts"
)

type Codex struct{}

// New creates a new codex agent.
func New() agent.Agent {
	return &Codex{}
}

func (c *Codex) Name() string {
	return "codex"
}

// Available checks if the codex command is available in PATH.
func (c *Codex) Available() error {
	_, err := exec.LookPath("codex")
	return err
}

// codexHome returns the base codex config/data directory, honoring the
// CODEX_HOME override (codex itself supports this — observed referenced in
// this machine's own config.toml, under a bundled MCP server's env block),
// else defaulting to ~/.codex. Used as the root for both config.toml
// (trust seeding) and sessions/ (activity).
func codexHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// SetupWorkspace prepares the worktree for a codex launch: it pre-seeds
// directory trust in $CODEX_HOME/config.toml so the interactive TUI's
// blocking trust prompt does not stall a headless launch, and writes the
// (skills-stripped) system prompt into <worktree>/AGENTS.md.
func (c *Codex) SetupWorkspace(spec agent.LaunchSpec) error {
	if err := seedCodexTrust(spec.WorktreePath); err != nil {
		return fmt.Errorf("seed codex trust: %w", err)
	}
	if err := upsertAgentsMD(spec.WorktreePath, spec.SystemPrompt); err != nil {
		return fmt.Errorf("upsert AGENTS.md: %w", err)
	}
	return nil
}

// rocketBlockRe matches a "<!-- rocket:start -->" ... "<!-- rocket:end -->"
// block (markers and content included) in AGENTS.md.
var rocketBlockRe = regexp.MustCompile(`(?s)<!-- rocket:start -->.*?<!-- rocket:end -->\n?`)

// upsertAgentsMD idempotently writes spec's skills-stripped system prompt
// into <worktreePath>/AGENTS.md, wrapped in rocket block markers:
//
//   - file absent: create it, content is just the rocket block.
//   - file exists without our markers: prepend the rocket block, a blank
//     line, then the existing content untouched.
//   - file exists with our markers: replace the block's content in place
//     (idempotent — re-running with the same prompt is a no-op diff).
//
// No-ops entirely if spec.SystemPrompt is empty (mirrors claudecode's
// SetupWorkspace, which skips writing .rocket-prompt.md in that case).
func upsertAgentsMD(worktreePath, systemPrompt string) error {
	if systemPrompt == "" {
		return nil
	}

	stripped := prompts.StripSkills(systemPrompt)
	block := "<!-- rocket:start -->\n" + stripped + "\n<!-- rocket:end -->\n"

	path := filepath.Join(worktreePath, "AGENTS.md")

	perm := os.FileMode(0o644)
	var existing []byte
	if data, err := os.ReadFile(path); err == nil {
		existing = data
		if info, statErr := os.Stat(path); statErr == nil {
			perm = info.Mode().Perm()
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	var out []byte
	switch {
	case len(existing) == 0:
		out = []byte(block)
	case rocketBlockRe.Match(existing):
		out = rocketBlockRe.ReplaceAll(existing, []byte(block))
	default:
		out = append([]byte(block+"\n"), existing...)
	}

	return atomicWriteFile(worktreePath, path, out, perm)
}

// seedCodexTrust ensures worktreePath is marked trusted in
// $CODEX_HOME/config.toml, matching the exact table shape codex itself
// writes (confirmed by Task 2 recon against this machine's real config):
//
//	[projects."<path>"]
//	trust_level = "trusted"
//
// It only appends a new table if one for this exact path doesn't already
// exist (TOML forbids redefining a table, and we don't want to touch
// unrelated config content — no general TOML parser is used, this is a
// narrow, additive text operation). A flock-guarded read-modify-write
// mirrors claudecode's trustWorktree/lockClaudeJSON pattern to serialize
// concurrent SetupWorkspace calls.
func seedCodexTrust(worktreePath string) error {
	base := codexHome()
	if base == "" {
		return fmt.Errorf("resolve codex home dir")
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}

	resolved := worktreePath
	if r, err := filepath.EvalSymlinks(worktreePath); err == nil {
		resolved = r
	}

	path := filepath.Join(base, "config.toml")

	unlock, err := lockFile(path + ".rocket-lock")
	if err != nil {
		return err
	}
	defer unlock()

	perm := os.FileMode(0o644)
	var existing []byte
	if data, err := os.ReadFile(path); err == nil {
		existing = data
		if info, statErr := os.Stat(path); statErr == nil {
			perm = info.Mode().Perm()
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	header := fmt.Sprintf("[projects.%q]", resolved)
	if bytes.Contains(existing, []byte(header)) {
		// Already trusted (or at least already has a table for this path);
		// leave the file untouched.
		return nil
	}

	out := existing
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, []byte("\n"+header+"\ntrust_level = \"trusted\"\n")...)

	return atomicWriteFile(base, path, out, perm)
}

// atomicWriteFile writes data to path via create-temp-then-rename in dir,
// preserving perm. This prevents corruption if the write is interrupted
// mid-flight and avoids ever leaving a partially-written target file.
func atomicWriteFile(dir, path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(dir, ".rocket-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tempPath, perm); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// lockFile acquires an exclusive flock on lockPath (creating it if
// necessary), serializing concurrent read-modify-write cycles against the
// file it guards across goroutines/processes. The caller must invoke the
// returned func (typically via defer) to release the lock and close the fd.
func lockFile(lockPath string) (func(), error) {
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

// LaunchCommand builds the interactive codex invocation for the given spec.
// It intentionally never uses the `codex exec` subcommand: exec is
// non-interactive and exits after one turn, which is incompatible with
// rocket's tmux-injection model for follow-up messages (per recon, exec
// also doesn't accept -a/--ask-for-approval at all).
//
// Flags are exactly what Task 2's recon verified live for full
// auto-approval in interactive mode: `--sandbox workspace-write
// --ask-for-approval never`. -m sets the model when given. The positional
// PROMPT (confirmed live to become the first user message) carries
// FirstMessage when given, preceded by a `--` separator so a brief that
// happens to start with "-" (e.g. "-fix the bug") is never mistaken for a
// flag by clap's argument parser. Verified live against codex-cli 0.138.0:
// `codex --sandbox workspace-write --ask-for-approval never "-say ok"`
// (no `--`) fails fast with a clap parse error ("the argument '--sandbox
// <SANDBOX_MODE>' cannot be used multiple times" — clap re-parsing "-say"
// as short flags), while the same invocation with `--` inserted before the
// prompt parses cleanly and only fails later on "stdin is not a terminal"
// (a runtime TTY issue, not an argument-parsing one) — proof clap accepts
// `--` as the flags/positionals separator here.
func (c *Codex) LaunchCommand(spec agent.LaunchSpec) []string {
	cmd := []string{"codex", "--sandbox", "workspace-write", "--ask-for-approval", "never"}

	if spec.Model != "" {
		cmd = append(cmd, "-m", spec.Model)
	}

	if spec.FirstMessage != "" {
		cmd = append(cmd, "--", spec.FirstMessage)
	}

	return cmd
}

// Env returns environment variables for the agent. Same ROCKET_* keys as
// claudecode.Env, minus CLAUDECODE (that var is claude-code-specific and
// recon found nothing codex-specific to set in its place).
func (c *Codex) Env(spec agent.LaunchSpec) map[string]string {
	return map[string]string{
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
	agent.Register("codex", New)
}
