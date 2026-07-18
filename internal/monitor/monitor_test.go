package monitor

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// fakeRuntime is a fake of runtime.Runtime. Only List is exercised by the
// monitor; the rest are no-ops to satisfy the interface.
type fakeRuntime struct {
	names []string
	err   error
}

func (f *fakeRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{}, nil
}
func (f *fakeRuntime) Inject(ctx context.Context, h runtime.Handle, text string) error { return nil }
func (f *fakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return "", nil
}
func (f *fakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool    { return true }
func (f *fakeRuntime) Destroy(ctx context.Context, h runtime.Handle) error { return nil }
func (f *fakeRuntime) AttachCommand(h runtime.Handle) []string             { return nil }
func (f *fakeRuntime) List(ctx context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.names, nil
}

// fakeProber is a fake paneProber.
type fakeProber struct {
	onlyShell map[string]bool
	err       map[string]error
}

func (f *fakeProber) onlyShellRunning(ctx context.Context, tmuxName string) (bool, error) {
	if err, ok := f.err[tmuxName]; ok {
		return false, err
	}
	return f.onlyShell[tmuxName], nil
}

// fakeAgent implements agent.Agent, exposing only Activity behavior needed
// by the monitor.
type fakeAgent struct {
	state activity.State
	ts    time.Time
	err   error
}

func (f *fakeAgent) Name() string                                 { return "fake-mon" }
func (f *fakeAgent) Available() error                             { return nil }
func (f *fakeAgent) SetupWorkspace(spec agent.LaunchSpec) error   { return nil }
func (f *fakeAgent) LaunchCommand(spec agent.LaunchSpec) []string { return nil }
func (f *fakeAgent) Env(spec agent.LaunchSpec) map[string]string  { return nil }
func (f *fakeAgent) Activity(ctx context.Context, ref agent.ActivityRef) (activity.State, time.Time, error) {
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	return f.state, f.ts, nil
}

// testMonitor builds a Monitor wired to a real (temp-dir) store+bus, a
// fake runtime and a fake pane prober, plus a resolveAgent closure that
// looks up agents by name in the given map.
func testMonitor(t *testing.T, rt *fakeRuntime, prober *fakeProber, agents map[string]*fakeAgent) (*Monitor, *store.Store, *bus.Bus) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	b := bus.New(st)
	cfg := &config.Config{
		Home:                 dir,
		ActivityPollInterval: 5 * time.Second,
		ReadyToIdle:          5 * time.Minute,
	}

	resolveAgent := func(name string) (agent.Agent, error) {
		a, ok := agents[name]
		if !ok {
			return nil, errors.New("agent not found: " + name)
		}
		return a, nil
	}

	m := &Monitor{
		st:           st,
		bus:          b,
		rt:           rt,
		cfg:          cfg,
		resolveAgent: resolveAgent,
		prober:       prober,
		push:         make(map[string]pushEntry),
		cache:        make(map[string]activity.State),
	}
	return m, st, b
}

func seedSession(t *testing.T, st *store.Store, sess store.Session) {
	t.Helper()
	if err := st.AddRepo(store.Repo{ID: "repo1", Path: "/tmp/repo1", DefaultBranch: "main"}); err != nil && !errors.Is(err, store.ErrExists) {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: "proj1", Name: "proj1", MainRepo: "repo1"}); err != nil && !errors.Is(err, store.ErrExists) {
		t.Fatalf("AddProject: %v", err)
	}
	if sess.ProjectID == "" {
		sess.ProjectID = "proj1"
	}
	if sess.RepoID == "" {
		sess.RepoID = "repo1"
	}
	if sess.Kind == "" {
		sess.Kind = "worker"
	}
	if sess.FeatureSlug == "" {
		sess.FeatureSlug = "feat1"
	}
	if sess.Branch == "" {
		sess.Branch = "feature/feat1/task1"
	}
	if sess.WorktreePath == "" {
		sess.WorktreePath = "/tmp/wt/" + sess.ID
	}
	if sess.State == "" {
		sess.State = "running"
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
}

func drainEvents(ch <-chan store.Event) []store.Event {
	var out []store.Event
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-time.After(50 * time.Millisecond):
			return out
		}
	}
}

// TestSweepAgentActive verifies that an agent reporting Active updates the
// store and publishes an event.
func TestSweepAgentActive(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	now := time.Now()
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: now}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	sess, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Activity != string(activity.Active) {
		t.Errorf("Activity = %q, want active", sess.Activity)
	}

	events := drainEvents(ch)
	found := false
	for _, e := range events {
		if e.Type == "session.activity_changed" && e.SessionID == "sess1" {
			found = true
			if e.Data["to"] != "active" {
				t.Errorf("to = %v, want active", e.Data["to"])
			}
			if e.Data["source"] != "poll" {
				t.Errorf("source = %v, want poll", e.Data["source"])
			}
		}
	}
	if !found {
		t.Errorf("expected session.activity_changed event, got %v", events)
	}
}

