package monitor

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// refAgent records the ActivityRef of every Activity call.
type refAgent struct {
	fakeAgent
	mu   sync.Mutex
	refs []agent.ActivityRef
}

func (a *refAgent) Activity(ctx context.Context, ref agent.ActivityRef) (activity.State, time.Time, error) {
	a.mu.Lock()
	a.refs = append(a.refs, ref)
	a.mu.Unlock()
	return a.fakeAgent.Activity(ctx, ref)
}

func (a *refAgent) lastRef() agent.ActivityRef {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.refs[len(a.refs)-1]
}

// Activity gets the session's launch time as Since — created_at, then the
// latest restore — so an adapter can ignore an earlier run's journal in
// the same worktree (Kimi after restore). The returned state is used as is.
func TestActivityReceivesLaunchSince(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	m, st, _ := testMonitor(t, rt, &fakeProber{onlyShell: map[string]bool{}}, map[string]*fakeAgent{})
	ag := &refAgent{fakeAgent: fakeAgent{state: activity.Active, ts: time.Now()}}
	m.resolveAgent = func(string) (agent.Agent, error) { return ag, nil }
	seedSession(t, st, store.Session{ID: "sess1", Agent: claudeCodeAgent, TmuxName: "sess1"})
	sess, err := st.GetSession("sess1")
	if err != nil {
		t.Fatal(err)
	}

	m.sweep(context.Background())
	if got := ag.lastRef(); !got.Since.Equal(time.Unix(sess.CreatedAt, 0)) || got.WorktreePath != sess.WorktreePath {
		t.Fatalf("ref = %+v, want Since = created_at %d", got, sess.CreatedAt)
	}
	if got := activityOf(t, st, "sess1"); got != string(activity.Active) {
		t.Fatalf("activity = %q, want the adapter's active", got)
	}

	restored := sess.CreatedAt + 100
	if _, err := st.AppendEvent(store.Event{Type: "session.restored", SessionID: "sess1", TS: restored}); err != nil {
		t.Fatal(err)
	}
	m.sweep(context.Background())
	if got := ag.lastRef(); !got.Since.Equal(time.Unix(restored, 0)) {
		t.Fatalf("Since = %v, want restore time %d", got.Since, restored)
	}
}

// kimiStartupAgent is startupAgent speaking the Kimi pane dialect.
type kimiStartupAgent struct{ *startupAgent }

func (kimiStartupAgent) PaneDialect() string { return runtime.PaneDialectKimi }

func TestKimiStartScreenBlocksAsksAndAlerts(t *testing.T) {
	rt := &permFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1", "orch1"}}, pane: pane(t, "kimi-trust.pane")}
	m, st, _ := testMonitor(t, rt, &fakeProber{onlyShell: map[string]bool{}}, map[string]*fakeAgent{})
	ag := kimiStartupAgent{&startupAgent{fakeAgent: fakeAgent{err: agent.ErrNoSignal}, err: agent.ErrNoSignal}}
	orch := &fakeAgent{state: activity.Ready}
	m.resolveAgent = func(name string) (agent.Agent, error) {
		switch name {
		case "kimi-code":
			return ag, nil
		case claudeCodeAgent:
			return orch, nil
		}
		return nil, errors.New("agent not found: " + name)
	}
	seedSession(t, st, store.Session{ID: "orch1", Kind: "orchestrator", Agent: claudeCodeAgent, TmuxName: "orch1"})
	seedSession(t, st, store.Session{ID: "sess1", Agent: "kimi-code", TmuxName: "sess1", ParentID: "orch1", Activity: string(activity.Ready)})

	for i := 0; i < 3; i++ {
		m.sweep(context.Background())
	}

	if got := activityOf(t, st, "sess1"); got != string(activity.Blocked) {
		t.Errorf("activity = %q, want blocked", got)
	}
	q, ok := pendingOf(t, st)
	if !ok || !q.IsStartScreen() || !q.Permission.Unnumbered {
		t.Fatalf("pending = %+v, want an unnumbered start-screen quiz", q)
	}
	var labels []string
	for _, o := range q.Questions[0].Options {
		labels = append(labels, o.Label)
	}
	if want := []string{"Trust and continue", "Exit"}; !reflect.DeepEqual(labels, want) || q.Permission.Title != "Trust this folder?" {
		t.Errorf("quiz = %q %q", q.Permission.Title, labels)
	}
	msgs := messagesTo(t, st, "orch1")
	if len(msgs) != 1 {
		t.Fatalf("alerts = %d, want 1", len(msgs))
	}
	want := "[rocket] session sess1 is stuck on a Kimi Code start screen: «Trust this folder?»"
	if !strings.HasPrefix(msgs[0].Body, want) || !strings.Contains(msgs[0].Body, "Trust and continue") {
		t.Errorf("body = %q", msgs[0].Body)
	}

	// Kimi's composer: the screen is gone, the quiz closes.
	rt.setPane(pane(t, "kimi-idle.pane"))
	m.sweep(context.Background())
	m.sweep(context.Background())
	if _, ok := pendingOf(t, st); ok {
		t.Error("quiz still pending after the screen went away")
	}
}

// The Codex start-screen quiz stays numbered.
func TestCodexStartScreenQuizIsNumbered(t *testing.T) {
	m, st, _, _, _ := startMonitor(t, "codex-start-trust.pane", true)
	m.sweep(context.Background())
	q, ok := pendingOf(t, st)
	if !ok || q.Permission.Unnumbered {
		t.Fatalf("pending = %+v", q)
	}
}
