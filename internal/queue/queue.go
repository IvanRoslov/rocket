// Package queue implements rocket's per-recipient FIFO message delivery
// queue: one goroutine per recipient with a queued message, injecting text
// into the recipient's session once it is idle enough to receive it.
package queue

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// maxAttempts is the total number of delivery attempts (including the
// first) before a message is given up on and marked failed.
const maxAttempts = 5

// defaultBackoff returns the real backoff schedule: 1s, 2s, 4s, 8s, 16s for
// attempt 1..5 (attempt is 1-based, the number of the attempt that just
// failed).
func defaultBackoff(attempt int) time.Duration {
	d := time.Second
	for i := 1; i < attempt; i++ {
		d *= 2
	}
	return d
}

// Queue delivers queued inter-session messages, one recipient at a time in
// FIFO order, waiting for the recipient's activity state to allow delivery.
type Queue struct {
	st          *store.Store
	bus         *bus.Bus
	rt          runtime.Runtime
	cfg         *config.Config
	getActivity func(sessionID string) (activity.State, bool)

	// backoff computes the sleep duration after a failed delivery attempt
	// (1-based attempt number that just failed). Overridable by tests.
	backoff func(attempt int) time.Duration

	mu      sync.Mutex
	workers map[string]bool
}

// New builds a Queue. getActivity is typically Monitor.Activity.
func New(st *store.Store, b *bus.Bus, rt runtime.Runtime, cfg *config.Config, getActivity func(sessionID string) (activity.State, bool)) *Queue {
	return &Queue{
		st:          st,
		bus:         b,
		rt:          rt,
		cfg:         cfg,
		getActivity: getActivity,
		backoff:     defaultBackoff,
		workers:     make(map[string]bool),
	}
}

// Run starts a delivery worker for every recipient with an already-queued
// message, then runs housekeeping (timeout expiry every minute, retention
// purge once a day) until ctx is cancelled.
func (q *Queue) Run(ctx context.Context) {
	recipients, err := q.st.ListQueuedRecipients()
	if err != nil {
		slog.Error("queue: list queued recipients", "error", err)
	} else {
		for _, to := range recipients {
			q.Wake(to)
		}
	}

	housekeeping := time.NewTicker(time.Minute)
	defer housekeeping.Stop()

	dailyPurge := time.NewTicker(24 * time.Hour)
	defer dailyPurge.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-housekeeping.C:
			q.expireTimedOut()
		case <-dailyPurge.C:
			q.purgeOld()
		}
	}
}

// expireTimedOut fails every queued message older than cfg.QueueTimeout,
// publishing message.failed{reason:"timeout"} for each.
func (q *Queue) expireTimedOut() {
	cutoff := time.Now().Add(-q.cfg.QueueTimeout).Unix()
	expired, err := q.st.ExpireQueuedBefore(cutoff)
	if err != nil {
		slog.Error("queue: expire queued messages", "error", err)
		return
	}
	for _, m := range expired {
		q.bus.Publish("message.failed", m.ToSession, map[string]any{
			"id":     m.ID,
			"to":     m.ToSession,
			"reason": "timeout",
		})
	}
}

// purgeOld deletes messages and events older than 30 days.
func (q *Queue) purgeOld() {
	cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
	if err := q.st.PurgeOld(cutoff); err != nil {
		slog.Error("queue: purge old messages/events", "error", err)
	}
}

// Wake ensures exactly one delivery worker goroutine is running for
// recipient to. Calling Wake repeatedly for the same recipient while a
// worker is already running (or about to start) is a no-op.
func (q *Queue) Wake(to string) {
	q.mu.Lock()
	if q.workers[to] {
		q.mu.Unlock()
		return
	}
	q.workers[to] = true
	q.mu.Unlock()

	go q.deliverLoop(to)
}

// deliverLoop drains the queue for recipient to, one message at a time,
// exiting (and unregistering itself) once the queue is empty. It guards
// against the classic lost-wakeup race: after deciding to exit, it
// re-checks the queue before actually unregistering.
func (q *Queue) deliverLoop(to string) {
	ctx := context.Background()

	for {
		msg, ok, err := q.st.NextQueuedMessage(to)
		if err != nil {
			slog.Error("queue: next queued message", "to", to, "error", err)
			q.unregister(to)
			return
		}
		if !ok {
			if q.unregisterIfStillEmpty(to) {
				return
			}
			continue
		}

		q.deliver(ctx, msg)
	}
}

// unregister removes to from the worker set unconditionally.
func (q *Queue) unregister(to string) {
	q.mu.Lock()
	delete(q.workers, to)
	q.mu.Unlock()
}

