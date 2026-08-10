package queue

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// heldDialogPane is a live capture (Claude Code 2.1.226) of a pane showing the
// held-peer-message approval dialog, shared with the runtime package's own
// detector tests.
func heldDialogPane(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "runtime", "testdata", "held-dialog-open.pane"))
	if err != nil {
		t.Fatalf("read held-dialog fixture: %v", err)
	}
	return string(b)
}

// TestQueue_HeldDialogHoldsDeliveryThenResumes is the bug this task exists
// for: injecting while that dialog is open dismisses it, so the held copy is
// dropped AND the injected text never reaches the conversation — while the
// scrollback banner makes the queue believe it was delivered. The message
// must simply wait, exactly like it does for a pending quiz.
func TestQueue_HeldDialogHoldsDeliveryThenResumes(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.ComposerBusyDeadline = time.Hour
	h.addRunningSession(t, "recv", activity.Ready)

	var dialogOpen atomic.Bool
	dialogOpen.Store(true)
	pane := heldDialogPane(t)
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) {
		if dialogOpen.Load() {
			return pane, nil
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
		t.Fatalf("status = %q while the held-message dialog is open, want queued", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times while the dialog is open, want 0", n)
	}
	m, err := h.st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.Attempts != 0 {
		t.Errorf("attempts = %d while deferred, want 0 (a deferral is not a failed attempt)", m.Attempts)
	}

	dialogOpen.Store(false)
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"message delivered once the held dialog is gone")
}

// TestQueue_HeldDialogNeverForced pins that the dialog is not subject to the
// composer busy-deadline escape hatch. Forcing a draft through costs a
// recoverable draft; forcing this through destroys the message itself.
func TestQueue_HeldDialogNeverForced(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.ComposerBusyDeadline = time.Nanosecond // deadline long past
	h.addRunningSession(t, "recv", activity.Ready)

	pane := heldDialogPane(t)
	h.rt.setCaptureEscaped(func(_ runtime.Handle, _ int) (string, error) { return pane, nil })

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	time.Sleep(300 * time.Millisecond)
	if n := h.rt.callCount(); n != 0 {
		t.Fatalf("Inject called %d times past the busy deadline, want 0 — the dialog has no deadline", n)
	}
	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Fatalf("status = %q, want queued", got)
	}
}

// TestQueue_InjectHeldDialogErrorRequeues covers the race the pane gate
// cannot close on its own: the dialog opened between the gate's capture and
// Inject's own. Nothing was typed, so no attempt may be burned.
func TestQueue_InjectHeldDialogErrorRequeues(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	var refused atomic.Int32
	h.rt.setInject(func(callIdx int, _ runtime.Handle, _ string) error {
		if callIdx == 0 {
			refused.Add(1)
			return runtime.ErrHeldDialog
		}
		return nil
	})

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"message delivered on the pass after the dialog closed")
	if refused.Load() != 1 {
		t.Fatalf("first Inject was not the refused one (%d)", refused.Load())
	}
	m, err := h.st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.Attempts != 1 {
		t.Errorf("attempts = %d, want 1 — a refused inject is a deferral, not an attempt", m.Attempts)
	}
}
