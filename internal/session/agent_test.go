package session

import (
	"context"
	"errors"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// testAgentRow builds a registered agent whose dir exists, so StartAgent gets
// past its directory check.
func testAgentRow(t *testing.T, id string) store.Agent {
	t.Helper()
	return store.Agent{ID: id, Dir: t.TempDir(), Command: "claude", Enabled: true}
}

func TestStartAgentCreatesSessionAndRow(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	a := testAgentRow(t, "sre")

	sess, err := m.StartAgent(context.Background(), a)
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if sess.ID != "sre" || sess.Kind != AgentSessionKind || sess.State != "running" {
		t.Fatalf("session = %+v", sess)
	}
	if sess.WorktreePath != a.Dir {
		t.Errorf("WorktreePath = %q, want the agent dir %q", sess.WorktreePath, a.Dir)
	}

	if len(rt.created) != 1 {
		t.Fatalf("runtime sessions created = %d, want 1", len(rt.created))
	}
	spec := rt.created[0]
	if spec.Name != "sre" || spec.Dir != a.Dir || spec.Command != "claude" {
		t.Errorf("create spec = %+v", spec)
	}
	if spec.Env["ROCKET_SESSION_ID"] != "sre" {
		t.Errorf("ROCKET_SESSION_ID = %q, want sre", spec.Env["ROCKET_SESSION_ID"])
	}
	if spec.Env["ROCKET_SOCKET"] == "" {
		t.Errorf("ROCKET_SOCKET not set: %+v", spec.Env)
	}

	stored, err := st.GetSession("sre")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Kind != AgentSessionKind || stored.TmuxName != "sre" {
		t.Errorf("stored session = %+v", stored)
	}
}

func TestStartAgentWithoutCommandRunsAShell(t *testing.T) {
	m, _, _, rt, _ := testManager(t)
	a := testAgentRow(t, "sre")
	a.Command = ""

	if _, err := m.StartAgent(context.Background(), a); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if rt.created[0].Command != "" {
		t.Errorf("Command = %q, want empty (the runtime falls back to a shell)", rt.created[0].Command)
	}
}

func TestStartAgentRejectsMissingDirAndDisabled(t *testing.T) {
	m, _, _, _, _ := testManager(t)

	noDir := store.Agent{ID: "sre", Enabled: true}
	_, err := m.StartAgent(context.Background(), noDir)
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "agent_no_dir" {
		t.Fatalf("StartAgent without dir = %v, want agent_no_dir", err)
	}

	missing := store.Agent{ID: "sre", Dir: "/definitely/not/here", Enabled: true}
	_, err = m.StartAgent(context.Background(), missing)
	if !errors.As(err, &verr) || verr.Code != "agent_dir_missing" {
		t.Fatalf("StartAgent with a missing dir = %v, want agent_dir_missing", err)
	}

	disabled := testAgentRow(t, "sre")
	disabled.Enabled = false
	_, err = m.StartAgent(context.Background(), disabled)
	if !errors.As(err, &verr) || verr.Code != "agent_disabled" {
		t.Fatalf("StartAgent disabled = %v, want agent_disabled", err)
	}
}

func TestStartAgentRefusesToRestartALiveSession(t *testing.T) {
	m, _, _, rt, _ := testManager(t)
	a := testAgentRow(t, "sre")

	if _, err := m.StartAgent(context.Background(), a); err != nil {
		t.Fatalf("first StartAgent: %v", err)
	}
	_, err := m.StartAgent(context.Background(), a)
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "agent_live" {
		t.Fatalf("second StartAgent = %v, want agent_live", err)
	}
	if len(rt.created) != 1 {
		t.Errorf("runtime sessions created = %d, want 1", len(rt.created))
	}
}

func TestStopAgentKillsTheSessionButKeepsTheRegistration(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	a := testAgentRow(t, "sre")
	if err := st.AddAgent(a); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	if _, err := m.StartAgent(context.Background(), a); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	if err := m.StopAgent(context.Background(), "sre"); err != nil {
		t.Fatalf("StopAgent: %v", err)
	}
	if len(rt.destroyed) != 1 || rt.destroyed[0] != "sre" {
		t.Errorf("destroyed = %v, want [sre]", rt.destroyed)
	}

	sess, err := st.GetSession("sre")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.State != "done" {
		t.Errorf("state = %q, want done", sess.State)
	}
	if _, err := st.GetAgent("sre"); err != nil {
		t.Errorf("the registration must survive a stop: %v", err)
	}
}

func TestAdoptAgentSessionIsIdempotentAndRefusesCollisions(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	a := testAgentRow(t, "sre")

	sess, err := m.AdoptAgentSession(a)
	if err != nil {
		t.Fatalf("AdoptAgentSession: %v", err)
	}
	if sess.State != "running" {
		t.Fatalf("adopted session = %+v", sess)
	}
	if _, err := m.AdoptAgentSession(a); err != nil {
		t.Fatalf("second AdoptAgentSession: %v", err)
	}

	// A session of another kind under the same name is never overwritten.
	other := testAgentRow(t, "billing-orch")
	if err := st.AddSession(store.Session{
		ID: "billing-orch", Kind: "orchestrator", ProjectID: "", RepoID: "",
		Agent: "fake", TmuxName: "billing-orch", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if _, err := m.AdoptAgentSession(other); err == nil {
		t.Fatal("AdoptAgentSession over an orchestrator session: want an error")
	}
	stored, err := st.GetSession("billing-orch")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Kind != "orchestrator" {
		t.Errorf("kind = %q, want the orchestrator row untouched", stored.Kind)
	}
}

func TestAdoptAgentSessionRevivesADeadRow(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	a := testAgentRow(t, "sre")

	if _, err := m.AdoptAgentSession(a); err != nil {
		t.Fatalf("AdoptAgentSession: %v", err)
	}
	if err := m.RetireAgentSession("sre"); err != nil {
		t.Fatalf("RetireAgentSession: %v", err)
	}
	if sess, _ := st.GetSession("sre"); sess.State != "done" {
		t.Fatalf("state after retire = %q, want done", sess.State)
	}

	if _, err := m.AdoptAgentSession(a); err != nil {
		t.Fatalf("re-adopt: %v", err)
	}
	sess, err := st.GetSession("sre")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.State != "running" {
		t.Errorf("state after re-adopt = %q, want running", sess.State)
	}
}

func TestRetireAgentSessionLeavesOtherKindsAlone(t *testing.T) {
	m, st, _, _, _ := testManager(t)

	if err := st.AddSession(store.Session{
		ID: "billing-orch", Kind: "orchestrator", Agent: "fake",
		TmuxName: "billing-orch", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := m.RetireAgentSession("billing-orch"); err != nil {
		t.Fatalf("RetireAgentSession: %v", err)
	}
	sess, err := st.GetSession("billing-orch")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.State != "running" {
		t.Errorf("state = %q, want running (untouched)", sess.State)
	}

	// A missing session is not an error either.
	if err := m.RetireAgentSession("ghost"); err != nil {
		t.Errorf("RetireAgentSession(ghost) = %v, want nil", err)
	}
}
