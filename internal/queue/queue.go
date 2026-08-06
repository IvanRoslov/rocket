// Package queue implements rocket's per-recipient FIFO message delivery
// queue: one goroutine per recipient with a queued message, injecting text
// into the recipient's session once it is idle enough to receive it.
package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// largeMessageLineThreshold is the line-count cutoff (independent of
// cfg.LargeMessageThreshold's byte cutoff) above which a message body is
// treated as "large" and routed via the worktree inbox file instead of
// direct injection — see prepareText.
const largeMessageLineThreshold = 20

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

	// getSession looks up a recipient's session. Defaults to st.GetSession;
	// overridable by tests to simulate transient (non-ErrNotFound) lookup
	// errors without a real store failure.
	getSession func(sessionID string) (store.Session, error)

	// backoff computes the sleep duration after a failed delivery attempt
	// (1-based attempt number that just failed). Overridable by tests.
	backoff func(attempt int) time.Duration

	mu      sync.Mutex
	workers map[string]bool

	ctxMu sync.RWMutex
	// runCtx is the context passed to Run, stored so Wake-spawned delivery
	// goroutines can honor daemon shutdown instead of running forever. It is
	// nil until Run is called.
	runCtxVal context.Context
}

// New builds a Queue. getActivity is typically Monitor.Activity.
func New(st *store.Store, b *bus.Bus, rt runtime.Runtime, cfg *config.Config, getActivity func(sessionID string) (activity.State, bool)) *Queue {
	return &Queue{
		st:          st,
		bus:         b,
		rt:          rt,
		cfg:         cfg,
		getActivity: getActivity,
		getSession:  st.GetSession,
		backoff:     defaultBackoff,
		workers:     make(map[string]bool),
	}
}

// runCtx returns the context passed to Run, or context.Background() if Run
// hasn't been called yet (e.g. Wake is invoked very early, before the
// daemon's goroutine running Queue.Run has had a chance to start — the
// normal daemon wiring calls Run before serving, so this fallback is only
// hit in that narrow startup window).
func (q *Queue) runCtx() context.Context {
	q.ctxMu.RLock()
	defer q.ctxMu.RUnlock()
	if q.runCtxVal != nil {
		return q.runCtxVal
	}
	return context.Background()
}

// Recover performs the queue's startup recovery pass: it resets messages
// orphaned in status "delivering" by a previous crash back to "queued" (see
// store.ResetDelivering for why this is necessary — no query anywhere reads
// "delivering" back into the live queue, so without this they would be
// silently lost, breaking FIFO for anything queued behind them), then wakes
// a delivery worker for every recipient with a queued message.
//
// Callers MUST call Recover synchronously (not in a goroutine) and have it
// complete BEFORE the API starts serving requests. Otherwise an early
// POST /v1/messages could call Wake for a recipient whose delivery worker
// then races the recovery pass, breaking FIFO ordering for that recipient.
func (q *Queue) Recover(ctx context.Context) {
	q.ctxMu.Lock()
	if q.runCtxVal == nil {
		q.runCtxVal = ctx
	}
	q.ctxMu.Unlock()

	if n, err := q.st.ResetDelivering(); err != nil {
		slog.Error("queue: reset delivering messages", "error", err)
	} else if n > 0 {
		slog.Warn("queue: recovered messages orphaned in delivering status", "count", n)
	}

	recipients, err := q.st.ListQueuedRecipients()
	if err != nil {
		slog.Error("queue: list queued recipients", "error", err)
	} else {
		for _, to := range recipients {
			q.Wake(to)
		}
	}
}

