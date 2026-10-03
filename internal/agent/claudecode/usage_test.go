package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

var _ agent.UsageReader = (*ClaudeCode)(nil)

func installUsageFixture(t *testing.T, dir, name, fixture, worktree string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "usage", fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), "__WORKTREE__", worktree))
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUsageAllMatchingClaudeTranscripts(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	worktree := t.TempDir()
	dir := transcriptDir(worktree)
	installUsageFixture(t, dir, "session-1.jsonl", "main.jsonl", worktree)
	installUsageFixture(t, dir, "session-2.jsonl", "second.jsonl", worktree)
	installUsageFixture(t, filepath.Join(dir, "session-1", "subagents"), "agent-a.jsonl", "subagent.jsonl", worktree)
	installUsageFixture(t, filepath.Join(dir, "orphan", "subagents"), "agent-b.jsonl", "subagent.jsonl", worktree)

	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got, err := (&ClaudeCode{}).Usage(context.Background(), worktree, since)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found {
		t.Fatal("Found=false, want true")
	}
	want := map[string]agent.Tokens{
		"claude-sonnet-5-5": {Input: 5, CacheWrite: 10, CacheRead: 3, Output: 7, Messages: 2},
		"claude-opus-5-5":   {Input: 4, CacheRead: 2, Output: 8, Messages: 1},
		"claude-haiku-4-5":  {Input: 1, CacheRead: 6, Output: 4, Messages: 1},
	}
	if !reflect.DeepEqual(got.Models, want) {
		t.Errorf("Models = %#v, want %#v", got.Models, want)
	}
	if wantFirst := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC); !got.FirstAt.Equal(wantFirst) {
		t.Errorf("FirstAt = %v, want %v", got.FirstAt, wantFirst)
	}
	if wantLast := time.Date(2026, 10, 3, 10, 7, 0, 0, time.UTC); !got.LastAt.Equal(wantLast) {
		t.Errorf("LastAt = %v, want %v", got.LastAt, wantLast)
	}
}

func TestUsageMissingClaudeDirectory(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	got, err := (&ClaudeCode{}).Usage(context.Background(), filepath.Join(t.TempDir(), "absent"), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Found || len(got.Models) != 0 || !got.FirstAt.IsZero() || !got.LastAt.IsZero() {
		t.Fatalf("Usage = %#v, want empty result", got)
	}
}

func TestUsageClaudeSinceExcludesOldFileBesideNewFile(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	worktree := t.TempDir()
	dir := transcriptDir(worktree)
	installUsageFixture(t, dir, "old.jsonl", "old.jsonl", worktree)
	installUsageFixture(t, dir, "new.jsonl", "second.jsonl", worktree)
	old := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "old.jsonl"), old, old); err != nil {
		t.Fatal(err)
	}
	got, err := (&ClaudeCode{}).Usage(context.Background(), worktree, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]agent.Tokens{"claude-sonnet-5-5": {Input: 3, Output: 2, Messages: 1}}
	if !got.Found || !reflect.DeepEqual(got.Models, want) {
		t.Fatalf("Usage = %#v, want only new file %#v", got, want)
	}
}

func TestUsageUnreadableClaudeFileReturnsNoPartialResult(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	worktree := t.TempDir()
	dir := transcriptDir(worktree)
	installUsageFixture(t, dir, "session-1.jsonl", "main.jsonl", worktree)
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "session-2.jsonl")); err != nil {
		t.Fatal(err)
	}
	got, err := (&ClaudeCode{}).Usage(context.Background(), worktree, time.Time{})
	if err == nil {
		t.Fatal("Usage error = nil, want unreadable file error")
	}
	if got.Found || len(got.Models) != 0 {
		t.Fatalf("Usage = %#v, want no partial result", got)
	}
}

func TestUsageClaudeReadsFull16MiBJSONLRecord(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	worktree := t.TempDir()
	dir := transcriptDir(worktree)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"type":"user","cwd":"` + worktree + `"}` + "\n" +
		strings.Repeat(" ", 16*1024*1024-2) + "{}\n" +
		`{"type":"assistant","cwd":"` + worktree + `","message":{"id":"after-large","model":"claude-sonnet-5-5","usage":{"input_tokens":1}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "large.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (&ClaudeCode{}).Usage(context.Background(), worktree, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Models["claude-sonnet-5-5"] != (agent.Tokens{Input: 1, Messages: 1}) {
		t.Fatalf("Usage = %#v, want assistant after large record", got)
	}
}
