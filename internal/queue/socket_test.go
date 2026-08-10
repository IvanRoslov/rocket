package queue

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

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
