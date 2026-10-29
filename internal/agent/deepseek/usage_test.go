package deepseek

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func writeUsageFixture(t *testing.T, worktree, usage string) {
	t.Helper()
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	resolved, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		t.Fatal(err)
	}
	slug := strings.NewReplacer("/", "-", ".", "-").Replace(resolved)
	dir := filepath.Join(configDir, "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	record := fmt.Sprintf("{\"type\":\"assistant\",\"timestamp\":\"2026-10-29T10:00:00Z\",\"cwd\":%q,\"message\":{\"id\":\"message-1\",\"model\":\"deepseek-flash\",\"usage\":{%s}}}\n", worktree, usage)
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readUsage(t *testing.T, worktree string) agent.Usage {
	t.Helper()
	usage, err := New().(agent.UsageReader).Usage(context.Background(), worktree, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

func TestUsageCacheHitWithoutFabricatedCacheWrite(t *testing.T) {
	worktree := t.TempDir()
	writeUsageFixture(t, worktree, `"input_tokens":150,"cache_read_input_tokens":900,"output_tokens":20`)

	got := readUsage(t, worktree)
	want := map[string]agent.Tokens{
		"deepseek-flash": {Input: 150, CacheRead: 900, Output: 20, Messages: 1},
	}
	if !got.Found || !reflect.DeepEqual(got.Models, want) {
		t.Fatalf("Usage = %#v, want cache hit with no fabricated CacheWrite: %#v", got, want)
	}
}

func TestUsageCacheMiss(t *testing.T) {
	worktree := t.TempDir()
	writeUsageFixture(t, worktree, `"input_tokens":150,"output_tokens":20`)

	got := readUsage(t, worktree)
	want := map[string]agent.Tokens{
		"deepseek-flash": {Input: 150, Output: 20, Messages: 1},
	}
	if !got.Found || !reflect.DeepEqual(got.Models, want) {
		t.Fatalf("Usage = %#v, want cache miss with zero CacheRead/CacheWrite: %#v", got, want)
	}
}
