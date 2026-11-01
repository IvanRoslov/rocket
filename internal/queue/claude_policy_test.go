package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

func acceptedClaudeFixture(t *testing.T, timeout time.Duration) (*testHarness, *fakeSocket, store.Message, claudeinstance.Snapshot) {
	t.Helper()
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	f := installClaudeSocket(t, h, true)
	h.q.cfg.QueueTimeout = timeout
	id, e := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
	if e != nil {
		t.Fatal(e)
	}
	msg, _ := h.st.GetMessage(id)
	h.q.deliver(h.ctx, msg)
	msg, _ = h.st.GetMessage(id)
	v, e := h.st.GetClaudeDelivery(id)
	if e != nil || msg.Status != "accepted" {
		t.Fatalf("fixture %+v error=%v", msg, e)
	}
	return h, f, msg, v
}
func TestClaudeNonpositiveAgeDisabledSurvivesAgeRestoreAndLateProof(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			h, f, msg, v := acceptedClaudeFixture(t, timeout)
			if !v.Deadline.IsZero() {
				t.Fatalf("new numeric fallback persisted: %s", v.Deadline)
			}
			h.q.now = func() time.Time { return v.PreparedAt.Add(48 * time.Hour) }
			h.q.cfg.QueueTimeout = time.Second // persisted disabled mode wins over later config
			h.st.ResetDelivering()
			h.ac.set("recv", activity.Exited)
			if n := h.q.confirmClaudeRecipient(h.ctx, []store.Message{msg}); n != 1 {
				t.Fatalf("live disabled-age pending=%d", n)
			}
			got, _ := h.st.GetMessage(msg.ID)
			if got.Status != "accepted" {
				t.Fatalf("age/false-exit closed live target: %+v", got)
			}
			appendQueueClaudeProof(t, v, "late-exact")
			if n := h.q.confirmClaudeRecipient(h.ctx, []store.Message{msg}); n != 0 {
				t.Fatalf("late exact pending=%d", n)
			}
			got, _ = h.st.GetMessage(msg.ID)
			if got.Status != "delivered" || f.sendCount() != 1 || h.rt.callCount() != 0 {
				t.Fatalf("late delivery %+v sends=%d inject=%d", got, f.sendCount(), h.rt.callCount())
			}
		})
	}
}
func TestClaudeSavedTargetLifecycleIsIndependentOfCurrentHints(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Hour} {
		for _, which := range []string{"saved dead", "same generation terminal", "same generation missing", "new generation terminal old live", "new generation old unknown", "missing without generation evidence", "persisted exited", "probe error", "death with proof", "cancelled death"} {
			t.Run(fmt.Sprintf("%s/%s", timeout, which), func(t *testing.T) {
				h, f, msg, v := acceptedClaudeFixture(t, timeout)
				original := h.q.getSession
				switch which {
				case "saved dead", "death with proof", "cancelled death":
					h.q.claudeProbe = socketProbe{}
				case "same generation terminal":
					sess, _ := h.st.GetSession("recv")
					sess.State = "killed"
					h.st.UpdateSession(sess)
				case "same generation missing":
					h.q.getSession = func(string) (store.Session, error) { return store.Session{}, store.ErrNotFound }
				case "new generation terminal old live", "new generation old unknown":
					l, _, _ := h.st.GetClaudeLaunch("recv")
					l.Generation = "new"
					h.st.PutClaudeLaunch(l)
					sess, _ := h.st.GetSession("recv")
					sess.State = "killed"
					h.st.UpdateSession(sess)
					if which == "new generation old unknown" {
						h.q.claudeProbe = socketProbe{err: errors.New("uncertain")}
					}
				case "missing without generation evidence":
					h.q.getSession = func(string) (store.Session, error) { return store.Session{}, store.ErrNotFound }
					// A new generation cannot establish closure of the old saved target.
					l, _, _ := h.st.GetClaudeLaunch("recv")
					l.Generation = "new"
					h.st.PutClaudeLaunch(l)
				case "persisted exited":
					h.st.UpdateSessionActivity("recv", "exited", time.Now().Unix())
					h.ac.set("recv", activity.Exited)
				case "probe error":
					h.q.claudeProbe = socketProbe{err: errors.New("uncertain")}
				}
				if which == "death with proof" {
					appendQueueClaudeProof(t, v, "final-proof")
				}
				ctx := h.ctx
				if which == "cancelled death" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				h.q.confirmClaudeRecipient(ctx, []store.Message{msg})
				got, _ := h.st.GetMessage(msg.ID)
				want := "accepted"
				if which == "saved dead" || which == "same generation terminal" || which == "same generation missing" {
					want = "failed"
				}
				if which == "death with proof" {
					want = "delivered"
				}
				if got.Status != want || (want == "failed" && got.Reason != "delivery_unconfirmed") || f.sendCount() != 1 || h.rt.callCount() != 0 {
					t.Fatalf("saved lifecycle %+v want=%s sends=%d inject=%d", got, want, f.sendCount(), h.rt.callCount())
				}
				if want == "failed" {
					appendQueueClaudeProof(t, v, "too-late")
					h.q.getSession = original
					h.q.confirmClaudeRecipient(h.ctx, []store.Message{got})
					final, _ := h.st.GetMessage(msg.ID)
					if final.Status != "failed" {
						t.Fatal("terminal failure reopened")
					}
				}
			})
		}
	}
}
func TestClaudeConfiguredDefaultAndPositiveDeadlineRemainExact(t *testing.T) {
	h := newTestQueue(t)
	msg := store.Message{CreatedAt: time.Now().Unix()}
	cfg, e := config.Load(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	for _, timeout := range []time.Duration{cfg.QueueTimeout, 7 * time.Minute} {
		h.q.cfg.QueueTimeout = timeout
		if got := h.q.claudeDeadline(msg); !got.Equal(time.Unix(msg.CreatedAt, 0).Add(timeout)) {
			t.Fatalf("configured deadline changed: %s", got)
		}
	}
}

func TestClaudeUnmarkedNonpositiveKeepsGenericHousekeeping(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			h := newTestQueue(t)
			h.addRunningSession(t, "recv", activity.Ready)
			f := installClaudeSocket(t, h, false)
			h.q.cfg.QueueTimeout = timeout
			id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "unavailable", CreatedAt: time.Now().Add(-time.Minute).Unix()})
			h.q.expireTimedOut()
			m, _ := h.st.GetMessage(id)
			if m.Status != "failed" || m.Reason != "timeout" || m.Attempts != 0 || f.sendCount() != 0 || h.rt.callCount() != 0 {
				t.Fatalf("baseline generic expiry changed: %+v", m)
			}
		})
	}
}