// Run runs housekeeping (timeout expiry every minute, retention purge once a
// day) until ctx is cancelled. Callers must call Recover(ctx) synchronously
// before Run (and before serving the API) — see Recover's doc comment.
func (q *Queue) Run(ctx context.Context) {
	q.ctxMu.Lock()
	q.runCtxVal = ctx
	q.ctxMu.Unlock()

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
// publishing message.failed{reason:"timeout"} for each and notifying senders.
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
		// Notify sender on timeout (same as delivery failure).
		if m.FromSession != "" {
			q.notifySenderOfFailure(m, "timeout")
		}
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
	ctx := q.runCtx()

	for {
		if ctx.Err() != nil {
			// Shutting down: don't spin re-fetching the same still-queued
			// message forever (deliver returns immediately without changing
			// its status once ctx is cancelled). It will be picked up again
			// on the next Wake/Run.
			q.unregister(to)
			return
		}

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
		sess, err := q.getSession(msg.ToSession)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				q.fail(msg, "recipient_gone")
				return
			}
			// Transient lookup failure (e.g. a momentary DB error): don't
			// fail the message on a fluke. Log and retry after a short
			// wait via the same fallback-tick mechanism used while waiting
			// for the recipient to become ready.
			slog.Error("queue: get session (transient, will retry)", "to", msg.ToSession, "error", err)
			if !q.waitForReady(ctx, msg.ToSession) {
				return // ctx cancelled
			}
			continue
		}
		switch sess.State {
		case "running":
			// proceed to activity check below
		case "spawning":
			// Recipient session exists but hasn't finished starting yet:
			// wait and re-check, same as an active/unknown activity state,
			// rather than failing the message as recipient_gone.
			if !q.waitForReady(ctx, msg.ToSession) {
				return // ctx cancelled
			}
			continue
		default:
			// Terminal (done/killed/errored) or otherwise not deliverable.
			q.fail(msg, "recipient_gone")
			return
		}

		if sess.PendingQuiz != "" {
			// Recipient has a pending AskUserQuestion quiz on screen: text
			// injected now would corrupt the TUI widget. Hold the message
			// (stays queued, no failure, no events, no retries burned) and
			// wait/re-check: the quiz-resolve path publishes a
			// session.quiz_resolved bus event that wakes this straight back
			// up (with the 2s fallback ticker as a backstop).
			if !q.waitForReady(ctx, msg.ToSession) {
				return // ctx cancelled
			}
			continue
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

// waitForReady blocks until a bus event signals the recipient may be ready
// (session.activity_changed, or session.quiz_resolved when a held quiz
// clears) or a 2s fallback tick fires, then returns true so the caller
// re-checks eligibility. Returns false if ctx is cancelled.
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
			if e.SessionID != to {
				continue
			}
			if e.Type == "session.activity_changed" || e.Type == "session.quiz_resolved" {
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
	text := q.prepareText(msg, sess)

	claimed := false
	for {
		msg.Attempts++

		if !claimed {
			// Atomically claim the queued->delivering transition. If this
			// loses the CAS, the message is no longer "queued" — e.g.
			// expireTimedOut already marked it "failed" for timing out
			// concurrently with this worker deciding to deliver it. Without
			// this check the worker would blindly inject and then overwrite
			// "failed" back to "delivered", reviving a message it shouldn't.
			ok, err := q.st.ClaimMessage(msg.ID)
			if err != nil {
				slog.Error("queue: claim message", "id", msg.ID, "error", err)
				return
			}
			if !ok {
				slog.Warn("queue: message no longer queued at claim time, skipping delivery", "id", msg.ID)
				return
			}
			claimed = true
		}

		if err := q.st.UpdateMessageStatus(msg.ID, "delivering", msg.Attempts, 0, ""); err != nil {
			slog.Error("queue: update message delivering", "id", msg.ID, "error", err)
		}

		err := q.rt.Inject(ctx, handle, text)

		var retryEligible bool
		switch {
		case err == nil:
			q.deliverSuccess(msg)
			return
		case errors.Is(err, runtime.ErrNotDelivered):
			// Inject positively established that nothing was submitted and
			// cleared the composer, so there is no duplicate-message risk
			// and no draft left to look for: retry, and on exhaustion fail
			// so the sender is told. Deliberately NOT folded into the
			// ErrSubmitUnconfirmed branch below — its tail probe would see
			// the marker gone (Inject erased it) and record a known
			// non-delivery as "delivered", silently dropping the message.
			retryEligible = true
		case errors.Is(err, runtime.ErrSubmitUnconfirmed):
			// Mirror Inject's own confirmWindow: a narrow tail of the
			// pane's bottom rows (input line + footer chrome). Chat-style
			// TUIs echo a submitted message permanently into a scrolling
			// history area above this window, so a wide/whole-pane capture
			// would see the marker forever and could never conclude
			// "delivered" — see tmux.Inject's confirmWindow doc.
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
	if err := q.st.UpdateMessageStatus(msg.ID, "delivered", msg.Attempts, now, ""); err != nil {
		slog.Error("queue: update message delivered", "id", msg.ID, "error", err)
	}
	q.bus.Publish("message.delivered", msg.ToSession, map[string]any{
		"id":   msg.ID,
		"to":   msg.ToSession,
		"from": msg.FromSession,
	})
}

// fail marks a message failed and publishes message.failed with reason.
// If the sender (msg.FromSession) is a live session, it also enqueues a failure
// notification to the sender (as a system message with from="").
func (q *Queue) fail(msg store.Message, reason string) {
	if err := q.st.UpdateMessageStatus(msg.ID, "failed", msg.Attempts, 0, reason); err != nil {
		slog.Error("queue: update message failed", "id", msg.ID, "error", err)
	}
	q.bus.Publish("message.failed", msg.ToSession, map[string]any{
		"id":     msg.ID,
		"to":     msg.ToSession,
		"reason": reason,
	})

	// Notify sender if the failed message has one and the sender is live.
	if msg.FromSession != "" {
		q.notifySenderOfFailure(msg, reason)
	}
}

// notifySenderOfFailure enqueues a failure notice to the sender if their session
// is live (spawning or running). Errors are logged as warnings and do not fail
// the original fail() operation.
func (q *Queue) notifySenderOfFailure(msg store.Message, reason string) {
	senderSess, err := q.getSession(msg.FromSession)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			// Transient error (not gone) — log warning but don't fail the fail().
			slog.Warn("queue: get sender session for failure notice", "from", msg.FromSession, "error", err)
		}
		return
	}

	// Only notify if the sender's session is live.
	if senderSess.State != "spawning" && senderSess.State != "running" {
		return
	}

	// Enqueue a system message (from="") to notify the sender about the failure.
	notice := store.Message{
		FromSession: "", // system message — this prevents recursive notifications
		ToSession:   msg.FromSession,
		Body: fmt.Sprintf(
			"[rocket] delivery FAILED: message #%d to %s (%s). The body is preserved in message history; use rocket send --wait for critical messages.",
			msg.ID, msg.ToSession, reason,
		),
	}

	_, err = q.st.AddMessage(notice)
	if err != nil {
		slog.Warn("queue: add failure notice to sender", "from", msg.FromSession, "error", err)
		return
	}

	// Wake the sender's queue worker if needed.
	q.bus.Publish("message.queued", msg.FromSession, map[string]any{})
	q.Wake(msg.FromSession)
}

// formatBody prefixes the message body with the sender when known.
func formatBody(msg store.Message) string {
	if msg.FromSession != "" {
		return "[from " + msg.FromSession + "] " + msg.Body
	}
	return msg.Body
}

// prepareText returns the text to actually inject for msg: the full
// (sender-prefixed) body for small messages, or — for large messages to a
// recipient with a worktree — a short pointer to a file holding the full
// body, written under the recipient's worktree.
//
// This exists because injecting large pastes (~6KB+) directly into a TUI's
// input line intermittently loses them even though injection appears to
// succeed (see runtime.Inject's settle-pause doc comment for the
// mechanics). Routing large bodies through a file the recipient reads
// itself sidesteps the paste path entirely for the risky case, while small
// messages keep the original (already reliable) direct-injection path
// unchanged.
//
// A message counts as "large" if its body exceeds cfg.LargeMessageThreshold
// bytes OR has more than largeMessageLineThreshold lines. If writing the
// inbox file fails for any reason, prepareText logs a warning and falls
// back to the original full-body injection.
func (q *Queue) prepareText(msg store.Message, sess store.Session) string {
	large := len(msg.Body) > q.cfg.LargeMessageThreshold || lineCount(msg.Body) > largeMessageLineThreshold
	if !large || sess.WorktreePath == "" {
		return formatBody(msg)
	}

	relPath, err := writeInboxFile(sess.WorktreePath, msg.ID, msg.Body)
	if err != nil {
		slog.Warn("queue: write inbox file failed, falling back to full-body injection", "id", msg.ID, "to", msg.ToSession, "worktree", sess.WorktreePath, "error", err)
		return formatBody(msg)
	}

	prefix := ""
	if msg.FromSession != "" {
		prefix = "[from " + msg.FromSession + "] "
	}
	return prefix + "[large message] Full text written to " + relPath + " — read that file now."
}

// lineCount returns the number of lines in body (1 for a body with no
// newlines, 0 for an empty body).
func lineCount(body string) int {
	if body == "" {
		return 0
	}
	return strings.Count(body, "\n") + 1
}

// writeInboxFile writes body to <worktreePath>/.rocket/inbox/msg-<id>.md,
// creating the inbox directory if needed, and returns the path relative to
// worktreePath (for use in the pointer message shown to the recipient). The
// write is atomic (temp file + rename) so a concurrently-reading recipient
// never observes a partial file. The file is intentionally never deleted —
// it doubles as delivery history.
func writeInboxFile(worktreePath string, id int64, body string) (string, error) {
	dir := filepath.Join(worktreePath, ".rocket", "inbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir inbox dir: %w", err)
	}

	relPath := filepath.Join(".rocket", "inbox", fmt.Sprintf("msg-%d.md", id))
	fullPath := filepath.Join(worktreePath, relPath)

	tmp, err := os.CreateTemp(dir, "msg-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed away

	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return "", fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		return "", fmt.Errorf("rename into place: %w", err)
	}

	return relPath, nil
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
