package monitor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	_ "github.com/IvanRoslov/rocket/internal/agent/codex"
	"github.com/IvanRoslov/rocket/internal/store"
)

const codexAgent = "codex"

// startupAgent is a codex-like fake: no journal (Activity → ErrNoSignal)
// and a scriptable HasStartedTurn.
type startupAgent struct {
	fakeAgent
	mu      sync.Mutex
	started bool
	err     error
	calls   int
	since   time.Time
}

func (a *startupAgent) HasStartedTurn(ctx context.Context, ref agent.ActivityRef) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.since = ref.Since
	return a.started, a.err
}

func (a *startupAgent) lastSince() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.since
}

func (a *startupAgent) setStarted(v bool) {
	a.mu.Lock()
	a.started = v
	a.mu.Unlock()
}

var _ agent.StartupProber = (*startupAgent)(nil)

// startMonitor builds a monitor with codex session sess1 (parent orch1 when
// withParent) whose pane shows fixture and whose journal has no turn yet.
func startMonitor(t *testing.T, fixture string, withParent bool) (*Monitor, *store.Store, *permFakeRuntime, *startupAgent, *[]string) {
	t.Helper()
	rt := &permFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1", "orch1"}}, pane: pane(t, fixture)}
	m, st, _ := testMonitor(t, rt, &fakeProber{onlyShell: map[string]bool{}}, map[string]*fakeAgent{})
	ag := &startupAgent{fakeAgent: fakeAgent{err: agent.ErrNoSignal}, err: agent.ErrNoSignal}
	orch := &fakeAgent{state: activity.Ready}
	m.resolveAgent = func(name string) (agent.Agent, error) {
		switch name {
		case codexAgent:
			return ag, nil
		case claudeCodeAgent:
			return orch, nil
		}
		return nil, errors.New("agent not found: " + name)
	}
	var woken []string
	var wmu sync.Mutex
	m.SetWake(func(id string) {
		wmu.Lock()
		woken = append(woken, id)
		wmu.Unlock()
	})

	parent := ""
	if withParent {
		seedSession(t, st, store.Session{ID: "orch1", Kind: "orchestrator", Agent: claudeCodeAgent, TmuxName: "orch1"})
		parent = "orch1"
	}
	seedSession(t, st, store.Session{ID: "sess1", Agent: codexAgent, TmuxName: "sess1", ParentID: parent, Activity: string(activity.Ready)})
	return m, st, rt, ag, &woken
}

func messagesTo(t *testing.T, st *store.Store, to string) []store.Message {
	t.Helper()
	all, err := st.ListMessages(to, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Message
	for _, msg := range all {
		if msg.ToSession == to {
			out = append(out, msg)
		}
	}
	return out
}

func TestStartScreenBlocksAndAsksQuiz(t *testing.T) {
	m, st, _, _, _ := startMonitor(t, "codex-start-trust.pane", true)

	m.sweep(context.Background())

	if got := activityOf(t, st, "sess1"); got != string(activity.Blocked) {
		t.Errorf("activity = %q, want blocked", got)
	}
	q, ok := pendingOf(t, st)
	if !ok || !q.IsPermission() || !q.Permission.StartScreen {
		t.Fatalf("pending = %+v ok=%v, want a start-screen permission quiz", q, ok)
	}
	if q.Permission.Title != "Trust this folder?" {
		t.Errorf("quiz title = %q", q.Permission.Title)
	}
	var labels []string
	for _, o := range q.Questions[0].Options {
		labels = append(labels, o.Label)
	}
	if want := []string{"Trust and continue", "Quit"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("options = %q, want %q", labels, want)
	}
	if q.Raw == "" || q.Permission.PromptID == 0 {
		t.Errorf("raw/prompt id missing: %+v", q)
	}
}

func TestStartScreenAlertsParentOnce(t *testing.T) {
	m, st, _, _, woken := startMonitor(t, "codex-start-trust.pane", true)

	for i := 0; i < 3; i++ {
		m.sweep(context.Background())
	}

	msgs := messagesTo(t, st, "orch1")
	if len(msgs) != 1 {
		t.Fatalf("messages to parent = %d, want 1: %+v", len(msgs), msgs)
	}
	want := "[rocket] session sess1 is stuck on a Codex start screen: «Trust this folder?»"
	if !strings.HasPrefix(msgs[0].Body, want) {
		t.Errorf("body = %q, want prefix %q", msgs[0].Body, want)
	}
	for _, opt := range []string{"1. Trust and continue", "2. Quit"} {
		if !strings.Contains(msgs[0].Body, opt) {
			t.Errorf("body = %q, want it to list %q", msgs[0].Body, opt)
		}
	}
	if msgs[0].FromSession != "" || msgs[0].Status != "queued" {
		t.Errorf("message = %+v, want a queued system message", msgs[0])
	}
	if len(*woken) != 1 || (*woken)[0] != "orch1" {
		t.Errorf("woken = %v, want [orch1]", *woken)
	}
}

func TestStartScreenAlertsAgainForADifferentScreen(t *testing.T) {
	m, st, rt, _, _ := startMonitor(t, "codex-start-update.pane", true)

	m.sweep(context.Background())
	rt.setPane(pane(t, "codex-start-trust.pane"))
	// The pending quiz switches after permissionChangeThreshold sweeps.
	m.sweep(context.Background())
	m.sweep(context.Background())

	if n := len(messagesTo(t, st, "orch1")); n != 2 {
		t.Errorf("messages to parent = %d, want 2 (update, then trust)", n)
	}
	q, _ := pendingOf(t, st)
	if q.Permission == nil || q.Permission.Title != "Trust this folder?" {
		t.Errorf("pending = %+v, want the trust screen", q)
	}
}

func TestStartScreenWithoutParentLogsProblem(t *testing.T) {
	m, st, _, _, _ := startMonitor(t, "codex-start-daemon.pane", false)
	taskID, err := st.AddTask(store.Task{Title: "orch task", ProjectID: "proj1", SessionID: "sess1", Status: "in_progress"})
	if err != nil {
		t.Fatal(err)
	}

	m.sweep(context.Background())
	m.sweep(context.Background())

	logs, err := st.ListTaskLog(taskID, "problem")
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("problem logs = %d, want 1: %+v", len(logs), logs)
	}
	if !strings.Contains(logs[0].Body, "«Background server has incompatible feature settings»") {
		t.Errorf("log body = %q", logs[0].Body)
	}
}

