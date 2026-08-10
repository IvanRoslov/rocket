package queue

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// fakeSocket is a scriptable SocketSender.
type fakeSocket struct {
	mu        sync.Mutex
	available bool
	sendErr   error
	attempts  int      // Send calls, including the ones that returned sendErr
	sends     []string // texts passed to Send, in call order
	names     []string // fromName passed alongside each send
}

func (f *fakeSocket) Available(store.Session) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.available
}

func (f *fakeSocket) Send(_ store.Session, fromName, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.sendErr != nil {
		if errors.Is(f.sendErr, ErrHeld) {
			// Mirror ClaudeSocketSender's heldSessions memory, which the
			// SocketSender contract requires: a process that held one message
			// holds them all, so it stops being "available".
			f.available = false
		}
		return f.sendErr
	}
	f.sends = append(f.sends, text)
	f.names = append(f.names, fromName)
	return nil
}

// attemptCount is every Send call, unlike sendCount which counts only the
// ones that got as far as accepting the text.
func (f *fakeSocket) attemptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

func (f *fakeSocket) sendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sends)
}

func (f *fakeSocket) lastSend() (text, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) == 0 {
		return "", ""
	}
	return f.sends[len(f.sends)-1], f.names[len(f.names)-1]
}

// TestSocketDeliverySucceedsWithoutInject is the core promise: when the socket
// carries the message, the recipient's pane is never touched at all.
func TestSocketDeliverySucceedsWithoutInject(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Idle)

	sock := &fakeSocket{available: true}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{FromSession: "orch", ToSession: "recv", Body: "hello over socket"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered over socket")

	if sock.sendCount() != 1 {
		t.Errorf("socket sends = %d, want 1", sock.sendCount())
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0 when the socket succeeded", n)
	}
	text, name := sock.lastSend()
	if text != "[from orch] hello over socket" {
		t.Errorf("socket text = %q, want the same body tmux would have injected", text)
	}
	if name != "orch" {
		t.Errorf("fromName = %q, want the rocket sender id", name)
	}
}

// TestSocketFromNameForSystemMessages pins the human/system fallback name.
func TestSocketFromNameForSystemMessages(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Idle)

	sock := &fakeSocket{available: true}
	h.q.SetSocketSender(sock)

	if _, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "from a human"}); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	waitUntil(t, func() bool { return sock.sendCount() == 1 }, "socket send")

	text, name := sock.lastSend()
	if name != "rocket" {
		t.Errorf("fromName = %q, want \"rocket\" for a message with no sender", name)
	}
	if text != "from a human" {
		t.Errorf("socket text = %q, want the unprefixed body", text)
	}
}

// TestSocketFailureFallsBackToInject pins that a socket error costs the
// message nothing: tmux picks it up inside the very same attempt.
func TestSocketFailureFallsBackToInject(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Idle)

	sock := &fakeSocket{available: true, sendErr: errors.New("dial: connection refused")}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "hello with fallback"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered via tmux fallback")

	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want exactly 1 fallback injection", n)
	}
	m, err := h.st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.Attempts != 1 {
		t.Errorf("attempts = %d, want 1: a socket failure must not burn an extra attempt", m.Attempts)
	}
}

// TestSocketDeliveryDisabledByConfig pins the escape hatch.
func TestSocketDeliveryDisabledByConfig(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.SocketDelivery = false
	h.addRunningSession(t, "recv", activity.Idle)

	sock := &fakeSocket{available: true}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "config off"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered via tmux")

	if sock.sendCount() != 0 {
		t.Errorf("socket sends = %d, want 0 when socket_delivery is off", sock.sendCount())
	}
	if n := h.rt.callCount(); n != 1 {
		t.Errorf("Inject called %d times, want 1", n)
	}
}

// TestSocketSkippedForNonClaudeAgent pins that the transport is not offered to
// agents that have no such socket. The real sender enforces this too, but the
// queue must not even consult it — Available is the seam tests script.
func TestSocketSkippedForNonClaudeAgent(t *testing.T) {
	h := newTestQueue(t)
	if err := h.st.AddSession(store.Session{
		ID: "recv", Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "codex",
		Branch: "main", WorktreePath: "/tmp/recv", TmuxName: "recv", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	h.ac.set("recv", activity.Idle)

	sock := &fakeSocket{available: true}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "codex message"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered via tmux")

	if sock.sendCount() != 0 {
		t.Errorf("socket sends = %d, want 0 for a non-claude-code agent", sock.sendCount())
	}
	if n := h.rt.callCount(); n != 1 {
		t.Errorf("Inject called %d times, want 1", n)
	}
}

// TestQuizPendingDeliversOverSocket: a quiz blocks the TUI, not the socket.
func TestQuizPendingDeliversOverSocket(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.WaitingInput)
	if err := h.st.SetPendingQuiz("recv", "q-1"); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	sock := &fakeSocket{available: true}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "during a quiz"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered over socket during a quiz")

	if sock.sendCount() != 1 {
		t.Errorf("socket sends = %d, want 1", sock.sendCount())
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0: tmux must stay blocked during a quiz", n)
	}
}

// TestQuizPendingWithoutSocketStaysQueued pins the unchanged pre-existing rule.
func TestQuizPendingWithoutSocketStaysQueued(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.WaitingInput)
	if err := h.st.SetPendingQuiz("recv", "q-1"); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	sock := &fakeSocket{available: false}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "during a quiz, no socket"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	// Several fallback ticks' worth of proof that nothing is being delivered.
	time.Sleep(150 * time.Millisecond)

	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Errorf("status = %q, want queued while a quiz is pending and no socket exists", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0 during a pending quiz", n)
	}
	if sock.sendCount() != 0 {
		t.Errorf("socket sends = %d, want 0 when the socket is unavailable", sock.sendCount())
	}
}

