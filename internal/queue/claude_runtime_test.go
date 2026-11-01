package queue

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/store"
)

// Reviewer counterexample (task5916): send crosses the positive age deadline.
type deadlineCrossingSocket struct {
	*fakeSocket
	after func()
}

func (d deadlineCrossingSocket) Send(ctx context.Context, v claudeinstance.Snapshot) (SocketOutcome, error) {
	out, err := d.fakeSocket.Send(ctx, v)
	d.after()
	return out, err
}

func TestReview5916DeadlineCrossedDuringSendStrandsDelivering(t *testing.T) {
	for _, outcome := range []SocketOutcome{SocketUnknown, SocketNotSent} {
		t.Run(string(outcome), func(t *testing.T) {
			h := newTestQueue(t)
			h.addRunningSession(t, "recv", activity.Ready)
			f := installClaudeSocket(t, h, true)
			f.outcome = outcome
			h.q.cfg.QueueTimeout = time.Hour
			created := time.Now().Add(-time.Hour + 2*time.Second).Unix()
			id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback", CreatedAt: created})
			deadline := time.Unix(created, 0).Add(time.Hour)
			h.q.SetSocketSender(deadlineCrossingSocket{f, func() { h.q.now = func() time.Time { return deadline.Add(time.Second) } }})
			msg, _ := h.st.GetMessage(id)
			h.q.deliver(h.ctx, msg)
			got, _ := h.st.GetMessage(id)
			v, _ := h.st.GetClaudeDelivery(id)
			t.Logf("after deliver: status=%s reason=%s phase=%s", got.Status, got.Reason, v.Phase)
			// Exact proof appears; run the normal confirmer pass many times.
			appendQueueClaudeProof(t, v, "exact")
			st := &confirmState{used: make(map[string]map[recordKey]struct{}), restored: make(map[string]bool)}
			for i := 0; i < 3; i++ {
				h.q.confirmPass(h.ctx, st)
			}
			got, _ = h.st.GetMessage(id)
			v, _ = h.st.GetClaudeDelivery(id)
			t.Logf("after confirmer: status=%s reason=%s phase=%s sends=%d", got.Status, got.Reason, v.Phase, f.sendCount())
			if got.Status != "failed" || got.Reason != "delivery_unconfirmed" || v.Phase != "unconfirmed" || f.sendCount() != 1 || h.rt.callCount() != 0 {
				t.Fatalf("expired claim not settled safely: %+v phase=%s sends=%d", got, v.Phase, f.sendCount())
			}
			h.q.deliver(h.ctx, got)
			if f.sendCount() != 1 {
				t.Fatal("expired claim resent")
			}
		})
	}
}

func runtimeConfirmState() *confirmState {
	return &confirmState{used: make(map[string]map[recordKey]struct{}), restored: make(map[string]bool)}
}