func TestStartScreenSkippedAfterTurnStarted(t *testing.T) {
	m, st, rt, ag, _ := startMonitor(t, "codex-start-trust.pane", true)
	ag.setStarted(true)
	ag.err = nil

	m.sweep(context.Background())
	m.sweep(context.Background())

	if n := rt.captured(40); n != 0 {
		t.Errorf("pane captured %d times at the start-screen depth, want 0", n)
	}
	if ag.calls != 1 {
		t.Errorf("HasStartedTurn called %d times, want 1 (cached once true)", ag.calls)
	}
	if _, ok := pendingOf(t, st); ok {
		t.Error("a quiz was published for a started session")
	}
	if n := len(messagesTo(t, st, "orch1")); n != 0 {
		t.Errorf("messages to parent = %d, want 0", n)
	}
}

func TestStartScreenClearsWhenGone(t *testing.T) {
	m, st, rt, _, _ := startMonitor(t, "codex-start-trust.pane", true)
	m.sweep(context.Background())

	rt.setPane(pane(t, "codex-start-neg-idle.pane"))
	m.sweep(context.Background())
	m.sweep(context.Background())

	if _, ok := pendingOf(t, st); ok {
		t.Error("start-screen quiz still pending after the screen went away")
	}
	if got := activityOf(t, st, "sess1"); got != string(activity.Ready) {
		t.Errorf("activity = %q, want ready", got)
	}
}

func TestStartScreenClearsWhenTurnStarts(t *testing.T) {
	m, st, rt, ag, _ := startMonitor(t, "codex-start-trust.pane", true)
	m.sweep(context.Background())

	rt.setPane(pane(t, "codex-start-neg-idle.pane"))
	ag.setStarted(true)
	ag.err = nil
	ag.fakeAgent = fakeAgent{state: activity.Active}
	m.sweep(context.Background())

	if _, ok := pendingOf(t, st); ok {
		t.Error("start-screen quiz still pending after the first turn started")
	}
	if got := activityOf(t, st, "sess1"); got != string(activity.Active) {
		t.Errorf("activity = %q, want active", got)
	}
}

func TestStartScreenIgnoresAgentsWithoutProber(t *testing.T) {
	rt := &permFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1"}}, pane: pane(t, "codex-start-trust.pane")}
	agents := map[string]*fakeAgent{claudeCodeAgent: {state: activity.Ready}}
	m, st, _ := testMonitor(t, rt, &fakeProber{onlyShell: map[string]bool{}}, agents)
	seedSession(t, st, store.Session{ID: "sess1", Agent: claudeCodeAgent, TmuxName: "sess1"})

	m.sweep(context.Background())

	if _, ok := pendingOf(t, st); ok {
		t.Error("a quiz was published for an agent without StartupProber")
	}
	if got := activityOf(t, st, "sess1"); got == string(activity.Blocked) {
		t.Error("claude-code session marked blocked by the start-screen check")
	}
}

