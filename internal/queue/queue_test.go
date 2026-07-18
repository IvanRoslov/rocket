package queue

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// fakeRuntime records Inject calls and lets tests script per-call behavior.
type fakeRuntime struct {
	mu sync.Mutex

	injectCalls []string // texts passed to Inject, in call order
	injectFn    func(callIdx int, h runtime.Handle, text string) error
	captureFn   func(h runtime.Handle, lines int) (string, error)
}

func (f *fakeRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{Name: spec.Name}, nil
}

func (f *fakeRuntime) Inject(ctx context.Context, h runtime.Handle, text string) error {
	f.mu.Lock()
	idx := len(f.injectCalls)
	f.injectCalls = append(f.injectCalls, text)
	fn := f.injectFn
	f.mu.Unlock()

	if fn != nil {
		return fn(idx, h, text)
	}
	return nil
}

func (f *fakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	f.mu.Lock()
	fn := f.captureFn
	f.mu.Unlock()
	if fn != nil {
		return fn(h, lines)
	}
	return "", nil
}

func (f *fakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool    { return true }
func (f *fakeRuntime) Destroy(ctx context.Context, h runtime.Handle) error { return nil }
func (f *fakeRuntime) AttachCommand(h runtime.Handle) []string             { return nil }
func (f *fakeRuntime) List(ctx context.Context) ([]string, error)          { return nil, nil }

func (f *fakeRuntime) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.injectCalls))
	copy(out, f.injectCalls)
	return out
}

func (f *fakeRuntime) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.injectCalls)
}

// fakeActivity is a mutable, mutex-guarded getActivity source for tests.
type fakeActivity struct {
	mu    sync.Mutex
	state map[string]activity.State
	known map[string]bool
}

func newFakeActivity() *fakeActivity {
	return &fakeActivity{state: make(map[string]activity.State), known: make(map[string]bool)}
}

func (f *fakeActivity) get(id string) (activity.State, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state[id], f.known[id]
}

func (f *fakeActivity) set(id string, s activity.State) {
	f.mu.Lock()
	f.state[id] = s
	f.known[id] = true
	f.mu.Unlock()
}

// testHarness bundles a Queue with its dependencies for assertions.
type testHarness struct {
	q  *Queue
	st *store.Store
	b  *bus.Bus
	rt *fakeRuntime
	ac *fakeActivity
}

func newTestQueue(t *testing.T) *testHarness {
	t.Helper()

	path := filepath.Join(t.TempDir(), "rocket.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	b := bus.New(st)
	rt := &fakeRuntime{}
	ac := newFakeActivity()
	cfg := &config.Config{QueueTimeout: 30 * time.Minute}

	q := New(st, b, rt, cfg, ac.get)
	q.backoff = func(attempt int) time.Duration { return time.Millisecond }

	return &testHarness{q: q, st: st, b: b, rt: rt, ac: ac}
}

// addRunningSession inserts a session row in state "running" with the given
// tmux name equal to id, and sets its activity via the fake.
func (h *testHarness) addRunningSession(t *testing.T, id string, state activity.State) {
	t.Helper()
	if err := h.st.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "claude-code",
		Branch: "main", WorktreePath: "/tmp/" + id, TmuxName: id, State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	h.ac.set(id, state)
}

func waitUntil(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", msg)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func messageStatus(t *testing.T, st *store.Store, id int64) string {
	t.Helper()
	m, err := st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage(%d): %v", id, err)
	}
	return m.Status
}

func TestQueue_FIFODelivery(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	id1, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "first"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	id2, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "second"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool {
		return messageStatus(t, h.st, id1) == "delivered" && messageStatus(t, h.st, id2) == "delivered"
	}, "both messages delivered")

	calls := h.rt.calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want 2", calls)
	}
	if calls[0] != "first" || calls[1] != "second" {
		t.Errorf("calls = %v, want [first second]", calls)
	}
}

func TestQueue_WaitsWhileActiveThenDelivers(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Active)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	// Give the worker a moment to start waiting; it should NOT deliver yet.
	time.Sleep(150 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status = %q while active, want queued", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times while active, want 0", n)
	}

	h.ac.set("recv", activity.Ready)
	h.b.Publish("session.activity_changed", "recv", map[string]any{"to": "ready"})

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "message delivered after becoming ready")
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want 1", n)
	}
}

