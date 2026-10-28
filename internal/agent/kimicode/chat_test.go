package kimicode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func TestTranscriptTailReadsRealMessagesAndOneToolCall(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	session := writeState(t, home, "wd_"+filepath.Base(wt)+"_one", "session_one", wt, 1793194497730)
	copyWireFixture(t, session, "main", "completed-tool-turn.wire.jsonl")

	entries, cursor, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []agent.ChatEntry{
		{Role: "user", Text: "Use shell to run sleep 12 then printf tool-ok; reply DONE.", TS: time.UnixMilli(1793194497779).Unix()},
		{Role: "tool", ToolName: "Bash", Text: "sleep 12 && printf tool-ok", TS: time.UnixMilli(1793194523631).Unix()},
		{Role: "assistant", Text: "DONE", TS: time.UnixMilli(1793194545265).Unix()},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %+v; want %+v", entries, want)
	}
	if !strings.HasPrefix(cursor, mainWire(session)+":") {
		t.Fatalf("cursor = %q; want main wire path and offset", cursor)
	}
	more, nextCursor, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, cursor)
	if err != nil || len(more) != 0 || nextCursor != cursor {
		t.Fatalf("resume = %+v, %q, %v; want no entries and same cursor", more, nextCursor, err)
	}
	file, err := os.OpenFile(mainWire(session), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"type":"agent.message.appended","message":{"message":{"role":"assistant","content":[{"type":"text","text":"NEXT"}]}},"time":1793194546000}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	more, nextCursor, err = New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, cursor)
	if err != nil || !reflect.DeepEqual(more, []agent.ChatEntry{{Role: "assistant", Text: "NEXT", TS: time.UnixMilli(1793194546000).Unix()}}) || nextCursor == cursor {
		t.Fatalf("resume after append = %+v, %q, %v", more, nextCursor, err)
	}
}

func TestTranscriptTailShowsFailedTurnErrorFromFixture(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	session := writeState(t, home, "wd_"+filepath.Base(wt)+"_failed", "session_failed", wt, 1793195010000)
	copyWireFixture(t, session, "main", "failed-turn-synthetic.wire.jsonl")

	entries, _, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []agent.ChatEntry{{
		Role: "assistant",
		Text: "⚠ Kimi turn failed: HTTP 429: quota exhausted (synthetic)",
		TS:   time.UnixMilli(1793195011001).Unix(),
	}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("failed turn entries = %+v; want %+v", entries, want)
	}
}

func TestTranscriptTailShowsOnlyFailedOrBlockedTurnsWithError(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	session := writeState(t, home, "wd_"+filepath.Base(wt)+"_errors", "session_errors", wt, 1000)
	path := mainWire(session)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	longError := strings.Repeat("quota exhausted; ", 10)
	data := `{"type":"turn.ended","reason":"completed","error":{"message":"ignored"},"time":1000}` + "\n" +
		`{"type":"turn.ended","reason":"cancelled","error":{"message":"ignored"},"time":2000}` + "\n" +
		`{"type":"turn.ended","reason":"failed","error":{"message":""},"time":3000}` + "\n" +
		fmt.Sprintf(`{"type":"turn.ended","reason":"blocked","error":{"message":%q},"time":4000}`, longError) + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, _, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []agent.ChatEntry{{Role: "assistant", Text: "⚠ Kimi turn failed: " + longError, TS: 4}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("terminal turn entries = %+v; want %+v", entries, want)
	}
}

