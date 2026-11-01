package queue

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/nativeinput"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

type nativeCall struct {
	target    nativeinput.Target
	messageID int64
	text      string
}

type fakeNativeSender struct {
	mu       sync.Mutex
	calls    []nativeCall
	outcomes []nativeinput.Outcome
	errs     []error
}

func (s *fakeNativeSender) Send(ctx context.Context, target nativeinput.Target, messageID int64, text string) (nativeinput.Outcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, nativeCall{target: target, messageID: messageID, text: text})
	i := len(s.calls) - 1
	if i < len(s.errs) && s.errs[i] != nil {
		return s.outcomes[i], s.errs[i]
	}
	if i < len(s.outcomes) {
		return s.outcomes[i], nil
	}
	return nativeinput.Accepted, nil
}

func (s *fakeNativeSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

type nativeRuntime struct {
	*fakeRuntime
	injects   int
	sendKeys  int
	captures  int
	captureEs int
}

func (r *nativeRuntime) Inject(ctx context.Context, h runtime.Handle, text string, opts runtime.InjectOpts) error {
	r.injects++
	return r.fakeRuntime.Inject(ctx, h, text, opts)
}

func (r *nativeRuntime) SendKeys(ctx context.Context, h runtime.Handle, key string, literal bool) error {
	r.sendKeys++
	return r.fakeRuntime.SendKeys(ctx, h, key, literal)
}

func (r *nativeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	r.captures++
	return r.fakeRuntime.Capture(ctx, h, lines)
}

func (r *nativeRuntime) CaptureEscaped(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	r.captureEs++
	return r.fakeRuntime.CaptureEscaped(ctx, h, lines)
}

func TestNativeDeliveryDoesNotTouchTmux(t *testing.T) {
	h := newTestQueue(t)
	rt := installNativeRuntime(h)
	sender := &fakeNativeSender{}
	h.q.SetNativeSender(sender)
	h.startConfirmer()
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "hello native")
	at := h.clock.now().Add(time.Second)
	h.ag.confirmRecords(map[string][]time.Time{nativePiEnvelope(id, "hello native"): {at}})
	h.q.Wake("native")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "native journal confirmation")
	if sender.count() != 1 {
		t.Fatalf("native sends = %d, want 1", sender.count())
	}
	assertNoTmux(t, rt)
	msg, _ := h.st.GetMessage(id)
	if msg.DeliveryTransport != "pi-uds-v1" || msg.InjectAt == 0 {
		t.Fatalf("native snapshot = %+v", msg)
	}
	call := sender.calls[0]
	if call.target.SessionID != "native" || call.target.InstanceID != "launch-1" || call.target.SocketPath != "/tmp/native.sock" || call.messageID != id || call.text != nativePiEnvelope(id, "hello native") {
		t.Fatalf("native call = %+v", call)
	}
}

func TestNativeUnavailableNeverFallsBack(t *testing.T) {
	h := newTestQueue(t)
	rt := installNativeRuntime(h)
	sender := &fakeNativeSender{
		outcomes: []nativeinput.Outcome{nativeinput.NotSent, nativeinput.NotSent, nativeinput.NotSent, nativeinput.NotSent, nativeinput.NotSent},
		errs:     []error{errors.New("endpoint missing"), errors.New("endpoint missing"), errors.New("endpoint missing"), errors.New("endpoint missing"), errors.New("endpoint missing")},
	}
	h.q.SetNativeSender(sender)
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "do not paste")
	h.q.Wake("native")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "bounded native failure")
	if sender.count() != maxAttempts {
		t.Fatalf("native attempts = %d, want %d", sender.count(), maxAttempts)
	}
	assertNoTmux(t, rt)
	msg, _ := h.st.GetMessage(id)
	if msg.Reason != "native_delivery_unconfirmed" {
		t.Fatalf("failure reason = %q, want native_delivery_unconfirmed", msg.Reason)
	}
}

