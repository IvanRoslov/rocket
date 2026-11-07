package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

// startAgentWithFakeProfile registers agent id with a profile on the "fake"
// adapter and starts it, the way `rocket agent start --profile` does.
func startAgentWithFakeProfile(t *testing.T, m *Manager, st *store.Store, id string) (store.Agent, store.Session, error) {
	t.Helper()
	a := testAgentRow(t, id)
	if err := st.AddAgent(a); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	if err := st.CreateModelProfile(store.ModelProfile{Name: "p", Agent: "fake", Model: "m", Enabled: true}); err != nil {
		t.Fatalf("CreateModelProfile: %v", err)
	}
	sess, err := m.StartAgent(context.Background(), a, "p")
	return a, sess, err
}

// A persistent agent launched on a Claude-socket adapter must get the same
// exact launch binding a spawned session gets: without it the monitor never
// learns its activity and the queue holds every message until queue_timeout
// (incident: cto on a claude-code profile, 2026-11-01..07).
func TestStartAgentClaudeProfileRecordsLaunchBinding(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	testFakeAgent.claudeCompatible = true
	m.claudeProbe = launchProbe{time.Now().UTC()}
	rt.onCreate = func() {
		l, _, e := st.GetClaudeLaunch("cto")
		if e != nil || l.Generation == "" || l.PanePID != 0 {
			t.Errorf("launch must exist before runtime: %+v %v", l, e)
		}
		sess, e := st.GetSession("cto")
		if e != nil || sess.State != "spawning" {
			t.Errorf("session at runtime.Create = %+v %v, want spawning", sess, e)
		}
	}

	_, sess, err := startAgentWithFakeProfile(t, m, st, "cto")
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if sess.State != "running" {
		t.Fatalf("state = %q, want running", sess.State)
	}
	l, _, err := st.GetClaudeLaunch("cto")
	if err != nil || l.PanePID != 10 || l.PaneIdentity != "cto:@1.%1" {
		t.Fatalf("pane anchor = %+v %v", l, err)
	}
	if len(testFakeAgent.envCalls) != 1 || testFakeAgent.envCalls[0].ClaudeLaunchGeneration != l.Generation {
		t.Fatalf("adapter env must see the launch generation: %+v", testFakeAgent.envCalls)
	}
	if len(testFakeAgent.launchCalls) != 1 || testFakeAgent.launchCalls[0].ClaudeLaunchGeneration != l.Generation {
		t.Fatalf("adapter launch must see the launch generation: %+v", testFakeAgent.launchCalls)
	}
	if len(testFakeAgent.setupCalls) != 0 {
		t.Fatalf("SetupWorkspace must not touch an agent's own dir: %d calls", len(testFakeAgent.setupCalls))
	}
}

// A native-delivery adapter (Pi) gets its transport recorded before the
// runtime starts, and the launch/env carry its channel.
func TestStartAgentNativeProfileRecordsTransport(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	testFakeAgent.nativeLaunch = agent.NativeLaunch{Transport: "pi-uds-v1", InstanceID: "i-1", Endpoint: "/tmp/a.sock"}
	rt.onCreate = func() {
		sess, e := st.GetSession("cto")
		if e != nil || sess.DeliveryTransport != "pi-uds-v1" {
			t.Errorf("transport at runtime.Create = %+v %v", sess, e)
		}
	}

	_, sess, err := startAgentWithFakeProfile(t, m, st, "cto")
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if sess.DeliveryTransport != "pi-uds-v1" || sess.DeliveryInstance != "i-1" || sess.DeliveryEndpoint != "/tmp/a.sock" {
		t.Fatalf("returned session = %+v", sess)
	}
	stored, _ := st.GetSession("cto")
	if stored.DeliveryTransport != "pi-uds-v1" || stored.State != "running" {
		t.Fatalf("stored session = %+v", stored)
	}
	assertNativeSpec(t, testFakeAgent.launchCalls[0], testFakeAgent.nativeLaunch)
	assertNativeSpec(t, testFakeAgent.envCalls[0], testFakeAgent.nativeLaunch)
}

// Restarting an agent on a different harness must not keep the previous
// launch's delivery channel: a Pi transport left on the row would route a
// Claude agent's messages to a dead socket.
func TestStartAgentHarnessSwitchDropsPreviousTransport(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	testFakeAgent.nativeLaunch = agent.NativeLaunch{Transport: "pi-uds-v1", InstanceID: "i-1", Endpoint: "/tmp/a.sock"}
	a, _, err := startAgentWithFakeProfile(t, m, st, "cto")
	if err != nil {
		t.Fatalf("first StartAgent: %v", err)
	}
	if err := m.StopAgent(context.Background(), "cto"); err != nil {
		t.Fatalf("StopAgent: %v", err)
	}
	delete(rt.aliveMap, "cto")

	testFakeAgent.nativeLaunch = agent.NativeLaunch{}
	testFakeAgent.claudeCompatible = true
	m.claudeProbe = launchProbe{time.Now().UTC()}
	sess, err := m.StartAgent(context.Background(), a, "p")
	if err != nil {
		t.Fatalf("second StartAgent: %v", err)
	}
	stored, _ := st.GetSession("cto")
	if sess.DeliveryTransport != "" || stored.DeliveryTransport != "" || stored.DeliveryEndpoint != "" {
		t.Fatalf("stale transport survived harness switch: returned %+v stored %+v", sess, stored)
	}
	if l, _, err := st.GetClaudeLaunch("cto"); err != nil || l.PanePID != 10 {
		t.Fatalf("claude binding after switch = %+v %v", l, err)
	}
}

// A runtime that fails to start must not leave the agent row claiming a
// live session.
func TestStartAgentRuntimeFailureLeavesNoLiveRow(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	testFakeAgent.claudeCompatible = true
	rt.createErr = errors.New("tmux exploded")

	_, _, err := startAgentWithFakeProfile(t, m, st, "cto")
	if err == nil {
		t.Fatal("StartAgent succeeded despite runtime failure")
	}
	stored, err := st.GetSession("cto")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.State == "running" || stored.State == "spawning" {
		t.Fatalf("row after failed start = %q, want a dead state", stored.State)
	}
}

// An agent registered by command (no profile) whose CLI is Claude Code gets
// the binding too; its own argv is kept, only the launch env is added.
func TestStartAgentClaudeCommandRecordsLaunchBinding(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	m.claudeProbe = launchProbe{time.Now().UTC()}
	a := testAgentRow(t, "cto") // Command: "claude"

	if _, err := m.StartAgent(context.Background(), a, ""); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	l, _, err := st.GetClaudeLaunch("cto")
	if err != nil || l.Generation == "" || l.PanePID != 10 {
		t.Fatalf("binding = %+v %v", l, err)
	}
	created := rt.created[0]
	if created.Command != "claude" || created.Env["ROCKET_CLAUDE_LAUNCH"] != l.Generation {
		t.Fatalf("create spec = %+v", created)
	}
}
