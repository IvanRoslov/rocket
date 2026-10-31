package queue

import (
	"context"
	"errors"
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
	at := h.clock.now().Add(time.Second)
	h.ag.confirmRecords(map[string][]time.Time{"hello native": {at}})
	id := addMsg(t, h, "native", "hello native")
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
	if call.target.SessionID != "native" || call.target.InstanceID != "launch-1" || call.target.SocketPath != "/tmp/native.sock" || call.messageID != id || call.text != "hello native" {
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
	t1 := h.clock.now().Add(time.Second)
	t2 := t1.Add(time.Second)
	h.ag.confirmRecords(map[string][]time.Time{"same": {t1, t2}})
	id1 := addMsg(t, h, "native", "same")
	id2 := addMsg(t, h, "native", "same")
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
	h.ag.confirmRecords(map[string][]time.Time{"late": {h.clock.now()}})
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
	h.ag.confirmRecords(map[string][]time.Time{"recorded before exit": {h.clock.now().Add(time.Second)}})

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
	h.ag.confirmRecords(map[string][]time.Time{"recorded before restore": {h.clock.now().Add(time.Second)}})

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
