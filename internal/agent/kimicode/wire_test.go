package kimicode

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func writeState(t *testing.T, home, shard, session, cwd string, createdAt int64) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", shard, session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/state.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(b, &state); err != nil {
		t.Fatal(err)
	}
	// Kimi records the physical cwd (macOS temp dirs commonly start at a
	// logical /var path that resolves to /private/var).
	physical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	state["cwd"] = physical
	state["createdAt"] = createdAt
	b, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFindSessionNewestMatchingCwdAndSince(t *testing.T) {
	home := t.TempDir()
	wt := filepath.Join(t.TempDir(), "myrepo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	since := time.UnixMilli(1_000_000)
	writeState(t, home, "wd_myrepo_old", "session_old", wt, 999_999)
	want := writeState(t, home, "wd_myrepo_new", "session_new", wt, 1_000_002)
	writeState(t, home, "wd_myrepo_other", "session_other", t.TempDir(), 1_000_003)
	writeState(t, home, "wd_myrepo_older", "session_older", wt, 1_000_001)
	got, err := findSession(home, wt, since)
	if err != nil || got != want {
		t.Fatalf("findSession = %q, %v; want %q", got, err, want)
	}
}

func TestFindSessionResolvesSymlinkAndFallsBackToAllShards(t *testing.T) {
	home := t.TempDir()
	wt := filepath.Join(t.TempDir(), "physical")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "logical")
	if err := os.Symlink(wt, link); err != nil {
		t.Fatal(err)
	}
	want := writeState(t, home, "wd_stale_name_hash", "session_right", wt, 20)
	got, err := findSession(home, link, time.UnixMilli(20))
	if err != nil || got != want {
		t.Fatalf("findSession = %q, %v; want %q", got, err, want)
	}
}

func TestFindSessionFallsBackWhenPreferredShardHasNoMatchingCwd(t *testing.T) {
	home := t.TempDir()
	wt := filepath.Join(t.TempDir(), "myrepo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	writeState(t, home, "wd_myrepo_wrong", "session_other", t.TempDir(), 30)
	want := writeState(t, home, "wd_other_name_hash", "session_right", wt, 20)
	got, err := findSession(home, wt, time.UnixMilli(20))
	if err != nil || got != want {
		t.Fatalf("findSession = %q, %v; want %q", got, err, want)
	}
}

func TestFindSessionNoCurrentSession(t *testing.T) {
	home := t.TempDir()
	wt := t.TempDir()
	writeState(t, home, "wd_older_hash", "session_old", wt, 20)
	if _, err := findSession(home, wt, time.UnixMilli(21)); !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("findSession err = %v, want ErrNoSignal", err)
	}
}

func TestWirePathsIncludeMainAndSubagents(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"agent-1", "main", "agent-0"} {
		path := filepath.Join(dir, "agents", name, "wire.jsonl")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := mainWire(dir); got != filepath.Join(dir, "agents", "main", "wire.jsonl") {
		t.Fatalf("mainWire = %q", got)
	}
	want := []string{filepath.Join(dir, "agents", "agent-0", "wire.jsonl"), filepath.Join(dir, "agents", "agent-1", "wire.jsonl"), mainWire(dir)}
	got, err := agentWires(dir)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("agentWires = %v, %v; want %v", got, err, want)
	}
}

func TestReadTailHandlesLargeLinesAndBrokenRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.jsonl")
	big := strings.Repeat("x", 140_000)
	data := `{"type":"first","time":1000,"payload":"` + big + `"}` + "\n" + "{broken\n" + `{"type":"last","time":2000}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	records, err := readTail(path, nil)
	if err != nil || len(records) != 2 {
		t.Fatalf("readTail count = %d, err = %v", len(records), err)
	}
	if records[0].Type != "last" || records[0].Time.UnixMilli() != 2000 || records[1].Type != "first" || !strings.Contains(string(records[1].Raw), big) {
		t.Fatalf("records = %+v", records)
	}
	records, err = readTail(path, func(rec wireRecord) bool { return rec.Type == "last" })
	if err != nil || len(records) != 1 || records[0].Type != "last" {
		t.Fatalf("stopped records = %+v, err = %v", records, err)
	}
}
