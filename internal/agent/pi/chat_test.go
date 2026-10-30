package pi

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func writeChatSession(t *testing.T, home, worktree string, lines []string) string {
	t.Helper()
	return writeUsageSession(t, home, worktree, "2026-10-29_s1.jsonl", lines)
}

func appendChatRecords(t *testing.T, path string, lines ...string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func chatTexts(entries []agent.ChatEntry) []string {
	texts := make([]string, len(entries))
	for i, entry := range entries {
		texts[i] = entry.Text
	}
	return texts
}

func TestTranscriptTailReadsActiveBranchEntries(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	longCommand := strings.Repeat("界", 140) + `\nsecond line`
	writeChatSession(t, home, worktree, []string{
		`{"type":"model_change","id":"model","parentId":null,"timestamp":"2026-10-29T17:00:00Z","provider":"deepseek","modelId":"deepseek-v4-pro"}`,
		`{"type":"message","id":"user","parentId":"model","timestamp":"2026-10-29T17:00:01Z","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"message","id":"assistant","parentId":"user","timestamp":"2026-10-29T17:00:02Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"world"},{"type":"toolCall","id":"call1","name":"bash","arguments":{"command":"` + longCommand + `"}}]}}`,
		`{"type":"message","id":"tool-result","parentId":"assistant","timestamp":"2026-10-29T17:00:03Z","message":{"role":"toolResult","toolCallId":"call1","toolName":"bash","content":[{"type":"text","text":"ok"}]}}`,
		`{"type":"usage","id":"usage","parentId":"tool-result","timestamp":"2026-10-29T17:00:04Z","provider":"deepseek","model":"deepseek-v4-pro","usage":{"input":1}}`,
		`{"type":"message","id":"abandoned","parentId":"user","timestamp":"2026-10-29T17:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"old branch"}]}}`,
		`{"type":"compaction","id":"compact","parentId":"usage","timestamp":"2026-10-29T17:00:06Z","summary":"hidden","usage":{"input":1}}`,
		`{"type":"branch_summary","id":"summary","parentId":"compact","fromId":"abandoned","timestamp":"2026-10-29T17:00:07Z","summary":"hidden","usage":{"input":1}}`,
	})

	entries, cursor, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: worktree, SessionID: "s1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []agent.ChatEntry{
		{Role: "user", Text: "hello", TS: time.Date(2026, 10, 29, 17, 0, 1, 0, time.UTC).Unix()},
		{Role: "assistant", Text: "world", TS: time.Date(2026, 10, 29, 17, 0, 2, 0, time.UTC).Unix()},
		{Role: "tool", ToolName: "bash", TS: time.Date(2026, 10, 29, 17, 0, 2, 0, time.UTC).Unix()},
	}
	if len(entries) != len(want) {
		t.Fatalf("entries = %+v; want %+v", entries, want)
	}
	for i := range want {
		if entries[i].Role != want[i].Role || entries[i].Text != want[i].Text && want[i].Role != "tool" || entries[i].ToolName != want[i].ToolName || entries[i].TS != want[i].TS {
			t.Errorf("entry %d = %+v; want %+v", i, entries[i], want[i])
		}
	}
	tool := entries[2]
	if len([]rune(tool.Text)) != 120 || strings.Contains(tool.Text, "\n") || !strings.HasPrefix(tool.Text, `{"command":"`) || !strings.HasSuffix(tool.Text, "…") {
		t.Errorf("tool digest = %q; want bounded compact single-line JSON", tool.Text)
	}
	for _, text := range chatTexts(entries) {
		if text == "old branch" || text == "hidden" || text == "ok" {
			t.Errorf("inactive or non-chat record emitted: %+v", entries)
		}
	}
	if !strings.HasSuffix(cursor, ":summary") {
		t.Fatalf("cursor = %q; want last valid branch record summary", cursor)
	}
}