func TestNativeNotSentRetriesWithoutTmux(t *testing.T) {
	h := newTestQueue(t)
	rt := installNativeRuntime(h)
	sender := &fakeNativeSender{outcomes: []nativeinput.Outcome{nativeinput.NotSent, nativeinput.Accepted}}
	h.q.SetNativeSender(sender)
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "retry once")
	h.q.Wake("native")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted after safe retry")
	if sender.count() != 2 {
		t.Fatalf("native sends = %d, want 2", sender.count())
	}
	assertNoTmux(t, rt)
}

func TestNativeUnknownDoesNotRetryAfterRecover(t *testing.T) {
	h := newTestQueue(t)
	installNativeRuntime(h)
	sender := &fakeNativeSender{outcomes: []nativeinput.Outcome{nativeinput.Unknown, nativeinput.Accepted}}
	h.q.SetNativeSender(sender)
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "uncertain")
	h.q.Wake("native")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "unknown held for confirmation")

	h.q.Recover(h.ctx)
	if got := messageStatus(t, h.st, id); got != "accepted" {
		t.Fatalf("status after Recover = %q, want accepted", got)
	}
	if sender.count() != 1 {
		t.Fatalf("native sends after Recover = %d, want 1 (no blind retry)", sender.count())
	}
}

func TestNativeRecoverDeliveringMovesToConfirmation(t *testing.T) {
	h := newTestQueue(t)
	sender := &fakeNativeSender{}
	h.q.SetNativeSender(sender)
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "crash window")
	if ok, err := h.st.ClaimNativeMessage(id, "pi-uds-v1", h.clock.now().UnixMilli()); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}

	h.q.Recover(h.ctx)
	if got := messageStatus(t, h.st, id); got != "accepted" {
		t.Fatalf("status = %q, want accepted", got)
	}
	if sender.count() != 0 {
		t.Fatalf("native sends = %d, want 0", sender.count())
	}
}

func TestNativeDuplicateTextUsesDistinctMessageIDs(t *testing.T) {
	h := newTestQueue(t)
	sender := &fakeNativeSender{}
	h.q.SetNativeSender(sender)
	h.startConfirmer()
	h.addNativeSession(t, "native")
	id1 := addMsg(t, h, "native", "same")
	id2 := addMsg(t, h, "native", "same")
	t1 := h.clock.now().Add(time.Second)
	t2 := t1.Add(time.Second)
	h.ag.confirmRecords(map[string][]time.Time{
		nativePiEnvelope(id1, "same"): {t1},
		nativePiEnvelope(id2, "same"): {t2},
	})
	h.q.Wake("native")

	waitUntil(t, func() bool {
		return messageStatus(t, h.st, id1) == "delivered" && messageStatus(t, h.st, id2) == "delivered"
	}, "both equal texts confirmed separately")
	if sender.count() != 2 || sender.calls[0].messageID != id1 || sender.calls[1].messageID != id2 {
		t.Fatalf("native calls = %+v", sender.calls)
	}
}

func TestNativeLateConfirmationDoesNotRequeue(t *testing.T) {
	h := newTestQueue(t)
	sender := &fakeNativeSender{}
	h.q.SetNativeSender(sender)
	h.startConfirmer()
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "late")
	h.q.Wake("native")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")

	h.clock.advance(acceptedTurnGrace + time.Minute)
	h.q.kickConfirmer()
	if got := messageStatus(t, h.st, id); got != "accepted" {
		t.Fatalf("status after legacy grace = %q, want accepted", got)
	}
	if sender.count() != 1 {
		t.Fatalf("native sends = %d, want 1", sender.count())
	}
	h.ag.confirmRecords(map[string][]time.Time{nativePiEnvelope(id, "late"): {h.clock.now()}})
	h.q.kickConfirmer()
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "late confirmation")
}

