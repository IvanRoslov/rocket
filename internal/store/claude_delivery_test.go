package store

import (
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func claudeStoreFixture(t *testing.T, st *Store) (claudeinstance.Snapshot, int64) {
	t.Helper()
	now := time.Now().UTC()
	st.AddSession(Session{ID: "s", Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "deepseek", Branch: "main", WorktreePath: "/tmp/s", TmuxName: "s", State: "running"})
	l := claudeinstance.Launch{RocketSessionID: "s", Generation: "g", PaneIdentity: "s:@1.%1", PanePID: 10, PaneStart: now, StartedAt: now}
	if e := st.PutClaudeLaunch(l); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	os.WriteFile(path, nil, 0600)
	target := claudeinstance.Identity{RocketSessionID: "s", LaunchGeneration: "g", ClaudeSessionID: "329baa3b-27eb-40ed-82e1-1d9f3dde01e5", PID: 11, ProcessStart: now, PaneIdentity: l.PaneIdentity, Kind: "interactive", PeerProtocol: 1, SocketPath: "/tmp/c.sock", JournalPath: path}
	if ok, e := st.BindClaudeIdentity(target); !ok || e != nil {
		t.Fatalf("bind %v %v", ok, e)
	}
	boundary, e := claudeinstance.PinJournal(target)
	if e != nil {
		t.Fatal(e)
	}
	id, e := st.AddMessage(Message{ToSession: "s", Body: "feedback", Attempts: 2})
	if e != nil {
		t.Fatal(e)
	}
	return claudeinstance.Snapshot{MessageID: id, Transport: "claude-socket-v1", Target: target, WireID: "08edca10-0082-4bd4-b91a-a687a2690ba3", Envelope: "envelope", Boundary: boundary, PreparedAt: now, Deadline: now.Add(time.Minute)}, id
}
func TestClaudeClaimAtomicMarkerAndNonzeroAttempts(t *testing.T) {
	st := openTestStore(t)
	v, id := claudeStoreFixture(t, st)
	if ok, e := st.ClaimClaudeMessage(v); !ok || e != nil {
		t.Fatalf("claim %v %v", ok, e)
	}
	m, _ := st.GetMessage(id)
	if m.Status != "delivering" || m.Attempts != 3 || m.DeliveryTransport != "claude-socket-v1" {
		t.Fatalf("marker %+v", m)
	}
	got, e := st.GetClaudeDelivery(id)
	if e != nil || got.WireID != v.WireID || got.Phase != "possible_write" {
		t.Fatalf("snapshot %+v %v", got, e)
	}
	if ok, e := st.ClaimClaudeMessage(v); ok || e != nil {
		t.Fatalf("duplicate claim %v %v", ok, e)
	}
	st.ResetDelivering()
	m, _ = st.GetMessage(id)
	if m.Status != "accepted" {
		t.Fatalf("crash recovery requeued side effect %+v", m)
	}
}
