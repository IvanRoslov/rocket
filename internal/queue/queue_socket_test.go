package queue

import (
	"context"
	"errors"
	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/agent/claudecode"
	"github.com/IvanRoslov/rocket/internal/agent/deepseek"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/socketmsg"
	"github.com/IvanRoslov/rocket/internal/store"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeSocket struct {
	mu              sync.Mutex
	available       bool
	target          claudeinstance.Identity
	resolutionState claudeinstance.Liveness
	outcome         SocketOutcome
	sendErr         error
	attempts        int
	snapshots       []claudeinstance.Snapshot
	beforeClaim     func()
}

func (f *fakeSocket) Resolve(context.Context, store.Session) claudeinstance.Resolution {
	if f.resolutionState != "" {
		return claudeinstance.Resolution{State: f.resolutionState, Identity: f.target}
	}
	if !f.available {
		return claudeinstance.Resolution{State: claudeinstance.Unknown, Reason: "fixture unavailable"}
	}
	return claudeinstance.Resolution{State: claudeinstance.Verified, Identity: f.target}
}
func (f *fakeSocket) Prepare(_ context.Context, _ store.Session, name, text string) (claudeinstance.Snapshot, error) {
	if !f.available {
		return claudeinstance.Snapshot{}, ErrNoSocket
	}
	b, e := claudeinstance.PinJournal(f.target)
	if e != nil {
		return claudeinstance.Snapshot{}, e
	}
	wire, e := socketmsg.NewMsgID()
	if e != nil {
		return claudeinstance.Snapshot{}, e
	}
	v := claudeinstance.Snapshot{Transport: ClaudeSocketTransport, Target: f.target, WireID: wire, Envelope: socketmsg.Envelope("", name, text), Boundary: b}
	if f.beforeClaim != nil {
		f.beforeClaim()
	}
	return v, nil
}
func (f *fakeSocket) Send(_ context.Context, v claudeinstance.Snapshot) (SocketOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	f.snapshots = append(f.snapshots, v)
	out := f.outcome
	if out == "" {
		out = SocketUnknown
	}
	return out, f.sendErr
}
func (f *fakeSocket) sendCount() int    { f.mu.Lock(); defer f.mu.Unlock(); return f.attempts }
func (f *fakeSocket) attemptCount() int { return f.sendCount() }
func installClaudeSocket(t *testing.T, h *testHarness, available bool) *fakeSocket {
	t.Helper()
	h.q.SetAgentResolver(func(name string) (agent.Agent, error) {
		switch name {
		case "claude-code":
			return claudecode.New(), nil
		case "deepseek":
			return deepseek.New(), nil
		case "codex":
			return h.ag, nil
		default:
			return nil, errors.New("unsupported")
		}
	})
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	os.WriteFile(path, nil, 0600)
	l := claudeinstance.Launch{RocketSessionID: "recv", Generation: "g", PaneIdentity: "recv:@1.%1", PanePID: 10, PaneStart: now, StartedAt: now}
	if e := h.st.PutClaudeLaunch(l); e != nil {
		t.Fatal(e)
	}
	target := claudeinstance.Identity{RocketSessionID: "recv", LaunchGeneration: "g", ClaudeSessionID: "329baa3b-27eb-40ed-82e1-1d9f3dde01e5", PID: 11, ProcessStart: now, PaneIdentity: l.PaneIdentity, Kind: "interactive", PeerProtocol: 1, SocketPath: "/tmp/fixture.sock", JournalPath: path}
	if ok, e := h.st.BindClaudeIdentity(target); !ok || e != nil {
		t.Fatalf("bind %v %v", ok, e)
	}
	f := &fakeSocket{available: available, target: target}
	h.q.SetSocketSender(f)
	return f
}
func TestSocketDeliveryRequiresJournalWithoutInject(t *testing.T) {
	for _, name := range []string{"claude-code", "deepseek"} {
		t.Run(name, func(t *testing.T) {
			h := newTestQueue(t)
			h.addRunningSession(t, "recv", activity.Ready)
			sess, _ := h.st.GetSession("recv")
			sess.Agent = name
			h.st.UpdateSession(sess)
			f := installClaudeSocket(t, h, true)
			id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
			m, _ := h.st.GetMessage(id)
			h.q.deliver(h.ctx, m)
			m, _ = h.st.GetMessage(id)
			if m.Status != "accepted" || m.Attempts != 1 || f.sendCount() != 1 || h.rt.callCount() != 0 {
				t.Fatalf("unsafe delivery %+v sends=%d inject=%d", m, f.sendCount(), h.rt.callCount())
			}
		})
	}
}
func TestPossibleWriteNeverRetriesOrFallsBack(t *testing.T) {
	for _, out := range []SocketOutcome{SocketUnknown, SocketAccepted, SocketHeld, SocketDenied, SocketExpired} {
		t.Run(string(out), func(t *testing.T) {
			h := newTestQueue(t)
			h.addRunningSession(t, "recv", activity.Active)
			f := installClaudeSocket(t, h, true)
			f.outcome = out
			id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
			m, _ := h.st.GetMessage(id)
			h.q.deliver(h.ctx, m)
			h.st.ResetDelivering()
			for i := 0; i < 2; i++ {
				m, _ = h.st.GetMessage(id)
				h.q.deliver(h.ctx, m)
			}
			if f.sendCount() != 1 || h.rt.callCount() != 0 {
				t.Fatalf("resend/fallback sends=%d inject=%d", f.sendCount(), h.rt.callCount())
			}
		})
	}
}
func TestSocketDisabledAndIncompatibleKeepLegacy(t *testing.T) {
	for _, which := range []string{"disabled", "incompatible"} {
		t.Run(which, func(t *testing.T) {
			h := newTestQueue(t)
			h.addRunningSession(t, "recv", activity.Ready)
			f := installClaudeSocket(t, h, true)
			if which == "disabled" {
				h.q.cfg.SocketDelivery = false
			} else {
				h.q.SetAgentResolver(func(string) (agent.Agent, error) { return struct{ agent.Agent }{claudecode.New()}, nil })
			}
			id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "legacy"})
			m, _ := h.st.GetMessage(id)
			h.q.deliver(h.ctx, m)
			if f.sendCount() != 0 || h.rt.callCount() != 1 {
				t.Fatal("legacy capability boundary changed")
			}
		})
	}
}
func TestSocketKeepsDraftAndQuizUntouched(t *testing.T) {
	h := newTestQueue(t)
	h.addRunningSession(t, "recv", activity.Ready)
	f := installClaudeSocket(t, h, true)
	sess, _ := h.st.GetSession("recv")
	sess.PendingQuiz = "fixture quiz"
	h.st.UpdateSession(sess)
	id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback"})
	m, _ := h.st.GetMessage(id)
	h.q.deliver(h.ctx, m)
	if f.sendCount() != 1 || h.rt.callCount() != 0 {
		t.Fatal("socket touched quiz/composer")
	}
	after, _ := h.st.GetSession("recv")
	if after.PendingQuiz != sess.PendingQuiz {
		t.Fatal("quiz modified")
	}
}
