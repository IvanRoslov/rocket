package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
)

// writeTranscript creates <base>/projects/<slug>/<name>.jsonl with the given
// lines (one JSON object per line) and sets its mtime.
func writeTranscript(t *testing.T, base, worktreePath, name string, lines []string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(base, "projects", slugify(worktreePath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir transcript dir: %v", err)
	}
	path := filepath.Join(dir, name)
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return path
}

func TestSlugifyObservedScheme(t *testing.T) {
	// Observed on this machine: ls ~/.claude/projects/ shows
	// "-Users-ivanroslov-projects-rocket--claude-worktrees-phase-2-activity"
	// for worktree path
	// "/Users/ivanroslov/projects/rocket/.claude/worktrees/phase-2-activity".
	// Scheme: replace each '/' and '.' character with '-', 1-for-1 (no
	// collapsing of consecutive separators).
	got := slugify("/Users/ivanroslov/projects/rocket/.claude/worktrees/phase-2-activity")
	want := "-Users-ivanroslov-projects-rocket--claude-worktrees-phase-2-activity"
	if got != want {
		t.Errorf("slugify() = %q, want %q", got, want)
	}
}

func TestActivityFreshMtimeIsActive(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`,
	}, time.Now())

	cc := New()
	state, ts, err := cc.Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if state != activity.Active {
		t.Errorf("state = %q, want Active", state)
	}
	if ts.IsZero() {
		t.Errorf("timestamp should not be zero")
	}
}

func TestActivityOldAssistantIsReady(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	old := time.Now().Add(-5 * time.Minute)
	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"user","message":{"content":"hi"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}`,
	}, old)

	cc := New()
	state, ts, err := cc.Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if state != activity.Ready {
		t.Errorf("state = %q, want Ready", state)
	}
	if !ts.Equal(old) {
		t.Errorf("timestamp = %v, want %v", ts, old)
	}
}

func TestActivityApiErrorIsBlocked(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	old := time.Now().Add(-5 * time.Minute)
	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`,
		`{"type":"user","isApiErrorMessage":true,"message":{"content":"API Error: 500"}}`,
	}, old)

	cc := New()
	state, _, err := cc.Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if state != activity.Blocked {
		t.Errorf("state = %q, want Blocked", state)
	}
}

func TestActivityNestedApiErrorIsBlocked(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	old := time.Now().Add(-5 * time.Minute)
	// isApiErrorMessage nested inside a "message" object, not top-level.
	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"user","message":{"role":"user","content":"x","isApiErrorMessage":true}}`,
	}, old)

	cc := New()
	state, _, err := cc.Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if state != activity.Blocked {
		t.Errorf("state = %q, want Blocked", state)
	}
}

func TestActivityOldUserRecordIsReady(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	old := time.Now().Add(-5 * time.Minute)
	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"user","message":{"content":"hi"}}`,
	}, old)

	cc := New()
	state, _, err := cc.Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if state != activity.Ready {
		t.Errorf("state = %q, want Ready", state)
	}
}

func TestActivityNoDirReturnsErrNoSignal(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)

	cc := New()
	state, ts, err := cc.Activity(context.Background(), agent.ActivityRef{WorktreePath: "/tmp/nowhere"})
	if err != agent.ErrNoSignal {
		t.Fatalf("err = %v, want ErrNoSignal", err)
	}
	if state != "" {
		t.Errorf("state = %q, want empty", state)
	}
	if !ts.IsZero() {
		t.Errorf("ts = %v, want zero", ts)
	}
}

func TestActivityNoJSONLFilesReturnsErrNoSignal(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	dir := filepath.Join(base, "projects", slugify(wt))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cc := New()
	_, _, err := cc.Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != agent.ErrNoSignal {
		t.Fatalf("err = %v, want ErrNoSignal", err)
	}
}

func TestActivityPicksNewestFile(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	older := time.Now().Add(-10 * time.Minute)
	newer := time.Now().Add(-5 * time.Minute)

	writeTranscript(t, base, wt, "old.jsonl", []string{
		`{"type":"user","isApiErrorMessage":true,"message":{"content":"err"}}`,
	}, older)
	writeTranscript(t, base, wt, "new.jsonl", []string{
		`{"type":"assistant","message":{"content":"ok"}}`,
	}, newer)

	cc := New()
	state, ts, err := cc.Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity() error = %v", err)
	}
	if state != activity.Ready {
		t.Errorf("state = %q, want Ready (from newest file)", state)
	}
	if !ts.Equal(newer) {
		t.Errorf("ts = %v, want %v", ts, newer)
	}
}

func TestActivityDefaultBaseDirWhenEnvUnset(t *testing.T) {
	// When CLAUDE_CONFIG_DIR is unset, transcriptDir should fall back to
	// ~/.claude. We just verify it doesn't crash and returns ErrNoSignal
	// for a bogus worktree (since we can't easily control the real home
	// dir's contents in a unit test).
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	cc := New()
	_, _, err := cc.Activity(context.Background(), agent.ActivityRef{WorktreePath: "/definitely/not/a/real/worktree/path/xyz123"})
	if err != agent.ErrNoSignal {
		t.Fatalf("err = %v, want ErrNoSignal", err)
	}
}
