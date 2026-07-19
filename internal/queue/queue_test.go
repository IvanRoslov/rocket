package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

func (f *fakeRuntime) SendKeys(ctx context.Context, h runtime.Handle, key string, literal bool) error {
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

func (f *fakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool { return true }
func (f *fakeRuntime) PinWindowSize(ctx context.Context, h runtime.Handle, clientCols, clientRows int) error {
	return nil
}

func (f *fakeRuntime) UnpinWindowSize(ctx context.Context, h runtime.Handle) error { return nil }

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
	cfg := &config.Config{QueueTimeout: 30 * time.Minute, LargeMessageThreshold: 2048}

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
	waitUntilTimeout(t, 3*time.Second, cond, msg)
}

func waitUntilTimeout(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
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

func TestQueue_RunRecoversOrphanedDeliveringMessages(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "orphaned"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	// Simulate a crash mid-delivery: message stuck in "delivering".
	if err := h.st.UpdateMessageStatus(id, "delivering", 1, 0, ""); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}

	// ListQueuedRecipients (used by Run to decide who to Wake) never sees
	// "delivering" messages, so without recovery this message would never
	// be picked up at all.
	if got := messageStatus(t, h.st, id); got != "delivering" {
		t.Fatalf("precondition: status = %q, want delivering", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.q.Recover(ctx)
	go h.q.Run(ctx)

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"orphaned delivering message recovered and delivered")
}

func TestQueue_RecoverAloneResetsDeliveringWithoutRun(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "orphaned"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if err := h.st.UpdateMessageStatus(id, "delivering", 1, 0, ""); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}

	// Calling Recover synchronously, without ever starting Run, must still
	// reset and redeliver the orphaned message. This is the behavior the
	// daemon relies on to complete recovery before serving the API.
	h.q.Recover(context.Background())

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"orphaned delivering message recovered by Recover alone")
}

func TestQueue_CtxCancelStopsWaitingWorker(t *testing.T) {
	h := newTestQueue(t)
	// Activity stays "active" forever: the worker will sit in waitForReady.
	h.addRunningSession(t, "recv", activity.Active)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.q.Recover(ctx)
	go h.q.Run(ctx)

	// Let Recover's startup Wake spin the worker up into its wait loop.
	waitUntil(t, func() bool {
		h.q.mu.Lock()
		defer h.q.mu.Unlock()
		return h.q.workers["recv"]
	}, "worker registered")

	cancel()

	waitUntil(t, func() bool {
		h.q.mu.Lock()
		defer h.q.mu.Unlock()
		return !h.q.workers["recv"]
	}, "worker exits after ctx cancel")

	// The message must be left untouched (still queued), to be recovered on
	// the next start rather than lost or silently marked otherwise.
	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status after ctx cancel = %q, want queued", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times, want 0 (never became ready)", n)
	}
}

func TestQueue_TransientGetSessionErrorDoesNotFailMessage(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	var calls int32
	h.q.getSession = func(to string) (store.Session, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return store.Session{}, errors.New("transient db hiccup")
		}
		return h.st.GetSession(to)
	}

	h.q.Wake("recv")

	waitUntilTimeout(t, 5*time.Second, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"message delivered after transient GetSession error retried")

	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want 1", n)
	}
}

func TestQueue_NotFoundGetSessionFailsMessageAsRecipientGone(t *testing.T) {
	h := newTestQueue(t)

	id, err := h.st.AddMessage(store.Message{ToSession: "ghost", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.getSession = func(to string) (store.Session, error) {
		return store.Session{}, store.ErrNotFound
	}

	h.q.Wake("ghost")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" },
		"message failed for not-found recipient")
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times, want 0", n)
	}
}

// TestQueue_SpawningRecipientWaitsThenDelivers verifies that a recipient in
// state "spawning" is NOT failed as recipient_gone: the message must stay
// queued while spawning, then deliver once the session transitions to
// "running" with a ready activity signal.
func TestQueue_SpawningRecipientWaitsThenDelivers(t *testing.T) {
	h := newTestQueue(t)

	if err := h.st.AddSession(store.Session{
		ID: "recv", Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "claude-code",
		Branch: "main", WorktreePath: "/tmp/recv", TmuxName: "recv", State: "spawning",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	// While spawning, the message must stay queued (not failed).
	time.Sleep(150 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status = %q while spawning, want queued", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times while spawning, want 0", n)
	}

	// Flip session to running + activity ready, and notify.
	if err := h.st.UpdateSessionState("recv", "running"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}
	h.ac.set("recv", activity.Ready)
	h.q.Wake("recv") // in case the worker had already exited its loop
	h.b.Publish("session.activity_changed", "recv", map[string]any{"to": "ready"})

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"message delivered after spawning session becomes running")
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want 1", n)
	}
}

