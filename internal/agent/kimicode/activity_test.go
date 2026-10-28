package kimicode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
)

// fakeKimi builds a throwaway Kimi home (KIMI_CODE_HOME) holding one session
// for a fresh worktree whose main journal is the given wire content. The
// journal's mtime is set to mtime. It never touches the real ~/.kimi-code.
func fakeKimi(t *testing.T, wire []byte, createdAt int64, mtime time.Time) (wt, wirePath string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	wt = filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := writeState(t, home, "wd_repo_0123456789ab", "session_x", wt, createdAt)
	wirePath = mainWire(dir)
	if err := os.MkdirAll(filepath.Dir(wirePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wirePath, wire, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(wirePath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return wt, wirePath
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var oldMtime = time.Now().Add(-10 * time.Minute)

func TestActivityFixtures(t *testing.T) {
	cases := []struct {
		fixture string
		want    activity.State
	}{
		{"open-busy-turn.wire.jsonl", activity.Active},
		{"completed-tool-turn.wire.jsonl", activity.Ready},
		{"queued-second-message.wire.jsonl", activity.Ready},
		{"failed-turn-synthetic.wire.jsonl", activity.Blocked},
		{"interaction-open.wire.jsonl", activity.Blocked},
		{"interaction-dismissed.wire.jsonl", activity.Ready},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			wt, _ := fakeKimi(t, fixture(t, tc.fixture), 1, oldMtime)
			got, ts, err := New().Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
			if err != nil || got != tc.want {
				t.Fatalf("Activity = %q, %v; want %q", got, err, tc.want)
			}
			if !ts.Equal(oldMtime.Truncate(time.Second)) && ts.Sub(oldMtime).Abs() > time.Second {
				t.Fatalf("ts = %v, want journal mtime %v", ts, oldMtime)
			}
		})
	}
}

func TestActivityFreshMtimeIsActive(t *testing.T) {
	wt, _ := fakeKimi(t, fixture(t, "completed-tool-turn.wire.jsonl"), 1, time.Now())
	got, _, err := New().Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil || got != activity.Active {
		t.Fatalf("Activity = %q, %v; want active", got, err)
	}
}

func TestActivityFailedTurnStaysBlockedWhenFresh(t *testing.T) {
	wt, _ := fakeKimi(t, fixture(t, "failed-turn-synthetic.wire.jsonl"), 1, time.Now())
	got, _, err := New().Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil || got != activity.Blocked {
		t.Fatalf("Activity = %q, %v; want blocked", got, err)
	}
}

func TestActivityNewTurnAfterFailureIsActive(t *testing.T) {
	wire := append(fixture(t, "failed-turn-synthetic.wire.jsonl"),
		[]byte(`{"type":"turn.prompt","agentId":"main","input":[{"type":"text","text":"again"}],"origin":{"kind":"user"},"turnId":1,"time":1793195020000}`+"\n")...)
	wt, _ := fakeKimi(t, wire, 1, oldMtime)
	got, _, err := New().Activity(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil || got != activity.Active {
		t.Fatalf("Activity = %q, %v; want active", got, err)
	}
}

func TestActivityNoJournalIsNoSignal(t *testing.T) {
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	_, _, err := New().Activity(context.Background(), agent.ActivityRef{WorktreePath: t.TempDir()})
	if !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("err = %v, want ErrNoSignal", err)
	}
}

// After a restore, the previous Kimi session in the same worktree must not
// speak for the new process until it writes its own journal.
func TestActivitySinceSkipsOldSession(t *testing.T) {
	wt, _ := fakeKimi(t, fixture(t, "completed-tool-turn.wire.jsonl"), 1_000, oldMtime)
	_, _, err := New().Activity(context.Background(), agent.ActivityRef{WorktreePath: wt, Since: time.UnixMilli(2_000)})
	if !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("err = %v, want ErrNoSignal", err)
	}
}

func TestLastTurnFailureReporter(t *testing.T) {
	r := New().(agent.TurnFailureReporter)
	wt, _ := fakeKimi(t, fixture(t, "failed-turn-synthetic.wire.jsonl"), 1, oldMtime)
	id, msg, ok, err := r.LastTurnFailure(context.Background(), agent.ActivityRef{WorktreePath: wt})
	if err != nil || !ok || id != "session_x/0@1793195011001" || msg != "HTTP 429: quota exhausted (synthetic)" {
		t.Fatalf("LastTurnFailure = %q %q %v %v", id, msg, ok, err)
	}
	wt, _ = fakeKimi(t, fixture(t, "completed-tool-turn.wire.jsonl"), 1, oldMtime)
	if _, _, ok, err := r.LastTurnFailure(context.Background(), agent.ActivityRef{WorktreePath: wt}); err != nil || ok {
		t.Fatalf("completed turn: ok=%v err=%v", ok, err)
	}
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	if _, _, _, err := r.LastTurnFailure(context.Background(), agent.ActivityRef{WorktreePath: t.TempDir()}); !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("no journal: err = %v", err)
	}
}
