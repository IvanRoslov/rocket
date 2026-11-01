package queue

import (
	"context"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/store"
	"testing"
	"time"
)

func TestSelectedClaudeRouteNeverFallsBack(t *testing.T) {
	for _, reason := range []string{"missing socket", "ambiguous registry", "unsupported protocol", "invalid identity"} {
		t.Run(reason, func(t *testing.T) {
			h := newTestQueue(t)
			h.addRunningSession(t, "recv", activity.Ready)
			installClaudeSocket(t, h, false)
			id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "keep user draft"})
			if err != nil {
				t.Fatal(err)
			}
			for pass := 0; pass < 2; pass++ {
				msg, _ := h.st.GetMessage(id)
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				h.q.deliver(ctx, msg)
				cancel()
			}
			m, _ := h.st.GetMessage(id)
			if h.rt.callCount() != 0 || m.Status != "queued" || m.Attempts != 0 {
				t.Fatalf("selected Claude route fell back: inject=%d status=%s attempts=%d", h.rt.callCount(), m.Status, m.Attempts)
			}
		})
	}
}

func TestClaudeFalseExitedWaitsWithoutBurningAttempt(t *testing.T) {
	for _, which := range []string{"live false exited", "legacy persisted exited", "process uncertainty", "genuine blocked", "genuine root dead"} {
		t.Run(which, func(t *testing.T) {
			h := newTestQueue(t)
			h.addRunningSession(t, "recv", activity.Exited)
			f := installClaudeSocket(t, h, true)
			switch which {
			case "legacy persisted exited":
				h.st.UpdateSessionActivity("recv", "exited", time.Now().Unix())
				h.q.getActivity = func(id string) (activity.State, bool) {
					sess, e := h.st.GetSession(id)
					return activity.State(sess.Activity), e == nil
				}
			case "process uncertainty":
				f.resolutionState = claudeinstance.Unknown
			case "genuine blocked":
				h.ac.set("recv", activity.Blocked)
				h.st.UpdateSessionActivity("recv", "blocked", time.Now().Unix())
			case "genuine root dead":
				f.resolutionState = claudeinstance.Dead
			}
			id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
			m, _ := h.st.GetMessage(id)
			ctx, cancel := context.WithTimeout(h.ctx, 15*time.Millisecond)
			h.q.deliver(ctx, m)
			cancel()
			m, _ = h.st.GetMessage(id)
			want := "queued"
			if which == "genuine blocked" || which == "genuine root dead" {
				want = "failed"
			}
			if m.Status != want || m.Attempts != 0 || f.sendCount() != 0 || h.rt.callCount() != 0 {
				t.Fatalf("state guard %+v sends=%d inject=%d", m, f.sendCount(), h.rt.callCount())
			}
		})
	}
}