// unregisterIfStillEmpty unregisters to as a worker, but only commits to
// exiting if the queue is still empty by the time the worker map lock is
// held. If a message snuck in between the NextQueuedMessage check and here,
// it stays registered and the caller should loop again. Returns true if the
// worker successfully unregistered (should exit), false if it should keep
// running.
func (q *Queue) unregisterIfStillEmpty(to string) bool {
	q.mu.Lock()
	delete(q.workers, to)
	q.mu.Unlock()

	// Re-check after unregistering: if a message arrived concurrently with
	// (or just after) our last NextQueuedMessage call, Wake may have been a
	// no-op because we were still registered. Re-verify and, if there's
	// work, re-register and keep going instead of exiting.
	_, ok, err := q.st.NextQueuedMessage(to)
	if err != nil {
		slog.Error("queue: recheck queue on exit", "to", to, "error", err)
		return true
	}
	if !ok {
		return true
	}

	q.mu.Lock()
	if q.workers[to] {
		// Someone else re-registered (via Wake) in the meantime; let them run.
		q.mu.Unlock()
		return true
	}
	q.workers[to] = true
	q.mu.Unlock()
	return false
}

// deliver handles a single message end to end: eligibility checks, waiting
// for the recipient to be ready, injecting, and retrying/failing.
func (q *Queue) deliver(ctx context.Context, msg store.Message) {
	for {
		sess, err := q.st.GetSession(msg.ToSession)
		if err != nil || sess.State != "running" {
			q.fail(msg, "recipient_gone")
			return
		}

		state, known := q.getActivity(msg.ToSession)
		switch {
		case known && (state == activity.Blocked || state == activity.Exited):
			q.fail(msg, "recipient_unavailable")
			return
		case known && (state == activity.Ready || state == activity.Idle || state == activity.WaitingInput):
			// proceed to delivery
		default:
			// active, or unknown — wait and re-check.
			if !q.waitForReady(ctx, msg.ToSession) {
				return // ctx cancelled
			}
			continue
		}

		q.attemptDelivery(ctx, msg, sess)
		return
	}
}

// waitForReady blocks until the recipient's activity changes (via bus
// subscription) or a 2s fallback tick fires, then returns true so the
// caller re-checks eligibility. Returns false if ctx is cancelled.
func (q *Queue) waitForReady(ctx context.Context, to string) bool {
	ch, cancel := q.bus.Subscribe()
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			return true
		case e, ok := <-ch:
			if !ok {
				return true
			}
			if e.Type == "session.activity_changed" && e.SessionID == to {
				return true
			}
		}
	}
}

// attemptDelivery runs the full retry loop for a single message: inject,
// interpret the result, retry with backoff on eligible errors, and fail
// after maxAttempts.
func (q *Queue) attemptDelivery(ctx context.Context, msg store.Message, sess store.Session) {
	handle := runtime.Handle{Name: sess.TmuxName}
	text := formatBody(msg)

	for {
		msg.Attempts++
		if err := q.st.UpdateMessageStatus(msg.ID, "delivering", msg.Attempts, 0); err != nil {
			slog.Error("queue: update message delivering", "id", msg.ID, "error", err)
		}

		err := q.rt.Inject(ctx, handle, text)

		var retryEligible bool
		switch {
		case err == nil:
			q.deliverSuccess(msg)
			return
		case errors.Is(err, runtime.ErrSubmitUnconfirmed):
			out, capErr := q.rt.Capture(ctx, handle, 5)
			if capErr == nil && markerPresent(out, text) {
				retryEligible = true
			} else {
				// Marker gone (or capture failed): treat as delivered —
				// submission likely went through and confirmation was missed.
				q.deliverSuccess(msg)
				return
			}
		default:
			retryEligible = true
		}

		if !retryEligible || msg.Attempts >= maxAttempts {
			q.fail(msg, "delivery_failed")
			return
		}

		if !sleepCtx(ctx, q.backoff(msg.Attempts)) {
			return
		}
	}
}

// deliverSuccess marks a message delivered and publishes message.delivered.
func (q *Queue) deliverSuccess(msg store.Message) {
	now := time.Now().Unix()
	if err := q.st.UpdateMessageStatus(msg.ID, "delivered", msg.Attempts, now); err != nil {
		slog.Error("queue: update message delivered", "id", msg.ID, "error", err)
	}
	q.bus.Publish("message.delivered", msg.ToSession, map[string]any{
		"id":   msg.ID,
		"to":   msg.ToSession,
		"from": msg.FromSession,
	})
}

// fail marks a message failed and publishes message.failed with reason.
func (q *Queue) fail(msg store.Message, reason string) {
	if err := q.st.UpdateMessageStatus(msg.ID, "failed", msg.Attempts, 0); err != nil {
		slog.Error("queue: update message failed", "id", msg.ID, "error", err)
	}
	q.bus.Publish("message.failed", msg.ToSession, map[string]any{
		"id":     msg.ID,
		"to":     msg.ToSession,
		"reason": reason,
	})
}

// formatBody prefixes the message body with the sender when known.
func formatBody(msg store.Message) string {
	if msg.FromSession != "" {
		return "[from " + msg.FromSession + "] " + msg.Body
	}
	return msg.Body
}

// markerPresent reports whether the last non-empty line of text is still
// present in out (i.e. the injected text appears not to have been
// submitted yet).
func markerPresent(out, text string) bool {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var marker string
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			marker = lines[i]
			break
		}
	}
	if marker == "" {
		return false
	}
	return strings.Contains(out, marker)
}

// sleepCtx sleeps for d, honoring ctx cancellation. Returns false if ctx
// was cancelled before d elapsed.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