// TestSweepReadyOldPastThresholdBecomesIdle verifies the Ready->Idle
// threshold based on ReadyToIdle.
func TestSweepReadyOldPastThresholdBecomesIdle(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	old := time.Now().Add(-10 * time.Minute)
	agents := map[string]*fakeAgent{"fake": {state: activity.Ready, ts: old}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	sess, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Activity != string(activity.Idle) {
		t.Errorf("Activity = %q, want idle", sess.Activity)
	}

	events := drainEvents(ch)
	found := false
	for _, e := range events {
		if e.Type == "session.activity_changed" && e.Data["to"] == "idle" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected activity_changed to idle, got %v", events)
	}
}

// TestSweepTmuxMissingExited verifies that a session whose tmux is gone is
// marked Exited even though a fresher push exists (poll's Exited always
// wins over push).
func TestSweepTmuxMissingExited(t *testing.T) {
	rt := &fakeRuntime{names: []string{}} // sess1 not present
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: time.Now()}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	// Push a fresh Active state just before the sweep.
	m.PushUpdate("sess1", activity.Active, time.Now())

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	sess, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Activity != string(activity.Exited) {
		t.Errorf("Activity = %q, want exited (poll exited must win over push)", sess.Activity)
	}

	events := drainEvents(ch)
	found := false
	for _, e := range events {
		if e.Type == "session.activity_changed" && e.Data["to"] == "exited" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected activity_changed to exited, got %v", events)
	}
}

// TestSweepPaneOnlyShellExited verifies that a pane holding only a bare
// shell (e.g. the agent process exited) is treated as Exited.
func TestSweepPaneOnlyShellExited(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{"sess1": true}}
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: time.Now()}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	sess, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Activity != string(activity.Exited) {
		t.Errorf("Activity = %q, want exited", sess.Activity)
	}

	events := drainEvents(ch)
	found := false
	for _, e := range events {
		if e.Type == "session.activity_changed" && e.Data["to"] == "exited" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected activity_changed to exited, got %v", events)
	}
}

// TestPushNewerThanPollWins verifies that a push newer than the poll signal
// takes precedence.
func TestPushNewerThanPollWins(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	pollTS := time.Now().Add(-1 * time.Minute)
	agents := map[string]*fakeAgent{"fake": {state: activity.Ready, ts: pollTS}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	// Push a newer Active state (well within ReadyToIdle so it's not
	// downgraded).
	pushTS := time.Now()
	m.mu.Lock()
	m.push["sess1"] = pushEntry{state: activity.Active, ts: pushTS}
	m.mu.Unlock()

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	sess, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Activity != string(activity.Active) {
		t.Errorf("Activity = %q, want active (push should win)", sess.Activity)
	}

	events := drainEvents(ch)
	found := false
	for _, e := range events {
		if e.Type == "session.activity_changed" && e.Data["source"] == "poll" && e.Data["to"] == "active" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected activity_changed to active via poll (merged push), got %v", events)
	}
}

// TestPollNewerThanPushWins verifies that when the poll signal is newer
// than a stale push, the poll wins.
func TestPollNewerThanPushWins(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	pollTS := time.Now()
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: pollTS}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	// Push a stale Ready state.
	pushTS := time.Now().Add(-10 * time.Minute)
	m.mu.Lock()
	m.push["sess1"] = pushEntry{state: activity.Ready, ts: pushTS}
	m.mu.Unlock()

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	sess, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Activity != string(activity.Active) {
		t.Errorf("Activity = %q, want active (poll should win over stale push)", sess.Activity)
	}

	events := drainEvents(ch)
	if len(events) == 0 {
		t.Errorf("expected an activity_changed event")
	}
}

// TestSweepNoChangeNoEvent verifies that when the merged state matches the
// already-stored state, no store write or event happens.
func TestSweepNoChangeNoEvent(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	now := time.Now()
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: now}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1", Activity: string(activity.Active), ActivityTS: now.Unix()})

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	events := drainEvents(ch)
	for _, e := range events {
		if e.Type == "session.activity_changed" {
			t.Errorf("unexpected activity_changed event when state unchanged: %+v", e)
		}
	}
}

// TestPushUpdateInvalidStateIgnored verifies that PushUpdate ignores
// invalid states.
func TestPushUpdateInvalidStateIgnored(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {state: activity.Ready, ts: time.Now()}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	ch, cancel := b.Subscribe()
	defer cancel()

	m.PushUpdate("sess1", activity.State("bogus"), time.Now())

	events := drainEvents(ch)
	for _, e := range events {
		if e.Type == "session.activity_changed" {
			t.Errorf("unexpected activity_changed event for invalid pushed state: %+v", e)
		}
	}

	m.mu.Lock()
	_, hasPush := m.push["sess1"]
	m.mu.Unlock()
	if hasPush {
		t.Errorf("invalid pushed state should not be recorded in push map")
	}
}