// TestQueue_ClaimMessageRaceSkipsExpiredMessage simulates a timeout-expiry
// race against a live worker: a queued message is marked "failed" directly
// (as expireTimedOut would do), then Wake is called. The worker must lose
// the CAS in ClaimMessage and never call Inject, leaving the message failed
// instead of overwriting it back to delivered.
func TestQueue_ClaimMessageRaceSkipsExpiredMessage(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	// Simulate expiry racing the worker: mark the message failed directly,
	// bypassing the normal delivery path (as expireTimedOut's UPDATE would).
	if err := h.st.UpdateMessageStatus(id, "failed", 0, 0, ""); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}

	h.q.Wake("recv")

	// Give the worker a chance to run (it should find no queued message via
	// NextQueuedMessage and exit immediately without ever calling Inject).
	time.Sleep(200 * time.Millisecond)

	if got := messageStatus(t, h.st, id); got != "failed" {
		t.Fatalf("status = %q, want failed (must not be revived to delivered)", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times, want 0", n)
	}
}

// TestClaimMessageCAS exercises store.ClaimMessage directly: it succeeds
// exactly once for a queued message and fails once the status is no longer
// "queued".
func TestClaimMessageCAS(t *testing.T) {
	h := newTestQueue(t)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	ok, err := h.st.ClaimMessage(id)
	if err != nil {
		t.Fatalf("ClaimMessage: %v", err)
	}
	if !ok {
		t.Fatal("first ClaimMessage should succeed")
	}
	if got := messageStatus(t, h.st, id); got != "delivering" {
		t.Fatalf("status after claim = %q, want delivering", got)
	}

	ok, err = h.st.ClaimMessage(id)
	if err != nil {
		t.Fatalf("ClaimMessage (second): %v", err)
	}
	if ok {
		t.Fatal("second ClaimMessage should fail (no longer queued)")
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

// --- large-message inbox delivery ---

// addRunningSessionAt is like addRunningSession but lets the caller pick a
// real worktree path (needed for tests that actually write inbox files).
func (h *testHarness) addRunningSessionAt(t *testing.T, id, worktreePath string, state activity.State) {
	t.Helper()
	if err := h.st.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "claude-code",
		Branch: "main", WorktreePath: worktreePath, TmuxName: id, State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	h.ac.set(id, state)
}

func readInboxFile(t *testing.T, worktreePath string, id int64) string {
	t.Helper()
	path := filepath.Join(worktreePath, ".rocket", "inbox", fmt.Sprintf("msg-%d.md", id))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read inbox file %s: %v", path, err)
	}
	return string(data)
}

func TestQueue_LargeBodyDeliveredViaInboxFile(t *testing.T) {
	h := newTestQueue(t)
	wt := t.TempDir()
	h.addRunningSessionAt(t, "recv", wt, activity.Ready)

	body := strings.Repeat("x", 3000) // exceeds LargeMessageThreshold (2048)
	id, err := h.st.AddMessage(store.Message{ToSession: "recv", FromSession: "orch", Body: body})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "large message delivered")

	if got := readInboxFile(t, wt, id); got != body {
		t.Errorf("inbox file content = %q (len %d), want original body (len %d)", got[:20]+"...", len(got), len(body))
	}

	calls := h.rt.calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want 1", calls)
	}
	injected := calls[0]
	wantPointer := fmt.Sprintf(".rocket/inbox/msg-%d.md", id)
	if !strings.Contains(injected, wantPointer) {
		t.Errorf("injected text = %q, want it to contain %q", injected, wantPointer)
	}
	if !strings.HasPrefix(injected, "[from orch] ") {
		t.Errorf("injected text = %q, want [from orch] prefix", injected)
	}
	if strings.Contains(injected, body) {
		t.Error("injected text should not contain the full body")
	}
}

