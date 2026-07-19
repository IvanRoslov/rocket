package agent

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExcludeFromGit idempotently appends each of paths to worktree's git
// exclude file (.git/info/exclude, resolved via `git rev-parse --git-path`
// so this also works when <worktree>/.git is a file pointing at a separate
// gitdir, as in a `git worktree` checkout), skipping any path that is
// already tracked by git or already present in the exclude file.
//
// Best-effort: if worktree is not a git repository (e.g. in unit tests), or
// any git plumbing call fails, this logs at debug level and returns without
// error — exclude-file hygiene should never fail a caller's setup step.
func ExcludeFromGit(worktree string, paths []string) {
	excludePathOut, err := exec.Command("git", "-C", worktree, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		slog.Debug("agent: resolve git-path info/exclude failed, skipping", "worktree", worktree, "error", err)
		return
	}
	excludeRel := strings.TrimSpace(string(excludePathOut))
	excludePath := excludeRel
	if !filepath.IsAbs(excludePath) {
		excludePath = filepath.Join(worktree, excludeRel)
	}

	existing, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		slog.Debug("agent: read info/exclude failed, skipping", "path", excludePath, "error", err)
		return
	}
	existingLines := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		existingLines[strings.TrimSpace(line)] = true
	}

	var toAdd []string
	for _, p := range paths {
		checkTracked := exec.Command("git", "-C", worktree, "ls-files", "--error-unmatch", p)
		if err := checkTracked.Run(); err == nil {
			// Tracked by git — leave it (and info/exclude) alone.
			slog.Debug("agent: path is tracked by git, not adding to info/exclude", "worktree", worktree, "path", p)
			continue
		}
		if existingLines[p] {
			// Already present — idempotent no-op for this path.
			continue
		}
		toAdd = append(toAdd, p)
	}
	if len(toAdd) == 0 {
		return
	}

	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		slog.Debug("agent: mkdir info/exclude parent failed, skipping", "path", excludePath, "error", err)
		return
	}
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		slog.Debug("agent: open info/exclude failed, skipping", "path", excludePath, "error", err)
		return
	}
	defer f.Close()

	out := ""
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		out += "\n"
	}
	for _, p := range toAdd {
		out += p + "\n"
	}
	if _, err := f.WriteString(out); err != nil {
		slog.Debug("agent: write info/exclude failed", "path", excludePath, "error", err)
	}
}
