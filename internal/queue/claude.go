package queue

import (
	"context"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/store"
	"log/slog"
	"time"
)

func (q *Queue) claudeDeadline(msg store.Message) time.Time {
	timeout := 30 * time.Minute
	if q.cfg != nil && q.cfg.QueueTimeout > 0 {
		timeout = q.cfg.QueueTimeout
	}
	return time.Unix(msg.CreatedAt, 0).Add(timeout)
}

// deliverClaude returns false only while an unsent row should wait. It never
// dispatches through the legacy sender, regardless of probe or send outcome.
func (q *Queue) deliverClaude(ctx context.Context, msg store.Message, sess store.Session) bool {
	if !q.now().Before(q.claudeDeadline(msg)) {
		q.fail(msg, "timeout")
		return true
	}
	if msg.Attempts > 0 {
		q.fail(msg, "delivery_unconfirmed")
		return true
	}
	state, known := q.getActivity(sess.ID)
	if known && state == activity.Blocked {
		q.fail(msg, "recipient_unavailable")
		return true
	}
	if q.socket == nil {
		return false
	}
	r := q.socket.Resolve(ctx, sess)
	if r.State == claudeinstance.Dead {
		q.fail(msg, "recipient_unavailable")
		return true
	}
	if r.State != claudeinstance.Verified {
		return false
	}
	// Liveness does not assert readiness; an old unqualified exited hint waits
	// for independent activity instead of causing a zero-attempt terminal fail.
	if !known || state == activity.Exited {
		return false
	}
	switch state {
	case activity.Active, activity.Ready, activity.Idle, activity.WaitingInput:
	default:
		return false
	}
	snapshot, e := q.socket.Prepare(ctx, sess, socketFromName(msg), q.prepareText(msg, sess))
	if e != nil {
		return false
	}
	if !claudeinstance.SameIdentity(snapshot.Target, r.Identity) {
		return false
	}
	snapshot.MessageID = msg.ID
	snapshot.PreparedAt = q.now().UTC()
	snapshot.Deadline = q.claudeDeadline(msg).UTC()
	snapshot.Phase = "possible_write"
	if !snapshot.PreparedAt.Before(snapshot.Deadline) {
		q.fail(msg, "timeout")
		return true
	}
	ok, e := q.st.ClaimClaudeMessage(snapshot)
	if e != nil || !ok {
		slog.Warn("queue: Claude prewrite claim rejected", "id", msg.ID, "error", e)
		return true
	}
	outcome, sendErr := q.socket.Send(ctx, snapshot)
	if sendErr != nil {
		slog.Warn("queue: Claude delivery awaits outcome/proof", "id", msg.ID, "outcome", outcome, "error", sendErr)
	}
	switch outcome {
	case SocketNotSent, SocketAccepted, SocketHeld, SocketDenied, SocketExpired:
	default:
		outcome = SocketUnknown
	}
	if _, e = q.st.SetClaudePhase(msg.ID, snapshot.WireID, snapshot.Target, string(outcome), q.now()); e != nil {
		slog.Warn("queue: Claude outcome persistence uncertain; retaining claim", "id", msg.ID, "error", e)
	}
	q.kickConfirmer()
	return true
}
func (q *Queue) confirmClaudeRecipient(ctx context.Context, msgs []store.Message) (pending int) {
	for _, msg := range msgs {
		if ctx.Err() != nil {
			return pending + 1
		}
		// Journal settlement is added in the delivery-proof stage; even now this
		// route can only wait/expire, never resend through a legacy confirmer.
		ok, e := q.st.ExpireClaudeMessage(msg.ID, q.now())
		if e != nil || !ok {
			pending++
			continue
		}
		q.announceFailure(msg, "delivery_unconfirmed")
	}
	return pending
}
