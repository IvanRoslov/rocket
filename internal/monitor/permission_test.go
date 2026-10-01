package monitor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// permFakeRuntime is a fakeRuntime whose pane is scriptable and which
// records every capture depth asked for.
type permFakeRuntime struct {
	fakeRuntime
	mu    sync.Mutex
	pane  string
	depth []int
}

func (f *permFakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.depth = append(f.depth, lines)
	return f.pane, nil
}

func (f *permFakeRuntime) setPane(p string) {
	f.mu.Lock()
	f.pane = p
	f.mu.Unlock()
}

func (f *permFakeRuntime) captured(lines int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.depth {
		if d == lines {
			n++
		}
	}
	return n
}

func pane(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "runtime", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// permMonitor builds a monitor with one claude-code session sess1 whose
// agent reports waiting_input (the Notification hook fires on a permission
// dialog) and whose pane shows fixture.
func permMonitor(t *testing.T, fixture string) (*Monitor, *store.Store, *bus.Bus, *permFakeRuntime) {
	t.Helper()
	rt := &permFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1"}}, pane: pane(t, fixture)}
	agents := map[string]*fakeAgent{claudeCodeAgent: {state: activity.WaitingInput, ts: time.Now()}}
	m, st, b := testMonitor(t, rt, &fakeProber{onlyShell: map[string]bool{}}, agents)
	seedSession(t, st, store.Session{ID: "sess1", Agent: claudeCodeAgent, TmuxName: "sess1"})
	return m, st, b, rt
}

func pendingOf(t *testing.T, st *store.Store) (session.Quiz, bool) {
	t.Helper()
	sess, err := st.GetSession("sess1")
	if err != nil {
		t.Fatal(err)
	}
	return session.ParseQuiz(sess.PendingQuiz)
}

func eventCount(events []store.Event, typ string) int {
	n := 0
	for _, e := range events {
		if e.Type == typ && e.SessionID == "sess1" {
			n++
		}
	}
	return n
}

func permissionRows(t *testing.T, st *store.Store) []store.PermissionPromptRow {
	t.Helper()
	rows, err := st.ListResolvedPermissionPrompts("sess1")
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestPermissionAppearsAsPendingQuiz(t *testing.T) {
	m, st, b, _ := permMonitor(t, "permission-bash-rm.pane")
	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	q, ok := pendingOf(t, st)
	if !ok || !q.IsPermission() {
		t.Fatalf("pending = %+v ok=%v, want a permission quiz", q, ok)
	}
	want, _ := runtime.ParsePermissionPrompt(pane(t, "permission-bash-rm.pane"))
	if !q.PermissionPrompt().SameDialog(want) || len(q.Questions[0].Options) != len(want.Options) || q.Raw != want.Raw {
		t.Errorf("pending = %+v, want dialog %+v", q, want)
	}
	if q.AskedAt == 0 || q.Permission.PromptID == 0 {
		t.Errorf("asked_at=%d prompt_id=%d, want both set", q.AskedAt, q.Permission.PromptID)
	}
	if n := eventCount(drainEvents(ch), "session.quiz_asked"); n != 1 {
		t.Errorf("quiz_asked events = %d, want 1", n)
	}
	if rows := permissionRows(t, st); len(rows) != 0 {
		t.Errorf("resolved rows = %+v, want none while open", rows)
	}
}

func TestPermissionSameDialogIsStable(t *testing.T) {
	m, st, b, _ := permMonitor(t, "permission-bash-rm.pane")
	m.sweep(context.Background())
	first, _ := pendingOf(t, st)

	ch, cancel := b.Subscribe()
	defer cancel()
	for i := 0; i < 3; i++ {
		m.sweep(context.Background())
	}
	q, _ := pendingOf(t, st)
	if q.Permission.PromptID != first.Permission.PromptID || q.AskedAt != first.AskedAt {
		t.Errorf("pending changed while the same dialog stayed: %+v -> %+v", first.Permission, q.Permission)
	}
	events := drainEvents(ch)
	if n := eventCount(events, "session.quiz_asked") + eventCount(events, "session.quiz_resolved"); n != 0 {
		t.Errorf("quiz events = %d, want none", n)
	}
}

// TestPermissionNextDialogSwitchesCard covers two dialogs in a row: the
// agent asks the next Bash command (same title, different command).
func TestPermissionNextDialogSwitchesCard(t *testing.T) {
	m, st, b, rt := permMonitor(t, "permission-bash-rm.pane")
	m.sweep(context.Background())
	first, _ := pendingOf(t, st)

	ch, cancel := b.Subscribe()
	defer cancel()
	rt.setPane(pane(t, "permission-bash-outside-cwd.pane"))
	m.sweep(context.Background())

	q, ok := pendingOf(t, st)
	next, _ := runtime.ParsePermissionPrompt(pane(t, "permission-bash-outside-cwd.pane"))
	if !ok || !q.PermissionPrompt().SameDialog(next) {
		t.Fatalf("pending = %+v, want the second dialog", q.Permission)
	}
	if q.Permission.PromptID == first.Permission.PromptID {
		t.Errorf("second dialog reused the first journal row")
	}
	events := drainEvents(ch)
	if eventCount(events, "session.quiz_resolved") != 1 || eventCount(events, "session.quiz_asked") != 1 {
		t.Errorf("events = %+v, want one quiz_resolved and one quiz_asked", events)
	}
	rows := permissionRows(t, st)
	if len(rows) != 1 || rows[0].ID != first.Permission.PromptID || rows[0].AnsweredVia != "terminal" {
		t.Errorf("resolved rows = %+v, want the first one via terminal", rows)
	}
}

func TestPermissionGoneTwoTicksResolves(t *testing.T) {
	m, st, b, rt := permMonitor(t, "permission-bash-rm.pane")
	m.sweep(context.Background())
	q, _ := pendingOf(t, st)

	ch, cancel := b.Subscribe()
	defer cancel()
	rt.setPane(plainComposerTail)

	m.sweep(context.Background())
	if _, ok := pendingOf(t, st); !ok {
		t.Fatalf("pending cleared after one miss, want two")
	}
	m.sweep(context.Background())
	if _, ok := pendingOf(t, st); ok {
		t.Fatalf("pending still set after two misses")
	}
	if n := eventCount(drainEvents(ch), "session.quiz_resolved"); n != 1 {
		t.Errorf("quiz_resolved events = %d, want 1", n)
	}
	rows := permissionRows(t, st)
	if len(rows) != 1 || rows[0].ID != q.Permission.PromptID || rows[0].AnsweredVia != "terminal" || rows[0].ResolvedAt == 0 {
		t.Errorf("rows = %+v, want the dialog resolved via terminal", rows)
	}
}

func TestPermissionGoneAfterChatAnswerIsChat(t *testing.T) {
	m, st, _, rt := permMonitor(t, "permission-bash-rm.pane")
	m.sweep(context.Background())
	q, _ := pendingOf(t, st)
	if err := st.MarkPermissionPromptAnswered(q.Permission.PromptID, "Yes"); err != nil {
		t.Fatal(err)
	}

	rt.setPane(plainComposerTail)
	m.sweep(context.Background())
	m.sweep(context.Background())

	rows := permissionRows(t, st)
	if len(rows) != 1 || rows[0].AnsweredVia != "chat" || rows[0].AnswerLabel != "Yes" {
		t.Errorf("rows = %+v, want via chat with label Yes", rows)
	}
}

func TestPermissionDialogBackResetsMisses(t *testing.T) {
	m, st, _, rt := permMonitor(t, "permission-bash-rm.pane")
	m.sweep(context.Background())

	rt.setPane(plainComposerTail)
	m.sweep(context.Background()) // miss 1
	rt.setPane(pane(t, "permission-bash-rm.pane"))
	m.sweep(context.Background()) // back
	rt.setPane(plainComposerTail)
	m.sweep(context.Background()) // miss 1 again
	if _, ok := pendingOf(t, st); !ok {
		t.Fatalf("pending cleared after non-consecutive misses")
	}
}

func TestPermissionLeavesHookQuizAlone(t *testing.T) {
	m, st, b, _ := permMonitor(t, "permission-bash-rm.pane")
	hook := `{"questions":[{"question":"q","header":"h","multiSelect":false,"options":[]}],"asked_at":` + "1" + `}`
	_ = st.SetPendingQuiz("sess1", hook)
	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	sess, _ := st.GetSession("sess1")
	if sess.PendingQuiz != hook {
		t.Errorf("hook quiz replaced: %s", sess.PendingQuiz)
	}
	if n := eventCount(drainEvents(ch), "session.quiz_asked"); n != 0 {
		t.Errorf("quiz_asked = %d, want 0", n)
	}
}

func TestPermissionIgnoresOtherAgents(t *testing.T) {
	rt := &permFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1"}}, pane: pane(t, "permission-bash-rm.pane")}
	agents := map[string]*fakeAgent{"codex": {state: activity.WaitingInput, ts: time.Now()}}
	m, st, _ := testMonitor(t, rt, &fakeProber{onlyShell: map[string]bool{}}, agents)
	seedSession(t, st, store.Session{ID: "sess1", Agent: "codex", TmuxName: "sess1"})

	m.sweep(context.Background())
	if _, ok := pendingOf(t, st); ok {
		t.Errorf("permission quiz created for a codex session")
	}
}

func TestPermissionNotLookedForWhileWorking(t *testing.T) {
	rt := &permFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1"}}, pane: pane(t, "permission-bash-rm.pane")}
	agents := map[string]*fakeAgent{claudeCodeAgent: {state: activity.Active, ts: time.Now()}}
	m, st, _ := testMonitor(t, rt, &fakeProber{onlyShell: map[string]bool{}}, agents)
	seedSession(t, st, store.Session{ID: "sess1", Agent: claudeCodeAgent, TmuxName: "sess1"})

	m.sweep(context.Background())
	if _, ok := pendingOf(t, st); ok {
		t.Errorf("permission quiz created for an active session")
	}
	if n := rt.captured(runtime.PermissionCaptureLines); n != 0 {
		t.Errorf("captured the pane %d times for an active session, want 0", n)
	}
}

func TestPermissionLookedForWhenBlocked(t *testing.T) {
	rt := &permFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1"}}, pane: pane(t, "permission-bash-rm.pane")}
	agents := map[string]*fakeAgent{claudeCodeAgent: {state: activity.Blocked, ts: time.Now()}}
	m, st, _ := testMonitor(t, rt, &fakeProber{onlyShell: map[string]bool{}}, agents)
	seedSession(t, st, store.Session{ID: "sess1", Agent: claudeCodeAgent, TmuxName: "sess1"})

	m.sweep(context.Background())
	if q, ok := pendingOf(t, st); !ok || !q.IsPermission() {
		t.Errorf("no permission quiz for a blocked session")
	}
}

// TestPermissionCapturesAtSharedDepth pins the monitor to
// runtime.PermissionCaptureLines, the depth the answer path re-reads at.
func TestPermissionCapturesAtSharedDepth(t *testing.T) {
	m, _, _, rt := permMonitor(t, "permission-bash-multiline.pane")
	m.sweep(context.Background())
	if n := rt.captured(runtime.PermissionCaptureLines); n != 1 {
		t.Errorf("captures at %d rows = %d, want 1 (depths %v)", runtime.PermissionCaptureLines, n, rt.depth)
	}
}

// TestPermissionSurvivesRestart: a fresh monitor over the same store (the
// daemon restarted with the dialog still open) keeps the pending quiz and
// the journal row instead of opening a duplicate.
func TestPermissionSurvivesRestart(t *testing.T) {
	m, st, b, rt := permMonitor(t, "permission-bash-rm.pane")
	m.sweep(context.Background())
	before, _ := pendingOf(t, st)

	agents := map[string]*fakeAgent{claudeCodeAgent: {state: activity.WaitingInput, ts: time.Now()}}
	restarted := New(st, b, rt, m.cfg, func(name string) (agent.Agent, error) { return agents[name], nil })
	restarted.prober = &fakeProber{onlyShell: map[string]bool{}}
	restarted.sweep(context.Background())

	after, _ := pendingOf(t, st)
	if after.Permission.PromptID != before.Permission.PromptID || after.AskedAt != before.AskedAt {
		t.Errorf("pending changed across restart: %+v -> %+v", before.Permission, after.Permission)
	}

	// Lost pending (e.g. cleared by hand) with the row still open: the
	// restarted monitor re-publishes the card on the same row.
	_ = st.ClearPendingQuiz("sess1")
	restarted.sweep(context.Background())
	again, ok := pendingOf(t, st)
	if !ok || again.Permission.PromptID != before.Permission.PromptID {
		t.Errorf("re-published pending = %+v, want the same journal row %d", again.Permission, before.Permission.PromptID)
	}
	if rows := permissionRows(t, st); len(rows) != 0 {
		t.Errorf("resolved rows = %+v, want none (no duplicate closed)", rows)
	}
}

// TestPollQuizLeavesPermissionQuizAlone: the AskUserQuestion backstop must
// not read a permission dialog's pane (no quiz widget) as a closed quiz.
func TestPollQuizLeavesPermissionQuizAlone(t *testing.T) {
	m, st, _, _ := permMonitor(t, "permission-bash-rm.pane")
	p, _ := runtime.ParsePermissionPrompt(pane(t, "permission-bash-rm.pane"))
	id, _, _ := st.OpenPermissionPrompt("sess1", p.Title, p.Context, "[]", 1)
	q := session.NewPermissionQuiz(p, id, time.Now().Add(-time.Minute).Unix())
	raw, _ := json.Marshal(q)
	b := string(raw)
	_ = st.SetPendingQuiz("sess1", b)

	sess, _ := st.GetSession("sess1")
	for i := 0; i < 3; i++ {
		m.pollQuiz(context.Background(), sess)
	}
	if after, _ := st.GetSession("sess1"); after.PendingQuiz != b {
		t.Errorf("pollQuiz touched a permission quiz: %s", after.PendingQuiz)
	}
}
