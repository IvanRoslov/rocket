package queue

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"github.com/IvanRoslov/rocket/internal/store"
)

// writeRegistry writes one <pid>.json entry into dir, imitating what a live
// Claude Code process writes into ~/.claude/sessions.
func writeRegistry(t *testing.T, dir string, s socketmsg.Session) {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, fmt.Sprintf("%d.json", s.PID))
	if err := os.WriteFile(name, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// shortTempDir returns a temp dir with a deliberately short path.
//
// t.TempDir() embeds the test name, which on macOS lands under /var/folders/…
// and blows past the 103-byte sun_path limit for unix sockets — bind then
// fails with EINVAL. Socket dirs must come from here, not from t.TempDir().
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// liveSocket starts a listener at <dir>/<name>.sock and returns its path. The
// accept loop drains and drops connections: lookup only needs the connect to
// succeed, and Send only needs the write to be accepted.
func liveSocket(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name+".sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = io.Copy(io.Discard, c)
				_ = c.Close()
			}()
		}
	}()
	return path
}

func TestLookupMatchesByTmuxPrefix(t *testing.T) {
	regDir := t.TempDir()
	sockDir := shortTempDir(t)
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
	if got.MessagingSocketPath != sockPath {
		t.Errorf("MessagingSocketPath = %q, want %q", got.MessagingSocketPath, sockPath)
	}
}

func TestLookupRejects(t *testing.T) {
	regDir := t.TempDir()
	sockDir := shortTempDir(t)
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
		{"no registry entry at all", "missing"},
		{"entry has no socket path", "no-socket"},
		{"peer protocol we do not implement", "bad-proto"},
		{"socket file exists but nobody listens", "dead-sock"},
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
	sockDir := shortTempDir(t)
	live := liveSocket(t, sockDir, "live")

	// Two live entries claiming one tmux session name. We cannot tell which
	// pane the recipient is, and delivering to the wrong session is worse
	// than falling back to tmux.
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
		t.Error("non-claude-code agent must not match: only Claude Code exposes this socket")
	}
	if _, ok := s.lookup(store.Session{Agent: "claude-code", TmuxName: ""}); ok {
		t.Error("a session with no tmux name has nothing to match on, want no match")
	}
}

// TestLookupIgnoresMissingRegistryDir pins that a machine with no Claude Code
// registry at all degrades to "no socket" rather than erroring out.
func TestLookupIgnoresMissingRegistryDir(t *testing.T) {
	s := NewClaudeSocketSender(filepath.Join(t.TempDir(), "does-not-exist"))
	if _, ok := s.lookup(store.Session{Agent: "claude-code", TmuxName: "whatever"}); ok {
		t.Error("expected no match when the registry directory is absent")
	}
}

// TestAvailableAndSend exercises the two exported methods against a real
// listening socket: Available must agree with lookup, and Send must hand the
// bytes over without error.
func TestAvailableAndSend(t *testing.T) {
	regDir := t.TempDir()
	sockDir := shortTempDir(t)
	sockPath := liveSocket(t, sockDir, "9")

	writeRegistry(t, regDir, socketmsg.Session{
		PID: 9, SessionID: "uuid-live", Tmux: "worker-a:@3.%3",
		MessagingSocketPath: sockPath, PeerProtocol: 1,
	})

	s := NewClaudeSocketSender(regDir)
	sess := store.Session{Agent: "claude-code", TmuxName: "worker-a"}

	if !s.Available(sess) {
		t.Fatal("Available = false, want true for a live socket")
	}
	if err := s.Send(sess, "claude-code-orch", "[from claude-code-orch] hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	absent := store.Session{Agent: "claude-code", TmuxName: "nope"}
	if s.Available(absent) {
		t.Error("Available = true for a session with no registry entry")
	}
	if err := s.Send(absent, "x", "y"); err == nil {
		t.Error("Send to an unreachable session returned nil error")
	}
}

// replySocket is a recipient that answers every user message with a
// peer_message_status receipt of the given status, addressed to the reply
// socket named in the message's `from`. That is what a bypassPermissions
// session without crossSessionInbound=accept does (protocol doc §6).
func replySocket(t *testing.T, dir, name, status string) string {
	t.Helper()
	path := filepath.Join(dir, name+".sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				sc := bufio.NewScanner(c)
				sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
				for sc.Scan() {
					var m socketmsg.Message
					if json.Unmarshal(sc.Bytes(), &m) != nil || m.From == "" {
						continue
					}
					reply, err := net.Dial("unix", decodeAddrForTest(m.From))
					if err != nil {
						return
					}
					raw, _ := json.Marshal(socketmsg.Message{
						MsgV: socketmsg.ProtocolVersion, Type: "control",
						Action: "peer_message_status", Status: status, OrigMsgID: m.MsgID,
					})
					_, _ = reply.Write(append(raw, '\n'))
					_ = reply.Close()
				}
			}()
		}
	}()
	return path
}

