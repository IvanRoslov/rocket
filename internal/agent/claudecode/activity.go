package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
)

// freshWindow is how recent a transcript's mtime must be for the session to
// be considered actively working, regardless of the last record's type.
const freshWindow = 30 * time.Second

// tailReadSize is how many trailing bytes of the transcript file to read
// when looking for the last JSONL record.
const tailReadSize = 64 * 1024

// slugify converts a worktree path into the directory name Claude Code uses
// under ~/.claude/projects/. Observed scheme on this machine: every '/' and
// '.' character in the path is replaced with '-', one-for-one (consecutive
// separators are not collapsed). E.g.
//
//	/Users/ivanroslov/projects/rocket/.claude/worktrees/phase-2-activity
//
// becomes
//
//	-Users-ivanroslov-projects-rocket--claude-worktrees-phase-2-activity
func slugify(worktreePath string) string {
	replacer := strings.NewReplacer("/", "-", ".", "-")
	return replacer.Replace(worktreePath)
}

// configDir returns the base Claude Code config directory, honoring the
// CLAUDE_CONFIG_DIR override (used by real Claude Code and by tests), else
// defaulting to ~/.claude.
func configDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// transcriptDir returns the directory containing transcript .jsonl files
// for the given worktree path.
func transcriptDir(worktreePath string) string {
	base := configDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "projects", slugify(worktreePath))
}

// newestTranscript returns the path and mtime of the most recently modified
// *.jsonl file in dir, or an error if none is found.
func newestTranscript(dir string) (string, time.Time, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}, agent.ErrNoSignal
	}

	var bestPath string
	var bestMTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if bestPath == "" || info.ModTime().After(bestMTime) {
			bestPath = filepath.Join(dir, e.Name())
			bestMTime = info.ModTime()
		}
	}
	if bestPath == "" {
		return "", time.Time{}, agent.ErrNoSignal
	}
	return bestPath, bestMTime, nil
}

// lastNonEmptyLine reads (at most) the trailing tailReadSize bytes of the
// file at path and returns the last non-empty line.
func lastNonEmptyLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}

	start := int64(0)
	if info.Size() > tailReadSize {
		start = info.Size() - tailReadSize
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var last string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			last = line
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return last, nil
}

// isApiError reports whether the JSONL record (raw line + parsed top-level
// object) indicates an api-error record. Checks the top-level
// "isApiErrorMessage" field first, then falls back to a substring check on
// the raw line to catch the field nested inside e.g. "message".
func isApiError(raw string, top map[string]json.RawMessage) bool {
	if v, ok := top["isApiErrorMessage"]; ok {
		var b bool
		if err := json.Unmarshal(v, &b); err == nil && b {
			return true
		}
	}
	return strings.Contains(raw, `"isApiErrorMessage":true`)
}

// classify determines the activity state from the last transcript line and
// the file's mtime.
func classify(raw string, mtime time.Time) activity.State {
	if time.Since(mtime) < freshWindow {
		return activity.Active
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return activity.Ready
	}

	if isApiError(raw, top) {
		return activity.Blocked
	}

	if typeRaw, ok := top["type"]; ok {
		var t string
		if err := json.Unmarshal(typeRaw, &t); err == nil && t == "assistant" {
			return activity.Ready
		}
	}

	return activity.Ready
}

// Activity implements agent.Agent. It inspects the newest transcript file
// for the session's worktree and returns the raw source activity state and
// the timestamp of the last observed work. It does not apply idle
// thresholds — that is the monitor's responsibility.
func (c *ClaudeCode) Activity(ctx context.Context, ref agent.ActivityRef) (activity.State, time.Time, error) {
	dir := transcriptDir(ref.WorktreePath)
	if dir == "" {
		return "", time.Time{}, agent.ErrNoSignal
	}

	path, mtime, err := newestTranscript(dir)
	if err != nil {
		return "", time.Time{}, agent.ErrNoSignal
	}

	line, err := lastNonEmptyLine(path)
	if err != nil || line == "" {
		return "", time.Time{}, agent.ErrNoSignal
	}

	return classify(line, mtime), mtime, nil
}