func TestTranscriptTailReadsReconFixture(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	writeReconSession(t, home, worktree, "pi-session.jsonl", "2026-10-29_pi-recon.jsonl")

	entries, cursor, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: worktree, SessionID: "pi-recon"}, "")
	if err != nil {
		t.Fatal(err)
	}
	wantRoles := []string{"user", "tool", "user", "assistant", "user", "assistant"}
	if len(entries) != len(wantRoles) {
		t.Fatalf("entries = %+v; want roles %v", entries, wantRoles)
	}
	for i, role := range wantRoles {
		wantText := "<redacted>"
		if role == "tool" {
			wantText = `{"command":"<redacted>","timeout":30}`
		}
		if entries[i].Role != role || entries[i].Text != wantText {
			t.Errorf("entry %d = %+v; want role %s and text %q", i, entries[i], role, wantText)
		}
	}
	if entries[1].ToolName != "bash" || entries[0].TS != 1793294142 {
		t.Fatalf("real tool/timestamp entries = %+v", entries)
	}
	if !strings.HasSuffix(cursor, ":0d140ff3") {
		t.Fatalf("cursor = %q; want final real assistant ID", cursor)
	}
}

func TestTranscriptTailShowsProviderError(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	writeChatSession(t, home, worktree, []string{
		`{"type":"message","id":"error","parentId":null,"timestamp":"2026-10-29T17:00:00Z","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"401 unauthorized"}}`,
	})

	entries, _, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: worktree, SessionID: "s1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []agent.ChatEntry{{Role: "assistant", Text: "⚠ Pi turn failed: 401 unauthorized", TS: time.Date(2026, 10, 29, 17, 0, 0, 0, time.UTC).Unix()}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %+v; want %+v", entries, want)
	}
}

func TestTranscriptTailResumesAndResetsAcrossBranches(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	path := writeChatSession(t, home, worktree, []string{
		`{"type":"model_change","id":"model","parentId":null,"timestamp":"2026-10-29T17:00:00Z","provider":"deepseek","modelId":"deepseek-v4-pro"}`,
		`{"type":"message","id":"user","parentId":"model","timestamp":"2026-10-29T17:00:01Z","message":{"role":"user","content":[{"type":"text","text":"root"}]}}`,
		`{"type":"message","id":"old","parentId":"user","timestamp":"2026-10-29T17:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"old branch"}]}}`,
	})
	ref := agent.ActivityRef{WorktreePath: worktree, SessionID: "s1"}

	_, cursor, err := New().TranscriptTail(context.Background(), ref, "")
	if err != nil || !strings.HasSuffix(cursor, ":old") {
		t.Fatalf("initial cursor = %q, %v; want old", cursor, err)
	}
	entries, same, err := New().TranscriptTail(context.Background(), ref, cursor)
	if err != nil || len(entries) != 0 || same != cursor {
		t.Fatalf("repeat = %+v, %q, %v; want empty and unchanged", entries, same, err)
	}

	appendChatRecords(t, path, `{"type":"message","id":"next","parentId":"old","timestamp":"2026-10-29T17:00:03Z","message":{"role":"assistant","content":[{"type":"text","text":"next"}]}}`)
	entries, cursor, err = New().TranscriptTail(context.Background(), ref, cursor)
	if err != nil || !reflect.DeepEqual(chatTexts(entries), []string{"next"}) || !strings.HasSuffix(cursor, ":next") {
		t.Fatalf("append resume = %+v, %q, %v; want only next", entries, cursor, err)
	}

	appendChatRecords(t, path, `{"type":"message","id":"new","parentId":"user","timestamp":"2026-10-29T17:00:04Z","message":{"role":"user","content":[{"type":"text","text":"new branch"}]}}`)
	entries, cursor, err = New().TranscriptTail(context.Background(), ref, cursor)
	if err != nil || !reflect.DeepEqual(chatTexts(entries), []string{"root", "new branch"}) || !strings.HasSuffix(cursor, ":new") {
		t.Fatalf("branch reset = %+v, %q, %v; want current branch only", entries, cursor, err)
	}

	appendChatRecords(t, path, `{"type":"branch_summary","id":"summary","parentId":"new","timestamp":"2026-10-29T17:00:05Z","summary":"not chat","usage":{"input":1}}`)
	entries, cursor, err = New().TranscriptTail(context.Background(), ref, cursor)
	if err != nil || len(entries) != 0 || !strings.HasSuffix(cursor, ":summary") {
		t.Fatalf("non-chat advance = %+v, %q, %v; want empty with summary cursor", entries, cursor, err)
	}
}