// decodeAddrForTest inverts socketmsg.EncodeAddr: strip the uds: scheme and
// undo the %XX escaping.
func decodeAddrForTest(addr string) string {
	s := strings.TrimPrefix(addr, socketmsg.AddrScheme)
	out, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return out
}

// heldSender wires a sender, a held-happy recipient and a registry entry for
// it, and returns the sender plus the rocket session addressing it.
func heldSender(t *testing.T, pid int, status string) (*ClaudeSocketSender, store.Session) {
	t.Helper()
	sockDir := shortTempDir(t)
	regDir := t.TempDir()
	path := replySocket(t, sockDir, fmt.Sprint(pid), status)
	writeRegistry(t, regDir, socketmsg.Session{
		PID: pid, SessionID: fmt.Sprintf("sid-%d", pid), PeerProtocol: 1,
		MessagingSocketPath: path, Tmux: "rocket-recv:@1.%1",
	})

	r := socketmsg.NewReceipts("rocket-test")
	t.Cleanup(func() { _ = r.Close() })
	s := NewClaudeSocketSender(regDir)
	s.SetReceipts(r)
	s.heldWait = 3 * time.Second
	return s, store.Session{Agent: "claude-code", TmuxName: "rocket-recv"}
}

// TestSendReportsHeld is the whole point of the receipt channel: a held
// message is NOT delivered — it sits in the recipient's approval buffer and
// expires silently — so Send must say so instead of reporting success.
func TestSendReportsHeld(t *testing.T) {
	s, sess := heldSender(t, 999, "held")
	if err := s.Send(sess, "orch", "текст"); !errors.Is(err, ErrHeld) {
		t.Fatalf("Send err = %v, want ErrHeld", err)
	}
}

// TestSendSucceedsOnSilence pins the other half of the protocol: an accepted
// message never produces a receipt at all (protocol doc §7), so silence within
// the window is success, not a timeout failure.
func TestSendSucceedsOnSilence(t *testing.T) {
	regDir := t.TempDir()
	sockDir := shortTempDir(t)
	path := liveSocket(t, sockDir, "998")
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 998, SessionID: "sid-998", PeerProtocol: 1,
		MessagingSocketPath: path, Tmux: "rocket-recv:@1.%1",
	})

	r := socketmsg.NewReceipts("rocket-test")
	defer r.Close()
	s := NewClaudeSocketSender(regDir)
	s.SetReceipts(r)
	s.heldWait = 200 * time.Millisecond

	sess := store.Session{Agent: "claude-code", TmuxName: "rocket-recv"}
	if err := s.Send(sess, "orch", "текст"); err != nil {
		t.Fatalf("Send = %v, want nil when no receipt arrives", err)
	}
}

// TestSendWithoutReceiptsDoesNotWait: with no reply channel wired the old
// write-and-hope behavior stands, and nothing blocks on a receipt that could
// never be routed anywhere.
func TestSendWithoutReceiptsDoesNotWait(t *testing.T) {
	regDir := t.TempDir()
	sockDir := shortTempDir(t)
	path := replySocket(t, sockDir, "997", "held")
	writeRegistry(t, regDir, socketmsg.Session{
		PID: 997, SessionID: "sid-997", PeerProtocol: 1,
		MessagingSocketPath: path, Tmux: "rocket-recv:@1.%1",
	})

	s := NewClaudeSocketSender(regDir)
	s.heldWait = time.Hour // must go unused

	sess := store.Session{Agent: "claude-code", TmuxName: "rocket-recv"}
	start := time.Now()
	if err := s.Send(sess, "orch", "текст"); err != nil {
		t.Fatalf("Send = %v, want nil", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("Send waited %v with no receipt channel wired", d)
	}
}

// TestHeldSessionPrefersTmuxAfterwards: a session that held one message will
// hold the next one too — its inbound policy cannot change while the process
// lives — so the second message must not pay the wait again.
func TestHeldSessionPrefersTmuxAfterwards(t *testing.T) {
	s, sess := heldSender(t, 996, "held")
	if !s.Available(sess) {
		t.Fatal("Available = false before the first hold, want true")
	}
	if err := s.Send(sess, "orch", "первое"); !errors.Is(err, ErrHeld) {
		t.Fatalf("Send err = %v, want ErrHeld", err)
	}
	if s.Available(sess) {
		t.Error("Available = true after a hold, want false so the queue goes straight to tmux")
	}
	if err := s.Send(sess, "orch", "второе"); !errors.Is(err, ErrNoSocket) {
		t.Errorf("second Send err = %v, want ErrNoSocket", err)
	}
}
