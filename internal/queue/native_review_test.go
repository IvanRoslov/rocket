package queue

import (
	"context"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/nativeinput"
	"github.com/IvanRoslov/rocket/internal/store"
)

// claimNativeForTest moves id into the native delivering state exactly like a
// delivery attempt does before its side effect.
func claimNativeForTest(t *testing.T, h *testHarness, id int64, injectAt time.Time) {
	t.Helper()
	ok, err := h.st.ClaimNativeMessage(id, NativeTransportPiUDS, injectAt.UnixMilli())
	if err != nil || !ok {
		t.Fatalf("ClaimNativeMessage(%d) = %v, %v", id, ok, err)
	}
}

// TestNativeFinalProofTransitionsDeliveringToDelivered is review case 2: a
// delivering row whose journal proof is found by the final scan must become
// delivered, not stay delivering forever because ConfirmAccepted only matches
// accepted rows.
func TestNativeFinalProofTransitionsDeliveringToDelivered(t *testing.T) {
	h := newTestQueue(t)
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "final proof")
	injectAt := h.clock.now()
	claimNativeForTest(t, h, id, injectAt)
	proofAt := injectAt.Add(time.Second)
	h.ag.confirmRecords(map[string][]time.Time{"final proof": {proofAt}})

	msg, _ := h.st.GetMessage(id)
	sess, _ := h.st.GetSession("native")
	_, confirmer := h.q.recipientCaps(sess)
	h.q.failNativeAttempt(h.ctx, msg, sess, confirmer)

	got, err := h.st.GetMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "delivered" {
		t.Fatalf("status = %q, want delivered for a proof-backed final scan", got.Status)
	}
	if got.DeliveryProofAt != proofAt.UnixNano() || got.DeliveryProofText != "final proof" {
		t.Fatalf("proof = %d/%q, want %d/%q", got.DeliveryProofAt, got.DeliveryProofText, proofAt.UnixNano(), "final proof")
	}
}

// TestNativeRefusedSendsStillCheckJournal walks the production path: five
// refused sends end in the final scan, and a proof recorded in the journal
// makes the stored status delivered (not delivered merely because the
// confirmer ran).
func TestNativeRefusedSendsStillCheckJournal(t *testing.T) {
	h := newTestQueue(t)
	rt := installNativeRuntime(h)
	h.addNativeSession(t, "native")
	h.q.SetNativeSender(&fakeNativeSender{outcomes: []nativeinput.Outcome{
		nativeinput.NotSent, nativeinput.NotSent, nativeinput.NotSent, nativeinput.NotSent, nativeinput.NotSent,
	}})
	proofAt := h.clock.now().Add(time.Minute)
	h.ag.confirmRecords(map[string][]time.Time{"refused but recorded": {proofAt}})
	id := addMsg(t, h, "native", "refused but recorded")
	h.q.Wake("native")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered from the final journal scan")
	assertNoTmux(t, rt)
}

// TestNativeRefusedSendsIgnoreJournalBeforeClaim guards the proof window: the
// final scan must start at the first send attempt of THIS delivery, never at
// inject_at=0. An identical journal record from before the message existed is
// not its proof, even though every send reported not_sent.
func TestNativeRefusedSendsIgnoreJournalBeforeClaim(t *testing.T) {
	h := newTestQueue(t)
	rt := installNativeRuntime(h)
	h.addNativeSession(t, "native")
	h.q.SetNativeSender(&fakeNativeSender{outcomes: []nativeinput.Outcome{
		nativeinput.NotSent, nativeinput.NotSent, nativeinput.NotSent, nativeinput.NotSent, nativeinput.NotSent,
	}})
	h.ag.confirmRecords(map[string][]time.Time{"old same text": {h.clock.now().Add(-time.Hour)}})
	id := addMsg(t, h, "native", "old same text")
	h.q.Wake("native")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "old journal record must not prove a refused message")
	msg, _ := h.st.GetMessage(id)
	if msg.Reason != nativeDeliveryUnconfirmed {
		t.Fatalf("reason = %q, want %s", msg.Reason, nativeDeliveryUnconfirmed)
	}
	if msg.InjectAt == 0 {
		t.Fatal("claim must persist the proof window before the send attempts")
	}
	assertNoTmux(t, rt)
}

