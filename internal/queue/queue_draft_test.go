package queue

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// draftPane is a capture of a composer holding an unsent human draft (real
// shape: sigil, NO-BREAK SPACE, text, closing rule).
var draftPane = strings.Join([]string{
	"✻ Brewed for 2m 38s",
	"────────────────────────────────",
	"❯ drop the deprecated always-auth key",
	"────────────────────────────────",
	"  ⏵⏵ bypass permissions on (shift+tab to cycle)",
}, "\n")

// TestQueue_DraftHoldsDeliveryThenResumes is the user-visible promise: while
// someone is typing, their draft is neither erased nor raced — the message
// waits, stays queued, and burns no retry attempts. Once the composer is
// free the normal delivery cycle picks it up.
func TestQueue_DraftHoldsDeliveryThenResumes(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.ComposerBusyDeadline = time.Hour
	h.addRunningSession(t, "recv", activity.Ready)

	// atomic: the delivery goroutine reads this through setCapture while the
	// test body flips it below.
	var busy atomic.Bool
	busy.Store(true)
	h.rt.setCapture(func(_ runtime.Handle, _ int) (string, error) {
		if busy.Load() {
			return draftPane, nil
		}
		return "", nil
	})

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	time.Sleep(150 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status = %q while the composer holds a draft, want queued", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times while the composer holds a draft, want 0", n)
	}
	m, err := h.st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.Attempts != 0 {
		t.Errorf("attempts = %d while deferred, want 0 (a deferral is not a failed attempt)", m.Attempts)
	}

	busy.Store(false)
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"message delivered once the composer is free")
	if opts := h.rt.optsAt(0); opts.Force {
		t.Errorf("delivery on a free composer must not be forced")
	}
}

// TestQueue_DraftDeadlineForcesDelivery covers the safety valve: an
// abandoned draft (or a false positive of the heuristic) must not block a
// recipient's queue forever, so past composer_busy_deadline the message is
// delivered the old way, clearing whatever is in the composer.
func TestQueue_DraftDeadlineForcesDelivery(t *testing.T) {
	h := newTestQueue(t)
	// Everything about the deadline is time-based; a tiny one keeps the
	// test honest and fast.
	h.q.cfg.ComposerBusyDeadline = 50 * time.Millisecond
	h.addRunningSession(t, "recv", activity.Ready)

	h.rt.setCapture(func(_ runtime.Handle, _ int) (string, error) { return draftPane, nil })

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"message delivered after the busy deadline")
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want 1", n)
	}
	if opts := h.rt.optsAt(0); !opts.Force {
		t.Errorf("delivery past the busy deadline must pass InjectOpts{Force: true}")
	}
}

// TestQueue_ComposerBusyFromInjectDoesNotBurnAnAttempt covers the race the
// guard inside Inject exists for: the human started typing between the
// queue's probe and the C-u. The message must go back to queued, untouched,
// without consuming one of its retry attempts.
func TestQueue_ComposerBusyFromInjectDoesNotBurnAnAttempt(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.ComposerBusyDeadline = time.Hour
	h.addRunningSession(t, "recv", activity.Ready)

	// The probe sees a free composer; Inject reports the draft that
	// appeared a moment later. From the second call on, the pane reads busy
	// so the message settles in the deferred state instead of spinning.
	var injected int
	h.rt.setCapture(func(_ runtime.Handle, _ int) (string, error) {
		if injected > 0 {
			return draftPane, nil
		}
		return "", nil
	})
	h.rt.setInject(func(_ int, _ runtime.Handle, _ string) error {
		injected++
		return runtime.ErrComposerBusy
	})

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return h.rt.callCount() >= 1 }, "Inject attempted once")
	time.Sleep(150 * time.Millisecond)

	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status = %q after ErrComposerBusy, want queued", got)
	}
	m, err := h.st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.Attempts != 0 {
		t.Errorf("attempts = %d after ErrComposerBusy, want 0", m.Attempts)
	}
}

// TestQueue_UnreadablePaneDoesNotBlockDelivery pins the fail-open rule: a
// capture failure is not evidence of a draft, and must not defer anything.
func TestQueue_UnreadablePaneDoesNotBlockDelivery(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.ComposerBusyDeadline = time.Hour
	h.addRunningSession(t, "recv", activity.Ready)

	h.rt.setCapture(func(_ runtime.Handle, _ int) (string, error) {
		return "", errNoPane
	})

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"message delivered despite an unreadable pane")
}

// errNoPane stands in for tmux's "no such pane" — a capture that failed.
var errNoPane = errors.New("capture-pane: no such pane")
