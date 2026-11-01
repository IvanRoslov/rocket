package session

import (
	"context"
	"github.com/IvanRoslov/rocket/internal/claudeinstance"
	"testing"
	"time"
)

type launchProbe struct{ at time.Time }

func (p launchProbe) Pane(_ context.Context, s string) (claudeinstance.Pane, bool, error) {
	return claudeinstance.Pane{Identity: s + ":@1.%1", PID: 10, Start: p.at}, true, nil
}
func (p launchProbe) Process(context.Context, int) (claudeinstance.Process, bool, error) {
	return claudeinstance.Process{}, false, nil
}
func TestClaudeGenerationBeforeWorkspaceAndAnchorBeforeRunning(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	seedProjectRepo(t, st, "p", "r")
	testFakeAgent.claudeCompatible = true
	m.claudeProbe = launchProbe{time.Now().UTC()}
	rt.onCreate = func() {
		l, _, e := st.GetClaudeLaunch("claude-core")
		if e != nil || l.Generation == "" || l.PanePID != 0 {
			t.Errorf("launch must exist before runtime: %+v %v", l, e)
		}
	}
	sess, e := m.Spawn(context.Background(), SpawnReq{Project: "p", Repo: "r", Feature: "claude", Task: "core", AgentName: "fake"})
	if e != nil {
		t.Fatal(e)
	}
	l, _, e := st.GetClaudeLaunch(sess.ID)
	if e != nil || l.PanePID != 10 || l.PaneIdentity != sess.ID+":@1.%1" {
		t.Fatalf("anchor %+v %v", l, e)
	}
	if len(testFakeAgent.setupCalls) != 1 || testFakeAgent.setupCalls[0].ClaudeLaunchGeneration != l.Generation {
		t.Fatal("generation not passed before workspace setup")
	}
}
