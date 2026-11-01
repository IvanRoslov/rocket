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
	h.q.claudeProbe = probe
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

func validFixtureReceipt(v claudeinstance.Snapshot) socketmsg.Message {
	return socketmsg.Message{MsgV: 1, Type: "control", Action: "peer_message_status", OrigMsgID: v.WireID, SessionID: v.Target.ClaudeSessionID, From: socketmsg.EncodeAddr(v.Target.SocketPath), Status: "delivered"}
}
func TestClaudeReceiptRequiresEveryCorrelationField(t *testing.T) {
	for _, which := range []string{"valid", "missing UUID", "foreign UUID", "foreign socket", "wrong wire", "wrong type", "wrong action", "wrong version", "unknown status", "unconfirmed status", "replacement"} {
		t.Run(which, func(t *testing.T) {
			h, s, _, _ := productionSocketFixture(t)
			v := claimSocketFixture(t, h, s)
			m := validFixtureReceipt(v)
			switch which {
			case "missing UUID":
				m.SessionID = ""
			case "foreign UUID":
				m.SessionID = "foreign"
			case "foreign socket":
				m.From = "foreign"
			case "wrong wire":
				m.OrigMsgID = "foreign"
			case "wrong type":
				m.Type = "message"
			case "wrong action":
				m.Action = "other"
			case "wrong version":
				m.MsgV = 2
			case "unknown status":
				m.Status = "other"
			case "unconfirmed status":
				m.Status = "unconfirmed"
			case "replacement":
				l, _, _ := h.st.GetClaudeLaunch("recv")
				l.Generation = "g2"
				h.st.PutClaudeLaunch(l)
			}
			want := SocketUnknown
			if which == "valid" {
				want = SocketAccepted
			}
			if got := s.receiptOutcome(h.ctx, v, m); got != want {
				t.Fatalf("outcome %s want %s", got, want)
			}
			current, _ := h.st.GetMessage(v.MessageID)
			if current.Status == "delivered" {
				t.Fatal("receipt proved delivery")
			}
		})
	}
}
func TestClaudeLateSocketReceiptUpdatesPhaseWithoutDeliveryOrReopen(t *testing.T) {
	h, s, _, _ := productionSocketFixture(t)
	r := socketmsg.NewReceipts("rq-late")
	defer r.Close()
	s.SetReceipts(r)
	done := make(chan struct{}, 10)
	r.SetHandler(func(m socketmsg.Message) { s.handleReceipt(m); done <- struct{}{} })
	v := claimSocketFixture(t, h, s)
	if out, e := s.Send(h.ctx, v); out != SocketUnknown || e != nil {
		t.Fatalf("silence %s %v", out, e)
	}
	h.st.SetClaudePhase(v.MessageID, v.WireID, v.Target, "unknown", time.Now())
	receipt := validFixtureReceipt(v)
	send := func(status string) {
		receipt.Status = status
		c, e := net.DialTimeout("unix", v.ReplyPath, time.Second)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(receipt)
		_, e = c.Write(append(raw, '\n'))
		c.Close()
		if e != nil {
			t.Fatal(e)
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("late handler not invoked")
		}
	}
	send("held")
	saved, _ := h.st.GetClaudeDelivery(v.MessageID)
	if saved.Phase != "held" {
		t.Fatal(saved.Phase)
	}
	send("delivered")
	saved, _ = h.st.GetClaudeDelivery(v.MessageID)
	m, _ := h.st.GetMessage(v.MessageID)
	if saved.Phase != "accepted" || m.Status != "accepted" {
		t.Fatalf("receipt alone settled: %+v %s", m, saved.Phase)
	}
	send("held")
	saved, _ = h.st.GetClaudeDelivery(v.MessageID)
	if saved.Phase != "accepted" {
		t.Fatal("phase regressed")
	}
	h.st.ExpireClaudeMessage(v.MessageID, v.Deadline.Add(time.Second))
	send("delivered")
	m, _ = h.st.GetMessage(v.MessageID)
	if m.Status != "failed" || m.Reason != "delivery_unconfirmed" {
		t.Fatal("late callback reopened terminal failure")
	}
}

func TestClaudeUnreadableRegistryAlternativeCannotProveUniqueness(t *testing.T) {
	h, s, _, incoming := productionSocketFixture(t)
	v := claimSocketFixture(t, h, s)
	// The ordinary discovery/UI reader skips this record. It could be a
	// duplicate UUID writer whose metadata is being replaced or corrupted.
	if e := os.WriteFile(filepath.Join(s.sessionsDir, "12.json"), []byte("{"), 0600); e != nil {
		t.Fatal(e)
	}
	if out, _ := s.Send(h.ctx, v); out != SocketNotSent {
		t.Fatalf("unreadable alternative allowed write: %s", out)
	}
	select {
	case <-incoming:
		t.Fatal("wrote while uniqueness unknown")
	default:
	}
}

func TestClaudeDisabledAgeRetainsBoundedSocketOperation(t *testing.T) {
	h, s, _, incoming := productionSocketFixture(t)
	sess, _ := h.st.GetSession("recv")
	v, e := s.Prepare(h.ctx, sess, "rocket", "feedback")
	if e != nil {
		t.Fatal(e)
	}
	v.MessageID, _ = h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
	v.PreparedAt = time.Now().UTC()
	v.Deadline = time.Time{}
	if ok, e := h.st.ClaimClaudeMessage(v); !ok || e != nil {
		t.Fatalf("disabled claim %v %v", ok, e)
	}
	ctx, cancel := context.WithTimeout(h.ctx, time.Second)
	defer cancel()
	start := time.Now()
	out, e := s.Send(ctx, v)
	elapsed := time.Since(start)
	if out != SocketUnknown || e != nil || elapsed > time.Second {
		t.Fatalf("disabled-age operation %s error=%v elapsed=%s", out, e, elapsed)
	}
	select {
	case m := <-incoming:
		if m.SessionID != v.Target.ClaudeSessionID || m.MsgID != v.WireID {
			t.Fatal("unpinned wire")
		}
	case <-ctx.Done():
		t.Fatal("no exact temporary fixture send")
	}
}
