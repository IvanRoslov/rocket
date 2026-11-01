package queue

import (
	"context"
	"github.com/IvanRoslov/rocket/internal/activity"
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