func TestNativeNoTranscriptTimesOutWithoutRetry(t *testing.T) {
	h := newTestQueue(t)
	sender := &fakeNativeSender{outcomes: []nativeinput.Outcome{nativeinput.Unknown}}
	h.q.SetNativeSender(sender)
	h.startConfirmer()
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "missing transcript")
	h.q.Wake("native")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")

	h.clock.advance(31 * time.Minute)
	h.q.kickConfirmer()
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "unconfirmed timeout")
	msg, _ := h.st.GetMessage(id)
	if msg.Reason != "native_delivery_unconfirmed" || sender.count() != 1 {
		t.Fatalf("message = %+v, sends = %d", msg, sender.count())
	}
}

func TestNativeDisabledQueueTimeoutWaitsForConfirmation(t *testing.T) {
	h := newTestQueue(t)
	sender := &fakeNativeSender{outcomes: []nativeinput.Outcome{nativeinput.Unknown}}
	h.q.SetNativeSender(sender)
	h.startConfirmer()
	h.addNativeSession(t, "native")
	h.q.cfg.QueueTimeout = 0
	id := addMsg(t, h, "native", "wait")
	h.q.Wake("native")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")

	h.clock.advance(100 * time.Hour)
	h.q.kickConfirmer()
	if got := messageStatus(t, h.st, id); got != "accepted" {
		t.Fatalf("status with disabled timeout = %q, want accepted", got)
	}
	if sender.count() != 1 {
		t.Fatalf("native sends = %d, want 1", sender.count())
	}
}

func TestNativeTerminalFailureChecksJournalFirst(t *testing.T) {
	h := newTestQueue(t)
	sender := &fakeNativeSender{outcomes: []nativeinput.Outcome{nativeinput.Unknown}}
	h.q.SetNativeSender(sender)
	h.startConfirmer()
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "recorded before exit")
	h.q.Wake("native")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")
	h.ag.confirmRecords(map[string][]time.Time{nativePiEnvelope(id, "recorded before exit"): {h.clock.now().Add(time.Second)}})

	h.activityTo("native", activity.Exited)
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivery proven after exit")
}

func TestNativeRestoreChecksJournalBeforeFailure(t *testing.T) {
	h := newTestQueue(t)
	sender := &fakeNativeSender{outcomes: []nativeinput.Outcome{nativeinput.Unknown}}
	h.q.SetNativeSender(sender)
	h.startConfirmer()
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "recorded before restore")
	h.q.Wake("native")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")
	h.ag.confirmRecords(map[string][]time.Time{nativePiEnvelope(id, "recorded before restore"): {h.clock.now().Add(time.Second)}})

	h.b.Publish("session.restored", "native", nil)
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivery proven after restore")
}

func TestNativeExitWithoutConfirmationFails(t *testing.T) {
	h := newTestQueue(t)
	sender := &fakeNativeSender{outcomes: []nativeinput.Outcome{nativeinput.Unknown}}
	h.q.SetNativeSender(sender)
	h.startConfirmer()
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "lost")
	h.q.Wake("native")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")

	h.activityTo("native", activity.Exited)
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "failed" }, "unconfirmed exit failure")
	msg, _ := h.st.GetMessage(id)
	if msg.Reason != "native_delivery_unconfirmed" || sender.count() != 1 {
		t.Fatalf("message = %+v, sends = %d", msg, sender.count())
	}
}

