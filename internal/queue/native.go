package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/nativeinput"
	"github.com/IvanRoslov/rocket/internal/store"
)

const (
	// NativeTransportPiUDS is the only native transport implemented so far.
	NativeTransportPiUDS = agent.NativeTransportPiUDS

	// nativeConfirmTimeout bounds the final journal scan of a failed native
	// attempt. The scan runs under the caller's context, shortened by this
	// deadline, so a recipient's journal cannot strand the delivery worker.
	nativeConfirmTimeout = 5 * time.Second

	nativeDeliveryUnconfirmed    = "native_delivery_unconfirmed"
	unsupportedDeliveryTransport = "unsupported_delivery_transport"
)

type NativeSender interface {
	Send(ctx context.Context, target nativeinput.Target, messageID int64, text string) (nativeinput.Outcome, error)
}

func (q *Queue) SetNativeSender(sender NativeSender) { q.native = sender }

// deliveryRoute is how a message must be delivered, derived from the
// transport snapshots taken before the side effect. A nonempty transport
// Rocket does not implement is never downgraded to legacy visual delivery:
// it fails closed.
type deliveryRoute int

const (
	routeLegacy deliveryRoute = iota
	routeNative
	routeUnsupported
	routeClaude
)

// routeOf classifies msg for sess. A message snapshot always wins over the
// session's current transport: a legacy message accepted before its recipient
// became native keeps legacy confirmation semantics.
func routeOf(msg store.Message, sess store.Session) deliveryRoute {
	if msg.DeliveryTransport == ClaudeSocketTransport {
		return routeClaude
	}
	if msg.DeliveryTransport != "" {
		if msg.DeliveryTransport == NativeTransportPiUDS {
			return routeNative
		}
		return routeUnsupported
	}
	switch sess.DeliveryTransport {
	case "":
		return routeLegacy
	case NativeTransportPiUDS:
		// Only a not-yet-sent message may adopt the recipient's current
		// transport; accepted/delivering rows with an empty snapshot were
		// handled by the legacy path (see TestAcceptedLegacyMessageDoesNot
		// BecomeNativeAfterRestore).
		if msg.Status == "queued" {
			return routeNative
		}
		return routeLegacy
	default:
		return routeUnsupported
	}
}

func (q *Queue) attemptNativeDelivery(ctx context.Context, msg store.Message, sess store.Session, confirmer agent.DeliveryConfirmer) {
	text := q.prepareText(msg, sess)
	target := nativeinput.Target{
		SessionID: sess.ID, InstanceID: sess.DeliveryInstance, SocketPath: sess.DeliveryEndpoint,
	}

	// Ownership of the queued->delivering transition is claimed exactly once
	// per delivery invocation, independent of msg.Attempts: a queued row that
	// already carries attempts from an earlier lifecycle must still win the
	// CAS before anything can be sent.
	claimed := false
	for {
		msg.Attempts++
		if !claimed {
			injectAt := q.now().UnixMilli()
			ok, err := q.st.ClaimNativeMessage(msg.ID, NativeTransportPiUDS, injectAt)
			if err != nil {
				slog.Error("queue: claim native message", "id", msg.ID, "error", err)
				return
			}
			if !ok {
				slog.Warn("queue: native message no longer queued at claim time", "id", msg.ID)
				return
			}
			// The proof window of this delivery is the moment before its
			// FIRST send attempt: an older journal record cannot belong to
			// this message, and every retry of the same delivery shares the
			// same window. Keeping it in the local copy is what the terminal
			// journal scan uses; without it the scan starts at unix epoch.
			msg.InjectAt = injectAt
			claimed = true
		} else if err := q.st.SetMessageNativeDelivery(msg.ID, NativeTransportPiUDS, msg.InjectAt); err != nil {
			slog.Error("queue: refresh native delivery snapshot", "id", msg.ID, "error", err)
			return
		}

		outcome := nativeinput.Unknown
		var sendErr error
		if q.native == nil {
			outcome, sendErr = nativeinput.NotSent, errNativeSenderUnavailable
		} else {
			outcome, sendErr = q.native.Send(ctx, target, msg.ID, text)
		}
		switch outcome {
		case nativeinput.Accepted, nativeinput.Unknown:
			if sendErr != nil {
				slog.Warn("queue: native delivery needs journal confirmation", "id", msg.ID, "to", msg.ToSession, "outcome", outcome, "error", sendErr)
			}
			q.accept(msg)
			return
		case nativeinput.NotSent:
			slog.Warn("queue: native message was not sent", "id", msg.ID, "to", msg.ToSession, "attempt", msg.Attempts, "error", sendErr)
		default:
			slog.Warn("queue: invalid native outcome needs journal confirmation", "id", msg.ID, "to", msg.ToSession, "outcome", outcome, "error", sendErr)
			q.accept(msg)
			return
		}

		if msg.Attempts >= maxAttempts {
			q.failNativeAttempt(ctx, msg, sess, confirmer)
			return
		}
		if !sleepCtx(ctx, q.backoff(msg.Attempts)) {
			return
		}
	}
}

// failNativeAttempt is the terminal step of a native delivery whose sends
// were all refused. Before failing, it gives the recipient's journal one last
// bounded look: a bridge may have delivered on an attempt that reported
// not_sent, or an earlier attempt may already be recorded. The message is in
// status "delivering", so the settle path must be able to transition from
// that state (ConfirmDelivering), not only from "accepted".
func (q *Queue) failNativeAttempt(ctx context.Context, msg store.Message, sess store.Session, confirmer agent.DeliveryConfirmer) {
	if confirmer != nil {
		deadline := q.confirmTimeout
		if deadline <= 0 {
			deadline = nativeConfirmTimeout
		}
		confirmCtx, cancel := context.WithTimeout(ctx, deadline)
		defer cancel()
		st := &confirmState{used: make(map[string]map[recordKey]struct{})}
		ref := agent.ActivityRef{SessionID: sess.ID, WorktreePath: sess.WorktreePath}
		confirmed := q.confirmDeliveringOne(confirmCtx, st, confirmer, ref, sess.ID, msg, q.injectedText(msg, sess))
		cancel()
		if confirmed {
			return
		}
		if ctx.Err() != nil {
			// The caller is shutting down, not the recipient. Leaving the row
			// in "delivering" lets startup Recover turn it into "accepted" for
			// journal confirmation; failing it here would turn a graceful
			// shutdown into a permanent false failure.
			slog.Warn("queue: native final confirmation interrupted by shutdown, leaving delivering for Recover", "id", msg.ID, "to", msg.ToSession)
			return
		}
	}
	ok, err := q.st.FailDelivering(msg.ID, nativeDeliveryUnconfirmed)
	if err != nil {
		slog.Error("queue: fail native delivering message", "id", msg.ID, "error", err)
		return
	}
	if ok {
		slog.Warn("queue: native delivery never confirmed", "id", msg.ID, "to", msg.ToSession)
		q.announceFailure(msg, nativeDeliveryUnconfirmed)
	}
}

var errNativeSenderUnavailable = nativeSenderUnavailableError{}

type nativeSenderUnavailableError struct{}

func (nativeSenderUnavailableError) Error() string { return "native sender is not configured" }
