package codex

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

func TestTranscriptTailUserMessage(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	path := writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:06.210Z","payload":{"type":"user_message","message":"do the thing"}}`,
	}, now)

	c := New()
	entries, cursor, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Role != "user" || e.Text != "do the thing" {
		t.Errorf("entry = %+v, want {Role:user Text:%q}", e, "do the thing")
	}
	if e.TS == 0 {
		t.Errorf("TS should not be zero")
	}
	if !strings.Contains(cursor, path) {
		t.Errorf("cursor = %q, want to contain %q", cursor, path)
	}
}

func TestTranscriptTailAgentMessage(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:10.014Z","payload":{"type":"agent_message","message":"working on it","phase":"commentary"}}`,
	}, now)

	c := New()
	entries, _, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Role != "assistant" || entries[0].Text != "working on it" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestTranscriptTailFunctionCallIsTool(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`{"type":"response_item","timestamp":"2026-07-19T08:28:15.545Z","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\": \"git add PING.md\", \"workdir\": \"/tmp/wt\"}","call_id":"call_1"}}`,
	}, now)

	c := New()
	entries, _, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	e := entries[0]
	if e.Role != "tool" || e.ToolName != "exec_command" {
		t.Fatalf("entry = %+v", e)
	}
	want := `{"cmd":"git add PING.md","workdir":"/tmp/wt"}`
	if e.Text != want {
		t.Errorf("entry.Text = %q, want %q", e.Text, want)
	}
}

func TestTranscriptTailCustomToolCallIsTool(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`{"type":"response_item","timestamp":"2026-07-19T08:28:10.754Z","payload":{"type":"custom_tool_call","status":"completed","call_id":"call_2","name":"apply_patch","input":"*** Begin Patch\n*** Add File: PING.md\n+pong\n*** End Patch\n"}}`,
	}, now)

	c := New()
	entries, _, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	e := entries[0]
	if e.Role != "tool" || e.ToolName != "apply_patch" {
		t.Fatalf("entry = %+v", e)
	}
	if strings.Contains(e.Text, "\n") {
		t.Errorf("entry.Text should be single-line, got %q", e.Text)
	}
	want := "*** Begin Patch *** Add File: PING.md +pong *** End Patch"
	if e.Text != want {
		t.Errorf("entry.Text = %q, want %q", e.Text, want)
	}
}

func TestTranscriptTailWebSearchCallIsTool(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`{"type":"response_item","timestamp":"2026-07-19T08:28:15.545Z","payload":{"type":"web_search_call","status":"completed","action":{"type":"search","query":"golang json"}}}`,
	}, now)

	c := New()
	entries, _, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	e := entries[0]
	if e.Role != "tool" || e.ToolName != "web_search" {
		t.Fatalf("entry = %+v", e)
	}
	want := `{"type":"search","query":"golang json"}`
	if e.Text != want {
		t.Errorf("entry.Text = %q, want %q", e.Text, want)
	}
}