func TestQueue_ManyLinesDeliveredViaInboxFile(t *testing.T) {
	h := newTestQueue(t)
	wt := t.TempDir()
	h.addRunningSessionAt(t, "recv", wt, activity.Ready)

	lines := make([]string, 25) // > largeMessageLineThreshold (20), well under byte threshold
	for i := range lines {
		lines[i] = "line"
	}
	body := strings.Join(lines, "\n")

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: body})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "many-line message delivered")

	if got := readInboxFile(t, wt, id); got != body {
		t.Errorf("inbox file content mismatch: got %d bytes, want %d bytes", len(got), len(body))
	}

	injected := h.rt.calls()[0]
	wantPointer := fmt.Sprintf(".rocket/inbox/msg-%d.md", id)
	if !strings.Contains(injected, wantPointer) {
		t.Errorf("injected text = %q, want it to contain %q", injected, wantPointer)
	}
}

func TestQueue_SmallBodyUnchangedDirectInjection(t *testing.T) {
	h := newTestQueue(t)
	wt := t.TempDir()
	h.addRunningSessionAt(t, "recv", wt, activity.Ready)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", FromSession: "orch", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "small message delivered")

	injected := h.rt.calls()[0]
	if injected != "[from orch] hello" {
		t.Errorf("injected text = %q, want %q", injected, "[from orch] hello")
	}

	if _, err := os.Stat(filepath.Join(wt, ".rocket", "inbox")); !os.IsNotExist(err) {
		t.Errorf("inbox dir should not have been created for a small message, stat err = %v", err)
	}
}

func TestQueue_LargeBodyWithoutWorktreeDeliveredInFull(t *testing.T) {
	h := newTestQueue(t)
	// No worktree path -> recipient has no worktree.
	h.addRunningSessionAt(t, "recv", "", activity.Ready)

	body := strings.Repeat("x", 3000)
	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: body})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "large message without worktree delivered in full")

	injected := h.rt.calls()[0]
	if injected != body {
		t.Errorf("injected text len = %d, want full body len %d (unchanged)", len(injected), len(body))
	}
}

func TestQueue_InboxWriteFailureFallsBackToFullBody(t *testing.T) {
	h := newTestQueue(t)
	wt := t.TempDir()
	// Make .rocket a regular file so MkdirAll(".rocket/inbox") fails.
	if err := os.WriteFile(filepath.Join(wt, ".rocket"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("setup: write blocking file: %v", err)
	}
	h.addRunningSessionAt(t, "recv", wt, activity.Ready)

	body := strings.Repeat("x", 3000)
	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: body})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "message delivered via fallback despite write failure")

	injected := h.rt.calls()[0]
	if injected != body {
		t.Errorf("injected text len = %d, want full body len %d (fallback)", len(injected), len(body))
	}
}

func TestQueue_LargeBodyRetryDoesNotCorruptFile(t *testing.T) {
	h := newTestQueue(t)
	wt := t.TempDir()
	h.addRunningSessionAt(t, "recv", wt, activity.Ready)

	// Fail the first Inject attempt (retryable), succeed on the second.
	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		if idx == 0 {
			return errors.New("transient")
		}
		return nil
	}

	body := strings.Repeat("y", 3000)
	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: body})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "large message delivered after retry")

	if n := h.rt.callCount(); n != 2 {
		t.Fatalf("Inject called %d times, want 2 (one retry)", n)
	}
	calls := h.rt.calls()
	if calls[0] != calls[1] {
		t.Errorf("retry injected different pointer text: %q vs %q", calls[0], calls[1])
	}

	if got := readInboxFile(t, wt, id); got != body {
		t.Errorf("inbox file corrupted after retry: got %d bytes, want %d bytes", len(got), len(body))
	}
}

// --- sender failure notifications ---

