package queue

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/agent/claudecode"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"github.com/IvanRoslov/rocket/internal/store"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func shortTempDir(t *testing.T) string {
	t.Helper()
	d, e := os.MkdirTemp("/tmp", "rq")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}
func writeRegistry(t *testing.T, dir string, s socketmsg.Session) {
	t.Helper()
	b, e := json.Marshal(s)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", s.PID)), b, 0600); e != nil {
		t.Fatal(e)
	}
}

type socketProbe struct {
	pane claudeinstance.Pane
	root claudeinstance.Process
	err  error
}

func (p socketProbe) Pane(context.Context, string) (claudeinstance.Pane, bool, error) {
	return p.pane, true, p.err
}
func (p socketProbe) Process(_ context.Context, pid int) (claudeinstance.Process, bool, error) {
	if p.err != nil {
		return claudeinstance.Process{}, false, p.err
	}
	if pid == p.root.PID {
		return p.root, true, nil
	}
	return claudeinstance.Process{PID: p.pane.PID, Start: p.pane.Start, Name: "sh"}, true, nil
}
func productionSocketFixture(t *testing.T) (*testHarness, *ClaudeSocketSender, socketmsg.Session, <-chan socketmsg.Message) {
	t.Helper()
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	h.q.SetAgentResolver(func(string) (agent.Agent, error) { return claudecode.New(), nil })
	dir := t.TempDir()
	reg := filepath.Join(dir, "sessions")
	os.MkdirAll(reg, 0700)
	sock := filepath.Join(shortTempDir(t), "peer.sock")
	ln, e := net.Listen("unix", sock)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ln.Close() })
	received := make(chan socketmsg.Message, 10)
	go func() {
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			line, e := bufio.NewReader(c).ReadBytes('\n')
			io.Copy(io.Discard, c)
			c.Close()
			if e == nil {
				var m socketmsg.Message
				if json.Unmarshal(line, &m) == nil {
					received <- m
				}
			}
		}
	}()
	now := time.Now().UTC().Truncate(time.Second)
	l := claudeinstance.Launch{RocketSessionID: "recv", Generation: "g", PaneIdentity: "recv:@1.%1", PanePID: 10, PaneStart: now, StartedAt: now}
	if e = h.st.PutClaudeLaunch(l); e != nil {
		t.Fatal(e)
	}
	cc := socketmsg.Session{PID: 11, SessionID: "329baa3b-27eb-40ed-82e1-1d9f3dde01e5", CWD: t.TempDir(), Kind: "interactive", Tmux: l.PaneIdentity, ProcStart: now.Format(time.RFC3339), PeerProtocol: 1, MessagingSocketPath: sock}
	writeRegistry(t, reg, cc)
	probe := socketProbe{pane: claudeinstance.Pane{Identity: l.PaneIdentity, PID: 10, Start: now}, root: claudeinstance.Process{PID: 11, PPID: 10, Start: now, Name: "claude"}}
	identity := claudeinstance.Resolve(context.Background(), l, nil, []socketmsg.Session{cc}, probe, filepath.Join(dir, "projects")).Identity
	os.MkdirAll(filepath.Dir(identity.JournalPath), 0700)
	os.WriteFile(identity.JournalPath, nil, 0600)
	s := NewClaudeSocketSender(reg)
	s.SetStore(h.st)
	s.probe = probe
	s.heldWait = time.Millisecond
	h.q.SetSocketSender(s)
	return h, s, cc, received
}
func claimSocketFixture(t *testing.T, h *testHarness, s *ClaudeSocketSender) claudeinstance.Snapshot {
	t.Helper()
	sess, _ := h.st.GetSession("recv")
	v, e := s.Prepare(h.ctx, sess, "rocket", "feedback")
	if e != nil {
		t.Fatal(e)
	}
	v.MessageID, _ = h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
	v.PreparedAt = time.Now().UTC()
	v.Deadline = v.PreparedAt.Add(time.Minute)
	if ok, e := h.st.ClaimClaudeMessage(v); !ok || e != nil {
		t.Fatalf("claim %v %v", ok, e)
	}
	return v
}
func TestProductionSocketPinsClaimAndWireIdentity(t *testing.T) {
	h, s, _, incoming := productionSocketFixture(t)
	v := claimSocketFixture(t, h, s)
	out, e := s.Send(h.ctx, v)
	if e != nil || out != SocketUnknown {
		t.Fatalf("silence is uncertainty: %s %v", out, e)
	}
	select {
	case m := <-incoming:
		if m.SessionID != v.Target.ClaudeSessionID || m.MsgID != v.WireID || m.Message.Content != v.Envelope {
			t.Fatalf("wire identity %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("no fixture message")
	}
}
func TestProductionSocketRevalidatesBeforeWrite(t *testing.T) {
	h, s, _, incoming := productionSocketFixture(t)
	v := claimSocketFixture(t, h, s)
	l, _, _ := h.st.GetClaudeLaunch("recv")
	l.Generation = "replacement"
	h.st.PutClaudeLaunch(l)
	if out, _ := s.Send(h.ctx, v); out != SocketNotSent {
		t.Fatalf("changed generation wrote: %s", out)
	}
	select {
	case <-incoming:
		t.Fatal("wrote to replaced target")
	default:
	}
}
func TestActualSelectedRouteUnavailableNeverInjects(t *testing.T) {
	for _, reason := range []string{"missing socket", "ambiguous registry", "unsupported protocol", "invalid pane"} {
		t.Run(reason, func(t *testing.T) {
			h, s, cc, _ := productionSocketFixture(t)
			switch reason {
			case "missing socket":
				cc.MessagingSocketPath = ""
			case "ambiguous registry":
				other := cc
				other.PID = 12
				writeRegistry(t, s.sessionsDir, other)
			case "unsupported protocol":
				cc.PeerProtocol = 2
			case "invalid pane":
				cc.Tmux = "recv:@9.%9"
			}
			writeRegistry(t, s.sessionsDir, cc)
			id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "keep draft"})
			for pass := 0; pass < 2; pass++ {
				m, _ := h.st.GetMessage(id)
				ctx, cancel := context.WithTimeout(h.ctx, 10*time.Millisecond)
				h.q.deliver(ctx, m)
				cancel()
			}
			m, _ := h.st.GetMessage(id)
			if m.Status != "queued" || m.Attempts != 0 || h.rt.callCount() != 0 {
				t.Fatalf("fallback %+v inject=%d", m, h.rt.callCount())
			}
		})
	}
}
