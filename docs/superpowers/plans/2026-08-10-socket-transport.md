# Socket Transport Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver rocket queue messages to Claude Code sessions over their cross-session UDS inbox, falling back to tmux injection instantly on any error, so a human's composer draft is never erased.

**Architecture:** A small `SocketSender` interface is added to `internal/queue` as a seam (two methods: `Available`, `Send`). Its production implementation, `ClaudeSocketSender`, maps a rocket `store.Session` to a Claude Code registry entry by tmux name, probes the socket, and calls `socketmsg.Send`. The queue's delivery worker consults the seam before each `runtime.Inject`; any failure falls through to the existing tmux path within the same attempt. The pending-quiz gate moves from "block always" to "block only when the socket path is unavailable", because socket delivery never touches the TUI.

**Tech Stack:** Go 1.x, `internal/socketmsg` (merged in PR #68), `internal/queue`, `internal/config`, stdlib `testing`.

## Global Constraints

- Rebase on latest `origin/main` before starting. Required ancestors: `ce5360d` (`internal/socketmsg`) and `d449e10` (draft guard, PR #69). Already done: this branch sits on `d449e10`.
- Post-#69 interface facts this plan is written against: `Inject(ctx, h, text, opts runtime.InjectOpts)` takes a fourth argument; `runtime.ErrComposerBusy` means nothing was cleared or sent; `attemptDelivery(ctx, msg, sess, force) bool` returns `false` only for the composer-busy deferral; `config.ComposerBusyDeadline` exists.
- **Socket delivery is exempt from the draft guard.** That exemption is the point of this task, not a side effect: #69 defers delivery while a human is typing, and the socket makes deferring unnecessary because it never touches the composer.
- Registry match key: tmux prefix before `':'` in the registry entry's `tmux` field == `store.Session.TmuxName`. Additionally require `PeerProtocol == 1`, non-empty `MessagingSocketPath`, and a successful `socketmsg.Probe`. Any miss at any step → tmux injection.
- Put the registry entry's `SessionID` into the message's `session_id` field (closes the pid-reuse race).
- Do **not** set `from-mode`. Do **not** set `hop-chain` / `from-session`.
- Envelope `from-name`: the rocket sender id (`msg.FromSession`), or `"rocket"` when empty (human/system messages).
- The delivered text is **identical on both transports**: `prepareText` keeps deciding full-body vs `.rocket/inbox` pointer, and whatever it returns goes over the socket too. This preserves the brief's byte-identity requirement (line 11) and keeps `prepareText`'s existing call ordering. (The orchestrator briefly asked for full-body-over-socket and then retracted it in favour of this.)
- Successful socket write == delivered. There is no ack and no receipt listener; a return channel is out of scope.
- Config knob: `SocketDelivery bool` / yaml `socket_delivery`, default `true`, placed next to `QueueTimeout` / `LargeMessageThreshold`.
- Never fail a message because of the socket path. Log socket problems at debug/info, not error.
- Gate is local green: `go build ./... && go test ./...`. The repo has no CI.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/config/config.go` (modify) | Add `SocketDelivery` field + default `true`. |
| `internal/config/config_test.go` (modify) | Pin the default and the yaml override. |
| `internal/queue/socket.go` (create) | `SocketSender` interface + `ClaudeSocketSender` production impl (registry lookup, probe, send). |
| `internal/queue/socket_test.go` (create) | Unit tests for the registry-lookup/matching logic against a temp registry dir. |
| `internal/queue/queue.go` (modify) | `socket` field + `SetSocketSender`; transport selection in `attemptDelivery`; quiz gate in `deliver`; `socketFromName` helper. `prepareText` unchanged. |
| `internal/queue/queue_socket_test.go` (create) | The five behavior tests from the brief, using a fake `SocketSender`. |
| `internal/daemon/*.go` (modify) | Wire `ClaudeSocketSender` into the queue at startup. |
| `docs/06-messaging.md` (modify) | Document the transport, selection rules, fallback, delivered semantics, config knob. |

---

### Task 1: Config knob `socket_delivery`

**Files:**
- Modify: `internal/config/config.go` (struct near line 54, defaults near line 148)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `config.Config.SocketDelivery bool` (yaml `socket_delivery`), default `true`.

- [ ] **Step 1: Write the failing tests**

In `internal/config/config_test.go`, add to the defaults test (near the existing `LargeMessageThreshold` assertion at line 49) and add a new override test:

```go
	if !cfg.SocketDelivery {
		t.Error("expected SocketDelivery to default to true")
	}
```

```go
func TestLoadSocketDeliveryOverride(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"),
		[]byte("socket_delivery: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SocketDelivery {
		t.Error("expected socket_delivery: false to disable socket delivery")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'Default|SocketDelivery' -v`
Expected: FAIL — `cfg.SocketDelivery` undefined (compile error).

- [ ] **Step 3: Implement**

In the `Config` struct, directly after the `LargeMessageThreshold` field:

```go
	// SocketDelivery enables delivering queued messages to Claude Code
	// recipients over their cross-session UDS inbox instead of injecting
	// text into their tmux pane (see internal/queue/socket.go). Socket
	// delivery never touches the recipient's composer, so it does not erase
	// a half-typed draft. Any failure on the socket path falls back to tmux
	// injection within the same attempt, so turning this off only costs the
	// draft-preserving property, never delivery itself.
	SocketDelivery bool `yaml:"socket_delivery"`
```

In the defaults literal, directly after `LargeMessageThreshold: 2048,`:

```go
		SocketDelivery:            true,
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "config: add socket_delivery knob (default on)"
```

---

### Task 2: `SocketSender` seam and registry matching

**Files:**
- Create: `internal/queue/socket.go`
- Test: `internal/queue/socket_test.go`

**Interfaces:**
- Consumes: `store.Session` (fields `Agent`, `TmuxName`), `socketmsg.ListSessions`, `socketmsg.Probe`, `socketmsg.Send`, `socketmsg.Options`.
- Produces:
  - `type SocketSender interface { Available(sess store.Session) bool; Send(sess store.Session, fromName, text string) error }`
  - `func NewClaudeSocketSender(sessionsDir string) *ClaudeSocketSender`
  - `ClaudeSocketSender` implements `SocketSender`.
  - unexported `func (s *ClaudeSocketSender) lookup(sess store.Session) (socketmsg.Session, bool)` — used by both methods and tested directly.

- [ ] **Step 1: Write the failing tests**

Create `internal/queue/socket_test.go`:

```go
package queue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"github.com/IvanRoslov/rocket/internal/store"
)

// writeRegistry writes one <pid>.json entry into dir.
func writeRegistry(t *testing.T, dir string, s socketmsg.Session) {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, itoa(s.PID)+".json")
	if err := os.WriteFile(name, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}

// liveSocket starts a listener at <dir>/<name>.sock and returns its path.
func liveSocket(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name+".sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			io.Copy(io.Discard, c)
			c.Close()
		}
	}()
	return path
}

func TestLookupMatchesByTmuxPrefix(t *testing.T) {
	regDir := t.TempDir()
	sockDir := t.TempDir()
	sockPath := liveSocket(t, sockDir, "1")

	writeRegistry(t, regDir, socketmsg.Session{
		PID: 1, SessionID: "uuid-a", Tmux: "billing-orch:@12.%12",
		MessagingSocketPath: sockPath, PeerProtocol: 1,
	})

	s := NewClaudeSocketSender(regDir)
	got, ok := s.lookup(store.Session{Agent: "claude-code", TmuxName: "billing-orch"})
	if !ok {
		t.Fatal("expected a match for billing-orch")
	}
	if got.SessionID != "uuid-a" {
		t.Errorf("SessionID = %q, want uuid-a", got.SessionID)
	}
}

func TestLookupRejects(t *testing.T) {
	regDir := t.TempDir()
	sockDir := t.TempDir()
	live := liveSocket(t, sockDir, "live")

	writeRegistry(t, regDir, socketmsg.Session{
		PID: 2, SessionID: "b", Tmux: "other-name:@1.%1",
		MessagingSocketPath: live, PeerProtocol: 1,
	})
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 3, SessionID: "c", Tmux: "no-socket:@1.%1",
		MessagingSocketPath: "", PeerProtocol: 1,
	})
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 4, SessionID: "d", Tmux: "bad-proto:@1.%1",
		MessagingSocketPath: live, PeerProtocol: 2,
	})
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 5, SessionID: "e", Tmux: "dead-sock:@1.%1",
		MessagingSocketPath: filepath.Join(sockDir, "nobody.sock"), PeerProtocol: 1,
	})

	s := NewClaudeSocketSender(regDir)

	cases := []struct{ name, tmux string }{
		{"no registry entry", "missing"},
		{"empty socket path", "no-socket"},
		{"peer protocol mismatch", "bad-proto"},
		{"socket not listening", "dead-sock"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := s.lookup(store.Session{Agent: "claude-code", TmuxName: tc.tmux}); ok {
				t.Errorf("lookup(%q) matched, want no match", tc.tmux)
			}
		})
	}
}

func TestLookupRejectsNonClaudeAgentAndAmbiguity(t *testing.T) {
	regDir := t.TempDir()
	sockDir := t.TempDir()
	live := liveSocket(t, sockDir, "live")

	writeRegistry(t, regDir, socketmsg.Session{
		PID: 6, SessionID: "f", Tmux: "dup:@1.%1",
		MessagingSocketPath: live, PeerProtocol: 1,
	})
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 7, SessionID: "g", Tmux: "dup:@2.%2",
		MessagingSocketPath: live, PeerProtocol: 1,
	})
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 8, SessionID: "h", Tmux: "codex-one:@1.%1",
		MessagingSocketPath: live, PeerProtocol: 1,
	})

	s := NewClaudeSocketSender(regDir)

	if _, ok := s.lookup(store.Session{Agent: "claude-code", TmuxName: "dup"}); ok {
		t.Error("two live entries for one tmux name must be ambiguous, want no match")
	}
	if _, ok := s.lookup(store.Session{Agent: "codex", TmuxName: "codex-one"}); ok {
		t.Error("non-claude-code agent must not match")
	}
}
```

Add the imports the helpers need: `fmt`, `io`, `net`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/queue/ -run TestLookup -v`
Expected: FAIL — `NewClaudeSocketSender` undefined (compile error).

- [ ] **Step 3: Implement**

Create `internal/queue/socket.go`:

```go
package queue

import (
	"errors"
	"strings"

	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"github.com/IvanRoslov/rocket/internal/store"
)

// claudeCodeAgent is the store.Session.Agent value whose sessions expose a
// cross-session UDS inbox. Other agents have no such transport.
const claudeCodeAgent = "claude-code"

// supportedPeerProtocol is the only peer-protocol version internal/socketmsg
// was reverse-engineered against (see docs/design/cc-socket-protocol.md §9).
// A higher number means the CLI changed the protocol under us, so we treat
// the session as unreachable and let tmux injection carry the message.
const supportedPeerProtocol = 1

// SocketSender delivers a message body straight into a recipient's Claude
// Code inbox over its unix socket, bypassing the TUI composer entirely.
//
// It exists as an interface so the queue's delivery tests can drive both the
// success and the failure path without a real socket.
type SocketSender interface {
	// Available reports whether sess is reachable over a live socket right
	// now. It is a point-in-time answer: a socket can die between Available
	// and Send, which is why Send returns its own error.
	Available(sess store.Session) bool

	// Send delivers text to sess. fromName is the display name shown to the
	// recipient. A nil error means the bytes were accepted by the recipient's
	// socket — not that the recipient has read them (the protocol has no ack;
	// see docs/design/cc-socket-protocol.md §3).
	Send(sess store.Session, fromName, text string) error
}

// ClaudeSocketSender is the production SocketSender: it resolves a rocket
// session to a Claude Code registry entry and writes to that entry's socket.
type ClaudeSocketSender struct {
	// sessionsDir is the Claude Code session registry directory
	// (~/.claude/sessions by default).
	sessionsDir string
}

// NewClaudeSocketSender builds a sender reading the registry at sessionsDir.
// Pass socketmsg.SessionsDir() for the real one.
func NewClaudeSocketSender(sessionsDir string) *ClaudeSocketSender {
	return &ClaudeSocketSender{sessionsDir: sessionsDir}
}

// errNoSocket reports that sess has no reachable Claude Code socket. It is
// deliberately unexported and never surfaces to a sender: the queue turns it
// into a tmux injection, not into a delivery failure.
var errNoSocket = errors.New("queue: no live claude code socket for recipient")

// Available implements SocketSender.
func (s *ClaudeSocketSender) Available(sess store.Session) bool {
	_, ok := s.lookup(sess)
	return ok
}

// Send implements SocketSender.
func (s *ClaudeSocketSender) Send(sess store.Session, fromName, text string) error {
	cc, ok := s.lookup(sess)
	if !ok {
		return errNoSocket
	}
	_, err := socketmsg.Send(cc.MessagingSocketPath, text, socketmsg.Options{
		FromName: fromName,
		Priority: socketmsg.PriorityNext,
		// Pin the recipient's identity: if the pid was recycled between the
		// registry read and this write, the new owner drops the message
		// instead of showing a stranger's text (protocol doc §8).
		SessionID: cc.SessionID,
		// No From: rocket does not listen for receipts, and receipts only
		// exist for the held path, which rocket-spawned workers never take
		// (they run with crossSessionInbound=accept).
	})
	return err
}

// lookup resolves sess to a live Claude Code registry entry.
//
// The match key is the tmux session name: the registry's `tmux` field holds
// "<tmux-session>:@window.%pane", and rocket sets store.Session.TmuxName to
// the same tmux session name (internal/session/agent.go). That is a stronger
// key than the working directory, which several Claude processes can share.
//
// A match must additionally be addressable (non-empty socket path), speak the
// protocol version we implement, and actually answer a connect — stale entries
// outlive their process, so the file proves nothing on its own.
//
// Ambiguity is treated as no match: if two live entries claim one tmux name,
// we cannot tell which pane the recipient is, and delivering to the wrong
// session is worse than falling back to tmux.
func (s *ClaudeSocketSender) lookup(sess store.Session) (socketmsg.Session, bool) {
	if sess.Agent != claudeCodeAgent || sess.TmuxName == "" {
		return socketmsg.Session{}, false
	}
	all, err := socketmsg.ListSessions(s.sessionsDir)
	if err != nil {
		return socketmsg.Session{}, false
	}

	var match socketmsg.Session
	found := 0
	for _, cc := range all {
		name, _, _ := strings.Cut(cc.Tmux, ":")
		if name != sess.TmuxName {
			continue
		}
		if !cc.Addressable() || cc.PeerProtocol != supportedPeerProtocol {
			continue
		}
		if !socketmsg.Probe(cc.MessagingSocketPath, 0) {
			continue
		}
		match = cc
		found++
	}
	if found != 1 {
		return socketmsg.Session{}, false
	}
	return match, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/queue/ -run TestLookup -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/queue/socket.go internal/queue/socket_test.go
git commit -m "queue: add SocketSender seam and Claude Code registry lookup"
```

---

### Task 3: Transport selection in the delivery worker

**Files:**
- Modify: `internal/queue/queue.go` (struct near line 45, `deliver` line 319, `attemptDelivery` lines 385-400, `prepareText` line 560)
- Test: `internal/queue/queue_socket_test.go` (create)

**Interfaces:**
- Consumes: `SocketSender` from Task 2, `cfg.SocketDelivery` from Task 1.
- Produces:
  - `func (q *Queue) SetSocketSender(s SocketSender)` — daemon wiring hook (Task 4).
  - unexported `func (q *Queue) socketReady(sess store.Session) bool`
  - unexported `func socketFromName(msg store.Message) string`

`prepareText` is **not** changed: both transports carry the text it returns.

**Behavior being built (the brief's matrix, plus the draft-guard row #69 makes necessary):**

| case | expectation |
|---|---|
| socket send succeeds | delivered; `Inject` never called |
| socket send fails | falls back to `Inject` in the same attempt |
| `socket_delivery: false` | straight to `Inject`; sender never consulted |
| quiz pending + socket available | delivered over socket |
| quiz pending + no socket | stays `queued`, no attempts burned |
| composer holds a draft + socket available | delivered over socket, no deferral, draft untouched |
| composer holds a draft + no socket | unchanged #69 behavior: deferred, stays `queued` |

- [ ] **Step 1: Write the failing tests**

Create `internal/queue/queue_socket_test.go`:

```go
package queue

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/store"
)

// fakeSocket is a scriptable SocketSender.
type fakeSocket struct {
	mu        sync.Mutex
	available bool
	sendErr   error
	sends     []string // texts passed to Send, in order
}

func (f *fakeSocket) Available(sess store.Session) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.available
}

func (f *fakeSocket) Send(sess store.Session, fromName, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sends = append(f.sends, text)
	return nil
}

func (f *fakeSocket) sendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sends)
}

func TestSocketDeliverySucceedsWithoutInject(t *testing.T) {
	h := newQueueHarness(t)
	h.sess.Agent = "claude-code"
	h.putSession(h.sess)
	h.act.set(h.sess.ID, activity.Idle)

	sock := &fakeSocket{available: true}
	h.q.SetSocketSender(sock)

	id := h.enqueue("hello over socket")
	h.waitStatus(id, "delivered")

	if sock.sendCount() != 1 {
		t.Errorf("socket sends = %d, want 1", sock.sendCount())
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0 when the socket succeeded", n)
	}
}

func TestSocketFailureFallsBackToInject(t *testing.T) {
	h := newQueueHarness(t)
	h.sess.Agent = "claude-code"
	h.putSession(h.sess)
	h.act.set(h.sess.ID, activity.Idle)

	sock := &fakeSocket{available: true, sendErr: errors.New("dial: connection refused")}
	h.q.SetSocketSender(sock)

	id := h.enqueue("hello with fallback")
	h.waitStatus(id, "delivered")

	if n := h.rt.callCount(); n != 1 {
		t.Fatalf("Inject called %d times, want exactly 1 fallback injection", n)
	}
	if got := h.messageAttempts(id); got != 1 {
		t.Errorf("attempts = %d, want 1: a socket failure must not burn an extra attempt", got)
	}
}

func TestSocketDeliveryDisabledByConfig(t *testing.T) {
	h := newQueueHarness(t)
	h.cfg.SocketDelivery = false
	h.sess.Agent = "claude-code"
	h.putSession(h.sess)
	h.act.set(h.sess.ID, activity.Idle)

	sock := &fakeSocket{available: true}
	h.q.SetSocketSender(sock)

	id := h.enqueue("config off")
	h.waitStatus(id, "delivered")

	if sock.sendCount() != 0 {
		t.Errorf("socket sends = %d, want 0 when socket_delivery is off", sock.sendCount())
	}
	if n := h.rt.callCount(); n != 1 {
		t.Errorf("Inject called %d times, want 1", n)
	}
}

func TestQuizPendingDeliversOverSocket(t *testing.T) {
	h := newQueueHarness(t)
	h.sess.Agent = "claude-code"
	h.sess.PendingQuiz = "q-1"
	h.putSession(h.sess)
	h.act.set(h.sess.ID, activity.WaitingInput)

	sock := &fakeSocket{available: true}
	h.q.SetSocketSender(sock)

	id := h.enqueue("during a quiz")
	h.waitStatus(id, "delivered")

	if sock.sendCount() != 1 {
		t.Errorf("socket sends = %d, want 1", sock.sendCount())
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0: tmux must stay blocked during a quiz", n)
	}
}

func TestQuizPendingWithoutSocketStaysQueued(t *testing.T) {
	h := newQueueHarness(t)
	h.sess.Agent = "claude-code"
	h.sess.PendingQuiz = "q-1"
	h.putSession(h.sess)
	h.act.set(h.sess.ID, activity.WaitingInput)

	sock := &fakeSocket{available: false}
	h.q.SetSocketSender(sock)

	id := h.enqueue("during a quiz, no socket")

	// Give the worker several fallback ticks to prove it is not delivering.
	time.Sleep(300 * time.Millisecond)

	if got := h.messageStatus(id); got != "queued" {
		t.Errorf("status = %q, want queued while a quiz is pending and no socket exists", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0 during a pending quiz", n)
	}
	if sock.sendCount() != 0 {
		t.Errorf("socket sends = %d, want 0 when the socket is unavailable", sock.sendCount())
	}
}
```

Add the draft-guard pair. `#69`'s own tests show how to make `LooksLikeUserDraft` fire from a fake `Capture` — read `internal/queue/queue_test.go` for the exact pane fixture it uses and reuse that constant rather than inventing a new one:

```go
func TestComposerBusyDeliversOverSocket(t *testing.T) {
	h := newQueueHarness(t)
	h.sess.Agent = "claude-code"
	h.putSession(h.sess)
	h.act.set(h.sess.ID, activity.Idle)

	// Recipient is mid-draft: the tmux path would defer for
	// composer_busy_deadline. The socket must ignore that entirely.
	h.rt.captureFn = func(runtime.Handle, int) (string, error) {
		return draftPaneFixture, nil
	}

	sock := &fakeSocket{available: true}
	h.q.SetSocketSender(sock)

	id := h.enqueue("draft must survive")
	h.waitStatus(id, "delivered")

	if sock.sendCount() != 1 {
		t.Errorf("socket sends = %d, want 1", sock.sendCount())
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0: the draft must never be cleared", n)
	}
}

func TestComposerBusyWithoutSocketStillDefers(t *testing.T) {
	h := newQueueHarness(t)
	h.sess.Agent = "claude-code"
	h.putSession(h.sess)
	h.act.set(h.sess.ID, activity.Idle)

	h.rt.captureFn = func(runtime.Handle, int) (string, error) {
		return draftPaneFixture, nil
	}

	sock := &fakeSocket{available: false}
	h.q.SetSocketSender(sock)

	id := h.enqueue("no socket, draft on screen")
	time.Sleep(300 * time.Millisecond)

	if got := h.messageStatus(id); got != "queued" {
		t.Errorf("status = %q, want queued: #69's draft guard must be untouched without a socket", got)
	}
	if n := h.rt.callCount(); n != 0 {
		t.Errorf("Inject called %d times, want 0", n)
	}
}
```

This test file assumes a harness (`newQueueHarness`, `h.enqueue`, `h.waitStatus`, `h.messageStatus`, `h.messageAttempts`, `h.putSession`, fields `q`, `rt`, `act`, `cfg`, `sess`). Before writing the tests, read `internal/queue/queue_test.go` and reuse whatever setup helper already exists there; if the existing tests build their queue inline, extract that setup into `newQueueHarness` in `queue_test.go` first (a pure refactor — run `go test ./internal/queue/` before and after to confirm nothing changed) and only then write the file above against it. Do not duplicate a second harness.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/queue/ -run 'TestSocket|TestQuizPending' -v`
Expected: FAIL — `SetSocketSender` undefined (compile error).

- [ ] **Step 3: Implement**

3a. Add the field to `Queue` (after `getActivity`, near line 45):

```go
	// socket, when non-nil, is consulted before tmux injection for every
	// delivery: it hands the message to the recipient's Claude Code inbox
	// without touching their composer. nil means "tmux only".
	socket SocketSender
```

3b. Add the setter after `New`:

```go
// SetSocketSender installs the socket transport consulted before tmux
// injection. Passing nil disables it. It is called once during daemon wiring,
// before Recover/Run, and must not be called concurrently with delivery.
func (q *Queue) SetSocketSender(s SocketSender) { q.socket = s }

// socketReady reports whether this delivery may go over the socket at all:
// the transport must be wired, enabled by config, and the recipient must
// currently be reachable.
func (q *Queue) socketReady(sess store.Session) bool {
	return q.socket != nil && q.cfg.SocketDelivery && q.socket.Available(sess)
}
```

3c. Resolve the transport once per pass. Immediately after the `sess.State` switch in `deliver` (before the pending-quiz gate), insert:

```go
		// Resolve the transport once per pass: it decides both gates below
		// and the delivery itself, and each resolution costs a connect() to
		// the recipient's socket.
		viaSocket := q.socketReady(sess)
```

3d. Replace the pending-quiz gate in `deliver` with:

```go
		if sess.PendingQuiz != "" && !viaSocket {
			// Recipient has a pending AskUserQuestion quiz on screen: text
			// injected now would corrupt the TUI widget, and no socket is
			// available to route around it. Hold the message (stays queued,
			// no failure, no events, no retries burned) and wait/re-check:
			// the quiz-resolve path publishes a session.quiz_resolved bus
			// event that wakes this straight back up (with the 2s fallback
			// ticker as a backstop).
			//
			// Socket delivery is exempt because it never touches the TUI —
			// the message lands in the recipient's inbox queue and is read
			// after the quiz closes.
			if !q.waitForReady(ctx, msg.ToSession) {
				return // ctx cancelled
			}
			continue
		}
```

3e. Exempt the socket from the draft guard in `deliver`. Wrap #69's draft-guard block (the `force := false` … `busySince = time.Time{}` stanza) so it only runs on the tmux path:

```go
		// Draft guard — tmux path only. Socket delivery never sends C-u and
		// never touches the composer, so there is no draft to protect and
		// nothing to defer for: that is the whole reason this transport
		// exists. Leaving busySince zero here is deliberate — a socket
		// delivery must not accumulate "busyness" toward the force deadline.
		force := false
		if !viaSocket {
			if out, capErr := q.rt.Capture(ctx, runtime.Handle{Name: sess.TmuxName}, composerWindow); capErr == nil && runtime.LooksLikeUserDraft(out) {
				if busySince.IsZero() {
					busySince = time.Now()
				}
				if time.Since(busySince) < q.composerBusyDeadline() {
					slog.Info("queue: recipient composer holds a user draft, deferring delivery",
						"id", msg.ID, "to", msg.ToSession, "busy_for", time.Since(busySince).Round(time.Second))
					if !q.waitForReady(ctx, msg.ToSession) {
						return // ctx cancelled
					}
					continue
				}
				slog.Warn("queue: composer busy past the deadline, delivering anyway (draft will be cleared)",
					"id", msg.ID, "to", msg.ToSession, "deadline", q.composerBusyDeadline())
				force = true
			} else {
				busySince = time.Time{}
			}
		}
```

Then pass the transport into the call below it:

```go
		if !q.attemptDelivery(ctx, msg, sess, force, viaSocket) {
```

3f. In `attemptDelivery`, widen the signature and add the socket attempt. The head becomes:

```go
func (q *Queue) attemptDelivery(ctx context.Context, msg store.Message, sess store.Session, force, viaSocket bool) bool {
	handle := runtime.Handle{Name: sess.TmuxName}
	text := q.prepareText(msg, sess)
```

Text preparation is unchanged — both transports deliver exactly that text. Inside the retry loop, insert the socket attempt directly above the `Inject` call:

```go
		// Socket first: it delivers without touching the composer, so a
		// half-typed human draft survives. A socket failure is not the
		// message's failure — fall through to tmux in this same attempt.
		if viaSocket {
			sendErr := q.socket.Send(sess, socketFromName(msg), text)
			if sendErr == nil {
				q.deliverSuccess(msg)
				return true
			}
			slog.Info("queue: socket delivery failed, falling back to tmux injection",
				"id", msg.ID, "to", msg.ToSession, "error", sendErr)
			// Don't retry the socket on later attempts of this delivery: it
			// just failed, and Inject is the fallback we want from here on.
			viaSocket = false

			if sess.PendingQuiz != "" {
				// The socket died between deliver's gate and here, and a quiz
				// is on screen: injecting would corrupt the widget. Hand the
				// message back exactly like the composer-busy deferral does —
				// nothing was sent, so no attempt is consumed.
				msg.Attempts--
				if err := q.st.UpdateMessageStatus(msg.ID, "queued", msg.Attempts, 0, ""); err != nil {
					slog.Error("queue: requeue message after socket loss during quiz", "id", msg.ID, "error", err)
				}
				slog.Info("queue: socket lost during a pending quiz, message requeued",
					"id", msg.ID, "to", msg.ToSession)
				return false
			}
		}

		err := q.rt.Inject(ctx, handle, text, runtime.InjectOpts{Force: force})
```

Note what this buys for free: if the socket fails while a *draft* (not a quiz) is on screen, the code falls through to `Inject`, whose own guard returns `ErrComposerBusy`, and #69's existing branch requeues the message untouched. The next pass re-evaluates with `viaSocket` false and the draft guard applies normally.

3g. Add `socketFromName` next to `formatBody`:

```go
// socketFromName is the display name the recipient sees for this message:
// the rocket sender id, or "rocket" for human/system messages (which carry
// no sender). socketmsg sanitizes it further.
func socketFromName(msg store.Message) string {
	if msg.FromSession != "" {
		return msg.FromSession
	}
	return "rocket"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/queue/ -v`
Expected: PASS — the five new tests plus every pre-existing queue test (the tmux-only tests must be untouched, since `q.socket` is nil in them).

- [ ] **Step 5: Commit**

```bash
git add internal/queue/queue.go internal/queue/queue_socket_test.go internal/queue/queue_test.go
git commit -m "queue: deliver via Claude Code socket with instant tmux fallback"
```

---

### Task 4: Daemon wiring

**Files:**
- Modify: the daemon file that constructs the queue (find with `grep -rn "queue.New(" internal/ cmd/`)

**Interfaces:**
- Consumes: `queue.NewClaudeSocketSender`, `socketmsg.SessionsDir`, `Queue.SetSocketSender`.
- Produces: nothing new.

- [ ] **Step 1: Locate the construction site**

Run: `grep -rn "queue.New(" internal/ cmd/`
Expected: one production call site (plus test call sites, which stay as they are).

- [ ] **Step 2: Wire the sender**

Immediately after the `queue.New(...)` call, before `Recover`/`Run`:

```go
	// Claude Code recipients get their messages over the cross-session UDS
	// inbox when one is reachable; everything else keeps using tmux, and so
	// does this path whenever the socket is missing or refuses the write.
	q.SetSocketSender(queue.NewClaudeSocketSender(socketmsg.SessionsDir()))
```

Add the `internal/socketmsg` import.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Full test suite**

Run: `go test ./...`
Expected: all packages PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "daemon: wire Claude Code socket transport into the queue"
```

---

### Task 5: Documentation

**Files:**
- Modify: `docs/06-messaging.md`

- [ ] **Step 1: Update the flow block**

Replace the `доставщик` block (lines 16-22) with:

```
доставщик (в демоне, пер-получатель):
  ждёт activity получателя ∈ {ready, idle, waiting_input}
  → выбор транспорта (один раз за проход)
  → транспорт 1: сокет Claude Code (если доступен)
       # composer не трогаем: ни draft guard, ни pending-квиз не блокируют
       socketmsg.Send(<сокет получателя>, "[from <from>] <body>")
       → успешная запись = delivered
  → транспорт 2 (фолбэк, при любой ошибке выше):
       проверка черновика в composer'е: занят — остаёмся в queued (draft guard)
       runtime.Inject(получатель, "[from <from>] <body>")  # C-u, paste-buffer, адаптивный Enter
       → подтверждение: черновик покинул composer (capture-pane)
  → status=delivered, событие message.delivered
```

- [ ] **Step 2: Add the transport section**

Insert a new section before «Почему всё же инжекция в терминал»:

```markdown
## Транспорты доставки

Сообщение доставляется одним из двух способов. Выбор делается заново на каждой попытке.

**Сокет Claude Code (предпочтительный).** Живая сессия Claude Code слушает unix-сокет для cross-session сообщений (протокол разобран в [design/cc-socket-protocol.md](design/cc-socket-protocol.md)). Доставка по нему **не трогает composer получателя** — это и есть главная причина существования транспорта: инжекция в tmux стирает недописанный человеком черновик, а сокет кладёт сообщение в очередь промптов, не касаясь ввода.

Сокет используется, только если выполнено всё сразу:

- у получателя `agent == "claude-code"`;
- включён конфиг `socket_delivery` (по умолчанию `true`);
- в реестре `~/.claude/sessions/<pid>.json` нашлась **ровно одна** живая запись, у которой префикс поля `tmux` до `:` равен `tmux_name` сессии rocket;
- у записи непустой `messagingSocketPath` и `peerProtocol == 1`;
- `connect` по этому сокету удался (мёртвый сокет-файл остаётся на диске после падения процесса, поэтому наличие файла ничего не доказывает).

Ключ сопоставления — имя tmux-сессии, а не рабочий каталог: несколько процессов Claude могут делить один worktree, а имя tmux-сессии у rocket уникально (`store.Session.TmuxName`). Если под одно имя попали две живые записи, это считается неоднозначностью и сокет не используется — доставить не в ту сессию хуже, чем откатиться на tmux.

В сообщение кладётся `session_id` из найденной записи: если pid успели переиспользовать между чтением реестра и записью, новый владелец сокета отбросит сообщение, а не покажет чужой текст. `from-mode` не выставляется намеренно.

**tmux-инжект (фолбэк).** Любой промах на любом шаге выше — нет записи в реестре, не тот `peerProtocol`, сокет не отвечает, ошибка `connect`/`write` — приводит к немедленной инжекции в tmux **в рамках той же попытки**. Сбой сокета никогда не проваливает сообщение и не тратит попытку; он пишется в лог на уровне info.

**Семантика delivered.** В протоколе нет подтверждения в том же соединении: успешная запись означает «байты приняты», а не «получатель прочитал». Квитанции существуют только для придержанных сообщений, а сессии, поднятые rocket, работают с `crossSessionInbound: "accept"` (придержания не будет). Поэтому **успешная запись в сокет считается доставкой** — ровно так же, как сегодня подтверждение инжекции по capture-pane считается доставкой. Обратного канала нет.

**Большие сообщения — одинаково на обоих транспортах.** Правило про `.rocket/inbox/msg-<id>.md` (см. выше) действует независимо от транспорта: по сокету уходит ровно тот же текст, что ушёл бы в tmux, — полное тело или короткий указатель на файл. Так тело сообщения у получателя не зависит от того, каким путём оно приехало.

**Конфиг.** `socket_delivery: false` в `config.yaml` выключает транспорт целиком — вся доставка идёт через tmux. Выключение не ломает доставку, а лишь возвращает старое поведение со стиранием черновика.
```

- [ ] **Step 3: Update the draft-guard rule (line 30)**

The draft guard is now a tmux-path rule. Append to that bullet:

```markdown
Всё это относится к tmux-пути. Если получатель достижим по сокету Claude Code (см. «Транспорты доставки»), гвард не применяется вовсе: сокет не шлёт `C-u` и не трогает строку ввода, поэтому черновик человека переживает доставку без всякого откладывания — сообщение приходит сразу, черновик остаётся на месте. `composer_busy_deadline` при этом не тикает: «занятость» копится только на tmux-пути, где ей есть что стирать. Именно ради этого сценария транспорт и делался.
```

- [ ] **Step 4: Update the pending-quiz rule**

Replace the «Pending-квиз блокирует доставку» bullet (line 28) with:

```markdown
- **Pending-квиз блокирует tmux-доставку, но не сокет.** Пока у получателя открыт TUI-квиз AskUserQuestion (`pending_quiz` сессии непустой — см. [13-chat.md](13-chat.md), раздел «Квизы»), инжекция текста сломала бы виджет, поэтому tmux-путь закрыт. Если доступен сокет — сообщение доставляется по нему прямо во время квиза: он не трогает TUI. Если сокета нет, сообщение остаётся `queued`: попытки не тратятся, `failed` не ставится. После закрытия квиза (`session.quiz_resolved`) доставка возобновляется сразу — событие будит воркер очереди, 2-секундный фолбэк-тикер страхует.
```

- [ ] **Step 5: Update the closing section**

In «Почему всё же инжекция в терминал, а не „настоящий“ канал», append:

```markdown
Обновление: у Claude Code такой канал появился — cross-session UDS-инбокс, и rocket уже предпочитает его инжекции (см. «Транспорты доставки»). Модель данных очереди при этом не изменилась, что и предсказывал абзац выше: транспорт встал за тот же интерфейс. Инжекция остаётся универсальным фолбэком для всех остальных агентов и для сессий Claude Code без живого сокета.
```

- [ ] **Step 6: Commit**

```bash
git add docs/06-messaging.md
git commit -m "docs: document the Claude Code socket transport in 06-messaging"
```

---

### Task 6: Manual end-to-end run and PR

**Files:** none (verification + PR).

- [ ] **Step 1: Full local gate**

Run: `go build ./... && go test ./...`
Expected: everything PASS. This is the acceptance gate — the repo has no CI.

- [ ] **Step 2: Manual E2E**

Build the daemon from this branch and run it. Then, against a live rocket-spawned Claude Code session:

1. Type some text into that session's composer and **leave it unsubmitted**.
2. From another session: `rocket send <that-session-id> "socket transport smoke test"`.
3. Verify the message arrives in the recipient.
4. Verify the composer still holds the un-submitted draft.
5. Confirm the daemon log shows no fallback line for this delivery.

Then verify the fallback: `socket_delivery: false` in `config.yaml`, restart the daemon, repeat — the message must still arrive, and this time the draft is expected to be erased (that is the old behavior).

Record both runs verbatim for the PR body.

- [ ] **Step 3: Verification skill**

REQUIRED SUB-SKILL: `superpowers:verification-before-completion` before claiming done.

- [ ] **Step 4: Open the PR**

```bash
git push -u origin feature/claude-code/socket-transport
gh pr create --title "queue: доставка в Claude Code через cross-session UDS с фолбэком на tmux" --body-file <path>
```

The body must reference feature `claude-code` and subtask #1337, describe the transport-selection rules, state the delivered semantics, and include the two manual E2E runs from Step 2.

- [ ] **Step 5: Report**

```bash
rocket send claude-code-orch "PR открыт: <url>. Локально go build ./... && go test ./... зелёные, E2E прогон описан в теле PR."
```

---

## Self-Review

**Spec coverage:**

| Brief requirement | Task |
|---|---|
| Transport selection: agent claude-code + config + live probe | 2, 3 |
| Registry mapping, documented choice, stale entries | 2 (lookup + doc comment), 5 (docs) |
| Any error → immediate tmux fallback, same attempt, never fail | 3 (3d), 5 |
| Log at debug/info | 3 (3d uses slog.Info) |
| Success semantics: write == delivered, noted in docs | 3 (3d), 5 |
| Quiz: socket allowed, tmux blocked, else stays queued | 3 (3c, 3d), tests 4 and 5 in Task 3 |
| Config knob `socket_delivery` default ON, next to queue config | 1 |
| docs/06-messaging.md updated | 5 |
| Five unit tests with fake socket + fake runtime | 3 |
| Small seams | 2 (two-method interface) |
| Manual E2E described in PR body | 6 |
| `go build ./... && go test ./...` green | 6 |
| One PR | 6 |

**Resolved point:** the brief (line 12) and a since-retracted orchestrator answer disagreed on whether large bodies go over the socket whole or as an inbox-file pointer. Settled as **pointer on both transports**, which is also what the brief's byte-identity requirement (line 11) demands. Consequence: `prepareText` is untouched and `attemptDelivery` still computes its text once.

**Placeholder scan:** no TBDs; every code step carries real code. Task 3 Step 1 deliberately defers the harness shape to the existing `queue_test.go` rather than inventing a duplicate — that is an instruction to read and reuse, not a placeholder.

**Type consistency:** `SocketSender.Available(store.Session) bool` and `Send(store.Session, string, string) error` are used identically in Task 2 (impl), Task 3 (`socketReady`, `attemptDelivery`, `fakeSocket`). `NewClaudeSocketSender(string)` matches Task 4's call. `socketFromName`/`socketReady` are defined once and used consistently.
