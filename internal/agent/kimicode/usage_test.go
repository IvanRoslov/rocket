package kimicode

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

var _ agent.UsageReader = (*KimiCode)(nil)

func copyWireFixture(t *testing.T, sessionDir, agentID, fixture string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionDir, "agents", agentID, "wire.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUsageSumsModelsAndExcludesRecordsBeforeSince(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	session := writeState(t, home, "wd_"+filepath.Base(wt)+"_one", "session_one", wt, 1_000)
	path := filepath.Join(session, "agents", "main", "wire.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	rows := []string{
		`{"type":"usage.record","model":"old","usage":{"inputOther":999},"time":1000}`,
		`{"type":"usage.record","model":"model-a","usage":{"inputOther":3,"inputCacheRead":5,"inputCacheCreation":7,"output":11},"time":2000}`,
		`{broken`,
		`{"type":"usage.record","model":"model-a","usage":{"inputOther":13,"inputCacheRead":17,"inputCacheCreation":19,"output":23},"time":3000}`,
		`{"type":"usage.record","model":"model-b","usage":{"inputOther":29,"inputCacheRead":31,"inputCacheCreation":37,"output":41},"time":4000}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New().(agent.UsageReader).Usage(context.Background(), wt, time.UnixMilli(2000))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]agent.Tokens{
		"model-a": {Input: 16, CacheRead: 22, CacheWrite: 26, Output: 34, Messages: 2},
		"model-b": {Input: 29, CacheRead: 31, CacheWrite: 37, Output: 41, Messages: 1},
	}
	if !got.Found || !reflect.DeepEqual(got.Models, want) {
		t.Fatalf("Usage = %+v; want models %+v and Found", got, want)
	}
	if !got.FirstAt.Equal(time.UnixMilli(2000)) || !got.LastAt.Equal(time.UnixMilli(4000)) {
		t.Fatalf("usage times = %v..%v, want 2000..4000ms", got.FirstAt, got.LastAt)
	}
}

func TestUsageSumsAllSessionsAndChildJournalOnce(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	s1 := writeState(t, home, "wd_"+filepath.Base(wt)+"_one", "session_one", wt, 1793194497000)
	copyWireFixture(t, s1, "main", "completed-tool-turn.wire.jsonl")
	s2 := writeState(t, home, "wd_"+filepath.Base(wt)+"_two", "session_two", wt, 1793195833000)
	copyWireFixture(t, s2, "main", "subagent-parent.wire.jsonl")
	copyWireFixture(t, s2, "agent-0", "subagent-child.wire.jsonl")
	other := writeState(t, home, "wd_other_hash", "session_other", t.TempDir(), 1793195833000)
	copyWireFixture(t, other, "main", "completed-tool-turn.wire.jsonl")

	got, err := New().(agent.UsageReader).Usage(context.Background(), wt, time.UnixMilli(1793194497000))
	if err != nil {
		t.Fatal(err)
	}
	want := agent.Tokens{Input: 5512, CacheRead: 26112, Output: 190, Messages: 2}
	if !got.Found || !reflect.DeepEqual(got.Models, map[string]agent.Tokens{"kimi-code/k3.1-pro": want}) {
		t.Fatalf("Usage = %+v; want %+v once per usage.record", got, want)
	}
}

func TestUsageMissingAndEmptyJournal(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	reader := New().(agent.UsageReader)
	got, err := reader.Usage(context.Background(), wt, time.Time{})
	if err != nil || got.Found {
		t.Fatalf("missing Usage = %+v, %v; want Found=false", got, err)
	}
	session := writeState(t, home, "wd_"+filepath.Base(wt)+"_empty", "session_empty", wt, 1000)
	path := filepath.Join(session, "agents", "main", "wire.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = reader.Usage(context.Background(), wt, time.UnixMilli(1000))
	if err != nil || !got.Found || len(got.Models) != 0 {
		t.Fatalf("empty Usage = %+v, %v; want Found=true and no models", got, err)
	}
}

func TestUsageMatchesPhysicalCwdAndHandlesLongLines(t *testing.T) {
	home, wt := t.TempDir(), t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(wt, link); err != nil {
		t.Fatal(err)
	}
	session := writeState(t, home, "wd_stale_hash", "session_one", wt, 1000)
	path := filepath.Join(session, "agents", "main", "wire.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"agent.message.appended","message":{"message":{"role":"user","content":[{"type":"text","text":"` + strings.Repeat("x", 140_000) + `"}]}},"time":1000}` + "\n" + `{"type":"usage.record","model":"long","usage":{"inputOther":1},"time":1001}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := New().(agent.UsageReader).Usage(context.Background(), link, time.Time{})
	if err != nil || got.Models["long"].Input != 1 {
		t.Fatalf("Usage via symlink = %+v, %v", got, err)
	}
}

func TestUsageCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().(agent.UsageReader).Usage(ctx, t.TempDir(), time.Time{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Usage err = %v, want canceled", err)
	}
}

func TestUsageReadsJournalAfterWorktreeRemoval(t *testing.T) {
	home, wt := t.TempDir(), filepath.Join(t.TempDir(), "removed")
	t.Setenv("KIMI_CODE_HOME", home)
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	session := writeState(t, home, "wd_removed_hash", "session_one", wt, 1793194497730)
	copyWireFixture(t, session, "main", "completed-tool-turn.wire.jsonl")
	if err := os.Remove(wt); err != nil {
		t.Fatal(err)
	}
	got, err := New().(agent.UsageReader).Usage(context.Background(), wt, time.Time{})
	if err != nil || !got.Found || got.Models["kimi-code/k3.1-pro"].Input != 4075 {
		t.Fatalf("Usage after removal = %+v, %v; want historical usage", got, err)
	}
}

func TestUsageReturnsErrorForUnreadableMatchingJournalDiscovery(t *testing.T) {
	for _, broken := range []string{"state", "agents"} {
		t.Run(broken, func(t *testing.T) {
			home, wt := t.TempDir(), t.TempDir()
			t.Setenv("KIMI_CODE_HOME", home)
			good := writeState(t, home, "wd_"+filepath.Base(wt)+"_good", "session_good", wt, 1793194497730)
			copyWireFixture(t, good, "main", "completed-tool-turn.wire.jsonl")
			bad := writeState(t, home, "wd_"+filepath.Base(wt)+"_bad", "session_bad", wt, 1793194497730)
			var path string
			if broken == "state" {
				path = filepath.Join(bad, "state.json")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				path = filepath.Join(bad, "agents")
				if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := New().(agent.UsageReader).Usage(context.Background(), wt, time.Time{})
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("Usage err = %v, want discovery error naming %s", err, path)
			}
		})
	}
}
