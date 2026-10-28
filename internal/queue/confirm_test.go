package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// startConfirmer runs the confirmer loop under the harness context.
func (h *testHarness) startConfirmer() { go h.q.confirmLoop(h.ctx) }

// queuedByAgent makes every Inject report the Codex pending-queue outcome.
func (h *testHarness) queuedByAgent() {
	h.rt.setInject(func(idx int, hd runtime.Handle, text string) error {
		return fmt.Errorf("%w: session %q", runtime.ErrQueuedByAgent, hd.Name)
	})
}

// activityTo flips the recipient's activity and publishes the bus event the
// monitor would.
func (h *testHarness) activityTo(id string, s activity.State) {
	h.ac.set(id, s)
	h.b.Publish("session.activity_changed", id, map[string]any{"to": string(s)})
}

func TestQueuedByAgentBecomesAcceptedThenDelivered(t *testing.T) {
	h := newTestQueue(t)
	h.startConfirmer()
	h.addCodexSession(t, "cx", activity.Active)
	h.ag.setBusy(true)
	h.queuedByAgent()
	rec := recordBus(h)
	defer rec.cancel()

	var since time.Time
	h.ag.setConfirm(func(text string, s time.Time) (bool, time.Time, error) {
		since = s
		if text == "[from o] hi" {
			return true, s.Add(time.Second), nil
		}
		return false, time.Time{}, nil
	})
	id, _ := h.st.AddMessage(store.Message{FromSession: "o", ToSession: "cx", Body: "hi"})
	h.q.Wake("cx")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered after confirmation")
	m, _ := h.st.GetMessage(id)
	if m.DeliveredAt == 0 || m.InjectAt == 0 || since.UnixMilli() != m.InjectAt {
		t.Fatalf("message %+v, since=%v", m, since)
	}
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want 1", n)
	}
	got := rec.types("cx")
	if !contains(got, "message.accepted") || !contains(got, "message.delivered") {
		t.Fatalf("bus events = %v", got)
	}
}

func TestOneRecordConfirmsTwoMergedMessages(t *testing.T) {
	h := newTestQueue(t)
	h.startConfirmer()
	h.addCodexSession(t, "cx", activity.Ready)
	h.queuedByAgent()
	at := h.clock.now().Add(time.Second)
	// Codex merged both into one user record stamped at the same instant.
	h.ag.confirmRecords(map[string][]time.Time{"A": {at}, "B": {at}})
	id1 := addMsg(t, h, "cx", "A")
	id2 := addMsg(t, h, "cx", "B")
	h.q.Wake("cx")

	waitUntil(t, func() bool {
		return messageStatus(t, h.st, id1) == "delivered" && messageStatus(t, h.st, id2) == "delivered"
	}, "both confirmed by one record")
	if n := h.rt.callCount(); n != 2 {
		t.Fatalf("Inject called %d times, want 2", n)
	}
}

func TestTwoIdenticalMessagesNeedTwoTranscriptRecords(t *testing.T) {
	h := newTestQueue(t)
	h.startConfirmer()
	h.addCodexSession(t, "cx", activity.Ready)
	h.queuedByAgent()
	t1 := h.clock.now().Add(time.Second)
	t2 := t1.Add(time.Second)
	h.ag.confirmRecords(map[string][]time.Time{"same": {t1}})
	id1 := addMsg(t, h, "cx", "same")
	id2 := addMsg(t, h, "cx", "same")
	h.q.Wake("cx")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id1) == "delivered" }, "first confirmed")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id2) == "accepted" }, "second accepted")
	h.q.kickConfirmer()
	time.Sleep(150 * time.Millisecond)
	if got := messageStatus(t, h.st, id2); got != "accepted" {
		t.Fatalf("second identical message = %q, want accepted (one record must not confirm two messages)", got)
	}
	// The dedup moved since past t1 for the second message.
	var askedPastT1 bool
	for _, c := range h.ag.confirmCalls() {
		if c.text == "same" && !c.since.Before(t1) {
			askedPastT1 = true
		}
	}
	if !askedPastT1 {
		t.Fatalf("confirmer never searched past the used record: %+v", h.ag.confirmCalls())
	}

	h.ag.confirmRecords(map[string][]time.Time{"same": {t1, t2}})
	h.q.kickConfirmer()
	waitUntil(t, func() bool { return messageStatus(t, h.st, id2) == "delivered" }, "second confirmed by the second record")
	if n := h.rt.callCount(); n != 2 {
		t.Fatalf("Inject called %d times, want 2", n)
	}
}

