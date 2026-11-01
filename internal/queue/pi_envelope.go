package queue

import (
	"context"
	"strconv"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

// nativePiEnvelope is the pi-uds-v1 service header: the persisted Rocket
// message ID in canonical decimal form, one space, then the existing prepared
// text. The ID is always the absolute first prefix, even when the body itself
// starts with a similar header.
func nativePiEnvelope(messageID int64, preparedText string) string {
	return "[rocket #" + strconv.FormatInt(messageID, 10) + "] " + preparedText
}

// nativePiInjectedText rebuilds the exact native wire text read-only from the
// persisted message and the existing injectedText reconstruction. It never
// writes and never falls back to a bare text or a number-only match.
func (q *Queue) nativePiInjectedText(msg store.Message, sess store.Session) string {
	return nativePiEnvelope(msg.ID, q.injectedText(msg, sess))
}

// nativePiStrictProof is the Pi-only strict capability, kept structural and
// private so the public agent interfaces stay unchanged.
type nativePiStrictProof interface {
	ConfirmNativeDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error)
}

type nativePiConfirmer struct {
	strict nativePiStrictProof
}

// ConfirmDelivered delegates to the Pi strict whole-record method rather than
// the legacy substring/holdsLines matcher.
func (c *nativePiConfirmer) ConfirmDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	return c.strict.ConfirmNativeDelivered(ctx, ref, text, since)
}

// nativePiProofConfirmer adapts a recipient confirmer to the Pi strict method.
// An unsupported capability returns nil: the native path must stay
// unconfirmed instead of falling back to the legacy matcher.
func nativePiProofConfirmer(base agent.DeliveryConfirmer) agent.DeliveryConfirmer {
	if base == nil {
		return nil
	}
	strict, ok := base.(nativePiStrictProof)
	if !ok {
		return nil
	}
	return &nativePiConfirmer{strict: strict}
}
