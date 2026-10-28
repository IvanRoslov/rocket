package queue

import (
	"fmt"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// busRecorder collects the types of message.* events published for one
// recipient, in order.
type busRecorder struct {
	events <-chan store.Event
	cancel func()
}

func recordBus(h *testHarness) *busRecorder {
	ch, cancel := h.b.Subscribe()
	return &busRecorder{events: ch, cancel: cancel}
}

// types drains what has arrived so far (after a short settle) and returns
// the event types for session id.
func (r *busRecorder) types(id string) []string {
	var out []string
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case e := <-r.events:
			if e.SessionID == id {
				out = append(out, e.Type)
			}
		case <-timer.C:
			return out
		}
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestCodexBusyRecipientGetsMessageWhileActive(t *testing.T) {
	h := newTestQueue(t)
	h.ag.setBusy(true)
	h.addCodexSession(t, "cx", activity.Active)
	id := addMsg(t, h, "cx", "hello")

	before := h.clock.now().UnixMilli()
	h.q.Wake("cx")

	waitUntil(t, func() bool { return h.rt.callCount() == 1 }, "inject while the recipient is active")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")
	m, _ := h.st.GetMessage(id)
	if m.InjectAt < before || m.InjectAt > h.clock.now().UnixMilli() {
		t.Fatalf("inject_at = %d, want within [%d, now]", m.InjectAt, before)
	}
	if m.DeliveredAt != 0 || m.Attempts != 1 {
		t.Fatalf("accepted message: %+v", m)
	}
}

func TestQueuedByAgentBecomesAccepted(t *testing.T) {
	h := newTestQueue(t)
	h.addCodexSession(t, "cx", activity.Ready)
	h.rt.setInject(func(idx int, hd runtime.Handle, text string) error {
		return fmt.Errorf("%w: session %q", runtime.ErrQueuedByAgent, hd.Name)
	})
	rec := recordBus(h)
	defer rec.cancel()
	id := addMsg(t, h, "cx", "hello")

	h.q.Wake("cx")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted after the agent queued the text")
	time.Sleep(50 * time.Millisecond)
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want 1 (no redelivery of queued text)", n)
	}
	got := rec.types("cx")
	if !contains(got, "message.accepted") || contains(got, "message.delivered") {
		t.Fatalf("bus events = %v, want message.accepted and no message.delivered", got)
	}
}

func TestAcceptedDoesNotBlockNextMessage(t *testing.T) {
	h := newTestQueue(t)
	h.addCodexSession(t, "cx", activity.Ready)
	id1 := addMsg(t, h, "cx", "first")
	id2 := addMsg(t, h, "cx", "second")

	h.q.Wake("cx")

	// No confirmer is running, so nothing ever becomes delivered: the second
	// message must still be pasted right behind the first.
	waitUntil(t, func() bool { return h.rt.callCount() == 2 }, "second message injected while the first is accepted")
	waitUntil(t, func() bool {
		return messageStatus(t, h.st, id1) == "accepted" && messageStatus(t, h.st, id2) == "accepted"
	}, "both accepted")
	if calls := h.rt.calls(); calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("order = %v", calls)
	}
}

func TestClaudeRecipientPathUnchanged(t *testing.T) {
	h := newTestQueue(t)
	h.ag.setBusy(true) // irrelevant for a claude-code recipient
	h.addRunningSession(t, "cl", activity.Active)
	rec := recordBus(h)
	defer rec.cancel()
	id := addMsg(t, h, "cl", "hello")

	h.q.Wake("cl")

	time.Sleep(150 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "queued" || h.rt.callCount() != 0 {
		t.Fatalf("active claude recipient: status=%q injects=%d, want queued/0", got, h.rt.callCount())
	}

	h.ac.set("cl", activity.Ready)
	h.b.Publish("session.activity_changed", "cl", map[string]any{"to": "ready"})
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered directly")
	m, _ := h.st.GetMessage(id)
	if m.InjectAt != 0 || m.DeliveredAt == 0 {
		t.Fatalf("claude message: %+v", m)
	}
	if got := rec.types("cl"); contains(got, "message.accepted") || !contains(got, "message.delivered") {
		t.Fatalf("bus events = %v", got)
	}

	// ErrQueuedByAgent without a confirmer still means "delivered after one
	// paste": re-injecting would queue a duplicate.
	h.rt.setInject(func(idx int, hd runtime.Handle, text string) error {
		return fmt.Errorf("%w: session %q", runtime.ErrQueuedByAgent, hd.Name)
	})
	id2 := addMsg(t, h, "cl", "again")
	h.q.Wake("cl")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id2) == "delivered" }, "delivered after the agent queued the text")
	time.Sleep(50 * time.Millisecond)
	if n := h.rt.callCount(); n != 2 {
		t.Fatalf("Inject called %d times, want 2", n)
	}
}
