package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestActivityBenignTypeMentioningErrorSubstringIsReady(t *testing.T) {
	// A record of an unrecognized-but-well-formed type (e.g. response_item)
	// whose payload *text* happens to contain the literal `"type":"error"`
	// must not be misclassified as Blocked just because that substring
	// appears somewhere in the line — the JSON parsed successfully, so
	// classification must go strictly by the record's actual type field.
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()
	old := now.Add(-5 * time.Minute)

	writeSessionFile(t, home, now, "rollout-1", wt,
		[]string{`{"type":"response_item","timestamp":"x","payload":{"text":"I saw a log line containing \"type\":\"error\" in the output."}}`},
		old)

	c := New()
	state, _, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity failed: %v", err)
	}
	if state != activity.Ready {
		t.Errorf("state = %v, want Ready (benign type mentioning error substring must not flip to Blocked)", state)
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

func TestActivityFindsSessionInOldDateShard(t *testing.T) {
	home := withCodexHome(t)
	wt := t.TempDir()
	fiveDaysAgo := time.Now().Add(-5 * 24 * time.Hour)
	old := time.Now().Add(-5 * time.Minute)

	writeSessionFile(t, home, fiveDaysAgo, "rollout-old-shard", wt,
		[]string{`{"type":"event_msg","timestamp":"x","payload":{"type":"task_complete"}}`},
		old)

	c := New()
	state, _, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity failed to find session in a 5-day-old date shard: %v", err)
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

func TestActivityResolvesSymlinksInWorktreePath(t *testing.T) {
	home := withCodexHome(t)
	// Create a real directory and a symlink to it
	realDir := t.TempDir()
	symlinkDir := filepath.Join(t.TempDir(), "symlink")
	if err := os.Symlink(realDir, symlinkDir); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	now := time.Now()
	old := now.Add(-5 * time.Minute)

	// Write session with real path as cwd
	writeSessionFile(t, home, now, "rollout-1", realDir,
		[]string{`{"type":"event_msg","timestamp":"x","payload":{"type":"task_complete"}}`},
		old)

	// Query Activity with symlink path
	c := New()
	state, mtime, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: symlinkDir})
	if err != nil {
		t.Fatalf("Activity failed to match symlink to resolved path: %v", err)
	}
	if state != activity.Ready {
		t.Errorf("state = %v, want Ready", state)
	}
	if !mtime.Equal(old) {
		t.Errorf("mtime = %v, want %v", mtime, old)
	}
}

func TestActivityResolvesSymlinksInSessionCwd(t *testing.T) {
	home := withCodexHome(t)
	// Create a real directory and a symlink to it
	realDir := t.TempDir()
	symlinkDir := filepath.Join(t.TempDir(), "symlink")
	if err := os.Symlink(realDir, symlinkDir); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	now := time.Now()
	old := now.Add(-5 * time.Minute)

	// Write session with symlink path as cwd
	writeSessionFile(t, home, now, "rollout-1", symlinkDir,
		[]string{`{"type":"event_msg","timestamp":"x","payload":{"type":"task_complete"}}`},
		old)

	// Query Activity with real path
	c := New()
	state, mtime, err := c.Activity(context.TODO(), agent.ActivityRef{WorktreePath: realDir})
	if err != nil {
		t.Fatalf("Activity failed to match resolved path to symlink in session: %v", err)
	}
	if state != activity.Ready {
		t.Errorf("state = %v, want Ready", state)
	}
	if !mtime.Equal(old) {
		t.Errorf("mtime = %v, want %v", mtime, old)
	}
}

