package queue

import (
	"context"
	"errors"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/store"
	"log/slog"
	"time"
)

func (q *Queue) claudeDeadline(msg store.Message) time.Time {
	if q.cfg == nil || q.cfg.QueueTimeout <= 0 {
		return time.Time{}
	}
	return time.Unix(msg.CreatedAt, 0).Add(q.cfg.QueueTimeout)
}

// deliverClaude returns false only while an unsent row should wait. It never
// dispatches through the legacy sender, regardless of probe or send outcome.
func (q *Queue) deliverClaude(ctx context.Context, msg store.Message, sess store.Session) bool {
	if claudeAgeExpired(q.claudeDeadline(msg), q.now()) {
		q.fail(msg, "timeout")
		return true
	}
	if msg.Attempts > 0 {
		q.fail(msg, "delivery_unconfirmed")
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
	// Eligibility belongs to the resolved durable binding, never a pre-follow cache.
	l, pinned, event, e := q.st.GetClaudeBinding(ctx, sess.ID)
	current, sessionErr := q.st.GetSession(sess.ID)
	check, again, observed, checkErr := q.st.GetClaudeBinding(ctx, sess.ID)
	if e != nil || sessionErr != nil || checkErr != nil || pinned == nil || again == nil || l.BindingRevision != check.BindingRevision || event != observed || !claudeinstance.SameIdentity(*pinned, *again) || !claudeinstance.SameIdentity(*again, r.Identity) {
		return false
	}
	if current.Activity == string(activity.Blocked) {
		q.fail(msg, "recipient_unavailable")
		return true
	}
	if !claudeEligible(current.Activity) {
		return false
	}
	snapshot, e := q.socket.Prepare(ctx, sess, socketFromName(msg), q.prepareText(msg, sess))
	if e != nil {
		return false
	}
	if snapshot.BindingRevision != l.BindingRevision || !claudeinstance.SameIdentity(snapshot.Target, r.Identity) {
		return false
	}
	snapshot.MessageID = msg.ID
	snapshot.PreparedAt = q.now().UTC()
	snapshot.Deadline = q.claudeDeadline(msg).UTC()
	snapshot.Phase = "possible_write"
	if claudeAgeExpired(snapshot.Deadline, snapshot.PreparedAt) {
		q.fail(msg, "timeout")
		return true
	}
	ok, e := q.st.ClaimClaudeMessage(snapshot)
	if e != nil || !ok {
		slog.Warn("queue: Claude prewrite claim rejected", "id", msg.ID, "error", e)
		if e != nil {
			return false
		}
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
func claudeAgeExpired(deadline, now time.Time) bool {
	return !deadline.IsZero() && !now.Before(deadline)
}

// Lifecycle evidence belongs to the immutable saved target. Current generation
// changes and exited activity hints cannot decide an older process's fate.
func (q *Queue) claudeTargetEnded(ctx context.Context, v claudeinstance.Snapshot) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if claudeinstance.ExactLiveness(probeCtx, v.Target, q.claudeProbe) == claudeinstance.Dead {
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	sess, e := q.getSession(v.Target.RocketSessionID)
	gone := errors.Is(e, store.ErrNotFound)
	if e != nil && !gone {
		return false
	}
	terminal := sess.State == "done" || sess.State == "killed" || sess.State == "errored"
	if !gone && !terminal {
		return false
	}
	launch, _, e := q.st.GetClaudeLaunch(v.Target.RocketSessionID)
	return e == nil && launch.Generation == v.Target.LaunchGeneration
}
func (q *Queue) confirmClaudeRecipient(ctx context.Context, msgs []store.Message) (pending int) {
	for i, msg := range msgs {
		if ctx.Err() != nil {
			return pending + len(msgs) - i
		}
		v, e := q.st.GetClaudeDelivery(msg.ID)
		if e == nil && !claudeAgeExpired(v.Deadline, q.now()) && !claudeAgeExpired(v.Deadline, time.Now()) {
			ended := q.claudeTargetEnded(ctx, v)
			scanCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			proof, found, scanErr := claudeinstance.FindProof(scanCtx, v)
			if found && scanErr == nil && scanCtx.Err() == nil && ctx.Err() == nil {
				ok, confirmErr := q.st.ConfirmClaudeMessageContext(scanCtx, msg.ID, proof, q.now())
				if confirmErr != nil {
					slog.Warn("queue: Claude proof remains uncertain", "id", msg.ID, "error", confirmErr)
				}
				if ok {
					cancel()
					q.bus.Publish("message.delivered", msg.ToSession, map[string]any{"id": msg.ID, "to": msg.ToSession, "from": msg.FromSession})
					continue
				}
			}
			cancel()
			if ctx.Err() != nil {
				return pending + len(msgs) - i
			}
			if ended {
				ok, err := q.st.SetClaudePhase(msg.ID, v.WireID, v.Target, "unconfirmed", q.now())
				if err != nil {
					pending++
					continue
				}
				if ok {
					q.announceFailure(msg, "delivery_unconfirmed")
					continue
				}
			}
		}
		if ctx.Err() != nil {
			return pending + len(msgs) - i
		}
		ok, e := q.st.ExpireClaudeMessage(msg.ID, q.now())
		if e != nil {
			pending++
			continue
		}
		if ok {
			q.announceFailure(msg, "delivery_unconfirmed")
			continue
		}
		current, e := q.st.GetMessage(msg.ID)
		if e != nil || (current.Status != "delivered" && current.Status != "failed") {
			pending++
		}
	}
	return pending
}

func claudeEligible(state string) bool {
	switch activity.State(state) {
	case activity.Active, activity.Ready, activity.Idle, activity.WaitingInput:
		return true
	}
	return false
}
