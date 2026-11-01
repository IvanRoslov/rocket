package queue

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"os"
	"reflect"
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

	l.StartedAt = l.StartedAt.Add(time.Nanosecond)
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
	f.beforeClaim = func() {
		l, _, _ := h.st.GetClaudeLaunch("recv")
		l.Generation = "replaced"
		l.StartedAt = l.StartedAt.Add(time.Nanosecond)
		h.st.PutClaudeLaunch(l)
	}
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

func TestClaudeMixedEnvelopeVersionsRecoverWithoutRewriteOrResend(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	f := installClaudeSocket(t, h, true)
	// Retained v0 history is a fixture from the previous format, not a new v0 claim.
	now := time.Now().UTC()
	boundary, e := claudeinstance.PinJournal(f.target)
	if e != nil {
		t.Fatal(e)
	}
	legacyID, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "same feedback", Status: "accepted", Attempts: 1, DeliveryTransport: ClaudeSocketTransport})
	wire, e := socketmsg.NewMsgID()
	if e != nil {
		t.Fatal(e)
	}
	legacy := claudeinstance.Snapshot{MessageID: legacyID, Transport: ClaudeSocketTransport, Target: f.target, WireID: wire, Envelope: socketmsg.Envelope("", "rocket", "same feedback"), Boundary: boundary, PreparedAt: now, Deadline: now.Add(time.Hour)}
	raw, e := json.Marshal(legacy)
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256([]byte(legacy.Envelope))
	db, e := sql.Open("sqlite", h.dbPath)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = db.Exec(`INSERT INTO claude_deliveries(message_id,transport,wire_id,snapshot_json,claude_uuid,journal_device,journal_inode,journal_path,boundary_offset,envelope_hash,phase,deadline_ns) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, legacyID, legacy.Transport, legacy.WireID, string(raw), legacy.Target.ClaudeSessionID, boundary.Device, boundary.Inode, boundary.Path, boundary.Offset, hex.EncodeToString(hash[:]), "held", legacy.Deadline.UnixNano())
	if e != nil {
		t.Fatal(e)
	}
	id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "same feedback"})
	msg, _ := h.st.GetMessage(id)
	h.q.deliver(h.ctx, msg)
	modern, e := h.st.GetClaudeDelivery(id)
	if e != nil {
		t.Fatal(e)
	}
	beforeOld, _ := h.st.GetClaudeDelivery(legacyID)
	beforeNew := modern
	h.q.Recover(h.ctx)
	h.q.confirmPass(h.ctx, runtimeConfirmState())
	afterOld, _ := h.st.GetClaudeDelivery(legacyID)
	afterNew, _ := h.st.GetClaudeDelivery(id)
	if !reflect.DeepEqual(beforeOld, afterOld) || !reflect.DeepEqual(beforeNew, afterNew) || f.sendCount() != 1 || h.rt.callCount() != 0 {
		t.Fatalf("Recover rewrote or resent old=%+v new=%+v sends%d", afterOld, afterNew, f.sendCount())
	}
	appendQueueClaudeProof(t, beforeOld, "legacy-own-record")
	appendQueueClaudeProof(t, modern, "modern-own-record")
	h.q.confirmPass(h.ctx, runtimeConfirmState())
	for _, mid := range []int64{legacyID, id} {
		m, _ := h.st.GetMessage(mid)
		if m.Status != "delivered" || m.Attempts != 1 {
			t.Fatalf("mixed proof %+v", m)
		}
	}
	if f.sendCount() != 1 {
		t.Fatal("proof caused resend")
	}
}