func TestTranscriptTailDigestTruncationRussianText(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	// 200 Cyrillic runes (multi-byte in UTF-8) as the apply_patch input, to
	// verify truncation is rune-safe, not byte-safe.
	longRussian := strings.Repeat("привет", 40) // 6*40 = 240 runes
	writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`{"type":"response_item","timestamp":"2026-07-19T08:28:10.754Z","payload":{"type":"custom_tool_call","status":"completed","call_id":"call_3","name":"apply_patch","input":"` + longRussian + `"}}`,
	}, now)

	c := New()
	entries, _, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	e := entries[0]
	got := []rune(e.Text)
	if len(got) > 120 {
		t.Errorf("entry.Text rune length = %d, want <=120", len(got))
	}
	if !strings.HasSuffix(e.Text, "…") {
		t.Errorf("entry.Text = %q, want truncated with ellipsis suffix", e.Text)
	}
	// Must not have split a multi-byte rune: re-decoding must round-trip
	// (an invalid split would surface as a replacement rune during the
	// []rune() conversion above already, but assert utf8 validity too).
	if !isValidUTF8(e.Text) {
		t.Errorf("entry.Text is not valid UTF-8: %q", e.Text)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

func TestTranscriptTailSkipsSessionMetaTurnContextAndDuplicateResponseItemMessages(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	// session_meta is written automatically by writeSessionFile as line 1.
	writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`{"type":"turn_context","timestamp":"2026-07-19T08:28:06.209Z","payload":{"cwd":"` + wt + `"}}`,
		`{"type":"response_item","timestamp":"2026-07-19T08:28:06.208Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions"}]}}`,
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:06.210Z","payload":{"type":"user_message","message":"real user text"}}`,
		`{"type":"response_item","timestamp":"2026-07-19T08:28:06.210Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"real user text"}]}}`,
		`{"type":"response_item","timestamp":"2026-07-19T08:28:09.464Z","payload":{"type":"reasoning","summary":[]}}`,
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:10.014Z","payload":{"type":"agent_message","message":"the answer","phase":"final_answer"}}`,
		`{"type":"response_item","timestamp":"2026-07-19T08:28:10.014Z","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"the answer"}]}}`,
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:10.926Z","payload":{"type":"token_count","info":{}}}`,
		`{"type":"response_item","timestamp":"2026-07-19T08:28:15.647Z","payload":{"type":"function_call_output","call_id":"c1","output":"ok"}}`,
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:19.097Z","payload":{"type":"task_complete","last_agent_message":"the answer"}}`,
	}, now)

	c := New()
	entries, _, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want exactly 2 (real user text + agent message, no dupes/instructions/noise)", entries)
	}
	if entries[0].Role != "user" || entries[0].Text != "real user text" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].Role != "assistant" || entries[1].Text != "the answer" {
		t.Errorf("entries[1] = %+v", entries[1])
	}
}

func TestTranscriptTailGarbageLineIsSkipped(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	path := writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`not even json {{{`,
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:06.210Z","payload":{"type":"user_message","message":"real one"}}`,
	}, now)

	c := New()
	entries, cursor, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Text != "real one" {
		t.Fatalf("entries = %+v", entries)
	}

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
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	path := writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:06.210Z","payload":{"type":"user_message","message":"first"}}`,
	}, now)

	c := New()
	entries1, cursor1, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() #1 error = %v", err)
	}
	if len(entries1) != 1 || entries1[0].Text != "first" {
		t.Fatalf("entries1 = %+v", entries1)
	}

	entriesSame, cursorSame, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, cursor1)
	if err != nil {
		t.Fatalf("TranscriptTail() (repeat) error = %v", err)
	}
	if len(entriesSame) != 0 {
		t.Fatalf("entriesSame = %+v, want none", entriesSame)
	}
	if cursorSame != cursor1 {
		t.Errorf("cursorSame = %q, want unchanged %q", cursorSame, cursor1)
	}

	appendLine(t, path, `{"type":"event_msg","timestamp":"2026-07-19T08:29:06.210Z","payload":{"type":"user_message","message":"second"}}`)

	entries2, cursor2, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, cursor1)
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
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	path := writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:06.210Z","payload":{"type":"user_message","message":"complete"}}`,
	}, now)

	// Append a partial line with no trailing newline.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := f.WriteString(`{"type":"event_msg","timestamp":"2026-07-19T08:29:06.210Z","payload":{"type":"user_message","message":"incompl`); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	f.Close()

	c := New()
	entries, cursor, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Text != "complete" {
		t.Fatalf("entries = %+v, want only the complete line", entries)
	}

	// Complete the partial line AND append two further complete lines
	// after it in the same append — this is the "completes+more-lines"
	// case: re-tailing from the held-back cursor must return all three new
	// entries in one call, not just the one that completed.
	f2, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open append 2: %v", err)
	}
	more := `ete","phase":"x"}}` + "\n" +
		`{"type":"event_msg","timestamp":"2026-07-19T08:30:06.210Z","payload":{"type":"agent_message","message":"more1"}}` + "\n" +
		`{"type":"event_msg","timestamp":"2026-07-19T08:31:06.210Z","payload":{"type":"agent_message","message":"more2"}}` + "\n"
	if _, err := f2.WriteString(more); err != nil {
		t.Fatalf("append more: %v", err)
	}
	f2.Close()

	entries2, _, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, cursor)
	if err != nil {
		t.Fatalf("TranscriptTail() #2 error = %v", err)
	}
	if len(entries2) != 3 {
		t.Fatalf("entries2 = %+v, want 3 (completed line + two more appended after it)", entries2)
	}
	if entries2[0].Role != "user" || entries2[0].Text != "incomplete" {
		t.Errorf("entries2[0] = %+v", entries2[0])
	}
	if entries2[1].Text != "more1" || entries2[2].Text != "more2" {
		t.Errorf("entries2[1:] = %+v", entries2[1:])
	}
}

