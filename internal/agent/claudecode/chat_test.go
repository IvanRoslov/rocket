package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func TestTranscriptTailUserStringContent(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"user","timestamp":"2026-07-18T21:00:26.830Z","message":{"role":"user","content":"hello there"}}`,
	}, time.Now())

	cc := New()
	entries, cursor, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Role != "user" || e.Text != "hello there" {
		t.Errorf("entry = %+v, want {Role:user Text:%q}", e, "hello there")
	}
	if e.TS == 0 {
		t.Errorf("TS should not be zero")
	}
	if cursor == "" {
		t.Errorf("cursor should not be empty")
	}
}

func TestTranscriptTailUserBlockContent(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[from orchestrator] status?"}]}}`,
	}, time.Now())

	cc := New()
	entries, _, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Role != "user" || entries[0].Text != "[from orchestrator] status?" {
		t.Errorf("entry = %+v", entries[0])
	}
}

func TestTranscriptTailUserToolResultOnlyIsSkipped(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"x","content":"ok"}]}}`,
	}, time.Now())

	cc := New()
	entries, _, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %+v, want none", entries)
	}
}

func TestTranscriptTailAssistantTextAndToolUse(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	longInput := `{"file_path":"` + strings.Repeat("x", 200) + `.go","content":"package main"}`
	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"assistant","timestamp":"2026-07-18T21:10:24.655Z","message":{"role":"assistant","content":[` +
			`{"type":"text","text":"Reading the file, then editing it."},` +
			`{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/a.go"}},` +
			`{"type":"tool_use","id":"t2","name":"Write","input":` + longInput + `}` +
			`]}}`,
	}, time.Now())

	cc := New()
	entries, _, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3: %+v", len(entries), entries)
	}

	if entries[0].Role != "assistant" || entries[0].Text != "Reading the file, then editing it." {
		t.Errorf("entries[0] = %+v", entries[0])
	}

	if entries[1].Role != "tool" || entries[1].ToolName != "Read" {
		t.Errorf("entries[1] = %+v", entries[1])
	}
	if entries[1].Text != `{"file_path":"/tmp/a.go"}` {
		t.Errorf("entries[1].Text = %q", entries[1].Text)
	}

	if entries[2].Role != "tool" || entries[2].ToolName != "Write" {
		t.Errorf("entries[2] = %+v", entries[2])
	}
	if got := []rune(entries[2].Text); len(got) > 120 {
		t.Errorf("entries[2].Text len = %d, want <=120", len(got))
	}
	if !strings.HasSuffix(entries[2].Text, "…") {
		t.Errorf("entries[2].Text = %q, want truncated with ellipsis suffix", entries[2].Text)
	}
}

func TestTranscriptTailSkipsThinkingSystemAndOtherTypes(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"system","message":{"role":"system","content":"ignored"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"pondering..."}]}}`,
		`{"type":"mode","mode":"normal","sessionId":"abc"}`,
		`{"type":"summary","summary":"x"}`,
		`{"type":"user","message":{"role":"user","content":"the only real message"}}`,
	}, time.Now())

	cc := New()
	entries, _, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want exactly 1", entries)
	}
	if entries[0].Text != "the only real message" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
}

func TestTranscriptTailGarbageLineIsSkipped(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`not even json {{{`,
		`{"type":"user","message":{"role":"user","content":"real one"}}`,
	}, time.Now())

	cc := New()
	entries, cursor, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Text != "real one" {
		t.Fatalf("entries = %+v", entries)
	}

	// Cursor should point past both lines (garbage line consumed too).
	path := filepath.Join(base, "projects", slugify(wt), "sess.jsonl")
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("stat: %v", statErr)
	}
	wantCursor := path + ":" + strconv.FormatInt(info.Size(), 10)
	if cursor != wantCursor {
		t.Errorf("cursor = %q, want %q", cursor, wantCursor)
	}
}

func TestTranscriptTailCursorIncrementality(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	path := writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"user","message":{"role":"user","content":"first"}}`,
	}, time.Now())

	cc := New()
	entries1, cursor1, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() #1 error = %v", err)
	}
	if len(entries1) != 1 || entries1[0].Text != "first" {
		t.Fatalf("entries1 = %+v", entries1)
	}

	// A second call with the same cursor should return nothing new.
	entriesSame, cursorSame, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, cursor1)
	if err != nil {
		t.Fatalf("TranscriptTail() (repeat) error = %v", err)
	}
	if len(entriesSame) != 0 {
		t.Fatalf("entriesSame = %+v, want none", entriesSame)
	}
	if cursorSame != cursor1 {
		t.Errorf("cursorSame = %q, want unchanged %q", cursorSame, cursor1)
	}

	// Append a new line between calls.
	appendLine(t, path, `{"type":"user","message":{"role":"user","content":"second"}}`)

	entries2, cursor2, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, cursor1)
	if err != nil {
		t.Fatalf("TranscriptTail() #2 error = %v", err)
	}
	if len(entries2) != 1 || entries2[0].Text != "second" {
		t.Fatalf("entries2 = %+v", entries2)
	}
	if cursor2 == cursor1 {
		t.Errorf("cursor2 should have advanced past cursor1")
	}
}