func TestAcceptedStaysWhileRecipientActive(t *testing.T) {
	h := newTestQueue(t)
	h.startConfirmer()
	h.addCodexSession(t, "cx", activity.Active)
	h.ag.setBusy(true)
	h.queuedByAgent()
	rec := recordBus(h)
	defer rec.cancel()
	id := addMsg(t, h, "cx", "long one")
	h.q.Wake("cx")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")

	// A foreground command holds the turn far past queue_timeout (30m).
	h.clock.advance(31 * time.Minute)
	h.q.kickConfirmer()
	h.q.expireTimedOut()
	time.Sleep(150 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "accepted" {
		t.Fatalf("status = %q, want accepted while the recipient is active", got)
	}
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want 1", n)
	}
	if contains(rec.types("cx"), "message.failed") {
		t.Fatal("message.failed published while the recipient is active")
	}
}

func TestAcceptedRequeuedAfterTurnEndsWithoutConfirmation(t *testing.T) {
	h := newTestQueue(t)
	h.startConfirmer()
	h.addCodexSession(t, "cx", activity.Active)
	h.ag.setBusy(true)
	h.queuedByAgent()
	id := addMsg(t, h, "cx", "lost")
	h.q.Wake("cx")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")

	h.activityTo("cx", activity.Ready)
	h.clock.advance(59 * time.Second)
	h.q.kickConfirmer()
	time.Sleep(150 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "accepted" {
		t.Fatalf("status = %q at 59s, want accepted", got)
	}

	h.clock.advance(2 * time.Second)
	h.q.kickConfirmer()
	// Requeue consumes the attempt and the worker pastes again at once.
	waitUntil(t, func() bool { return h.rt.callCount() == 2 }, "re-injected after the 60s grace")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted again")
	m, _ := h.st.GetMessage(id)
	if m.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", m.Attempts)
	}
}

func TestAcceptedKeptWhilePaneShowsQueuedItem(t *testing.T) {
	h := newTestQueue(t)
	h.startConfirmer()
	h.addCodexSession(t, "cx", activity.Ready)
	h.queuedByAgent()
	h.rt.setCapture(func(hd runtime.Handle, lines int) (string, error) {
		return strings.Join([]string{
			"• Messages to be submitted after next tool call (press esc to interrupt and send immediately)",
			"  ↳ still waiting here",
			"› Ask Codex to do anything",
			"  gpt-6 · 12% context left",
		}, "\n"), nil
	})
	id := addMsg(t, h, "cx", "still waiting here")
	h.q.Wake("cx")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")

	h.clock.advance(2 * time.Minute)
	h.q.kickConfirmer()
	time.Sleep(150 * time.Millisecond)
	if got := messageStatus(t, h.st, id); got != "accepted" || h.rt.callCount() != 1 {
		t.Fatalf("status=%q injects=%d, want accepted/1 while the pane still queues the text", got, h.rt.callCount())
	}
}

func TestAcceptedRequeuedWhenRecipientExits(t *testing.T) {
	h := newTestQueue(t)
	h.startConfirmer()
	h.addCodexSession(t, "cx", activity.Ready)
	h.queuedByAgent()
	rec := recordBus(h)
	defer rec.cancel()
	id := addMsg(t, h, "cx", "doomed")
	h.q.Wake("cx")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")

	h.activityTo("cx", activity.Exited)
	// Requeued at once, then the worker applies the existing exited rule.
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "failed after requeue to an exited recipient")
	m, _ := h.st.GetMessage(id)
	if m.Reason != "recipient_unavailable" || m.Attempts != 1 {
		t.Fatalf("message %+v", m)
	}
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want 1", n)
	}
}

