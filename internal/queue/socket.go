package queue

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"github.com/IvanRoslov/rocket/internal/store"
)

// claudeCodeAgent is the store.Session.Agent value whose sessions expose a
// cross-session UDS inbox. No other agent has one.
const claudeCodeAgent = "claude-code"

// supportedPeerProtocol is the only peer-protocol version internal/socketmsg
// was reverse-engineered against (docs/design/cc-socket-protocol.md §9). A
// higher number means the CLI changed the protocol under us, so we treat the
// session as unreachable and let tmux injection carry the message rather than
// guessing at a format we have not verified.
const supportedPeerProtocol = 1

// SocketSender delivers a message body straight into a recipient's Claude Code
// inbox over its unix socket, bypassing the TUI composer entirely.
//
// It is an interface so the queue's delivery tests can drive both the success
// and the failure path without a real socket.
type SocketSender interface {
	// Available reports whether sess is reachable over a live socket right
	// now. It is a point-in-time answer: a socket can die between Available
	// and Send, which is why Send returns its own error.
	Available(sess store.Session) bool

	// Send delivers text to sess. fromName is the display name the recipient
	// sees. A nil error means the bytes were accepted by the recipient's
	// socket — not that the recipient has read them: the protocol has no ack
	// (docs/design/cc-socket-protocol.md §3).
	Send(sess store.Session, fromName, text string) error
}

// ClaudeSocketSender is the production SocketSender: it resolves a rocket
// session to a Claude Code registry entry and writes to that entry's socket.
type ClaudeSocketSender struct {
	// sessionsDir is the Claude Code session registry directory
	// (~/.claude/sessions by default).
	sessionsDir string

	// receipts is rocket's own listening socket, advertised as the reply
	// address so the recipient can tell us a message was held. nil disables
	// the wait entirely: we write and hope, which is the pre-receipt behavior.
	receipts *socketmsg.Receipts

	// heldWait overrides defaultHeldWait; tests shorten or lengthen it.
	heldWait time.Duration

	// heldSessions remembers Claude sessionIds that held a message. Its
	// lifetime is deliberately the daemon's: a sessionId is a fresh UUID per
	// claude process, so the memory expires by construction when the worker
	// restarts, and a daemon restart costs at most one more wait per session.
	heldMu       sync.Mutex
	heldSessions map[string]bool
}

// NewClaudeSocketSender builds a sender reading the registry at sessionsDir.
// Pass socketmsg.SessionsDir() for the real one.
func NewClaudeSocketSender(sessionsDir string) *ClaudeSocketSender {
	return &ClaudeSocketSender{
		sessionsDir:  sessionsDir,
		heldWait:     defaultHeldWait,
		heldSessions: make(map[string]bool),
	}
}

// SetReceipts installs the reply channel. It is called once during daemon
// wiring, before delivery starts.
func (s *ClaudeSocketSender) SetReceipts(r *socketmsg.Receipts) { s.receipts = r }

// ErrNoSocket reports that a session has no reachable Claude Code socket. The
// queue turns it into a tmux injection, never into a delivery failure.
var ErrNoSocket = errors.New("queue: no live claude code socket for recipient")

// ErrHeld reports that the recipient accepted the bytes but did NOT queue the
// message: it sits in their hold buffer awaiting the user's approval, and if
// nobody approves it, it expires silently after a few minutes (protocol doc
// §6). That is a non-delivery, so the queue must treat it exactly like a
// socket failure and fall back to tmux injection.
var ErrHeld = errors.New("queue: recipient held the message for user approval")

// defaultHeldWait is how long Send waits for a peer_message_status receipt
// after the write. The recipient emits `held` immediately from its inbound
// handler, so this only has to cover a local UDS round trip plus a busy Node
// event loop. Silence means accept — an accepted message never produces a
// receipt at all — so this window is pure added latency on the happy path and
// must stay small. It is paid at most once per recipient process, thanks to
// heldSessions.
const defaultHeldWait = 2 * time.Second

// Available implements SocketSender.
func (s *ClaudeSocketSender) Available(sess store.Session) bool {
	_, ok := s.lookup(sess)
	return ok
}