func TestQueue_UnconfirmedSubmitMarkerGoneTreatedAsDelivered(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		return runtime.ErrSubmitUnconfirmed
	}
	h.rt.captureFn = func(hd runtime.Handle, lines int) (string, error) {
		return "some unrelated pane output\n", nil
	}

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered despite unconfirmed submit")

	// Give any potential retry a moment to (wrongly) happen.
	time.Sleep(100 * time.Millisecond)
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want exactly 1 (no re-injection)", n)
	}
}

func TestQueue_UnconfirmedSubmitMarkerPresentRetriesThenSucceeds(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		if idx == 0 {
			return runtime.ErrSubmitUnconfirmed
		}
		return nil
	}
	h.rt.captureFn = func(hd runtime.Handle, lines int) (string, error) {
		// Marker (the message text) still visible in the pane -> retry.
		return "hello\n", nil
	}

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered after retry")

	if n := h.rt.callCount(); n != 2 {
		t.Fatalf("Inject called %d times, want 2 (one retry)", n)
	}
}

func TestQueue_PersistentFailureGivesUpAfterFiveAttempts(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		return errors.New("boom")
	}

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	ch, cancel := h.b.Subscribe()
	defer cancel()

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "message failed after 5 attempts")

	if n := h.rt.callCount(); n != 5 {
		t.Fatalf("Inject called %d times, want 5", n)
	}

	var gotReason string
	deadline := time.Now().Add(2 * time.Second)
	for gotReason == "" && time.Now().Before(deadline) {
		select {
		case e := <-ch:
			if e.Type == "message.failed" {
				if r, _ := e.Data["reason"].(string); r != "" {
					gotReason = r
				}
			}
		case <-time.After(2 * time.Second):
		}
	}
	if gotReason != "delivery_failed" {
		t.Fatalf("reason = %q, want delivery_failed", gotReason)
	}
}

func TestQueue_RecipientKilledFailsImmediately(t *testing.T) {
	h := newTestQueue(t)

	if err := h.st.AddSession(store.Session{
		ID: "recv", Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "claude-code",
		Branch: "main", WorktreePath: "/tmp/recv", TmuxName: "recv", State: "killed",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "message failed for killed recipient")
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times, want 0", n)
	}
}

func TestQueue_ExpireTimedOut(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.QueueTimeout = time.Hour

	// CreatedAt is unix-second granularity, so use a timestamp well outside
	// the timeout window instead of relying on real elapsed time.
	old := time.Now().Add(-2 * time.Hour).Unix()
	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello", CreatedAt: old})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	ch, cancel := h.b.Subscribe()
	defer cancel()

	h.q.expireTimedOut()

	if got := messageStatus(t, h.st, id); got != "failed" {
		t.Fatalf("status = %q, want failed", got)
	}

	select {
	case e := <-ch:
		if e.Type != "message.failed" {
			t.Fatalf("event type = %q, want message.failed", e.Type)
		}
		if r, _ := e.Data["reason"].(string); r != "timeout" {
			t.Fatalf("reason = %q, want timeout", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message.failed event")
	}
}

func TestQueue_WakeIsIdempotent(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.q.Wake("recv")
		}()
	}
	wg.Wait()

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "message delivered despite concurrent Wakes")

	// Give any duplicate worker a moment to (wrongly) double-deliver.
	time.Sleep(100 * time.Millisecond)
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want exactly 1 (Wake must be idempotent)", n)
	}
}

func TestFormatBody(t *testing.T) {
	got := formatBody(store.Message{FromSession: "orch", Body: "hi"})
	want := "[from orch] hi"
	if got != want {
		t.Errorf("formatBody = %q, want %q", got, want)
	}

	got2 := formatBody(store.Message{Body: "hi"})
	if got2 != "hi" {
		t.Errorf("formatBody(no from) = %q, want %q", got2, "hi")
	}
}

func TestMarkerPresent(t *testing.T) {
	if !markerPresent("foo\nhello\nbar", "hello") {
		t.Error("expected marker present")
	}
	if markerPresent("foo\nbar", "hello") {
		t.Error("expected marker absent")
	}
}
