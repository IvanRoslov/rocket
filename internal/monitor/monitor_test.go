package monitor

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
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
func (f *fakeRuntime) SendKeys(ctx context.Context, h runtime.Handle, key string, literal bool) error {
	return nil
}
func (f *fakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return "", nil
}
func (f *fakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool { return true }
func (f *fakeRuntime) PinWindowSize(ctx context.Context, h runtime.Handle, clientCols, clientRows int) error {
	return nil
}

func (f *fakeRuntime) UnpinWindowSize(ctx context.Context, h runtime.Handle) error { return nil }

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
// by the monitor. statSequence, if set, scripts successive TranscriptStat
// calls (one entry consumed per call; the last entry repeats once
// exhausted). statErr, if set, is returned by every TranscriptStat call
// instead.
type fakeAgent struct {
	state activity.State
	ts    time.Time
	err   error

	statSequence []statResult
	statErr      error
	statCalls    int
}

// statResult is one scripted (mtime, size) pair for fakeAgent.TranscriptStat.
type statResult struct {
	mtime int64
	size  int64
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

func (f *fakeAgent) TranscriptTail(ctx context.Context, ref agent.ActivityRef, cursor string) ([]agent.ChatEntry, string, error) {
	return nil, "", agent.ErrNoSignal
}

func (f *fakeAgent) TranscriptStat(ctx context.Context, ref agent.ActivityRef) (int64, int64, error) {
	if f.statErr != nil {
		return 0, 0, f.statErr
	}
	if len(f.statSequence) == 0 {
		return 0, 0, agent.ErrNoSignal
	}
	i := f.statCalls
	if i >= len(f.statSequence) {
		i = len(f.statSequence) - 1
	}
	f.statCalls++
	r := f.statSequence[i]
	return r.mtime, r.size, nil
}

// testMonitor builds a Monitor wired to a real (temp-dir) store+bus, a
// fake runtime and a fake pane prober, plus a resolveAgent closure that
// looks up agents by name in the given map.
func testMonitor(t *testing.T, rt runtime.Runtime, prober *fakeProber, agents map[string]*fakeAgent) (*Monitor, *store.Store, *bus.Bus) {
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
		chat:         make(map[string]chatStat),
		quizMiss:     make(map[string]int),
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

// TestSweepOnceUpdatesStoreActivity verifies that Monitor.SweepOnce, called
// directly (as the daemon does synchronously at startup, before
// queue.Recover), performs a real synchronous sweep pass that updates store
// activity — not a no-op or async-only trigger.
func TestSweepOnceUpdatesStoreActivity(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	now := time.Now()
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: now}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	ch, cancel := b.Subscribe()
	defer cancel()

	m.SweepOnce(context.Background())

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
		}
	}
	if !found {
		t.Errorf("expected session.activity_changed event from SweepOnce, got %v", events)
	}
}

// TestSweepFreshRunningSessionNoSignalStaysReady verifies that a
// freshly-running session with no stored ActivityTS (0, i.e. never reported
// any signal yet) that gets agent.ErrNoSignal from the agent stays Ready
// instead of being immediately demoted to Idle. Before the fix, ts was
// computed as time.Unix(0, 0) (the epoch), which is always far in the past,
// so applyMerge's Ready->Idle threshold fired instantly.
func TestSweepFreshRunningSessionNoSignalStaysReady(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {err: agent.ErrNoSignal}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{
		ID:       "sess1",
		Agent:    "fake",
		TmuxName: "sess1",
		State:    "running",
		Activity: "", // nothing reported yet
		// ActivityTS left at zero.
	})

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	sess, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Activity != string(activity.Ready) {
		t.Errorf("Activity = %q, want ready (must not be instantly demoted to idle)", sess.Activity)
	}

	events := drainEvents(ch)
	for _, e := range events {
		if e.Type == "session.activity_changed" && e.SessionID == "sess1" && e.Data["to"] == "idle" {
			t.Errorf("unexpected instant idle transition for fresh session: %+v", e)
		}
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

// TestSweepSpawningSessionNotExited verifies that a session in "spawning"
// state is not marked Exited by tmux checks, even if tmux List is empty (the
// session may not exist yet). With no agent signal and empty stored activity,
// activity remains untouched and no event is published.
func TestSweepSpawningSessionNotExited(t *testing.T) {
	rt := &fakeRuntime{names: []string{}} // sess1 not present in tmux
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {err: agent.ErrNoSignal}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{
		ID:       "sess1",
		Agent:    "fake",
		TmuxName: "sess1",
		State:    "spawning",
		Activity: "", // Empty activity at seed time
	})

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	sess, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	// Activity should remain empty; not forced to Ready or Exited.
	if sess.Activity != "" {
		t.Errorf("Activity = %q, want empty (spawning session should not be modified when agent gives ErrNoSignal)", sess.Activity)
	}

	events := drainEvents(ch)
	for _, e := range events {
		if e.Type == "session.activity_changed" {
			t.Errorf("unexpected activity_changed event for spawning session with no signal: %+v", e)
		}
	}
}