// A restored session relaunches in the same worktree: the cached «started»
// of the previous run must not hide a start screen of the new one.
func TestStartScreenCheckedAgainAfterRestore(t *testing.T) {
	m, st, rt, ag, _ := startMonitor(t, "codex-start-neg-idle.pane", true)
	ag.setStarted(true)
	ag.err = nil
	m.sweep(context.Background())
	if n := rt.captured(40); n != 0 {
		t.Fatalf("setup: captured %d times, want 0", n)
	}

	// Restore: a session.restored event after the launch, the old journal
	// no longer counts, and the new process shows the trust screen.
	restoredAt := time.Now().Unix() + 5
	if _, err := st.AppendEvent(store.Event{Type: "session.restored", SessionID: "sess1", TS: restoredAt}); err != nil {
		t.Fatal(err)
	}
	ag.setStarted(false)
	rt.setPane(pane(t, "codex-start-trust.pane"))
	m.sweep(context.Background())

	if got := activityOf(t, st, "sess1"); got != string(activity.Blocked) {
		t.Errorf("activity = %q, want blocked", got)
	}
	if q, ok := pendingOf(t, st); !ok || !q.IsStartScreen() {
		t.Errorf("pending = %+v, want a start-screen quiz", q)
	}
	if got, want := ag.lastSince(), time.Unix(restoredAt, 0); !got.Equal(want) {
		t.Errorf("HasStartedTurn got Since = %v, want the restore time %v", got, want)
	}
}

// The monitor bounds the probe by the session's launch: created_at when it
// was never restored.
func TestStartScreenProbeSinceIsCreatedAt(t *testing.T) {
	m, st, _, ag, _ := startMonitor(t, "codex-start-trust.pane", true)
	m.sweep(context.Background())
	sess, _ := st.GetSession("sess1")
	if got := ag.lastSince(); !got.Equal(time.Unix(sess.CreatedAt, 0)) {
		t.Errorf("Since = %v, want created_at %v", got, time.Unix(sess.CreatedAt, 0))
	}
}

// End to end with the real codex adapter: the previous run's journal (turn
// before the relaunch) and no new journal yet, trust on the pane.
func TestStartScreenRestoredCodexWithOldJournal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	wt := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	dir := filepath.Join(home, "sessions", old.Format("2006"), old.Format("01"), old.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := fmt.Sprintf(`{"type":"session_meta","timestamp":%q,"payload":{"cwd":%q}}`+"\n"+
		`{"type":"event_msg","timestamp":%q,"payload":{"type":"task_started","turn_id":"t1"}}`+"\n",
		old.UTC().Format(time.RFC3339Nano), wt, old.UTC().Format(time.RFC3339Nano))
	path := filepath.Join(dir, "rollout-old.jsonl")
	if err := os.WriteFile(path, []byte(journal), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	rt := &permFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1", "orch1"}}, pane: pane(t, "codex-start-trust.pane")}
	m, st, _ := testMonitor(t, rt, &fakeProber{onlyShell: map[string]bool{}}, map[string]*fakeAgent{})
	cx, err := agent.Get(codexAgent)
	if err != nil {
		t.Fatal(err)
	}
	m.resolveAgent = func(name string) (agent.Agent, error) { return cx, nil }
	seedSession(t, st, store.Session{ID: "orch1", Kind: "orchestrator", Agent: codexAgent, TmuxName: "orch1", WorktreePath: t.TempDir()})
	seedSession(t, st, store.Session{ID: "sess1", Agent: codexAgent, TmuxName: "sess1", ParentID: "orch1",
		WorktreePath: wt, CreatedAt: old.Add(-time.Hour).Unix()})
	if _, err := st.AppendEvent(store.Event{Type: "session.restored", SessionID: "sess1", TS: time.Now().Unix()}); err != nil {
		t.Fatal(err)
	}

	m.sweep(context.Background())

	if got := activityOf(t, st, "sess1"); got != string(activity.Blocked) {
		t.Errorf("activity = %q, want blocked", got)
	}
	if q, ok := pendingOf(t, st); !ok || !q.IsStartScreen() {
		t.Errorf("pending = %+v, want a start-screen quiz", q)
	}
}