// A real SQLite failure after Claim must not require ResetDelivering/restart.
func TestClaudePostclaimPersistenceErrorHasRuntimeSettlement(t *testing.T) {
	for _, age := range []string{"positive", "disabled"} {
		for _, settle := range []string{"proof", "closure"} {
			t.Run(age+"/"+settle, func(t *testing.T) {
				h := newTestQueue(t)
				h.addRunningSession(t, "recv", activity.Ready)
				f := installClaudeSocket(t, h, true)
				if age == "disabled" {
					h.q.cfg.QueueTimeout = 0
				}
				db, e := sql.Open("sqlite", h.dbPath)
				if e != nil {
					t.Fatal(e)
				}
				defer db.Close()
				h.q.SetSocketSender(deadlineCrossingSocket{f, func() {
					_, e := db.Exec(`CREATE TRIGGER reject_claude_phase BEFORE UPDATE OF phase ON claude_deliveries BEGIN SELECT RAISE(ABORT,'injected phase persistence failure'); END`)
					if e != nil {
						t.Fatal(e)
					}
				}})
				id, e := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
				if e != nil {
					t.Fatal(e)
				}
				m, _ := h.st.GetMessage(id)
				h.q.deliver(h.ctx, m)
				m, _ = h.st.GetMessage(id)
				v, e := h.st.GetClaudeDelivery(id)
				if e != nil || m.Status != "delivering" || v.Phase != "possible_write" {
					t.Fatalf("failure fixture: %+v %+v %v", m, v, e)
				}
				if _, e = db.Exec(`DROP TRIGGER reject_claude_phase`); e != nil {
					t.Fatal(e)
				}
				// Live/unknown is not closure, even though outcome persistence failed.
				if n := h.q.confirmPass(h.ctx, runtimeConfirmState()); n != 1 {
					t.Fatalf("uncertain claim pending=%d", n)
				}
				m, _ = h.st.GetMessage(id)
				if m.Status != "delivering" {
					t.Fatalf("uncertainty settled claim: %+v", m)
				}
				want := "delivered"
				if settle == "proof" {
					appendQueueClaudeProof(t, v, "runtime-proof")
				} else {
					want = "failed"
					if age == "positive" {
						h.q.now = func() time.Time { return v.Deadline.Add(time.Second) }
					} else {
						h.q.claudeProbe = socketProbe{} // exact saved PID/start no longer present
					}
				}
				for i := 0; i < 3; i++ {
					h.q.confirmPass(h.ctx, runtimeConfirmState())
				}
				m, _ = h.st.GetMessage(id)
				if m.Status != want || (want == "failed" && m.Reason != "delivery_unconfirmed") || m.Attempts != 1 || f.sendCount() != 1 || h.rt.callCount() != 0 {
					t.Fatalf("runtime settlement: %+v sends=%d", m, f.sendCount())
				}
				h.q.deliver(h.ctx, m)
				if f.sendCount() != 1 {
					t.Fatal("settled row resent")
				}
			})
		}
	}
}

type pausedClaudeSocket struct {
	*fakeSocket
	entered, release chan struct{}
}

func (s pausedClaudeSocket) Send(ctx context.Context, v claudeinstance.Snapshot) (SocketOutcome, error) {
	_, e := s.fakeSocket.Send(ctx, v)
	close(s.entered)
	<-s.release
	return SocketNotSent, e // stale sender outcome must not regress atomic proof
}
func TestClaudeConfirmerOverlapsSenderWithoutUncertaintyClosureOrDoubleProof(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Active)
	f := installClaudeSocket(t, h, true)
	h.q.cfg.QueueTimeout = 0
	paused := pausedClaudeSocket{f, make(chan struct{}), make(chan struct{})}
	h.q.SetSocketSender(paused)
	id, e := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
	if e != nil {
		t.Fatal(e)
	}
	m, _ := h.st.GetMessage(id)
	done := make(chan struct{})
	go func(msg store.Message) { defer close(done); h.q.deliver(h.ctx, msg) }(m)
	<-paused.entered
	// Always release the sender before test/store teardown, even on failure.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(paused.release); <-done }) }
	defer release()
	if n := h.q.confirmPass(h.ctx, runtimeConfirmState()); n != 1 {
		t.Fatalf("inflight uncertain pending=%d", n)
	}
	m, _ = h.st.GetMessage(id)
	if m.Status != "delivering" {
		t.Fatalf("inflight uncertain settled: %+v", m)
	}
	v, e := h.st.GetClaudeDelivery(id)
	if e != nil {
		t.Fatal(e)
	}
	appendQueueClaudeProof(t, v, "overlap-proof")
	h.q.confirmPass(h.ctx, runtimeConfirmState())
	h.q.confirmPass(h.ctx, runtimeConfirmState())
	m, _ = h.st.GetMessage(id)
	if m.Status != "delivered" || f.sendCount() != 1 || h.rt.callCount() != 0 {
		t.Fatalf("overlap proof: %+v sends=%d", m, f.sendCount())
	}
	release()
	m, _ = h.st.GetMessage(id)
	v, e = h.st.GetClaudeDelivery(id)
	if e != nil || m.Status != "delivered" || v.Phase != "delivered" {
		t.Fatalf("stale sender regressed proof: %+v phase=%s error=%v", m, v.Phase, e)
	}
	db, e := sql.Open("sqlite", h.dbPath)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	if e = db.QueryRow(`SELECT count(*) FROM claude_consumed_records WHERE message_id=?`, id).Scan(&count); e != nil || count != 1 {
		t.Fatalf("consumption=%d error=%v", count, e)
	}
}