// TestRestoreMarkerReachesNativeAndLegacyAccepted guards the shared restore
// marker: a recipient with both native and legacy accepted rows must let both
// halves see one session.restored event. The legacy row keeps its documented
// restore semantics (back to queued), not the 60s grace path.
func TestRestoreMarkerReachesNativeAndLegacyAccepted(t *testing.T) {
	h := newTestQueue(t)
	h.addNativeSession(t, "native")
	nativeID, err := h.st.AddMessage(store.Message{
		ToSession: "native", Body: "native accepted", Status: "accepted",
		DeliveryTransport: NativeTransportPiUDS, Attempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyID := addMsg(t, h, "native", "legacy accepted")
	if err := h.st.UpdateMessageStatus(legacyID, "accepted", 1, 0, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetMessageInjectAt(legacyID, h.clock.now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	st := &confirmState{used: make(map[string]map[recordKey]struct{}), restored: map[string]bool{"native": true}}
	h.q.confirmPass(h.ctx, st)

	if got := messageStatus(t, h.st, legacyID); got != "queued" {
		t.Fatalf("legacy accepted after restore = %q, want queued", got)
	}
	msg, _ := h.st.GetMessage(nativeID)
	if msg.Status != "failed" || msg.Reason != nativeDeliveryUnconfirmed {
		t.Fatalf("native accepted after restore = %+v, want failed/%s", msg, nativeDeliveryUnconfirmed)
	}
}

// TestNativeFinalProofAbsentFailsUnconfirmed is the other half of review case
// 2: no proof means an honest native_delivery_unconfirmed failure.
func TestNativeFinalProofAbsentFailsUnconfirmed(t *testing.T) {
	h := newTestQueue(t)
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "no proof")
	claimNativeForTest(t, h, id, h.clock.now())

	msg, _ := h.st.GetMessage(id)
	sess, _ := h.st.GetSession("native")
	_, confirmer := h.q.recipientCaps(sess)
	h.q.failNativeAttempt(h.ctx, msg, sess, confirmer)

	got, _ := h.st.GetMessage(id)
	if got.Status != "failed" || got.Reason != nativeDeliveryUnconfirmed {
		t.Fatalf("message = %+v, want failed/%s", got, nativeDeliveryUnconfirmed)
	}
}

// TestNativeFinalProofDoesNotReuseEarlierEqualRecord covers proof ownership on
// the terminal path: a record already spent on an earlier equal-text message
// must not settle a second delivering message.
func TestNativeFinalProofDoesNotReuseEarlierEqualRecord(t *testing.T) {
	h := newTestQueue(t)
	h.addNativeSession(t, "native")
	first := addMsg(t, h, "native", "same")
	if err := h.st.UpdateMessageStatus(first, "accepted", 1, 0, ""); err != nil {
		t.Fatal(err)
	}
	proofAt := h.clock.now().Add(time.Second)
	if ok, err := h.st.ConfirmAccepted(first, h.clock.now().Unix(), proofAt.UnixNano(), "same"); err != nil || !ok {
		t.Fatalf("confirm first = %v, %v", ok, err)
	}

	second := addMsg(t, h, "native", "same")
	claimNativeForTest(t, h, second, h.clock.now().Add(-time.Second))
	h.ag.confirmRecords(map[string][]time.Time{"same": {proofAt}})

	msg, _ := h.st.GetMessage(second)
	sess, _ := h.st.GetSession("native")
	_, confirmer := h.q.recipientCaps(sess)
	h.q.failNativeAttempt(h.ctx, msg, sess, confirmer)

	got, _ := h.st.GetMessage(second)
	if got.Status != "failed" || got.Reason != nativeDeliveryUnconfirmed {
		t.Fatalf("second message = %+v, want conservative failed/%s", got, nativeDeliveryUnconfirmed)
	}
}

// blockingFinalConfirmer never answers on its own: only ctx cancellation can
// release it. The final journal scan must pass a bounded, cancelable context.
type blockingFinalConfirmer struct{ *fakeAgent }

func (c *blockingFinalConfirmer) ConfirmDelivered(ctx context.Context, ref agent.ActivityRef, text string, since time.Time) (bool, time.Time, error) {
	<-ctx.Done()
	return false, time.Time{}, ctx.Err()
}

// TestNativeFinalConfirmationHonorsCallerCancellation is review case 2's
// cancelable requirement: the final scan must not use context.Background. On
// shutdown the row stays delivering, so startup Recover can turn it into
// "accepted" for journal confirmation instead of a permanent false failure.
func TestNativeFinalConfirmationHonorsCallerCancellation(t *testing.T) {
	h := newTestQueue(t)
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "blocked final scan")
	claimNativeForTest(t, h, id, h.clock.now())

	msg, _ := h.st.GetMessage(id)
	sess, _ := h.st.GetSession("native")
	confirmer := &blockingFinalConfirmer{fakeAgent: h.ag}
	ctx, cancel := context.WithCancel(h.ctx)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	done := make(chan struct{})
	go func() {
		h.q.failNativeAttempt(ctx, msg, sess, confirmer)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("final confirmation did not honor a bounded/cancelable context")
	}
	if got := messageStatus(t, h.st, id); got != "delivering" {
		t.Fatalf("status = %q, want delivering left for Recover after shutdown", got)
	}
}

// TestNativeFinalConfirmationBoundedByDeadline is the other half: with the
// daemon alive, an uncooperative journal cannot hold the delivery worker past
// the bounded scan, and the message fails honestly.
func TestNativeFinalConfirmationBoundedByDeadline(t *testing.T) {
	h := newTestQueue(t)
	h.addNativeSession(t, "native")
	h.q.confirmTimeout = 50 * time.Millisecond
	id := addMsg(t, h, "native", "slow final scan")
	claimNativeForTest(t, h, id, h.clock.now())

	msg, _ := h.st.GetMessage(id)
	sess, _ := h.st.GetSession("native")
	confirmer := &blockingFinalConfirmer{fakeAgent: h.ag}
	start := time.Now()
	h.q.failNativeAttempt(h.ctx, msg, sess, confirmer)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("final confirmation took %v, want the bounded deadline", elapsed)
	}
	got, _ := h.st.GetMessage(id)
	if got.Status != "failed" || got.Reason != nativeDeliveryUnconfirmed {
		t.Fatalf("message = %+v, want failed/%s", got, nativeDeliveryUnconfirmed)
	}
}

// addUnsupportedSession inserts a running session that carries a persisted,
// unsupported delivery transport but no tmux-side pending quiz.
func addUnsupportedSession(t *testing.T, h *testHarness, id, transport string) {
	t.Helper()
	if err := h.st.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "claude-code",
		Branch: "main", WorktreePath: "/tmp/" + id, TmuxName: id, State: "running",
		DeliveryTransport: transport,
	}); err != nil {
		t.Fatal(err)
	}
	h.ac.set(id, activity.Ready)
}