func TestTranscriptTailWaitsForCompleteLineAndSkipsMalformed(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	session := writeState(t, home, "wd_"+filepath.Base(wt)+"_one", "session_one", wt, 1000)
	path := mainWire(session)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"agent.message.appended","message":{"message":{"role":"user","content":[{"type":"text","text":"hello"}]}},"time":1000}` + "\n" + "{broken\n" + `{"type":"agent.message.appended","message":{"message":{"role":"assistant","content":[{"type":"text","text":"world"}]}},"time":2000}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := agent.ActivityRef{WorktreePath: wt}
	entries, cursor, err := New().TranscriptTail(context.Background(), ref, "")
	if err != nil || !reflect.DeepEqual(entries, []agent.ChatEntry{{Role: "user", Text: "hello", TS: 1}}) {
		t.Fatalf("incomplete tail = %+v, %q, %v", entries, cursor, err)
	}
	if err := os.WriteFile(path, []byte(data+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _, err = New().TranscriptTail(context.Background(), ref, cursor)
	if err != nil || !reflect.DeepEqual(entries, []agent.ChatEntry{{Role: "assistant", Text: "world", TS: 2}}) {
		t.Fatalf("completed tail = %+v, %v", entries, err)
	}
}

func TestTranscriptTailResetsCursorForRestoredSession(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	old := writeState(t, home, "wd_"+filepath.Base(wt)+"_one", "session_old", wt, 1000)
	oldWire := mainWire(old)
	if err := os.MkdirAll(filepath.Dir(oldWire), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldWire, []byte(`{"type":"agent.message.appended","message":{"message":{"role":"user","content":[{"type":"text","text":"old"}]}},"time":1000}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldEntries, oldCursor, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil || len(oldEntries) != 1 {
		t.Fatalf("old tail = %+v, %v", oldEntries, err)
	}
	current := writeState(t, home, "wd_"+filepath.Base(wt)+"_two", "session_new", wt, 3000)
	newWire := mainWire(current)
	if err := os.MkdirAll(filepath.Dir(newWire), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newWire, []byte(`{"type":"agent.message.appended","message":{"message":{"role":"user","content":[{"type":"text","text":"new"}]}},"time":3000}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, cursor, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt, Since: time.UnixMilli(2000)}, oldCursor)
	if err != nil || !reflect.DeepEqual(entries, []agent.ChatEntry{{Role: "user", Text: "new", TS: 3}}) || !strings.HasPrefix(cursor, newWire+":") {
		t.Fatalf("restored tail = %+v, %q, %v", entries, cursor, err)
	}
}

func TestTranscriptTailToolDigestIsSingleLineAndRuneBounded(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	session := writeState(t, home, "wd_"+filepath.Base(wt)+"_one", "session_one", wt, 1000)
	path := mainWire(session)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	command := strings.Repeat("界", 140) + "\n second line"
	line := `{"type":"context.append_loop_event","event":{"type":"tool.call","name":"Bash","display":{"command":"` + strings.Repeat("界", 140) + `\n second line"}},"time":1000}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: wt}, "")
	if err != nil || len(entries) != 1 || entries[0].ToolName != "Bash" || len([]rune(entries[0].Text)) > 120 || strings.Contains(entries[0].Text, "\n") || !strings.HasPrefix(command, strings.TrimSuffix(entries[0].Text, "…")) {
		t.Fatalf("digest entries = %+v, err = %v", entries, err)
	}
}

func TestTranscriptStatReportsCurrentMainJournal(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	session := writeState(t, home, "wd_"+filepath.Base(wt)+"_one", "session_one", wt, 1000)
	path := copyWireFixture(t, session, "main", "completed-tool-turn.wire.jsonl")
	mtime := time.Unix(200, 0)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	gotTime, gotSize, err := New().TranscriptStat(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil || gotTime != 200 || gotSize != info.Size() {
		t.Fatalf("TranscriptStat = %d, %d, %v; want 200, %d", gotTime, gotSize, err, info.Size())
	}
}

func TestTranscriptNoJournalReturnsNoSignal(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	ref := agent.ActivityRef{WorktreePath: wt}
	if _, _, err := New().TranscriptTail(context.Background(), ref, ""); !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("TranscriptTail err = %v, want ErrNoSignal", err)
	}
	if _, _, err := New().TranscriptStat(context.Background(), ref); !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("TranscriptStat err = %v, want ErrNoSignal", err)
	}
}
