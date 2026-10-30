package queue

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

type fakePi struct{ *fakeAgent }

func (*fakePi) PaneDialect() string  { return runtime.PaneDialectPi }
func (*fakePi) BracketedPaste() bool { return true }

func piPane(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "runtime", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func addPiSession(t *testing.T, h *testHarness, id string, state activity.State) *fakePi {
	t.Helper()
	p := &fakePi{fakeAgent: h.ag}
	h.ag.setBusy(true)
	h.q.SetAgentResolver(func(name string) (agent.Agent, error) {
		if name == "pi" {
			return p, nil
		}
		return nil, errors.New("unknown agent " + name)
	})
	if err := h.st.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "pi",
		Branch: "main", WorktreePath: filepath.Join(t.TempDir(), id), TmuxName: id, State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	h.ac.set(id, state)
	return p
}

func TestQueue_PiFirstMessageWaitsForEmptyComposer(t *testing.T) {
	h := newTestQueue(t)
	addPiSession(t, h, "pi", activity.Ready)
	var pane atomic.Value
	pane.Store("")
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) { return pane.Load().(string), nil })

	id := addMsg(t, h, "pi", "brief")
	h.q.Wake("pi")
	time.Sleep(150 * time.Millisecond)
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times before Pi's composer was up", n)
	}
	pane.Store(piPane(t, "pi-draft.pane"))
	time.Sleep(150 * time.Millisecond)
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times over a human draft", n)
	}
	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status = %q, want queued", got)
	}

	pane.Store(piPane(t, "pi-idle.pane"))
	waitUntil(t, func() bool { return h.rt.callCount() == 1 }, "brief injected once the composer is empty")
	opts := h.rt.optsAt(0)
	if !opts.Bracketed || opts.Dialect != runtime.PaneDialectPi || opts.Force {
		t.Fatalf("InjectOpts = %+v", opts)
	}
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted pending journal proof")
}

func TestQueue_PiDraftHoldsDelivery(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.ComposerBusyDeadline = time.Hour
	addPiSession(t, h, "pi", activity.Ready)
	var pane atomic.Value
	pane.Store(piPane(t, "pi-draft.pane"))
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) { return pane.Load().(string), nil })
	addMsg(t, h, "pi", "hello")
	h.q.Wake("pi")
	time.Sleep(150 * time.Millisecond)
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times over a Pi draft", n)
	}

	h.ac.set("pi", activity.Active)
	pane.Store(piPane(t, "pi-busy-queued.pane"))
	waitUntil(t, func() bool { return h.rt.callCount() == 1 }, "delivered into Pi's steering queue")
}

func TestQueue_PiDraftNeverForcedAfterDeadline(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.ComposerBusyDeadline = time.Nanosecond
	addPiSession(t, h, "pi", activity.Ready)
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) { return piPane(t, "pi-draft.pane"), nil })
	id := addMsg(t, h, "pi", "hello")
	h.q.Wake("pi")
	time.Sleep(250 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status = %q, want queued after the force deadline", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times after the force deadline, want 0", n)
	}
}

func TestQueue_PiAcceptedKeptWhileInPisQueue(t *testing.T) {
	h := newTestQueue(t)
	h.startConfirmer()
	addPiSession(t, h, "pi", activity.Active)
	busy := piPane(t, "pi-busy-queued.pane")
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) { return piPane(t, "pi-idle.pane"), nil })
	h.rt.setCapture(func(_ runtime.Handle, _ int) (string, error) { return busy, nil })
	id := addMsg(t, h, "pi", "<redacted-steering>")
	h.q.Wake("pi")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")

	h.activityTo("pi", activity.Blocked)
	h.clock.advance(2 * time.Minute)
	h.q.kickConfirmer()
	time.Sleep(150 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "accepted" || h.rt.callCount() != 1 {
		t.Fatalf("status=%q injects=%d, want accepted/1 while Pi owns the steering", got, h.rt.callCount())
	}

	at := h.clock.now().Add(time.Second)
	h.ag.confirmRecords(map[string][]time.Time{"<redacted-steering>": {at}})
	h.q.kickConfirmer()
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "journal proof delivered without repaste")
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want 1", n)
	}
}
