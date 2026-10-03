package codex

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

var _ agent.UsageReader = (*Codex)(nil)

func usageDayDir(day time.Time) string {
	return filepath.Join(sessionsRoot(), day.Format("2006"), day.Format("01"), day.Format("02"))
}

func installCodexUsageFixture(t *testing.T, day time.Time, name, fixture, worktree string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "usage", fixture))
	if err != nil {
		t.Fatal(err)
	}
	dir := usageDayDir(day)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.ReplaceAll(string(data), "__WORKTREE__", worktree)
	content = strings.ReplaceAll(content, "__DAY__", day.Format("2006-01-02"))
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUsageCodexCumulativeModelChangesAndDateWindow(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	worktree := t.TempDir()
	today := time.Now().In(time.Local)
	since := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -2)
	installCodexUsageFixture(t, today, "rollout-a.jsonl", "main.jsonl", worktree)
	installCodexUsageFixture(t, today, "rollout-b.jsonl", "second.jsonl", worktree)
	installCodexUsageFixture(t, today, "rollout-c.jsonl", "foreign.jsonl", worktree)
	installCodexUsageFixture(t, since.AddDate(0, 0, -1), "rollout-old.jsonl", "second.jsonl", worktree)

	got, err := (&Codex{}).Usage(context.Background(), worktree, since)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]agent.Tokens{
		"gpt-6-sol":   {Input: 68, CacheWrite: 8, CacheRead: 42, Output: 23, Reasoning: 6, Messages: 2},
		"gpt-6-astra": {Input: 30, CacheWrite: 2, CacheRead: 20, Output: 15, Reasoning: 5, Messages: 1},
	}
	if !got.Found || !reflect.DeepEqual(got.Models, want) {
		t.Fatalf("Usage = %#v, want Models %#v", got, want)
	}
	wantFirst, _ := time.Parse(time.RFC3339, today.Format("2006-01-02")+"T09:00:00Z")
	wantLast, _ := time.Parse(time.RFC3339, today.Format("2006-01-02")+"T11:03:00Z")
	if !got.FirstAt.Equal(wantFirst) || !got.LastAt.Equal(wantLast) {
		t.Errorf("timestamps = %v..%v, want %v..%v", got.FirstAt, got.LastAt, wantFirst, wantLast)
	}
}

func TestUsageCodexSkipsOlderShard(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	worktree := t.TempDir()
	today := time.Now().In(time.Local)
	installCodexUsageFixture(t, today.AddDate(0, 0, -2), "rollout-old.jsonl", "second.jsonl", worktree)
	oldDir := usageDayDir(today.AddDate(0, 0, -2))
	if err := os.Symlink(filepath.Join(oldDir, "missing"), filepath.Join(oldDir, "rollout-unreadable.jsonl")); err != nil {
		t.Fatal(err)
	}
	got, err := (&Codex{}).Usage(context.Background(), worktree, today.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Found || len(got.Models) != 0 {
		t.Fatalf("Usage = %#v, want no matching transcript", got)
	}
}

func TestUsageCodexOldSameDayRolloutDoesNotCountAsFound(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	worktree := t.TempDir()
	today := time.Now().In(time.Local)
	installCodexUsageFixture(t, today, "rollout-old.jsonl", "same_day_old.jsonl", worktree)
	since, err := time.Parse(time.RFC3339, today.Format("2006-01-02")+"T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	got, err := (&Codex{}).Usage(context.Background(), worktree, since)
	if err != nil {
		t.Fatal(err)
	}
	if got.Found || len(got.Models) != 0 || !got.FirstAt.IsZero() || !got.LastAt.IsZero() {
		t.Fatalf("Usage = %#v, want missing transcript for new session", got)
	}
}

func TestUsageCodexResetDoesNotSubtractTokens(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	worktree := t.TempDir()
	today := time.Now().In(time.Local)
	installCodexUsageFixture(t, today, "rollout-reset.jsonl", "reset.jsonl", worktree)
	got, err := (&Codex{}).Usage(context.Background(), worktree, today.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	want := agent.Tokens{Input: 110, Output: 25, Messages: 1}
	if !got.Found || got.Models["gpt-6-sol"] != want {
		t.Fatalf("Usage = %#v, want %#v", got, want)
	}
}

func TestUsageCodexUnreadableFileReturnsNoPartialResult(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	worktree := t.TempDir()
	today := time.Now().In(time.Local)
	installCodexUsageFixture(t, today, "rollout-a.jsonl", "second.jsonl", worktree)
	if err := os.Symlink(filepath.Join(usageDayDir(today), "missing"), filepath.Join(usageDayDir(today), "rollout-z.jsonl")); err != nil {
		t.Fatal(err)
	}
	got, err := (&Codex{}).Usage(context.Background(), worktree, today.AddDate(0, 0, -1))
	if err == nil {
		t.Fatal("Usage error = nil, want unreadable file error")
	}
	if got.Found || len(got.Models) != 0 {
		t.Fatalf("Usage = %#v, want no partial result", got)
	}
}

func TestUsageCodexReadsFull16MiBJSONLRecord(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	worktree := t.TempDir()
	today := time.Now().In(time.Local)
	dir := usageDayDir(today)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"type":"session_meta","payload":{"cwd":"` + worktree + `"}}` + "\n" +
		`{"type":"turn_context","payload":{"model":"gpt-6-sol"}}` + "\n" +
		strings.Repeat(" ", 16*1024*1024-2) + "{}\n" +
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1,"total_tokens":1}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout-large.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (&Codex{}).Usage(context.Background(), worktree, today.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Models["gpt-6-sol"] != (agent.Tokens{Input: 1, Messages: 1}) {
		t.Fatalf("Usage = %#v, want token count after large record", got)
	}
}
