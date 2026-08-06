package queue

import (
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// TestQueue_NotDeliveredRetriesThenFails covers runtime.ErrNotDelivered:
// Inject cleared the composer and nothing was submitted, so there is
// nothing to be careful about — retry, and once the attempts run out mark
// the message failed so the sender is actually told. Reporting this as
// "delivered" (which the ErrSubmitUnconfirmed branch does on an empty
// tail) would silently drop the message.
func TestQueue_NotDeliveredRetriesThenFails(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		return runtime.ErrNotDelivered
	}
	h.rt.captureFn = func(hd runtime.Handle, lines int) (string, error) {
		// The composer was cleared, so the marker is gone from the tail —
		// exactly the shape that must NOT be read as a delivery.
		return "some unrelated pane output\n", nil
	}

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" },
		"message failed after repeated known non-delivery")

	if n := h.rt.callCount(); n != maxAttempts {
		t.Fatalf("Inject called %d times, want %d (retried to exhaustion)", n, maxAttempts)
	}
	m, err := h.st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.Reason != "delivery_failed" {
		t.Fatalf("failure reason = %q, want %q", m.Reason, "delivery_failed")
	}
}

// TestQueue_NotDeliveredRetriesThenSucceeds is the happy tail of the same
// branch: a known non-delivery is retried, and a later attempt that lands
// marks the message delivered.
func TestQueue_NotDeliveredRetriesThenSucceeds(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		if idx == 0 {
			return runtime.ErrNotDelivered
		}
		return nil
	}

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"delivered after retrying a known non-delivery")

	time.Sleep(50 * time.Millisecond)
	if n := h.rt.callCount(); n != 2 {
		t.Fatalf("Inject called %d times, want 2", n)
	}
}
