package queue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// fakeKimi is fakeAgent with Kimi's pane needs: bracketed paste and the
// Kimi composer parser, plus a scriptable first-turn probe.
type fakeKimi struct {
	*fakeAgent
	started atomic.Bool
}

func (k *fakeKimi) HasStartedTurn(ctx context.Context, ref agent.ActivityRef) (bool, error) {
	if !k.started.Load() {
		return false, agent.ErrNoSignal
	}
	return true, nil
}

func (*fakeKimi) PaneDialect() string       { return runtime.PaneDialectKimi }
func (*fakeKimi) BracketedPaste() bool      { return true }
func (*fakeKimi) FirstMessageByInput() bool { return true }

func kimiPane(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "runtime", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func addKimiSession(t *testing.T, h *testHarness, id string, state activity.State) *fakeKimi {
	t.Helper()
	k := &fakeKimi{fakeAgent: h.ag}
	h.ag.setBusy(true)
	h.q.SetAgentResolver(func(name string) (agent.Agent, error) {
		if name == "kimi-code" {
			return k, nil
		}
		return nil, errors.New("unknown agent " + name)
	})
	if err := h.st.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "kimi-code",
		Branch: "main", WorktreePath: filepath.Join(t.TempDir(), id), TmuxName: id, State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	h.ac.set(id, state)
	return k
}

// The first message of a Kimi session (the brief) is queued at spawn. Kimi
// writes no journal before it, so activity reads as ready from the start;
// the pane decides: nothing is typed until Kimi's composer box is up and
// empty, and then it goes in as a bracketed paste.
func TestQueue_KimiFirstMessageWaitsForEmptyComposer(t *testing.T) {
	h := newTestQueue(t)
	addKimiSession(t, h, "kimi", activity.Ready)

	var pane atomic.Value
	pane.Store("") // TUI not drawn yet
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) { return pane.Load().(string), nil })

	id := addMsg(t, h, "kimi", "brief")
	h.q.Wake("kimi")
	time.Sleep(150 * time.Millisecond)
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times before Kimi's composer was up", n)
	}

	pane.Store(kimiPane(t, "kimi-trust.pane"))
	time.Sleep(150 * time.Millisecond)
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times on the trust screen", n)
	}
	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status = %q, want queued", got)
	}

	pane.Store(kimiPane(t, "kimi-idle.pane"))
	waitUntil(t, func() bool { return h.rt.callCount() == 1 }, "brief injected once the composer is empty")
	opts := h.rt.optsAt(0)
	if !opts.Bracketed || opts.Dialect != runtime.PaneDialectKimi || opts.Force {
		t.Fatalf("InjectOpts = %+v", opts)
	}
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted pending journal proof")
}

func TestQueue_KimiDraftHoldsDelivery(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.ComposerBusyDeadline = time.Hour
	addKimiSession(t, h, "kimi", activity.Ready)
	var busy atomic.Bool
	busy.Store(true)
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) {
		if busy.Load() {
			return kimiPane(t, "kimi-draft.pane"), nil
		}
		return kimiPane(t, "kimi-busy-queued.pane"), nil
	})
	addMsg(t, h, "kimi", "hello")
	h.q.Wake("kimi")
	time.Sleep(150 * time.Millisecond)
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times over a Kimi draft", n)
	}
	// Busy with Kimi's own queue: Kimi takes input mid-turn.
	h.ac.set("kimi", activity.Active)
	busy.Store(false)
	waitUntil(t, func() bool { return h.rt.callCount() == 1 }, "delivered into Kimi's queue")
}

// Claude Code recipients keep the default injection options.
func TestQueue_DefaultInjectOptsUnchanged(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) { return "", nil })
	addMsg(t, h, "recv", strings.Repeat("x", 3))
	h.q.Wake("recv")
	waitUntil(t, func() bool { return h.rt.callCount() == 1 }, "delivered")
	if opts := h.rt.optsAt(0); opts != (runtime.InjectOpts{}) {
		t.Fatalf("InjectOpts = %+v, want zero", opts)
	}
}