func TestTranscriptTailPartialTrailingLineHeldBack(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	dir := filepath.Join(base, "projects", slugify(wt))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "sess.jsonl")
	// One complete line, then a partial line with no trailing newline.
	content := `{"type":"user","message":{"role":"user","content":"complete"}}` + "\n" +
		`{"type":"user","message":{"role":"user","content":"incompl`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cc := New()
	entries, cursor, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Text != "complete" {
		t.Fatalf("entries = %+v, want only the complete line", entries)
	}

	// Cursor must point right after the complete line, not past the
	// partial one.
	wantOffset := int64(len(`{"type":"user","message":{"role":"user","content":"complete"}}` + "\n"))
	wantCursor := path + ":" + strconv.FormatInt(wantOffset, 10)
	if cursor != wantCursor {
		t.Errorf("cursor = %q, want %q", cursor, wantCursor)
	}

	// Complete the line and re-tail from the same cursor: it should now
	// appear.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := f.WriteString("ete\"}}\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	f.Close()

	entries2, _, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, cursor)
	if err != nil {
		t.Fatalf("TranscriptTail() #2 error = %v", err)
	}
	if len(entries2) != 1 || entries2[0].Text != "incomplete" {
		t.Fatalf("entries2 = %+v, want the now-complete line", entries2)
	}
}

func TestTranscriptTailRotatesToNewerFile(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	older := time.Now().Add(-10 * time.Minute)
	writeTranscript(t, base, wt, "old.jsonl", []string{
		`{"type":"user","message":{"role":"user","content":"in old file"}}`,
	}, older)

	cc := New()
	entries1, cursor1, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() #1 error = %v", err)
	}
	if len(entries1) != 1 || entries1[0].Text != "in old file" {
		t.Fatalf("entries1 = %+v", entries1)
	}
	if !strings.Contains(cursor1, "old.jsonl") {
		t.Fatalf("cursor1 = %q, want pointing at old.jsonl", cursor1)
	}

	// A newer transcript file now exists (new session/rotation).
	newer := time.Now()
	writeTranscript(t, base, wt, "new.jsonl", []string{
		`{"type":"user","message":{"role":"user","content":"in new file"}}`,
	}, newer)

	entries2, cursor2, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, cursor1)
	if err != nil {
		t.Fatalf("TranscriptTail() #2 error = %v", err)
	}
	if len(entries2) != 1 || entries2[0].Text != "in new file" {
		t.Fatalf("entries2 = %+v, want the new file's entry", entries2)
	}
	if !strings.Contains(cursor2, "new.jsonl") {
		t.Errorf("cursor2 = %q, want pointing at new.jsonl", cursor2)
	}
}

// TestTranscriptTailCursorOutsideTranscriptDirFallsBackToNewest covers
// finding 1 of the final-review fix brief: a client-supplied cursor whose
// path points outside this ref's transcript directory must be treated as
// invalid (same fallback as an empty/malformed cursor), not read.
func TestTranscriptTailCursorOutsideTranscriptDirFallsBackToNewest(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"user","message":{"role":"user","content":"legit"}}`,
	}, time.Now())

	// A file outside the transcript directory that an untrusted cursor
	// could point at.
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.jsonl")
	if err := os.WriteFile(outsidePath, []byte(`{"type":"user","message":{"role":"user","content":"SECRET"}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	cc := New()
	entries, _, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, outsidePath+":0")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Text != "legit" {
		t.Fatalf("entries = %+v, want fallback to the legit transcript (outside-dir cursor must be rejected)", entries)
	}
	for _, e := range entries {
		if strings.Contains(e.Text, "SECRET") {
			t.Fatalf("entries leaked outside-file content: %+v", entries)
		}
	}
}

func TestTranscriptTailNoDirReturnsErrNoSignal(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)

	cc := New()
	entries, cursor, err := cc.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: "/tmp/nowhere"}, "")
	if err != agent.ErrNoSignal {
		t.Fatalf("err = %v, want ErrNoSignal", err)
	}
	if entries != nil {
		t.Errorf("entries = %+v, want nil", entries)
	}
	if cursor != "" {
		t.Errorf("cursor = %q, want empty", cursor)
	}
}

func TestTranscriptStatValues(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)
	wt := "/tmp/some/worktree"

	mtime := time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	path := writeTranscript(t, base, wt, "sess.jsonl", []string{
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
	}, mtime)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	cc := New()
	gotMtime, gotSize, err := cc.TranscriptStat(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("TranscriptStat() error = %v", err)
	}
	if gotMtime != mtime.Unix() {
		t.Errorf("mtime = %d, want %d", gotMtime, mtime.Unix())
	}
	if gotSize != info.Size() {
		t.Errorf("size = %d, want %d", gotSize, info.Size())
	}
}

func TestTranscriptStatNoDirReturnsErrNoSignal(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", base)

	cc := New()
	_, _, err := cc.TranscriptStat(context.Background(), agent.ActivityRef{WorktreePath: "/tmp/nowhere"})
	if err != agent.ErrNoSignal {
		t.Fatalf("err = %v, want ErrNoSignal", err)
	}
}

// appendLine appends a single JSONL line (with trailing newline) to the
// file at path.
func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
}
