package pi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

var _ agent.UsageReader = (*Pi)(nil)

func writeUsageSession(t *testing.T, home, worktree, name string, lines []string) string {
	t.Helper()
	path := writeSessionFile(t, home, worktree, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUsageIncludesAllRecordTypesAndBranches(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	writeUsageSession(t, home, worktree, "2026-10-29_first.jsonl", []string{
		`{"type":"model_change","id":"model","parentId":null,"timestamp":"2026-10-29T17:00:00Z","provider":"deepseek","modelId":"deepseek-v4-pro"}`,
		`{"type":"message","id":"assistant","parentId":"model","timestamp":"2026-10-29T17:00:01Z","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","usage":{"input":100,"cacheRead":20,"cacheWrite":10,"output":5,"reasoning":3}}}`,
		`{"type":"message","id":"abandoned","parentId":"assistant","timestamp":"2026-10-29T17:00:02Z","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","usage":{"input":7,"cacheRead":8,"cacheWrite":9,"output":10,"reasoning":11}}}`,
		`{"type":"branch_summary","id":"summary","parentId":"assistant","fromId":"abandoned","timestamp":"2026-10-29T17:00:03Z","usage":{"input":40,"cacheRead":41,"cacheWrite":42,"output":43,"reasoning":44}}`,
		`{"type":"compaction","id":"compact","parentId":"summary","timestamp":"2026-10-29T17:00:04Z","usage":{"input":30,"cacheRead":31,"cacheWrite":32,"output":33,"reasoning":34}}`,
		`{"type":"usage","id":"standalone","parentId":"compact","timestamp":"2026-10-29T17:00:05Z","kind":"cache_warm","provider":"openai","model":"gpt-x","usage":{"input":50,"cacheRead":51,"cacheWrite":52,"output":53,"reasoning":54}}`,
		`{"type":"message","id":"openai","parentId":"standalone","timestamp":"2026-10-29T17:00:06Z","message":{"role":"assistant","provider":"openai","model":"gpt-x","usage":{"input":60,"cacheRead":61,"cacheWrite":62,"output":63,"reasoning":64}}}`,
	})
	writeUsageSession(t, home, worktree, "2026-10-29_second.jsonl", []string{
		`{"type":"message","id":"flash","parentId":null,"timestamp":"2026-10-29T17:01:00Z","message":{"role":"assistant","provider":"deepseek","model":"deepseek-flash","usage":{"input":1,"cacheRead":2,"cacheWrite":3,"output":4,"reasoning":5}}}`,
	})

	reader := New().(agent.UsageReader)
	got, err := reader.Usage(context.Background(), worktree, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := agent.Usage{
		Found: true,
		Models: map[string]agent.Tokens{
			"deepseek/deepseek-v4-pro": {Input: 177, CacheRead: 100, CacheWrite: 93, Output: 91, Reasoning: 92, Messages: 4},
			"openai/gpt-x":             {Input: 110, CacheRead: 112, CacheWrite: 114, Output: 116, Reasoning: 118, Messages: 2},
			"deepseek/deepseek-flash":  {Input: 1, CacheRead: 2, CacheWrite: 3, Output: 4, Reasoning: 5, Messages: 1},
		},
		FirstAt: time.Date(2026, 10, 29, 17, 0, 0, 0, time.UTC),
		LastAt:  time.Date(2026, 10, 29, 17, 1, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Usage = %#v; want %#v", got, want)
	}

	again, err := reader.Usage(context.Background(), worktree, time.Time{})
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("second Usage = %#v, %v; polling must not double totals", again, err)
	}
}

func TestUsageMissingEmptyAndSince(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	reader := New().(agent.UsageReader)

	got, err := reader.Usage(context.Background(), worktree, time.Time{})
	if err != nil || got.Found {
		t.Fatalf("missing Usage = %#v, %v; want Found=false", got, err)
	}

	writeUsageSession(t, home, worktree, "2026-10-29_empty.jsonl", nil)
	got, err = reader.Usage(context.Background(), worktree, time.Time{})
	if err != nil || !got.Found || len(got.Models) != 0 {
		t.Fatalf("empty Usage = %#v, %v; want Found=true and no models", got, err)
	}

	writeUsageSession(t, home, worktree, "2026-10-29_empty.jsonl", []string{
		`{"type":"message","id":"old","timestamp":"2026-10-29T17:00:00Z","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","usage":{"input":1}}}`,
		`{"type":"message","id":"boundary","timestamp":"2026-10-29T17:00:01Z","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","usage":{"input":2}}}`,
		`{"type":"message","id":"new","timestamp":"2026-10-29T17:00:02Z","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","usage":{"input":10}}}`,
	})
	since := time.Date(2026, 10, 29, 17, 0, 1, 0, time.UTC)
	got, err = reader.Usage(context.Background(), worktree, since)
	if err != nil {
		t.Fatal(err)
	}
	if got.Models["deepseek/deepseek-v4-pro"].Input != 12 || got.Models["deepseek/deepseek-v4-pro"].Messages != 2 {
		t.Fatalf("Usage since boundary = %#v; want input 12 and two records", got)
	}
	if !got.FirstAt.Equal(since) || !got.LastAt.Equal(time.Date(2026, 10, 29, 17, 0, 2, 0, time.UTC)) {
		t.Fatalf("Usage times = %v..%v; want %v..17:00:02", got.FirstAt, got.LastAt, since)
	}
}

func TestUsageModelChangeBeforeSinceAttributesCompaction(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	writeUsageSession(t, home, worktree, "2026-10-29_model.jsonl", []string{
		`{"type":"model_change","id":"model","timestamp":"2026-10-29T17:00:00Z","provider":"deepseek","modelId":"deepseek-v4-pro"}`,
		`{"type":"compaction","id":"compact","timestamp":"2026-10-29T17:00:02Z","usage":{"input":8,"cacheRead":4,"cacheWrite":2,"output":1,"reasoning":3}}`,
	})

	got, err := New().(agent.UsageReader).Usage(context.Background(), worktree, time.Date(2026, 10, 29, 17, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := agent.Tokens{Input: 8, CacheRead: 4, CacheWrite: 2, Output: 1, Reasoning: 3, Messages: 1}
	if !reflect.DeepEqual(got.Models, map[string]agent.Tokens{"deepseek/deepseek-v4-pro": want}) {
		t.Fatalf("Usage after model_change boundary = %#v; want %#v", got.Models, want)
	}
}

func TestUsageMatchesPhysicalWorktree(t *testing.T) {
	home, worktree, other := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	writeUsageSession(t, home, worktree, "2026-10-29_target.jsonl", []string{
		`{"type":"message","id":"target","timestamp":"2026-10-29T17:00:00Z","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","usage":{"input":5}}}`,
	})
	writeUsageSession(t, home, other, "2026-10-29_other.jsonl", []string{
		`{"type":"message","id":"other","timestamp":"2026-10-29T17:00:00Z","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","usage":{"input":1000}}}`,
	})
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(worktree, link); err != nil {
		t.Fatal(err)
	}

	got, err := New().(agent.UsageReader).Usage(context.Background(), link, time.Time{})
	if err != nil || got.Models["deepseek/deepseek-v4-pro"].Input != 5 || got.Models["deepseek/deepseek-v4-pro"].Messages != 1 {
		t.Fatalf("Usage through symlink = %#v, %v; want only target journal", got, err)
	}
}

func TestUsageHandlesLargeAndMalformedRecords(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	large := strings.Repeat("x", 140_000)
	path := writeUsageSession(t, home, worktree, "2026-10-29_large.jsonl", []string{
		`{"type":"message","id":"large","timestamp":"2026-10-29T17:00:00Z","message":{"role":"assistant","provider":"deepseek","model":"deepseek-v4-pro","content":[{"type":"text","text":"` + large + `"}],"usage":{"input":1}}}`,
		`{"type":"usage","id":"valid","timestamp":"2026-10-29T17:00:01Z","provider":"deepseek","model":"deepseek-v4-pro","usage":{"input":2}}`,
	})
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{broken\n" + `{"type":"usage","id":"partial","provider":"deepseek"`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := New().(agent.UsageReader).Usage(context.Background(), worktree, time.Time{})
	if err != nil || got.Models["deepseek/deepseek-v4-pro"].Input != 3 || got.Models["deepseek/deepseek-v4-pro"].Messages != 2 {
		t.Fatalf("Usage with large/malformed records = %#v, %v; want two records totaling 3", got, err)
	}
}

func TestUsageCompactionInheritsPrecedingAssistantModel(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	writeUsageSession(t, home, worktree, "2026-10-29_assistant_model.jsonl", []string{
		`{"type":"message","id":"assistant","timestamp":"2026-10-29T17:00:00Z","message":{"role":"assistant","provider":"openai","model":"gpt-x","usage":{"input":1}}}`,
		`{"type":"compaction","id":"compact","timestamp":"2026-10-29T17:00:01Z","usage":{"input":9,"cacheRead":3,"cacheWrite":2,"output":4,"reasoning":2}}`,
		`{"type":"branch_summary","id":"summary","timestamp":"2026-10-29T17:00:02Z","usage":{"input":5,"output":1}}`,
	})

	got, err := New().(agent.UsageReader).Usage(context.Background(), worktree, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := agent.Tokens{Input: 15, CacheRead: 3, CacheWrite: 2, Output: 5, Reasoning: 2, Messages: 3}
	if !reflect.DeepEqual(got.Models, map[string]agent.Tokens{"openai/gpt-x": want}) {
		t.Fatalf("Usage models = %#v; want assistant attribution %#v", got.Models, want)
	}
}

func TestUsageModelChangeTakesPrecedenceOverAssistant(t *testing.T) {
	home, worktree := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	writeUsageSession(t, home, worktree, "2026-10-29_precedence.jsonl", []string{
		`{"type":"model_change","id":"model","timestamp":"2026-10-29T17:00:00Z","provider":"deepseek","modelId":"deepseek-v4-pro"}`,
		`{"type":"message","id":"assistant","timestamp":"2026-10-29T17:00:01Z","message":{"role":"assistant","provider":"openai","model":"gpt-x","usage":{"input":1}}}`,
		`{"type":"compaction","id":"compact","timestamp":"2026-10-29T17:00:02Z","usage":{"input":9,"output":4}}`,
		`{"type":"branch_summary","id":"summary","timestamp":"2026-10-29T17:00:03Z","usage":{"input":5,"output":2}}`,
	})

	got, err := New().(agent.UsageReader).Usage(context.Background(), worktree, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]agent.Tokens{
		"deepseek/deepseek-v4-pro": {Input: 14, Output: 6, Messages: 2},
		"openai/gpt-x":             {Input: 1, Messages: 1},
	}
	if !reflect.DeepEqual(got.Models, want) {
		t.Fatalf("Usage models = %#v; want model_change precedence %#v", got.Models, want)
	}
}

func writeReconSession(t *testing.T, home, worktree, fixture string, names ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	name := fixture
	if len(names) != 0 {
		name = names[0]
	}
	path := writeSessionFile(t, home, worktree, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUsageReadsReconFixtures(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		want    agent.Tokens
	}{
		{fixture: "pi-usage.jsonl", want: agent.Tokens{Input: 7, CacheRead: 5, CacheWrite: 2, Output: 3, Messages: 1}},
		{fixture: "pi-compaction.jsonl", want: agent.Tokens{Input: 745, Output: 3612, Reasoning: 2973, Messages: 1}},
		{fixture: "pi-branch-summary.jsonl", want: agent.Tokens{Input: 32389, CacheRead: 5120, Output: 224, Messages: 2}},
		{fixture: "pi-session.jsonl", want: agent.Tokens{Input: 5221, CacheRead: 9984, Output: 116, Reasoning: 32, Messages: 3}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			home, worktree := t.TempDir(), t.TempDir()
			t.Setenv("PI_CODING_AGENT_DIR", home)
			writeReconSession(t, home, worktree, tc.fixture)
			got, err := New().(agent.UsageReader).Usage(context.Background(), worktree, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if !got.Found || !reflect.DeepEqual(got.Models, map[string]agent.Tokens{"deepseek/deepseek-v4-pro": tc.want}) {
				t.Fatalf("Usage = %#v; want %#v", got, tc.want)
			}
			if got.FirstAt.IsZero() || got.LastAt.IsZero() {
				t.Fatalf("Usage timestamps = %v..%v; want real fixture times", got.FirstAt, got.LastAt)
			}
		})
	}
}

func TestUsageReturnsJournalReadErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.jsonl")
	result := agent.Usage{Models: make(map[string]agent.Tokens)}
	err := scanPiUsage(context.Background(), path, time.Time{}, &result)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("scanPiUsage error = %v; want read error naming %s", err, path)
	}
}

func TestUsageCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().(agent.UsageReader).Usage(ctx, t.TempDir(), time.Time{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Usage error = %v; want context.Canceled", err)
	}
}