// TestQueue_DeliveryFailureNotifiesSender verifies that when a message delivery
// exhausts attempts, the sender (if their session is live) receives a notice.
func TestQueue_DeliveryFailureNotifiesSender(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	// Sender is not ready yet (active), so notice will stay queued.
	if err := h.st.AddSession(store.Session{
		ID: "sender", Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "claude-code",
		Branch: "main", WorktreePath: "/tmp/sender", TmuxName: "sender", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	h.ac.set("sender", activity.Active)

	// Message from sender to recv, but delivery will fail.
	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		return errors.New("boom")
	}

	id, err := h.st.AddMessage(store.Message{FromSession: "sender", ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	// The original message should fail.
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "message failed after 5 attempts")

	// Give the notification to be added after failure is marked.
	time.Sleep(200 * time.Millisecond)

	// Find the notice message for the sender. Since sender is not ready, it should stay queued.
	var noticeMsg store.Message
	for i := int64(1); i <= 100; i++ {
		msg, err := h.st.GetMessage(i)
		if err == store.ErrNotFound {
			continue
		}
		if err != nil {
			t.Fatalf("GetMessage(%d): %v", i, err)
		}
		if msg.ToSession == "sender" && msg.FromSession == "" {
			noticeMsg = msg
			break
		}
	}
	if noticeMsg.ID == 0 {
		t.Fatal("no notice found for sender")
	}

	// Verify the notice content.
	if !strings.Contains(noticeMsg.Body, "delivery FAILED") {
		t.Errorf("notice body = %q, want it to contain 'delivery FAILED'", noticeMsg.Body)
	}
	if !strings.Contains(noticeMsg.Body, "message #1") {
		t.Errorf("notice body = %q, want it to contain message ID", noticeMsg.Body)
	}
	if !strings.Contains(noticeMsg.Body, "recv") {
		t.Errorf("notice body = %q, want it to contain recipient 'recv'", noticeMsg.Body)
	}
	if !strings.Contains(noticeMsg.Body, "delivery_failed") {
		t.Errorf("notice body = %q, want it to contain reason 'delivery_failed'", noticeMsg.Body)
	}
}

// TestQueue_DeliveryFailureToKilledSenderNoNotice verifies that if the sender's
// session is not live (e.g. killed), no notice is enqueued.
func TestQueue_DeliveryFailureToKilledSenderNoNotice(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	// Sender session is in "killed" state, not live.
	if err := h.st.AddSession(store.Session{
		ID: "sender", Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "claude-code",
		Branch: "main", WorktreePath: "/tmp/sender", TmuxName: "sender", State: "killed",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		return errors.New("boom")
	}

	id, err := h.st.AddMessage(store.Message{FromSession: "sender", ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	// The original message should fail.
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "message failed")

	// Sender should NOT be in queued recipients.
	recipientMsgs, err := h.st.ListQueuedRecipients()
	if err != nil {
		t.Fatalf("ListQueuedRecipients: %v", err)
	}

	for _, r := range recipientMsgs {
		if r == "sender" {
			t.Fatal("sender should not be in queued recipients when session is killed")
		}
	}
}

// TestQueue_NoticeMessageHasEmptyFromSession verifies that the failure notice
// itself has FromSession="" (system message), preventing recursive notifications.
func TestQueue_NoticeMessageHasEmptyFromSession(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	// Sender is active, so notice will stay queued (not immediately delivered).
	if err := h.st.AddSession(store.Session{
		ID: "sender", Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "claude-code",
		Branch: "main", WorktreePath: "/tmp/sender", TmuxName: "sender", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	h.ac.set("sender", activity.Active)

	// Make Inject fail so the message is marked as failed, triggering a notice.
	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		return errors.New("boom")
	}

	msgID, err := h.st.AddMessage(store.Message{FromSession: "sender", ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	// Wait for the failure.
	waitUntil(t, func() bool { return messageStatus(t, h.st, msgID) == "failed" }, "message failed")

	// Give time for the notice to be added.
	time.Sleep(200 * time.Millisecond)

	// Find the notice message.
	var noticeMsg store.Message
	for i := int64(1); i <= 100; i++ {
		msg, err := h.st.GetMessage(i)
		if err == store.ErrNotFound {
			continue
		}
		if err != nil {
			t.Fatalf("GetMessage(%d): %v", i, err)
		}
		if msg.ToSession == "sender" && msg.FromSession == "" {
			noticeMsg = msg
			break
		}
	}
	if noticeMsg.ID == 0 {
		t.Fatal("notice not found for sender")
	}

	// The critical check: notice must have empty FromSession to prevent recursion.
	if noticeMsg.FromSession != "" {
		t.Errorf("notice.FromSession = %q, want empty (system message)", noticeMsg.FromSession)
	}
}

// TestQueue_NoticeDeliveryFailureDoesNotCauseRecursion verifies that if the
// notice itself fails to deliver, no second notice is generated (preventing recursion).
func TestQueue_NoticeDeliveryFailureDoesNotCauseRecursion(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	h.addRunningSession(t, "sender", activity.Ready)

	// Track how many times Inject is called.
	callCount := 0
	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		callCount++
		// Fail everything to simulate notice delivery also failing.
		return errors.New("boom")
	}

	// Send a message that will fail and generate a notice.
	msgID, err := h.st.AddMessage(store.Message{FromSession: "sender", ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	// Wait for the original message to fail (5 inject attempts).
	waitUntil(t, func() bool { return messageStatus(t, h.st, msgID) == "failed" }, "original message failed")

	// Now process the notice. The notice is now queued for "sender".
	// It should fail after 5 attempts but NOT generate another notice
	// (because notice has FromSession="").
	h.q.Wake("sender")

	// Wait for the notice to fail.
	var noticeID int64
	deadline := time.Now().Add(3 * time.Second)
	for noticeID == 0 && time.Now().Before(deadline) {
		recipientMsgs, _ := h.st.ListQueuedRecipients()
		if len(recipientMsgs) == 0 {
			// The notice has been processed and failed.
			for i := msgID + 1; i <= msgID+10; i++ {
				msg, err := h.st.GetMessage(i)
				if err == store.ErrNotFound {
					continue
				}
				if err != nil {
					t.Fatalf("GetMessage: %v", err)
				}
				if msg.Status == "failed" && msg.FromSession == "" {
					noticeID = msg.ID
					break
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if noticeID == 0 {
		t.Fatal("notice never failed as expected")
	}

	// The total Inject calls should be:
	// - 5 attempts for the original message
	// - 5 attempts for the notice (which has FromSession="")
	// Total: 10 calls
	// There should be no second notice (no recursion).
	expectedCalls := 10 // 5 for original + 5 for notice
	if callCount != expectedCalls {
		t.Fatalf("Inject called %d times, want %d (5 orig + 5 notice, no recursion)", callCount, expectedCalls)
	}

	// Verify that there are no messages for "sender" with status "queued" remaining
	// (the notice was processed, not requeued).
	for i := msgID + 1; i <= msgID+10; i++ {
		msg, err := h.st.GetMessage(i)
		if err == store.ErrNotFound {
			continue
		}
		if err != nil {
			t.Fatalf("GetMessage: %v", err)
		}
		if msg.ToSession == "sender" && msg.Status == "queued" {
			t.Errorf("unexpected queued message for sender: id=%d from=%q", msg.ID, msg.FromSession)
		}
	}
}

// TestQueue_TimeoutExpiryAlsoNotifiesSender verifies that when expireTimedOut()
// marks a message as failed, senders are also notified (same as delivery failure).
func TestQueue_TimeoutExpiryAlsoNotifiesSender(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.QueueTimeout = time.Hour

	// Add a live sender.
	h.addRunningSession(t, "sender", activity.Ready)

	// Create an old message (outside timeout window) from sender to a recipient.
	old := time.Now().Add(-2 * time.Hour).Unix()
	msgID, err := h.st.AddMessage(store.Message{
		FromSession: "sender", ToSession: "recv", Body: "hello", CreatedAt: old,
	})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	// Run expireTimedOut.
	h.q.expireTimedOut()

	// The original message should be failed with reason "timeout".
	if got := messageStatus(t, h.st, msgID); got != "failed" {
		t.Fatalf("status = %q, want failed", got)
	}

	// Sender should have been notified.
	recipientMsgs, err := h.st.ListQueuedRecipients()
	if err != nil {
		t.Fatalf("ListQueuedRecipients: %v", err)
	}

	senderInList := false
	for _, r := range recipientMsgs {
		if r == "sender" {
			senderInList = true
			break
		}
	}
	if !senderInList {
		t.Fatal("sender not in queued recipients after timeout expiry notice")
	}

	// Find and verify the notice.
	var noticeMsg store.Message
	for i := 1; i <= 100; i++ {
		msg, err := h.st.GetMessage(int64(i))
		if err == store.ErrNotFound {
			continue
		}
		if err != nil {
			t.Fatalf("GetMessage: %v", err)
		}
		if msg.ToSession == "sender" && msg.FromSession == "" && msg.Status == "queued" {
			noticeMsg = msg
			break
		}
	}
	if noticeMsg.ID == 0 {
		t.Fatal("timeout notice not found for sender")
	}

	// Verify the notice mentions the timeout reason.
	if !strings.Contains(noticeMsg.Body, "timeout") {
		t.Errorf("notice body = %q, want it to contain 'timeout'", noticeMsg.Body)
	}
}