// TestSweepPrunesStaleCacheEntries verifies that after sweep, cache and push
// map entries are pruned if their session no longer exists in the live list.
func TestSweepPrunesStaleCacheEntries(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}} // Only sess1 is live
	prober := &fakeProber{onlyShell: map[string]bool{}}
	now := time.Now()
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: now}}
	m, st, b := testMonitor(t, rt, prober, agents)

	// Seed two sessions.
	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})
	seedSession(t, st, store.Session{ID: "sess2", Agent: "fake", TmuxName: "sess2"})

	// Populate cache and push for both.
	m.PushUpdate("sess1", activity.Active, now)
	m.PushUpdate("sess2", activity.Active, now)

	ch, cancel := b.Subscribe()
	defer cancel()

	// Mark sess2 as exited/killed so it won't be in live list after sweep.
	if err := st.UpdateSessionState("sess2", "exited"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	m.sweep(context.Background())

	// Drain any events (we're not testing them here).
	_ = drainEvents(ch)

	// Verify sess1 is still in cache and push.
	m.mu.Lock()
	_, inCache1 := m.cache["sess1"]
	_, inPush1 := m.push["sess1"]
	_, inCache2 := m.cache["sess2"]
	_, inPush2 := m.push["sess2"]
	m.mu.Unlock()

	if !inCache1 {
		t.Errorf("sess1 should still be in cache")
	}
	if !inPush1 {
		t.Errorf("sess1 should still be in push map")
	}
	if inCache2 {
		t.Errorf("sess2 should be pruned from cache")
	}
	if inPush2 {
		t.Errorf("sess2 should be pruned from push map")
	}
}

// countChatUpdated counts session.chat_updated events for sessionID among
// events.
func countChatUpdated(events []store.Event, sessionID string) int {
	n := 0
	for _, e := range events {
		if e.Type == "session.chat_updated" && e.SessionID == sessionID {
			n++
		}
	}
	return n
}

// TestChatWatcherFirstObservationSeedsSilently verifies that the very first
// sweep observing a session's transcript stat does not publish
// session.chat_updated, even though there was no prior cached value to
// compare against — this avoids a ping storm for every already-running
// session on daemon start.
func TestChatWatcherFirstObservationSeedsSilently(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {
		state:        activity.Ready,
		ts:           time.Now(),
		statSequence: []statResult{{mtime: 100, size: 10}},
	}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())

	events := drainEvents(ch)
	if n := countChatUpdated(events, "sess1"); n != 0 {
		t.Errorf("chat_updated events on first observation = %d, want 0", n)
	}
}

// TestChatWatcherChangeEmitsOneEvent verifies that a transcript
// (mtime,size) change between two sweeps produces exactly one
// session.chat_updated event.
func TestChatWatcherChangeEmitsOneEvent(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {
		state: activity.Ready,
		ts:    time.Now(),
		statSequence: []statResult{
			{mtime: 100, size: 10},
			{mtime: 200, size: 20},
		},
	}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	ch, cancel := b.Subscribe()
	defer cancel()

	// First sweep: seeds silently.
	m.sweep(context.Background())
	_ = drainEvents(ch)

	// Second sweep: (mtime,size) changed -> exactly one event.
	m.sweep(context.Background())
	events := drainEvents(ch)
	if n := countChatUpdated(events, "sess1"); n != 1 {
		t.Errorf("chat_updated events after change = %d, want 1", n)
	}
}

// TestChatWatcherNoChangeNoEvent verifies that an unchanged (mtime,size)
// between sweeps produces no session.chat_updated event.
func TestChatWatcherNoChangeNoEvent(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {
		state: activity.Ready,
		ts:    time.Now(),
		statSequence: []statResult{
			{mtime: 100, size: 10},
			{mtime: 100, size: 10},
		},
	}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())
	_ = drainEvents(ch)

	m.sweep(context.Background())
	events := drainEvents(ch)
	if n := countChatUpdated(events, "sess1"); n != 0 {
		t.Errorf("chat_updated events with no change = %d, want 0", n)
	}
}