// turnFixtureLines reads testdata/activity/<name>.jsonl and returns its
// non-empty lines, to be appended after the session_meta line by
// writeSessionFile (the cwd in session_meta depends on the test's temp
// worktree, so it cannot live in the fixture itself).
func turnFixtureLines(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "activity", name+".jsonl"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// activityFromFixture writes a session file built from lines with an mtime
// 10 minutes in the past (well outside freshWindow, so the turn events are
// the only thing that can make the session Active) and returns Activity's
// state.
func activityFromFixture(t *testing.T, lines []string) activity.State {
	t.Helper()
	home := withCodexHome(t)
	wt := t.TempDir()
	now := time.Now()
	writeSessionFile(t, home, now, "rollout-1", wt, lines, now.Add(-10*time.Minute))

	state, _, err := New().Activity(context.TODO(), agent.ActivityRef{WorktreePath: wt})
	if err != nil {
		t.Fatalf("Activity failed: %v", err)
	}
	return state
}

func TestActivityActiveDuringOpenTurn(t *testing.T) {
	if got := activityFromFixture(t, turnFixtureLines(t, "open_turn")); got != activity.Active {
		t.Errorf("state = %v, want Active (task_started without task_complete, stale mtime)", got)
	}
}

func TestActivityReadyAfterTaskComplete(t *testing.T) {
	if got := activityFromFixture(t, turnFixtureLines(t, "completed_turn")); got != activity.Ready {
		t.Errorf("state = %v, want Ready", got)
	}
}

func TestActivityReadyAfterTurnAborted(t *testing.T) {
	if got := activityFromFixture(t, turnFixtureLines(t, "aborted_turn")); got != activity.Ready {
		t.Errorf("state = %v, want Ready", got)
	}
}

func TestActivityActiveWhenTurnStartBeyondTail(t *testing.T) {
	lines := turnFixtureLines(t, "open_turn")
	// A long command with lots of output: well over two 64 KB blocks of
	// records after task_started, none of them a turn event. One record is
	// itself larger than a block, so it straddles block boundaries.
	out := strings.Repeat("x", 2000)
	for i := 0; i < 80; i++ {
		lines = append(lines, fmt.Sprintf(`{"timestamp":"x","type":"response_item","payload":{"type":"function_call_output","call_id":"call-%d","output":%q}}`, i, out))
	}
	lines = append(lines, fmt.Sprintf(`{"timestamp":"x","type":"response_item","payload":{"type":"function_call_output","call_id":"big","output":%q}}`, strings.Repeat("y", 3*tailReadSize)))
	lines = append(lines, `{"timestamp":"x","type":"event_msg","payload":{"type":"token_count","info":null}}`)

	if got := activityFromFixture(t, lines); got != activity.Active {
		t.Errorf("state = %v, want Active (turn start lies beyond the 64 KB tail)", got)
	}
}

func TestActivityBlockedOnErrorStillWins(t *testing.T) {
	if got := activityFromFixture(t, turnFixtureLines(t, "open_turn_error")); got != activity.Blocked {
		t.Errorf("state = %v, want Blocked (error record wins over an open turn)", got)
	}
}

func TestLastTurnEvent(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, lines ...string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	started := `{"type":"event_msg","payload":{"type":"task_started","turn_id":"A"}}`
	complete := `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"A"}}`
	// A response_item whose text merely mentions a turn event must not count.
	mention := `{"type":"response_item","payload":{"type":"message","content":[{"type":"output_text","text":"{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\"}}"}]}}`

	cases := []struct {
		name      string
		path      string
		wantKind  string
		wantFound bool
	}{
		{"open", write("open.jsonl", started, `{"type":"event_msg","payload":{"type":"token_count"}}`), "task_started", true},
		{"complete", write("complete.jsonl", started, complete), "task_complete", true},
		{"no trailing newline partial", write("partial.jsonl", started, complete, `{"type":"event_msg","pay`), "task_complete", true},
		{"mention ignored", write("mention.jsonl", started, mention), "task_started", true},
		{"none", write("none.jsonl", `{"type":"session_meta","payload":{}}`), "", false},
		{"empty", write("empty.jsonl"), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, found, err := lastTurnEvent(tc.path)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if kind != tc.wantKind || found != tc.wantFound {
				t.Errorf("got (%q, %v), want (%q, %v)", kind, found, tc.wantKind, tc.wantFound)
			}
		})
	}

	if _, _, err := lastTurnEvent(filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Error("missing file: want error")
	}
}
