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