func TestTranscriptTailSkipsMalformedAndPartialRecords(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	path := writeChatSession(t, home, worktree, []string{
		`{"type":"message","id":"user","parentId":null,"timestamp":"2026-10-29T17:00:00Z","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`,
		`{broken`,
		`{"type":"message","id":"assistant","parentId":"user","timestamp":"2026-10-29T17:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"world"}]}}`,
		`{"type":"message","id":"partial","parentId":"assistant"`,
	})
	ref := agent.ActivityRef{WorktreePath: worktree, SessionID: "s1"}
	entries, cursor, err := New().TranscriptTail(context.Background(), ref, "")
	if err != nil || !reflect.DeepEqual(chatTexts(entries), []string{"hello", "world"}) || !strings.HasSuffix(cursor, ":assistant") {
		t.Fatalf("partial tail = %+v, %q, %v; want two valid entries", entries, cursor, err)
	}

	appendChatRecords(t, path, `{"type":"message","id":"new","parentId":"assistant","timestamp":"2026-10-29T17:00:02Z","message":{"role":"user","content":[{"type":"text","text":"new"}]}}`)
	entries, _, err = New().TranscriptTail(context.Background(), ref, cursor)
	if err != nil || !reflect.DeepEqual(chatTexts(entries), []string{"new"}) {
		t.Fatalf("resume after partial = %+v, %v; want only new", entries, err)
	}
}

func TestTranscriptStatReportsExactSession(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	path, _ := seedSessionFiles(t, home, worktree, t.TempDir())
	mtime := time.Unix(200, 0)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	gotTime, gotSize, err := New().TranscriptStat(context.Background(), agent.ActivityRef{WorktreePath: worktree, SessionID: "s1"})
	if err != nil || gotTime != 200 || gotSize != info.Size() {
		t.Fatalf("TranscriptStat = %d, %d, %v; want 200, %d", gotTime, gotSize, err, info.Size())
	}
}

func TestTranscriptTailMissingTimestampUsesZero(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	writeChatSession(t, home, worktree, []string{
		`{"type":"message","id":"user","parentId":null,"message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`,
	})

	entries, _, err := New().TranscriptTail(context.Background(), agent.ActivityRef{WorktreePath: worktree, SessionID: "s1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].TS != 0 {
		t.Fatalf("entries = %+v; want one entry with TS=0", entries)
	}
}

func TestTranscriptNoJournalReturnsNoSignal(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	ref := agent.ActivityRef{WorktreePath: worktree, SessionID: "missing"}
	if _, _, err := New().TranscriptTail(context.Background(), ref, ""); !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("TranscriptTail error = %v; want ErrNoSignal", err)
	}
	if _, _, err := New().TranscriptStat(context.Background(), ref); !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("TranscriptStat error = %v; want ErrNoSignal", err)
	}
}

func TestTranscriptCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ref := agent.ActivityRef{WorktreePath: t.TempDir(), SessionID: "s1"}
	if _, _, err := New().TranscriptTail(ctx, ref, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("TranscriptTail error = %v; want context.Canceled", err)
	}
	if _, _, err := New().TranscriptStat(ctx, ref); !errors.Is(err, context.Canceled) {
		t.Fatalf("TranscriptStat error = %v; want context.Canceled", err)
	}
}
