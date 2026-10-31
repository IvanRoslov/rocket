package queue

import (
	"context"
	"log/slog"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/nativeinput"
	"github.com/IvanRoslov/rocket/internal/store"
)

const (
	NativeTransportPiUDS      = "pi-uds-v1"
	nativeDeliveryUnconfirmed = "native_delivery_unconfirmed"
)

type NativeSender interface {
	Send(ctx context.Context, target nativeinput.Target, messageID int64, text string) (nativeinput.Outcome, error)
}

func (q *Queue) SetNativeSender(sender NativeSender) { q.native = sender }

func nativeDelivery(msg store.Message, sess store.Session) bool {
	if msg.DeliveryTransport != "" {
		return msg.DeliveryTransport == NativeTransportPiUDS
	}
	return msg.Status == "queued" && sess.DeliveryTransport == NativeTransportPiUDS
}

func (q *Queue) attemptNativeDelivery(ctx context.Context, msg store.Message, sess store.Session, confirmer agent.DeliveryConfirmer) {
	text := q.prepareText(msg, sess)
	target := nativeinput.Target{
		SessionID: sess.ID, InstanceID: sess.DeliveryInstance, SocketPath: sess.DeliveryEndpoint,
	}

	for {
		msg.Attempts++
		injectAt := q.now().UnixMilli()
		if msg.Attempts == 1 {
			ok, err := q.st.ClaimNativeMessage(msg.ID, NativeTransportPiUDS, injectAt)
			if err != nil {
				slog.Error("queue: claim native message", "id", msg.ID, "error", err)
				return
			}
			if !ok {
				slog.Warn("queue: native message no longer queued at claim time", "id", msg.ID)
				return
			}
		} else if err := q.st.SetMessageNativeDelivery(msg.ID, NativeTransportPiUDS, injectAt); err != nil {
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
			q.failNativeAttempt(msg, sess, confirmer)
			return
		}
		if !sleepCtx(ctx, q.backoff(msg.Attempts)) {
			return
		}
	}
}

func (q *Queue) failNativeAttempt(msg store.Message, sess store.Session, confirmer agent.DeliveryConfirmer) {
	if confirmer != nil {
		st := &confirmState{used: make(map[string]map[recordKey]struct{})}
		ref := agent.ActivityRef{SessionID: sess.ID, WorktreePath: sess.WorktreePath}
		if q.confirmOne(context.Background(), st, confirmer, ref, sess.ID, msg, q.injectedText(msg, sess)) {
			return
		}
	}
	q.fail(msg, nativeDeliveryUnconfirmed)
}

var errNativeSenderUnavailable = nativeSenderUnavailableError{}

type nativeSenderUnavailableError struct{}

func (nativeSenderUnavailableError) Error() string { return "native sender is not configured" }