func TestTranscriptTailRotatesToNewerFile(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()

	older := time.Now().Add(-10 * time.Minute)
	writeSessionFile(t, home, older, "rollout-old", wt, []string{
		`{"type":"event_msg","timestamp":"2026-07-19T08:20:00.000Z","payload":{"type":"user_message","message":"in old file"}}`,
	}, older)

	c := New()
	entries1, cursor1, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatalf("TranscriptTail() #1 error = %v", err)
	}
	if len(entries1) != 1 || entries1[0].Text != "in old file" {
		t.Fatalf("entries1 = %+v", entries1)
	}
	if !strings.Contains(cursor1, "rollout-old") {
		t.Fatalf("cursor1 = %q, want pointing at rollout-old", cursor1)
	}

	newer := time.Now()
	writeSessionFile(t, home, newer, "rollout-new", wt, []string{
		`{"type":"event_msg","timestamp":"2026-07-19T08:30:00.000Z","payload":{"type":"user_message","message":"in new file"}}`,
	}, newer)

	entries2, cursor2, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, cursor1)
	if err != nil {
		t.Fatalf("TranscriptTail() #2 error = %v", err)
	}
	if len(entries2) != 1 || entries2[0].Text != "in new file" {
		t.Fatalf("entries2 = %+v, want the new file's entry", entries2)
	}
	if !strings.Contains(cursor2, "rollout-new") {
		t.Errorf("cursor2 = %q, want pointing at rollout-new", cursor2)
	}
}

// TestTranscriptTailCursorOutsideSessionsRootFallsBackToNewest covers
// finding 1 of the final-review fix brief: a client-supplied cursor whose
// path points outside $CODEX_HOME/sessions/ must be treated as invalid
// (same fallback as an empty/malformed cursor), not read.
func TestTranscriptTailCursorOutsideSessionsRootFallsBackToNewest(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	writeSessionFile(t, home, now, "rollout-1", wt, []string{
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:06.210Z","payload":{"type":"user_message","message":"legit"}}`,
	}, now)

	// A file outside $CODEX_HOME/sessions/ that an untrusted cursor could
	// point at.
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "secret.jsonl")
	if err := os.WriteFile(outsidePath, []byte(`{"type":"event_msg","timestamp":"x","payload":{"type":"user_message","message":"SECRET"}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	c := New()
	entries, _, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, outsidePath+":0")
	if err != nil {
		t.Fatalf("TranscriptTail() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Text != "legit" {
		t.Fatalf("entries = %+v, want fallback to the legit session (outside-root cursor must be rejected)", entries)
	}
	for _, e := range entries {
		if strings.Contains(e.Text, "SECRET") {
			t.Fatalf("entries leaked outside-file content: %+v", entries)
		}
	}
}

func TestTranscriptTailNoMatchingSessionReturnsErrNoSignal(t *testing.T) {
	withCodexHome(t)
	wt := t.TempDir()

	c := New()
	entries, cursor, err := c.TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
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
	home := withCodexHome(t)
	wt := t.TempDir()

	mtime := time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	path := writeSessionFile(t, home, mtime, "rollout-1", wt, []string{
		`{"type":"event_msg","timestamp":"2026-07-19T08:28:06.210Z","payload":{"type":"user_message","message":"hi"}}`,
	}, mtime)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	c := New()
	gotMtime, gotSize, err := c.TranscriptStat(context.Background(), agent.ActivityRef{WorktreePath: wt})
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

func TestTranscriptStatNoMatchingSessionReturnsErrNoSignal(t *testing.T) {
	withCodexHome(t)
	wt := t.TempDir()

	c := New()
	_, _, err := c.TranscriptStat(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != agent.ErrNoSignal {
		t.Fatalf("err = %v, want ErrNoSignal", err)
	}
}