// A message parked in Kimi's own queue (rows «❯ …» above the composer) is
// not requeued while it waits there, even if Kimi stays blocked on a
// question for longer than the grace period.
func TestQueue_KimiAcceptedKeptWhileInKimisQueue(t *testing.T) {
	h := newTestQueue(t)
	h.startConfirmer()
	addKimiSession(t, h, "kimi", activity.Active)
	busy := kimiPane(t, "kimi-busy-queued.pane")
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) { return kimiPane(t, "kimi-idle.pane"), nil })
	h.rt.setCapture(func(_ runtime.Handle, _ int) (string, error) { return busy, nil })
	id := addMsg(t, h, "kimi", "reply QUEUED_OK")
	h.q.Wake("kimi")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")

	h.activityTo("kimi", activity.Blocked)
	h.clock.advance(2 * time.Minute)
	h.q.kickConfirmer()
	time.Sleep(150 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "accepted" || h.rt.callCount() != 1 {
		t.Fatalf("status=%q injects=%d, want accepted/1 while Kimi's queue holds the text", got, h.rt.callCount())
	}
}

// The monitor can flip a Kimi session to blocked on a start screen (and set
// its start-screen quiz) between the queue's session read and its activity
// read. The brief must wait for the human's answer, not fail.
func TestQueue_KimiBriefHeldWhenBlockedOnStartScreen(t *testing.T) {
	h := newTestQueue(t)
	addKimiSession(t, h, "kimi", activity.Ready)
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) { return kimiPane(t, "kimi-trust.pane"), nil })
	stale, err := h.st.GetSession("kimi")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetPendingQuiz("kimi", `{"questions":[{"question":"Trust this folder?"}],"source":"permission","permission":{"prompt_id":1,"start_screen":true,"unnumbered":true}}`); err != nil {
		t.Fatal(err)
	}
	h.ac.set("kimi", activity.Blocked)
	first := true
	h.q.getSession = func(id string) (store.Session, error) {
		if first {
			first = false
			return stale, nil // read before the monitor's sweep
		}
		return h.st.GetSession(id)
	}
	id := addMsg(t, h, "kimi", "brief")
	h.q.Wake("kimi")
	time.Sleep(200 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status = %q, want queued until the start screen is answered", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times", n)
	}
}

// The human answered Kimi's trust screen: the quiz is gone, but the stored
// activity is still the start screen's blocked (no journal yet, so nothing
// overrides it) for a sweep. The brief must wait, then go in once the
// composer is up — not fail as recipient_unavailable.
func TestQueue_KimiBriefHeldWhileBlockedBeforeFirstTurn(t *testing.T) {
	h := newTestQueue(t)
	addKimiSession(t, h, "kimi", activity.Blocked)
	var pane atomic.Value
	pane.Store(kimiPane(t, "kimi-trust.pane"))
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) { return pane.Load().(string), nil })
	id := addMsg(t, h, "kimi", "brief")
	h.q.Wake("kimi")
	time.Sleep(200 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status = %q while blocked before the first turn, want queued", got)
	}
	pane.Store(kimiPane(t, "kimi-idle.pane"))
	h.activityTo("kimi", activity.Ready)
	waitUntil(t, func() bool { return h.rt.callCount() == 1 }, "brief injected once ready")
}

// After the first turn a blocked Kimi (failed turn) fails new messages as
// before.
func TestQueue_KimiBlockedAfterFirstTurnFails(t *testing.T) {
	h := newTestQueue(t)
	k := addKimiSession(t, h, "kimi", activity.Blocked)
	k.started.Store(true)
	id := addMsg(t, h, "kimi", "hello")
	h.q.Wake("kimi")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "failed as recipient_unavailable")
}