// TestComposerBusyDeliversOverSocket is the headline interaction with the
// draft guard (#69): a human is mid-sentence, and the message still arrives —
// without the deferral the tmux path would need, and without erasing a
// character of what they typed.
func TestComposerBusyDeliversOverSocket(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.ComposerBusyDeadline = time.Hour
	h.addRunningSession(t, "recv", activity.Ready)

	h.rt.setCapture(func(runtime.Handle, int) (string, error) {
		return draftPane, nil
	})

	sock := &fakeSocket{available: true}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "draft must survive"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "delivered" }, "delivered over socket despite the draft")

	if sock.sendCount() != 1 {
		t.Errorf("socket sends = %d, want 1", sock.sendCount())
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0: the human's draft must never be cleared", n)
	}
}

// TestComposerBusyWithoutSocketStillDefers pins that #69's behavior is intact
// wherever the socket is not available.
func TestComposerBusyWithoutSocketStillDefers(t *testing.T) {
	h := newTestQueue(t)
	h.q.cfg.ComposerBusyDeadline = time.Hour
	h.addRunningSession(t, "recv", activity.Ready)

	h.rt.setCapture(func(runtime.Handle, int) (string, error) {
		return draftPane, nil
	})

	sock := &fakeSocket{available: false}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "no socket, draft on screen"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	time.Sleep(150 * time.Millisecond)

	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Errorf("status = %q, want queued: the draft guard must be untouched without a socket", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0", n)
	}
}

// TestSocketLostDuringQuizRequeues covers the narrow race where the socket
// dies between deliver's gate and the send while a quiz is on screen: tmux is
// forbidden, so the message must go back to queued untouched rather than be
// injected into the widget or burn an attempt.
func TestSocketLostDuringQuizRequeues(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.WaitingInput)
	if err := h.st.SetPendingQuiz("recv", "q-1"); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	// Available says yes, Send then fails — exactly the race.
	sock := &fakeSocket{available: true, sendErr: errors.New("socket vanished")}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{ToSession: "recv", Body: "racy"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	time.Sleep(150 * time.Millisecond)

	if got := messageStatus(t, h.st, id); got != "queued" {
		t.Errorf("status = %q, want queued after losing the socket mid-quiz", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0: the quiz widget must not be touched", n)
	}
	m, err := h.st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.Attempts != 0 {
		t.Errorf("attempts = %d, want 0: a deferral is not a failed attempt", m.Attempts)
	}
}

// TestHeldMessageFallsBackToInject pins the hole this whole receipt channel
// exists to close: the recipient held the message instead of queueing it, and
// rocket used to mark that "delivered". ErrHeld must send the delivery down
// the tmux path so the message actually arrives.
//
// It must NOT do so in the same pass, though: a hold is exactly what puts the
// approval dialog on the recipient's screen, and the dialog needs a moment to
// render. Injecting into it dismisses it and loses both copies of the message,
// so the hold requeues and the next pass re-evaluates the pane (where the
// held-dialog gate takes over).
func TestHeldMessageFallsBackToInject(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Idle)

	sock := &fakeSocket{available: true, sendErr: ErrHeld}
	h.q.SetSocketSender(sock)

	id, err := h.st.AddMessage(store.Message{FromSession: "orch", ToSession: "recv", Body: "важное"})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	h.q.Wake("recv")

	waitUntil(t, func() bool { return sock.attemptCount() >= 1 }, "socket send attempted")
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times right after the hold, want 0 — the dialog is rendering", n)
	}

	waitUntilTimeout(t, 5*time.Second,
		func() bool { return messageStatus(t, h.st, id) == "delivered" },
		"delivered via tmux on a later pass after a hold")
	if n := h.rt.callCount(); n != 1 {
		t.Errorf("Inject called %d times, want 1 — a held message must go out over tmux", n)
	}
	m, err := h.st.GetMessage(id)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.Attempts != 1 {
		t.Errorf("attempts = %d, want 1 — the hold is a deferral, not a burned attempt", m.Attempts)
	}
}