// TestChatWatcherKilledSessionNotWatched verifies that once a session is no
// longer live (excluded from ListSessions' default live-only filter), the
// chat watcher no longer polls or emits for it, and its cache entry is
// pruned.
func TestChatWatcherKilledSessionNotWatched(t *testing.T) {
	rt := &fakeRuntime{names: []string{"sess1"}}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {
		state: activity.Ready,
		ts:    time.Now(),
		statSequence: []statResult{
			{mtime: 100, size: 10},
			{mtime: 200, size: 20},
		},
	}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})

	ch, cancel := b.Subscribe()
	defer cancel()

	// First sweep: seeds silently.
	m.sweep(context.Background())
	_ = drainEvents(ch)

	// Kill the session so it drops out of the live-sessions listing.
	if err := st.UpdateSessionState("sess1", "exited"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	m.sweep(context.Background())
	events := drainEvents(ch)
	if n := countChatUpdated(events, "sess1"); n != 0 {
		t.Errorf("chat_updated events for killed session = %d, want 0", n)
	}

	m.mu.Lock()
	_, inChat := m.chat["sess1"]
	m.mu.Unlock()
	if inChat {
		t.Errorf("sess1 should be pruned from chat map after going terminal")
	}
}

// quizFakeRuntime extends fakeRuntime with a scriptable Capture, for
// pollQuiz tests.
type quizFakeRuntime struct {
	fakeRuntime
	captureOut string
	captureErr error
}

func (f *quizFakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return f.captureOut, f.captureErr
}

const quizWidgetTail = "❯ 1. Red\n  2. Green\n  4. Type something.\n\nEnter to select · ↑/↓ to navigate · Esc to cancel"
const plainComposerTail = "⏺ done\n\n❯ \n  ⏵⏵ bypass permissions on"

func seedPendingQuiz(t *testing.T, st *store.Store, id string, askedAt int64) {
	t.Helper()
	if err := st.SetPendingQuiz(id, `{"questions":[{"question":"q"}],"asked_at":`+strconv.FormatInt(askedAt, 10)+`}`); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}
}

func TestPollQuizWidgetGoneClearsAfterTwoMisses(t *testing.T) {
	rt := &quizFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1"}}, captureOut: plainComposerTail}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: time.Now()}}
	m, st, b := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})
	seedPendingQuiz(t, st, "sess1", time.Now().Add(-time.Minute).Unix())

	ch, cancel := b.Subscribe()
	defer cancel()

	m.sweep(context.Background())
	sess, _ := st.GetSession("sess1")
	if sess.PendingQuiz == "" {
		t.Fatalf("pending cleared after ONE miss, want two-miss threshold")
	}

	m.sweep(context.Background())
	sess, _ = st.GetSession("sess1")
	if sess.PendingQuiz != "" {
		t.Fatalf("pending not cleared after two misses")
	}

	var resolved bool
	for _, e := range drainEvents(ch) {
		if e.Type == "session.quiz_resolved" && e.SessionID == "sess1" {
			resolved = true
		}
	}
	if !resolved {
		t.Errorf("expected session.quiz_resolved event")
	}
}

func TestPollQuizWidgetStillVisibleKeepsPending(t *testing.T) {
	rt := &quizFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1"}}, captureOut: quizWidgetTail}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: time.Now()}}
	m, st, _ := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})
	seedPendingQuiz(t, st, "sess1", time.Now().Add(-time.Minute).Unix())

	for i := 0; i < 4; i++ {
		m.sweep(context.Background())
	}
	sess, _ := st.GetSession("sess1")
	if sess.PendingQuiz == "" {
		t.Fatalf("pending cleared while widget still visible")
	}
}

func TestPollQuizFreshQuizGraceNotChecked(t *testing.T) {
	rt := &quizFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1"}}, captureOut: plainComposerTail}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: time.Now()}}
	m, st, _ := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})
	seedPendingQuiz(t, st, "sess1", time.Now().Unix())

	for i := 0; i < 4; i++ {
		m.sweep(context.Background())
	}
	sess, _ := st.GetSession("sess1")
	if sess.PendingQuiz == "" {
		t.Fatalf("pending cleared during render grace, want untouched")
	}
}

func TestPollQuizWidgetReappearingResetsMissStreak(t *testing.T) {
	rt := &quizFakeRuntime{fakeRuntime: fakeRuntime{names: []string{"sess1"}}, captureOut: plainComposerTail}
	prober := &fakeProber{onlyShell: map[string]bool{}}
	agents := map[string]*fakeAgent{"fake": {state: activity.Active, ts: time.Now()}}
	m, st, _ := testMonitor(t, rt, prober, agents)

	seedSession(t, st, store.Session{ID: "sess1", Agent: "fake", TmuxName: "sess1"})
	seedPendingQuiz(t, st, "sess1", time.Now().Add(-time.Minute).Unix())

	m.sweep(context.Background()) // miss 1
	rt.captureOut = quizWidgetTail
	m.sweep(context.Background()) // widget back: streak resets
	rt.captureOut = plainComposerTail
	m.sweep(context.Background()) // miss 1 again — must not clear yet
	sess, _ := st.GetSession("sess1")
	if sess.PendingQuiz == "" {
		t.Fatalf("pending cleared after non-consecutive misses, want streak reset")
	}
}
