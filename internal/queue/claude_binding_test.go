package queue

import (
	"context"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"github.com/IvanRoslov/rocket/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeSameRootConversationFollow(t *testing.T) {
	h, s, cc, _ := productionSocketFixture(t)
	sess, _ := h.st.GetSession("recv")
	old := s.Resolve(h.ctx, sess)
	if old.State != claudeinstance.Verified {
		t.Fatal(old)
	}
	cc.SessionID = "239baa3b-27eb-40ed-82e1-1d9f3dde01e5"
	newPath := filepath.Join(filepath.Dir(old.Identity.JournalPath), cc.SessionID+".jsonl")
	if e := os.WriteFile(newPath, nil, 0600); e != nil {
		t.Fatal(e)
	}
	writeRegistry(t, s.sessionsDir, cc)
	r := s.Resolve(h.ctx, sess)
	if r.State != claudeinstance.Verified || r.Identity.ClaudeSessionID != cc.SessionID {
		t.Fatalf("same-root follow rejected: %+v", r)
	}
	current, _ := h.st.GetSession("recv")
	if current.Activity != "" {
		t.Fatalf("borrowed A eligibility: %s", current.Activity)
	}
}

func TestClaudeClearBetweenClaimAndSendHasZeroIO(t *testing.T) {
	h, s, cc, in := productionSocketFixture(t)
	v := claimSocketFixture(t, h, s)
	cc.SessionID = "239baa3b-27eb-40ed-82e1-1d9f3dde01e5"
	os.WriteFile(filepath.Join(filepath.Dir(v.Target.JournalPath), cc.SessionID+".jsonl"), nil, 0600)
	writeRegistry(t, s.sessionsDir, cc)
	outcome, _ := s.Send(context.Background(), v)
	if outcome != SocketNotSent {
		t.Fatalf("clear outcome %s", outcome)
	}
	select {
	case <-in:
		t.Fatal("wrote after clear")
	case <-time.After(10 * time.Millisecond):
	}
	m, _ := h.st.GetMessage(v.MessageID)
	if ok, e := h.st.SetClaudePhase(v.MessageID, v.WireID, v.Target, string(outcome), time.Now()); !ok || e != nil {
		t.Fatal(ok, e)
	}
	m, _ = h.st.GetMessage(v.MessageID)
	if m.Status != "failed" || m.Reason != "claude_not_sent" {
		t.Fatalf("prewrite settlement %+v", m)
	}
	if m.Attempts != 1 {
		t.Fatal(m)
	}
}

func TestClaudeEligibilityRecheckedAfterFollow(t *testing.T) {
	h, s, cc, in := productionSocketFixture(t)
	sess, _ := h.st.GetSession("recv")
	old := s.Resolve(h.ctx, sess)
	cc.SessionID = "239baa3b-27eb-40ed-82e1-1d9f3dde01e5"
	os.WriteFile(filepath.Join(filepath.Dir(old.Identity.JournalPath), cc.SessionID+".jsonl"), nil, 0600)
	writeRegistry(t, s.sessionsDir, cc)
	id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "wait for B"})
	m, _ := h.st.GetMessage(id)
	ctx, cancel := context.WithTimeout(h.ctx, 15*time.Millisecond)
	defer cancel()
	h.q.deliver(ctx, m)
	m, _ = h.st.GetMessage(id)
	if m.Status != "queued" || m.Attempts != 0 {
		t.Fatalf("A ready licensed B: %+v", m)
	}
	select {
	case <-in:
		t.Fatal("borrowed A ready sent B")
	default:
	}
}

type claudeSenderRestoreProbe struct {
	socketProbe
	before func(int)
}

func (p *claudeSenderRestoreProbe) Process(ctx context.Context, pid int) (claudeinstance.Process, bool, error) {
	if p.before != nil {
		p.before(pid)
	}
	if pid == 11 && p.root.PID != 11 {
		return claudeinstance.Process{}, false, nil
	}
	return p.socketProbe.Process(ctx, pid)
}
func TestClaudeRestoreDuringSenderProbeCannotFailFreshFeedback(t *testing.T) {
	h, s, _, _ := productionSocketFixture(t)
	sess, _ := h.st.GetSession("recv")
	r := s.Resolve(h.ctx, sess)
	probe := &claudeSenderRestoreProbe{socketProbe: s.probe.(socketProbe)}
	s.probe = probe
	probe.before = func(pid int) {
		if pid != 11 {
			return
		}
		probe.before = nil
		l, _, _ := h.st.GetClaudeLaunch("recv")
		l.Generation = "replacement"
		l.StartedAt = l.StartedAt.Add(time.Nanosecond)
		h.st.PutClaudeLaunch(l)
		target := r.Identity
		target.LaunchGeneration = l.Generation
		target.PID = 22
		target.ProcessStart = target.ProcessStart.Add(time.Nanosecond)
		if ok, e := h.st.BindClaudeIdentity(target); !ok || e != nil {
			t.Fatal(ok, e)
		}
		probe.root.PID = 22
		probe.root.Start = target.ProcessStart
		h.st.UpdateSessionActivity("recv", "active", time.Now().Unix())
	}
	id, _ := h.st.AddMessage(store.Message{ToSession: "recv", Body: "feedback for restored root"})
	msg, _ := h.st.GetMessage(id)
	ctx, cancel := context.WithTimeout(h.ctx, 15*time.Millisecond)
	defer cancel()
	h.q.deliver(ctx, msg)
	got, _ := h.st.GetMessage(id)
	if got.Status != "queued" || got.Attempts != 0 {
		t.Fatalf("old dead root rejected new feedback: %+v", got)
	}
}