func TestNativeRecoveryDoesNotReuseEqualTextProof(t *testing.T) {
	for _, collision := range []bool{false, true} {
		name := "delayed proof"
		if collision {
			name = "timestamp collision"
		}
		t.Run(name, func(t *testing.T) {
			h := newTestQueue(t)
			h.addNativeSession(t, "native")
			base := h.clock.now()
			proofAt := base.Add(time.Second)
			if collision {
				proofAt = base
			}
			h.ag.confirmRecords(map[string][]time.Time{"same": {proofAt}})
			first := addMsg(t, h, "native", "same")
			firstMsg, _ := h.st.GetMessage(first)
			firstMsg.Status = "accepted"
			firstMsg.Attempts = 1
			firstMsg.InjectAt = base.Add(-2 * time.Second).UnixMilli()
			if err := h.st.UpdateMessageStatus(first, "accepted", 1, 0, ""); err != nil {
				t.Fatal(err)
			}
			if err := h.st.SetMessageInjectAt(first, firstMsg.InjectAt); err != nil {
				t.Fatal(err)
			}
			st1 := &confirmState{used: make(map[string]map[recordKey]struct{}), restored: make(map[string]bool)}
			_, confirmer := h.q.recipientCaps(store.Session{Agent: "codex"})
			if !h.q.confirmOne(h.ctx, st1, confirmer, agent.ActivityRef{SessionID: "native"}, "native", firstMsg, "same") {
				t.Fatal("first equal-text message was not confirmed")
			}

			second := addMsg(t, h, "native", "same")
			if ok, err := h.st.ClaimNativeMessage(second, "pi-uds-v1", base.Add(-time.Second).UnixMilli()); err != nil || !ok {
				t.Fatalf("second claim = %v, %v", ok, err)
			}
			h.q.Recover(h.ctx)
			h.activityTo("native", activity.Exited)
			st2 := &confirmState{used: make(map[string]map[recordKey]struct{}), restored: make(map[string]bool)}
			h.q.confirmPass(h.ctx, st2)

			msg, _ := h.st.GetMessage(second)
			if msg.Status != "failed" || msg.Reason != "native_delivery_unconfirmed" {
				t.Fatalf("unsent equal-text message = %+v, want conservative failure, not false delivery", msg)
			}
		})
	}
}

func TestAcceptedLegacyMessageDoesNotBecomeNativeAfterRestore(t *testing.T) {
	h := newTestQueue(t)
	h.addNativeSession(t, "native")
	id := addMsg(t, h, "native", "legacy paste")
	if err := h.st.UpdateMessageStatus(id, "accepted", maxAttempts, 0, ""); err != nil {
		t.Fatal(err)
	}
	st := &confirmState{used: make(map[string]map[recordKey]struct{}), restored: make(map[string]bool)}
	h.q.confirmPass(h.ctx, st)

	msg, _ := h.st.GetMessage(id)
	if msg.Status != "failed" || msg.Reason != "not_confirmed" {
		t.Fatalf("accepted legacy message = %+v, want legacy not_confirmed", msg)
	}
}

