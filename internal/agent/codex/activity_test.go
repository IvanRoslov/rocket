package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
)

// writeSessionFile writes a fake codex session JSONL file for cwd under
// home/sessions/<day:YYYY/MM/DD>/rollout-<name>.jsonl, with the given
// trailing lines appended after the session_meta line, and sets its mtime.
func writeSessionFile(t *testing.T, home string, day time.Time, name, cwd string, lines []string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", day.Format("2006"), day.Format("01"), day.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, name+".jsonl")

	meta := fmt.Sprintf(`{"type":"session_meta","timestamp":"2026-07-19T00:00:00Z","payload":{"id":"abc","cwd":%q}}`, cwd)
	all := append([]string{meta}, lines...)

	content := ""
	for _, l := range all {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return path
}

func TestActivityActiveOnFreshMtime(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	writeSessionFile(t, home, now, "rollout-1", wt,
		[]string{`{"type":"event_msg","timestamp":"x","payload":{"type":"task_complete","last_agent_message":"done"}}`},
		now.Add(-1*time.Second))

	c := New()
	state, _, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity failed: %v", err)
	}
	if state != activity.Active {
		t.Errorf("state = %v, want Active", state)
	}
}

func TestActivityReadyOnTaskComplete(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()
	old := now.Add(-5 * time.Minute)

	writeSessionFile(t, home, now, "rollout-1", wt,
		[]string{
			`{"type":"event_msg","timestamp":"x","payload":{"type":"token_count","usage":{}}}`,
			`{"type":"event_msg","timestamp":"x","payload":{"type":"task_complete","last_agent_message":"done","duration_ms":100}}`,
		},
		old)

	c := New()
	state, mtime, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity failed: %v", err)
	}
	if state != activity.Ready {
		t.Errorf("state = %v, want Ready", state)
	}
	if !mtime.Equal(old) {
		t.Errorf("mtime = %v, want %v", mtime, old)
	}
}

func TestActivityBlockedOnErrorEvent(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()
	old := now.Add(-5 * time.Minute)

	writeSessionFile(t, home, now, "rollout-1", wt,
		[]string{`{"type":"event_msg","timestamp":"x","payload":{"type":"error","message":"boom"}}`},
		old)

	c := New()
	state, _, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity failed: %v", err)
	}
	if state != activity.Blocked {
		t.Errorf("state = %v, want Blocked", state)
	}
}

func TestActivityUnknownTrailingTypeIsReady(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()
	old := now.Add(-5 * time.Minute)

	writeSessionFile(t, home, now, "rollout-1", wt,
		[]string{`{"type":"turn_context","timestamp":"x","payload":{"turn_id":"t1"}}`},
		old)

	c := New()
	state, _, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity failed: %v", err)
	}
	if state != activity.Ready {
		t.Errorf("state = %v, want Ready for unrecognized trailing record type", state)
	}
}

func TestActivityNoMatchingSessionReturnsErrNoSignal(t *testing.T) {
	withCodexHome(t)
	wt := t.TempDir()

	c := New()
	_, _, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != agent.ErrNoSignal {
		t.Errorf("err = %v, want ErrNoSignal", err)
	}
}

func TestActivityIgnoresNonMatchingCwd(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	other := t.TempDir()
	now := time.Now()
	old := now.Add(-5 * time.Minute)

	writeSessionFile(t, home, now, "rollout-other", other,
		[]string{`{"type":"event_msg","timestamp":"x","payload":{"type":"task_complete"}}`},
		old)

	c := New()
	_, _, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != agent.ErrNoSignal {
		t.Errorf("err = %v, want ErrNoSignal (session belongs to a different cwd)", err)
	}
}

func TestActivityFindsSessionInYesterdaysDir(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	yesterday := time.Now().Add(-24 * time.Hour)
	old := time.Now().Add(-5 * time.Minute)

	writeSessionFile(t, home, yesterday, "rollout-midnight", wt,
		[]string{`{"type":"event_msg","timestamp":"x","payload":{"type":"task_complete"}}`},
		old)

	c := New()
	state, _, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity failed to find session in yesterday's dir: %v", err)
	}
	if state != activity.Ready {
		t.Errorf("state = %v, want Ready", state)
	}
}

func TestActivityPicksNewestMatchingFile(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()

	older := now.Add(-10 * time.Minute)
	newer := now.Add(-1 * time.Minute)

	writeSessionFile(t, home, now, "rollout-older", wt,
		[]string{`{"type":"event_msg","timestamp":"x","payload":{"type":"error"}}`},
		older)
	writeSessionFile(t, home, now, "rollout-newer", wt,
		[]string{`{"type":"event_msg","timestamp":"x","payload":{"type":"task_complete"}}`},
		newer)

	c := New()
	state, mtime, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity failed: %v", err)
	}
	if !mtime.Equal(newer) {
		t.Errorf("mtime = %v, want newer file's mtime %v", mtime, newer)
	}
	if state != activity.Ready {
		t.Errorf("state = %v, want Ready (from the newer file, not the older error one)", state)
	}
}
