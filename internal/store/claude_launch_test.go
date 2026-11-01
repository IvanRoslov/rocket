package store

import (
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"testing"
	"time"
)

func TestClaudeLaunchGenerationBindingCAS(t *testing.T) {
	st := openTestStore(t)
	if e := st.AddSession(Session{ID: "s", Kind: "worker", ProjectID: "p", RepoID: "r", Agent: "deepseek", Branch: "main", WorktreePath: "/tmp/s", TmuxName: "s", State: "running"}); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	l := claudeinstance.Launch{RocketSessionID: "s", Generation: "g1", PaneIdentity: "s:@1.%1", PanePID: 10, PaneStart: now, StartedAt: now}
	if e := st.PutClaudeLaunch(l); e != nil {
		t.Fatal(e)
	}
	got, pinned, e := st.GetClaudeLaunch("s")
	if e != nil || got.Generation != "g1" || pinned != nil {
		t.Fatalf("launch %+v %v %v", got, pinned, e)
	}
	id := claudeinstance.Identity{RocketSessionID: "s", LaunchGeneration: "g1", ClaudeSessionID: "uuid", PID: 11, ProcessStart: now, PaneIdentity: l.PaneIdentity, Kind: "interactive", PeerProtocol: 1, SocketPath: "/tmp/c.sock", JournalPath: "/tmp/uuid.jsonl"}
	if ok, e := st.BindClaudeIdentity(id); !ok || e != nil {
		t.Fatalf("bind %v %v", ok, e)
	}
	foreign := id
	foreign.PID = 12
	if ok, e := st.BindClaudeIdentity(foreign); ok || e != nil {
		t.Fatalf("overwrite binding %v %v", ok, e)
	}
	if e = st.PutClaudeLaunch(l); e != nil {
		t.Fatal(e)
	}
	_, pinned, e = st.GetClaudeLaunch("s")
	if e != nil || pinned == nil || pinned.PID != 11 {
		t.Fatalf("same generation erased binding %+v %v", pinned, e)
	}
	l.Generation = "g2"

	l.StartedAt = l.StartedAt.Add(time.Nanosecond)
	if e = st.PutClaudeLaunch(l); e != nil {
		t.Fatal(e)
	}
	if ok, e := st.BindClaudeIdentity(id); ok || e != nil {
		t.Fatalf("stale bind %v %v", ok, e)
	}
}