func TestNativeRetryClaimsBeforeSendWithPreviousAttempts(t *testing.T) {
	h := newTestQueue(t)
	rt := installNativeRuntime(h)
	h.addNativeSession(t, "native")
	id, err := h.st.AddMessage(store.Message{ToSession: "native", Body: "retry", Attempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := h.st.GetMessage(id)
	sess, _ := h.st.GetSession("native")
	h.q.SetNativeSender(nativeSenderFunc(func(ctx context.Context, target nativeinput.Target, messageID int64, text string) (nativeinput.Outcome, error) {
		current, _ := h.st.GetMessage(id)
		if current.Status != "delivering" {
			t.Fatalf("status before send = %q, want claimed delivering", current.Status)
		}
		return nativeinput.Accepted, nil
	}))

	h.q.attemptNativeDelivery(h.ctx, msg, sess, nil)
	if got := messageStatus(t, h.st, id); got != "accepted" {
		t.Fatalf("status = %q, want accepted", got)
	}
	assertNoTmux(t, rt)
}

func TestNativeDuplicateClaimDoesNotSend(t *testing.T) {
	h := newTestQueue(t)
	rt := installNativeRuntime(h)
	h.addNativeSession(t, "native")
	sender := &fakeNativeSender{}
	h.q.SetNativeSender(sender)
	id, err := h.st.AddMessage(store.Message{ToSession: "native", Body: "owned elsewhere", Attempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := h.st.ClaimMessage(id); err != nil || !ok {
		t.Fatalf("other owner claim = %v, %v", ok, err)
	}
	stale, _ := h.st.GetMessage(id)
	stale.Status = "queued"
	sess, _ := h.st.GetSession("native")

	h.q.attemptNativeDelivery(h.ctx, stale, sess, nil)
	if sender.count() != 0 {
		t.Fatalf("native sends after lost claim = %d, want 0", sender.count())
	}
	assertNoTmux(t, rt)
}

func TestUnsupportedTransportDoesNotUseTmux(t *testing.T) {
	h := newTestQueue(t)
	rt := installNativeRuntime(h)
	h.addNativeSession(t, "native")
	if err := h.st.ClearPendingQuiz("native"); err != nil {
		t.Fatal(err)
	}
	id, err := h.st.AddMessage(store.Message{ToSession: "native", Body: "unknown protocol", DeliveryTransport: "other-v1"})
	if err != nil {
		t.Fatal(err)
	}
	queued, ok, err := h.st.NextQueuedMessage("native")
	if err != nil || !ok {
		t.Fatalf("NextQueuedMessage = %v, %v", ok, err)
	}
	h.q.deliver(h.ctx, queued)

	if got := messageStatus(t, h.st, id); got != "failed" {
		t.Fatalf("status = %q, want failed for unsupported transport", got)
	}
	msg, _ := h.st.GetMessage(id)
	if msg.Reason != "unsupported_delivery_transport" {
		t.Fatalf("reason = %q, want unsupported_delivery_transport", msg.Reason)
	}
	assertNoTmux(t, rt)
}

func TestNativeNoResponseBecomesAcceptedWithoutRetry(t *testing.T) {
	h := newTestQueue(t)
	dir, err := os.MkdirTemp("", "nq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "n.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	connections := make(chan struct{}, 2)
	release := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		connections <- struct{}{}
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		<-release
		_ = conn.Close()
	}()
	t.Cleanup(func() { close(release) })
	h.addNativeSession(t, "native")
	if err := h.st.UpdateSessionDelivery("native", "pi-uds-v1", "launch-1", socketPath); err != nil {
		t.Fatal(err)
	}
	h.q.SetNativeSender(nativeinput.SenderFunc(nativeinput.Send))
	id := addMsg(t, h, "native", "no response")
	h.q.Wake("native")

	waitUntilTimeout(t, 8*time.Second, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "unknown held for confirmation after response timeout")
	select {
	case <-connections:
	default:
		t.Fatal("no native connection observed")
	}
	select {
	case <-connections:
		t.Fatal("native request was retried after response timeout")
	case <-time.After(100 * time.Millisecond):
	}
}

type nativeSenderFunc func(context.Context, nativeinput.Target, int64, string) (nativeinput.Outcome, error)

func (f nativeSenderFunc) Send(ctx context.Context, target nativeinput.Target, messageID int64, text string) (nativeinput.Outcome, error) {
	return f(ctx, target, messageID, text)
}

func (h *testHarness) addNativeSession(t *testing.T, id string) {
	t.Helper()
	if err := h.st.AddSession(store.Session{
		ID: id, Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "codex",
		Branch: "main", WorktreePath: "/tmp/" + id, TmuxName: id, State: "running", PendingQuiz: "quiz",
		DeliveryTransport: "pi-uds-v1", DeliveryInstance: "launch-1", DeliveryEndpoint: "/tmp/native.sock",
	}); err != nil {
		t.Fatal(err)
	}
	h.ac.set(id, activity.Ready)
	h.ag.setBusy(true)
}

func installNativeRuntime(h *testHarness) *nativeRuntime {
	rt := &nativeRuntime{fakeRuntime: h.rt}
	h.q.rt = rt
	return rt
}

func assertNoTmux(t *testing.T, rt *nativeRuntime) {
	t.Helper()
	if rt.injects != 0 || rt.sendKeys != 0 || rt.captures != 0 || rt.captureEs != 0 {
		t.Fatalf("tmux calls: Inject=%d SendKeys=%d Capture=%d CaptureEscaped=%d", rt.injects, rt.sendKeys, rt.captures, rt.captureEs)
	}
}

var _ agent.DeliveryConfirmer = (*fakeAgent)(nil)
