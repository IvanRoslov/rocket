package queue

import (
	"fmt"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// TestQueue_QueuedByAgentIsNotRedelivered: the agent took the text into its
// own queue (runtime.ErrQueuedByAgent). Injecting again would queue a
// duplicate, so the message is delivered after a single Inject. (The
// confirmed-by-transcript `accepted` status replaces this in a later step.)
func TestQueue_QueuedByAgentIsNotRedelivered(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)

	h.rt.injectFn = func(idx int, hd runtime.Handle, text string) error {
		return fmt.Errorf("%w: session %q", runtime.ErrQueuedByAgent, hd.Name)
	}

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"delivered after the agent queued the text")

	time.Sleep(50 * time.Millisecond)
	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want 1 (no redelivery of queued text)", n)
	}
}
