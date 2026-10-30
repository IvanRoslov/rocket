package pi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
)

var piOldMtime = time.Now().Add(-10 * time.Minute)

func piFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fakePiSession(t *testing.T, contents, sessionID string, mtime time.Time) (worktree, path string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", home)
	worktree = t.TempDir()
	path = writeSessionFile(t, home, worktree, "2026-10-29_"+sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return worktree, path
}

func firstPiLines(text string, n int) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	return strings.Join(lines[:n], "\n") + "\n"
}

func piMessageLine(t *testing.T, id, parentID, role, text string, at int64) string {
	t.Helper()
	return piMessageLineWithStop(t, id, parentID, role, text, at, "", "")
}

func piMessageLineWithStop(t *testing.T, id, parentID, role, text string, at int64, stopReason, errorMessage string) string {
	t.Helper()
	rec := map[string]any{
		"type":      "message",
		"id":        id,
		"parentId":  parentID,
		"timestamp": time.UnixMilli(at).UTC().Format(time.RFC3339Nano),
		"message": map[string]any{
			"role":         role,
			"content":      []map[string]string{{"type": "text", "text": text}},
			"stopReason":   stopReason,
			"errorMessage": errorMessage,
		},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func TestActivity(t *testing.T) {
	session := piFixture(t, "pi-session.jsonl")
	providerError := piFixture(t, "pi-provider-error.jsonl")
	for _, tc := range []struct {
		name  string
		wire  string
		mtime time.Time
		want  activity.State
	}{
		{name: "open user turn", wire: firstPiLines(session, 5), mtime: piOldMtime, want: activity.Active},
		{name: "unfinished tool", wire: firstPiLines(session, 6), mtime: piOldMtime, want: activity.Active},
		{name: "final assistant", wire: session, mtime: piOldMtime, want: activity.Ready},
		{name: "provider error", wire: providerError, mtime: piOldMtime, want: activity.Blocked},
		{name: "fresh final assistant", wire: session, mtime: time.Now(), want: activity.Active},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wt, _ := fakePiSession(t, tc.wire, "s1", tc.mtime)
			got, ts, err := New().Activity(context.Background(), agent.ActivityRef{SessionID: "s1", WorktreePath: wt})
			if err != nil || got != tc.want {
				t.Fatalf("Activity = %q, %v; want %q", got, err, tc.want)
			}
			if ts.Sub(tc.mtime).Abs() > time.Second {
				t.Fatalf("timestamp = %v, want journal mtime %v", ts, tc.mtime)
			}
		})
	}

	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	_, _, err := New().Activity(context.Background(), agent.ActivityRef{SessionID: "missing", WorktreePath: t.TempDir()})
	if !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("missing journal error = %v, want ErrNoSignal", err)
	}
}

func TestActivityNoConversationIsNoSignal(t *testing.T) {
	wt, _ := fakePiSession(t, firstPiLines(piFixture(t, "pi-session.jsonl"), 4), "s1", piOldMtime)
	_, _, err := New().Activity(context.Background(), agent.ActivityRef{SessionID: "s1", WorktreePath: wt})
	if !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("error = %v, want ErrNoSignal before the first user turn", err)
	}
}

func TestActivityMetadataDoesNotChangeState(t *testing.T) {
	wire := piFixture(t, "pi-session.jsonl") +
		`{"type":"usage","id":"u1","parentId":"0d140ff3","timestamp":"2026-10-29T17:18:00Z"}` + "\n" +
		`{"type":"custom","id":"c1","parentId":"u1","timestamp":"2026-10-29T17:18:01Z"}` + "\n" +
		`{"type":"model_change","id":"m2","parentId":"c1","timestamp":"2026-10-29T17:18:02Z","provider":"deepseek","modelId":"deepseek-v4-pro"}` + "\n"
	wt, _ := fakePiSession(t, wire, "s1", piOldMtime)
	got, _, err := New().Activity(context.Background(), agent.ActivityRef{SessionID: "s1", WorktreePath: wt})
	if err != nil || got != activity.Ready {
		t.Fatalf("Activity = %q, %v; want ready", got, err)
	}
}

func TestActivityUsesActiveBranch(t *testing.T) {
	wire := `{"type":"session","id":"s1","timestamp":"2026-10-29T17:14:03.557Z"}` + "\n" +
		piMessageLine(t, "u1", "", "user", "start", 1000) +
		piMessageLineWithStop(t, "abandoned", "u1", "assistant", "", 2000, "error", "old failure") +
		piMessageLineWithStop(t, "current", "u1", "assistant", "done", 3000, "stop", "")
	wt, _ := fakePiSession(t, wire, "s1", piOldMtime)
	got, _, err := New().Activity(context.Background(), agent.ActivityRef{SessionID: "s1", WorktreePath: wt})
	if err != nil || got != activity.Ready {
		t.Fatalf("Activity = %q, %v; want ready from the active branch", got, err)
	}
}

func TestLastTurnFailure(t *testing.T) {
	wt, _ := fakePiSession(t, piFixture(t, "pi-provider-error.jsonl"), "s1", piOldMtime)
	r := New().(agent.TurnFailureReporter)
	id, msg, ok, err := r.LastTurnFailure(context.Background(), agent.ActivityRef{SessionID: "s1", WorktreePath: wt})
	if err != nil || !ok || id != "s1/89673fb3" || msg != "401: <redacted-authentication-error>" {
		t.Fatalf("LastTurnFailure = %q %q %v %v", id, msg, ok, err)
	}

	wt, _ = fakePiSession(t, piFixture(t, "pi-session.jsonl"), "s1", piOldMtime)
	if _, _, ok, err := r.LastTurnFailure(context.Background(), agent.ActivityRef{SessionID: "s1", WorktreePath: wt}); err != nil || ok {
		t.Fatalf("completed turn: ok=%v err=%v", ok, err)
	}

	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	if _, _, _, err := r.LastTurnFailure(context.Background(), agent.ActivityRef{SessionID: "missing", WorktreePath: t.TempDir()}); !errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("missing journal error = %v, want ErrNoSignal", err)
	}
}

func TestActivityReadErrorPropagates(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(home, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", home)
	_, _, err := New().Activity(context.Background(), agent.ActivityRef{SessionID: "s1", WorktreePath: t.TempDir()})
	if err == nil || errors.Is(err, agent.ErrNoSignal) {
		t.Fatalf("error = %v, want real session-store read failure", err)
	}
}