type claudeCancelDeathProbe struct {
	claudeinstance.Probe
	cancel context.CancelFunc
}

func (p claudeCancelDeathProbe) Process(context.Context, int) (claudeinstance.Process, bool, error) {
	p.cancel()
	return claudeinstance.Process{}, false, nil
}
func TestClaudeCancellationAfterDeathProbeLeavesOwnershipForRecover(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Hour} {
		t.Run(timeout.String(), func(t *testing.T) {
			h, f, msg, _ := acceptedClaudeFixture(t, timeout)
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			h.q.claudeProbe = claudeCancelDeathProbe{Probe: h.q.claudeProbe, cancel: cancel}
			h.q.confirmClaudeRecipient(ctx, []store.Message{msg})
			got, _ := h.st.GetMessage(msg.ID)
			if got.Status != "accepted" || f.sendCount() != 1 || h.rt.callCount() != 0 {
				t.Fatalf("caller shutdown closed ownership %+v", got)
			}
			h.st.ResetDelivering()
			got, _ = h.st.GetMessage(msg.ID)
			if got.Status != "accepted" {
				t.Fatal("Recover requeued uncertain side effect")
			}
		})
	}
}

func TestClaudeDeletedSessionWithoutGenerationDoesNotProveSavedRootDeath(t *testing.T) {
	h, f, msg, v := acceptedClaudeFixture(t, 0)
	db, e := sql.Open("sqlite", h.dbPath+"?_pragma=foreign_keys(1)")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`DELETE FROM sessions WHERE id='recv'`); e != nil {
		t.Fatal(e)
	}
	if _, _, e = h.st.GetClaudeLaunch("recv"); !errors.Is(e, store.ErrNotFound) {
		t.Fatalf("generation deletion fixture failed: %v", e)
	}
	if n := h.q.confirmClaudeRecipient(h.ctx, []store.Message{msg}); n != 1 {
		t.Fatalf("missing generation pending=%d", n)
	}
	got, _ := h.st.GetMessage(msg.ID)
	if got.Status != "accepted" || f.sendCount() != 1 || h.rt.callCount() != 0 {
		t.Fatalf("absence pretended to prove death %+v", got)
	}
	appendQueueClaudeProof(t, v, "orphan-root-proof")
	h.q.confirmClaudeRecipient(h.ctx, []store.Message{msg})
	got, _ = h.st.GetMessage(msg.ID)
	if got.Status != "delivered" {
		t.Fatalf("saved target proof lost after session deletion %+v", got)
	}
}