// TestUnsupportedSessionTransportDoesNotUseTmux is review case 3: a persisted
// session transport Rocket does not implement must fail closed, never fall
// back to visual delivery.
func TestUnsupportedSessionTransportDoesNotUseTmux(t *testing.T) {
	h := newTestQueue(t)
	rt := installNativeRuntime(h)
	addUnsupportedSession(t, h, "unsup", "other-v1")
	id := addMsg(t, h, "unsup", "must not paste")
	h.q.Wake("unsup")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "unsupported session transport failure")
	msg, _ := h.st.GetMessage(id)
	if msg.Reason != unsupportedDeliveryTransport {
		t.Fatalf("reason = %q, want %s", msg.Reason, unsupportedDeliveryTransport)
	}
	assertNoTmux(t, rt)
}

// TestUnsupportedAcceptedTransportFailsWithoutTmux covers the confirmation
// path: an accepted message on an unsupported transport must not be probed
// with tmux captures or requeued onto the visual path.
func TestUnsupportedAcceptedTransportFailsWithoutTmux(t *testing.T) {
	h := newTestQueue(t)
	rt := installNativeRuntime(h)
	h.addNativeSession(t, "native")
	h.q.cfg.QueueTimeout = 0
	id, err := h.st.AddMessage(store.Message{
		ToSession: "native", Body: "unsupported accepted", Status: "accepted",
		DeliveryTransport: "other-v1", Attempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	st := &confirmState{used: make(map[string]map[recordKey]struct{}), restored: make(map[string]bool)}
	h.q.confirmPass(h.ctx, st)

	msg, _ := h.st.GetMessage(id)
	if msg.Status != "failed" || msg.Reason != unsupportedDeliveryTransport {
		t.Fatalf("message = %+v, want failed/%s", msg, unsupportedDeliveryTransport)
	}
	assertNoTmux(t, rt)
}

// TestNativePurgeKeepsProofForPendingConfirmationAcrossRecover is review case
// 4 end to end: housekeeping must keep the consumed proof of an earlier
// equal-text message while a later native confirmation is still pending, or
// Recover would let that later message reuse the old record as its own proof.
func TestNativePurgeKeepsProofForPendingConfirmationAcrossRecover(t *testing.T) {
	h := newTestQueue(t)
	h.addNativeSession(t, "native")
	h.q.cfg.QueueTimeout = 0 // unresolved native state waits for exit/restore

	proofAt := h.clock.now().Add(time.Second)
	first, err := h.st.AddMessage(store.Message{ToSession: "native", Body: "same", CreatedAt: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.UpdateMessageStatus(first, "accepted", 1, 0, ""); err != nil {
		t.Fatal(err)
	}
	if ok, err := h.st.ConfirmAccepted(first, h.clock.now().Unix(), proofAt.UnixNano(), "same"); err != nil || !ok {
		t.Fatalf("confirm first = %v, %v", ok, err)
	}

	second, err := h.st.AddMessage(store.Message{ToSession: "native", Body: "same", CreatedAt: 100})
	if err != nil {
		t.Fatal(err)
	}
	claimNativeForTest(t, h, second, h.clock.now().Add(-time.Second))

	h.ag.confirmRecords(map[string][]time.Time{"same": {proofAt}})
	h.q.Recover(h.ctx) // native delivering -> accepted for journal confirmation
	if err := h.st.PurgeOld(500); err != nil {
		t.Fatalf("PurgeOld: %v", err)
	}
	if _, err := h.st.GetMessage(first); err != nil {
		t.Fatalf("consumed proof row purged while a native confirmation was pending: %v", err)
	}
	if _, err := h.st.GetMessage(second); err != nil {
		t.Fatalf("unresolved native row purged: %v", err)
	}

	st := &confirmState{used: make(map[string]map[recordKey]struct{}), restored: make(map[string]bool)}
	h.q.confirmPass(h.ctx, st)
	if got := messageStatus(t, h.st, second); got != "accepted" {
		t.Fatalf("pending native message settled with another message's proof: %q", got)
	}
}
