package queue

import (
	"errors"
	"strings"

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
}

// NewClaudeSocketSender builds a sender reading the registry at sessionsDir.
// Pass socketmsg.SessionsDir() for the real one.
func NewClaudeSocketSender(sessionsDir string) *ClaudeSocketSender {
	return &ClaudeSocketSender{sessionsDir: sessionsDir}
}

// ErrNoSocket reports that a session has no reachable Claude Code socket. The
// queue turns it into a tmux injection, never into a delivery failure.
var ErrNoSocket = errors.New("queue: no live claude code socket for recipient")

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
	_, err := socketmsg.Send(cc.MessagingSocketPath, text, socketmsg.Options{
		FromName: fromName,
		Priority: socketmsg.PriorityNext,
		// Pin the recipient's identity: if the pid was recycled between the
		// registry read and this write, the new owner of the socket drops the
		// message instead of showing a stranger's text (protocol doc §8).
		SessionID: cc.SessionID,
		// From is deliberately empty: rocket does not listen for receipts,
		// and receipts only exist for the held path, which rocket-spawned
		// workers never take (they run with crossSessionInbound=accept).
		//
		// from-mode is likewise never asserted — claiming "bypass" would let
		// our messages through a bypass-mode recipient's guard with
		// privileges we have no business claiming (protocol doc §5.2).
	})
	return err
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
	return match, true
}