func TestAcceptedRequeuedWhenRecipientRestored(t *testing.T) {
	h := newTestQueue(t)
	h.startConfirmer()
	h.addCodexSession(t, "cx", activity.Ready)
	h.queuedByAgent()
	id := addMsg(t, h, "cx", "survives restore")
	h.q.Wake("cx")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")
	first, _ := h.st.GetMessage(id)

	h.clock.advance(time.Second)
	h.b.Publish("session.restored", "cx", nil)
	waitUntil(t, func() bool { return h.rt.callCount() == 2 }, "re-injected into the restored recipient")
	waitUntil(t, func() bool {
		m, _ := h.st.GetMessage(id)
		return m.Status == "accepted" && m.InjectAt > first.InjectAt
	}, "accepted again with a fresh inject_at")
}

func TestAcceptedFailsNotConfirmedAfterMaxAttempts(t *testing.T) {
	h := newTestQueue(t)
	h.addCodexSession(t, "cx", activity.Ready)
	h.addRunningSession(t, "o", activity.Ready)
	rec := recordBus(h)
	defer rec.cancel()
	id, _ := h.st.AddMessage(store.Message{FromSession: "o", ToSession: "cx", Body: "never lands", Status: "accepted", Attempts: maxAttempts})
	_ = h.st.SetMessageInjectAt(id, h.clock.now().Add(-61*time.Second).UnixMilli())

	h.startConfirmer()
	h.q.kickConfirmer()

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "failed not_confirmed")
	m, _ := h.st.GetMessage(id)
	if m.Reason != "not_confirmed" {
		t.Fatalf("reason = %q", m.Reason)
	}
	if got := rec.types("cx"); !contains(got, "message.failed") {
		t.Fatalf("bus events = %v", got)
	}
	// The sender was told, through the usual failure notice.
	waitUntil(t, func() bool {
		msgs, _ := h.st.ListMessages("o", 0)
		for _, n := range msgs {
			if n.ToSession == "o" && strings.Contains(n.Body, "not_confirmed") {
				return true
			}
		}
		return false
	}, "sender notified")
	if n := h.rt.callCount(); n > 1 {
		t.Fatalf("Inject called %d times for an exhausted message", n)
	}
}

// newTestQueueOn builds a second harness over an existing store, the way a
// restarted daemon sees the rows a previous one left behind.
func newTestQueueOn(t *testing.T, prev *testHarness) *testHarness {
	t.Helper()
	rt := &fakeRuntime{}
	ag := &fakeAgent{}
	clock := newFakeClock()
	q := New(prev.st, prev.b, rt, prev.q.cfg, prev.ac.get)
	q.backoff = func(attempt int) time.Duration { return time.Millisecond }
	q.SetAgentResolver(func(name string) (agent.Agent, error) {
		if name == "codex" {
			return ag, nil
		}
		return nil, errors.New("unknown agent " + name)
	})
	q.now = clock.now
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		waitUntilTimeout(t, 2*time.Second, func() bool {
			q.mu.Lock()
			defer q.mu.Unlock()
			return len(q.workers) == 0
		}, "second queue's workers to exit")
	})
	return &testHarness{q: q, st: prev.st, b: prev.b, rt: rt, ac: prev.ac, ag: ag, clock: clock, ctx: ctx, cancel: cancel}
}

func TestRecoverKeepsAcceptedAndConfirmsAfterRestart(t *testing.T) {
	h1 := newTestQueue(t)
	h1.addCodexSession(t, "cx", activity.Ready)
	id, _ := h1.st.AddMessage(store.Message{ToSession: "cx", Body: "pre-restart", Status: "accepted", Attempts: 1})
	_ = h1.st.SetMessageInjectAt(id, h1.clock.now().Add(-5*time.Second).UnixMilli())
	// A sibling orphaned in "delivering" is still reset to queued by Recover.
	orphan, _ := h1.st.AddMessage(store.Message{ToSession: "cx", Body: "orphan", Status: "delivering", Attempts: 1})
	h1.cancel()

	h2 := newTestQueueOn(t, h1)
	h2.ag.confirmRecords(map[string][]time.Time{"pre-restart": {h2.clock.now()}})
	h2.q.Recover(h2.ctx)
	h2.startConfirmer()

	waitUntil(t, func() bool { return messageStatus(t, h2.st, id) == "delivered" }, "accepted message confirmed after restart")
	waitUntil(t, func() bool { return messageStatus(t, h2.st, orphan) == "accepted" }, "orphan requeued by Recover and pasted")
	for _, c := range h2.rt.calls() {
		if c == "pre-restart" {
			t.Fatal("accepted message was pasted again after restart")
		}
	}
}
