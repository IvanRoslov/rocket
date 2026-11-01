package queue

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/store"
)

func appendQueueClaudeProof(t *testing.T, v claudeinstance.Snapshot, record string) {
	t.Helper()
	raw, e := json.Marshal(map[string]any{"type": "user", "sessionId": v.Target.ClaudeSessionID, "uuid": record, "message": map[string]string{"role": "user", "content": v.Envelope}})
	if e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(v.Boundary.Path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write(append(raw, '\n')); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
}
func TestClaudeConfirmationRequiresExactProofAndSurvivesRestore(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Active)
	f := installClaudeSocket(t, h, true)
	f.outcome = SocketHeld
	id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
	msg, _ := h.st.GetMessage(id)
	h.q.deliver(h.ctx, msg)
	v, e := h.st.GetClaudeDelivery(id)
	if e != nil {
		t.Fatal(e)
	}
	// A replacement current generation/config cannot redirect the immutable send.
	l, _, _ := h.st.GetClaudeLaunch("recv")
	l.Generation = "replacement"
	h.st.PutClaudeLaunch(l)
	h.q.cfg.SocketDelivery = false
	h.q.SetSocketSender(nil)
	h.ac.set("recv", activity.Exited)
	state := &confirmState{used: make(map[string]map[recordKey]struct{}), restored: map[string]bool{"recv": true}}
	if n := h.q.confirmPass(h.ctx, state); n != 1 {
		t.Fatalf("held pending=%d", n)
	}
	msg, _ = h.st.GetMessage(id)
	if msg.Status != "accepted" {
		t.Fatalf("restore requeued %+v", msg)
	}
	appendQueueClaudeProof(t, v, "exact-record")
	if n := h.q.confirmPass(h.ctx, state); n != 0 {
		t.Fatalf("exact proof pending=%d", n)
	}
	msg, _ = h.st.GetMessage(id)
	if msg.Status != "delivered" || msg.Attempts != 1 || f.sendCount() != 1 || h.rt.callCount() != 0 {
		t.Fatalf("settlement %+v sends=%d inject=%d", msg, f.sendCount(), h.rt.callCount())
	}
	h.q.confirmPass(h.ctx, state)
}
func TestClaudeConfirmationDeadlineAndCancellationNeverReplay(t *testing.T) {
	for _, which := range []string{"deadline", "cancel", "missing journal"} {
		t.Run(which, func(t *testing.T) {
			h := newTestQueue(t)
			h.addRunningSession(t, "recv", activity.Ready)
			f := installClaudeSocket(t, h, true)
			id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
			msg, _ := h.st.GetMessage(id)
			h.q.deliver(h.ctx, msg)
			v, _ := h.st.GetClaudeDelivery(id)
			appendQueueClaudeProof(t, v, "late-record")
			ctx := h.ctx
			if which == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if which == "deadline" {
				h.q.cfg.QueueTimeout = 0 // saved positive deadline remains authoritative
				h.q.now = func() time.Time { return v.Deadline.Add(time.Second) }
			}
			if which == "missing journal" {
				os.Remove(v.Boundary.Path)
			}
			msg, _ = h.st.GetMessage(id)
			h.q.confirmClaudeRecipient(ctx, []store.Message{msg})
			msg, _ = h.st.GetMessage(id)
			want := "accepted"
			if which == "deadline" {
				want = "failed"
			}
			if msg.Status != want || (want == "failed" && msg.Reason != "delivery_unconfirmed") || f.sendCount() != 1 || h.rt.callCount() != 0 {
				t.Fatalf("unsafe timeout/cancel %+v", msg)
			}
		})
	}
}
func TestClaudePrewritePersistenceFailureSendsNothing(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	f := installClaudeSocket(t, h, true)
	f.beforeClaim = func() { l, _, _ := h.st.GetClaudeLaunch("recv"); l.Generation = "replaced"; h.st.PutClaudeLaunch(l) }
	id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
	msg, _ := h.st.GetMessage(id)
	sess, _ := h.st.GetSession("recv")
	h.q.deliverClaude(h.ctx, msg, sess)
	got, _ := h.st.GetMessage(id)
	if got.Status != "queued" || got.Attempts != 0 || f.sendCount() != 0 || h.rt.callCount() != 0 {
		t.Fatalf("persistence failure sent %+v", got)
	}
}

func TestClaudeHousekeepingOwnedQueuedUsesSavedDeadlineWithConfigDisabled(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	f := installClaudeSocket(t, h, true)
	id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
	m, _ := h.st.GetMessage(id)
	h.q.deliver(h.ctx, m)
	h.q.cfg.SocketDelivery = false
	h.q.cfg.QueueTimeout = -time.Minute
	// Accepted side effects remain under the Claude confirmer's immutable
	// deadline, unaffected by the configured generic negative cutoff.
	h.q.expireTimedOut()
	m, _ = h.st.GetMessage(id)
	if m.Status != "accepted" || m.Attempts != 1 || f.sendCount() != 1 || h.rt.callCount() != 0 {
		t.Fatalf("generic expiry altered accepted owner %+v", m)
	}
}