// Send implements SocketSender.
func (s *ClaudeSocketSender) Send(sess store.Session, fromName, text string) error {
	cc, ok := s.lookup(sess)
	if !ok {
		return ErrNoSocket
	}
	// The reply address, the msg id and the waiter must all exist BEFORE the
	// write: the `held` receipt is emitted from the recipient's inbound
	// handler and can land before Send returns.
	//
	// The address has to sit in the recipient's own socket directory and end
	// in .sock or the receipt is silently never sent (protocol doc §7) —
	// hence deriving the directory from the recipient's path.
	var (
		from   string
		msgID  string
		wait   <-chan socketmsg.Message
		cancel = func() {}
	)
	if s.receipts != nil {
		addr, addrErr := s.receipts.Addr(filepath.Dir(cc.MessagingSocketPath))
		id, idErr := socketmsg.NewMsgID()
		switch {
		case addrErr != nil:
			// No reply channel: deliver blind rather than not at all.
			slog.Warn("queue: no receipt listener, socket delivery cannot detect a hold",
				"error", addrErr)
		case idErr != nil:
			slog.Warn("queue: cannot mint a msg id for receipt correlation", "error", idErr)
		default:
			from, msgID = addr, id
			wait, cancel = s.receipts.Watch(id)
		}
	}
	defer cancel()

	if _, err := socketmsg.Send(cc.MessagingSocketPath, text, socketmsg.Options{
		From:     from,
		MsgID:    msgID,
		FromName: fromName,
		Priority: socketmsg.PriorityNext,
		// Pin the recipient's identity: if the pid was recycled between the
		// registry read and this write, the new owner of the socket drops the
		// message instead of showing a stranger's text (protocol doc §8).
		SessionID: cc.SessionID,
		// from-mode is deliberately never asserted — claiming "bypass" would
		// let our messages through a bypass-mode recipient's guard with
		// privileges we have no business claiming (protocol doc §5.2). We
		// take the hold and route around it instead.
	}); err != nil {
		return err
	}

	if wait == nil {
		return nil
	}
	err := s.awaitReceipt(wait)
	if errors.Is(err, ErrHeld) {
		s.rememberHeld(cc.SessionID)
	}
	return err
}

// awaitReceipt turns the recipient's verdict into an error, or nil.
//
// Silence is success: `accept` never produces a receipt (protocol doc §7), so
// the only thing this window can realistically catch is a hold, which is
// emitted at once. `denied`/`expired` only ever follow a `held` we already
// reacted to, but they are handled here too in case one races into the window.
func (s *ClaudeSocketSender) awaitReceipt(wait <-chan socketmsg.Message) error {
	window := s.heldWait
	if window <= 0 {
		window = defaultHeldWait
	}
	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case m := <-wait:
		switch m.Status {
		case "held", "denied", "expired":
			return fmt.Errorf("%w (status %s)", ErrHeld, m.Status)
		default:
			// "delivered" (the user approved it before our window closed) and
			// anything unknown: the message is in the recipient's queue.
			return nil
		}
	case <-timer.C:
		return nil
	}
}

// rememberHeld records that sessionID holds peer messages, so later deliveries
// skip the socket instead of paying the wait again.
func (s *ClaudeSocketSender) rememberHeld(sessionID string) {
	if sessionID == "" {
		return
	}
	s.heldMu.Lock()
	defer s.heldMu.Unlock()
	if s.heldSessions == nil {
		s.heldSessions = make(map[string]bool)
	}
	if !s.heldSessions[sessionID] {
		slog.Warn("queue: recipient holds peer messages, preferring tmux for it from now on",
			"claude_session_id", sessionID)
	}
	s.heldSessions[sessionID] = true
}

func (s *ClaudeSocketSender) heldRecently(sessionID string) bool {
	s.heldMu.Lock()
	defer s.heldMu.Unlock()
	return s.heldSessions[sessionID]
}

// lookup resolves sess to a live Claude Code registry entry.
//
// The match key is the tmux session name: the registry's `tmux` field holds
// "<tmux-session>:@window.%pane", and rocket sets store.Session.TmuxName to
// that same tmux session name (internal/session/agent.go). That beats matching
// on the working directory, which several Claude processes can share — a
// worktree with a second claude open in it would be ambiguous, while the tmux
// session name is unique per rocket session by construction.
//
// A match must additionally be addressable (non-empty socket path), speak the
// protocol version we implement, and actually answer a connect: stale entries
// outlive their process, so the file on disk proves nothing on its own.
//
// Ambiguity is treated as no match. If two live entries claim one tmux name we
// cannot tell which pane the recipient is, and delivering to the wrong session
// is worse than falling back to tmux.
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
	if s.heldRecently(match.SessionID) {
		// This process already held one of our messages. Its inbound policy
		// cannot change while it lives, so the socket is a dead end for it:
		// report no match and let tmux carry everything from here.
		return socketmsg.Session{}, false
	}
	return match, true
}
