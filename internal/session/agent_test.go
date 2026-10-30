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

	sess, err := m.StartAgent(context.Background(), a, "")
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

func TestStartAgentWithProfileUsesAdapterAndSavesChoice(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	a := testAgentRow(t, "sre")
	if err := st.AddAgent(a); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	p := store.ModelProfile{Name: "deep", Agent: "fake", Model: "opus", Effort: "high", Enabled: true}
	if err := st.CreateModelProfile(p); err != nil {
		t.Fatalf("CreateModelProfile: %v", err)
	}

	sess, err := m.StartAgent(context.Background(), a, "deep")
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if len(testFakeAgent.launchCalls) != 1 {
		t.Fatalf("LaunchCommand calls = %d, want 1", len(testFakeAgent.launchCalls))
	}
	launch := testFakeAgent.launchCalls[0]
	if launch.Kind != AgentSessionKind || launch.SessionID != "sre" ||
		launch.WorktreePath != a.Dir || launch.Model != "opus" || launch.Effort != "high" ||
		launch.SystemPrompt != "" || launch.FirstMessage != "" {
		t.Errorf("launch spec = %+v", launch)
	}
	if len(testFakeAgent.setupCalls) != 0 {
		t.Errorf("SetupWorkspace calls = %d, want 0", len(testFakeAgent.setupCalls))
	}
	if len(rt.created) != 1 {
		t.Fatalf("runtime sessions created = %d, want 1", len(rt.created))
	}
	created := rt.created[0]
	if created.Command != "'fake-agent' '--session' 'sre' '--msg' ''" {
		t.Errorf("Command = %q, want fake adapter launch", created.Command)
	}
	if created.Env["FAKE_ENV"] != "1" || created.Env["ROCKET_SESSION_ID"] != "sre" || created.Env["ROCKET_SOCKET"] == "" {
		t.Errorf("Env = %+v", created.Env)
	}
	if sess.Agent != "fake" || sess.Profile != "deep" || sess.Model != "opus" || sess.Effort != "high" {
		t.Errorf("session snapshot = %+v", sess)
	}
	registered, err := st.GetAgent("sre")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if registered.LaunchProfile != "deep" {
		t.Errorf("LaunchProfile = %q, want deep", registered.LaunchProfile)
	}

	p.Model = "sonnet"
	p.Effort = "low"
	if err := st.UpdateModelProfile(p); err != nil {
		t.Fatalf("UpdateModelProfile: %v", err)
	}
	stored, err := st.GetSession("sre")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Profile != "deep" || stored.Model != "opus" || stored.Effort != "high" {
		t.Errorf("snapshot changed after profile edit: %+v", stored)
	}
}

func TestStartAgentReusesSavedProfileWithLatestValues(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	a := testAgentRow(t, "sre")
	a.LaunchProfile = "deep"
	if err := st.CreateModelProfile(store.ModelProfile{
		Name: "deep", Agent: "fake", Model: "sonnet", Effort: "low", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateModelProfile: %v", err)
	}

	sess, err := m.StartAgent(context.Background(), a, "")
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	launch := testFakeAgent.launchCalls[len(testFakeAgent.launchCalls)-1]
	if launch.Model != "sonnet" || launch.Effort != "low" {
		t.Errorf("launch model/effort = %q/%q, want sonnet/low", launch.Model, launch.Effort)
	}
	if sess.Agent != "fake" || sess.Profile != "deep" || sess.Model != "sonnet" || sess.Effort != "low" {
		t.Errorf("session snapshot = %+v", sess)
	}
}

func TestStartAgentRejectsUnknownOrDisabledProfile(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	a := testAgentRow(t, "sre")
	a.LaunchProfile = "old"
	if err := st.AddAgent(a); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	if err := st.CreateModelProfile(store.ModelProfile{Name: "off", Agent: "fake", Enabled: false}); err != nil {
		t.Fatalf("CreateModelProfile: %v", err)
	}

	_, err := m.StartAgent(context.Background(), a, "missing")
	assertValidationCode(t, err, "profile_not_found")
	_, err = m.StartAgent(context.Background(), a, "off")
	assertValidationCode(t, err, "profile_disabled")
	if len(rt.created) != 0 {
		t.Fatalf("runtime sessions created = %d, want 0", len(rt.created))
	}
	registered, getErr := st.GetAgent("sre")
	if getErr != nil {
		t.Fatalf("GetAgent: %v", getErr)
	}
	if registered.LaunchProfile != "old" {
		t.Errorf("LaunchProfile = %q, want unchanged old", registered.LaunchProfile)
	}

	a.LaunchProfile = "deleted"
	_, err = m.StartAgent(context.Background(), a, "")
	assertValidationCode(t, err, "profile_not_found")
	if len(rt.created) != 0 {
		t.Errorf("deleted saved profile fell back to legacy command")
	}
}

func TestAdoptAgentSessionIgnoresSavedLaunchProfile(t *testing.T) {
	m, _, _, _, _ := testManager(t)
	a := testAgentRow(t, "sre")
	a.LaunchProfile = "deep"

	sess, _, err := m.AdoptAgentSession(a)
	if err != nil {
		t.Fatalf("AdoptAgentSession: %v", err)
	}
	if sess.Agent != "claude-code" || sess.Profile != "" || sess.Model != "" || sess.Effort != "" {
		t.Errorf("adopted session = %+v, want legacy command detection and empty snapshot", sess)
	}
}

func TestAdoptAgentSessionPreservesProfileSnapshot(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	a := testAgentRow(t, "sre")
	if err := st.AddAgent(a); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	if err := st.CreateModelProfile(store.ModelProfile{
		Name: "deep", Agent: "fake", Model: "opus", Effort: "high", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateModelProfile: %v", err)
	}
	if _, err := m.StartAgent(context.Background(), a, "deep"); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}

	sess, fresh, err := m.AdoptAgentSession(a)
	if err != nil {
		t.Fatalf("AdoptAgentSession: %v", err)
	}
	if fresh {
		t.Error("fresh = true for an already-live profile session")
	}
	if sess.Agent != "fake" || sess.Profile != "deep" || sess.Model != "opus" || sess.Effort != "high" {
		t.Errorf("adopted profile session = %+v, want launch snapshot preserved", sess)
	}
	stored, err := st.GetSession("sre")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Agent != "fake" || stored.Profile != "deep" || stored.Model != "opus" || stored.Effort != "high" {
		t.Errorf("stored profile session = %+v, want launch snapshot preserved", stored)
	}
}

func TestStartAgentWithoutCommandRunsAShell(t *testing.T) {
	m, _, _, rt, _ := testManager(t)
	a := testAgentRow(t, "sre")
	a.Command = ""

	if _, err := m.StartAgent(context.Background(), a, ""); err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	if rt.created[0].Command != "" {
		t.Errorf("Command = %q, want empty (the runtime falls back to a shell)", rt.created[0].Command)
	}
}

func TestStartAgentRejectsMissingDirAndDisabled(t *testing.T) {
	m, _, _, _, _ := testManager(t)

	noDir := store.Agent{ID: "sre", Enabled: true}
	_, err := m.StartAgent(context.Background(), noDir, "")
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "agent_no_dir" {
		t.Fatalf("StartAgent without dir = %v, want agent_no_dir", err)
	}

	missing := store.Agent{ID: "sre", Dir: "/definitely/not/here", Enabled: true}
	_, err = m.StartAgent(context.Background(), missing, "")
	if !errors.As(err, &verr) || verr.Code != "agent_dir_missing" {
		t.Fatalf("StartAgent with a missing dir = %v, want agent_dir_missing", err)
	}

	disabled := testAgentRow(t, "sre")
	disabled.Enabled = false
	_, err = m.StartAgent(context.Background(), disabled, "")
	if !errors.As(err, &verr) || verr.Code != "agent_disabled" {
		t.Fatalf("StartAgent disabled = %v, want agent_disabled", err)
	}
}

func TestStartAgentRefusesToRestartALiveSession(t *testing.T) {
	m, _, _, rt, _ := testManager(t)
	a := testAgentRow(t, "sre")

	if _, err := m.StartAgent(context.Background(), a, ""); err != nil {
		t.Fatalf("first StartAgent: %v", err)
	}
	_, err := m.StartAgent(context.Background(), a, "")
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
	if _, err := m.StartAgent(context.Background(), a, ""); err != nil {
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

	sess, fresh, err := m.AdoptAgentSession(a)
	if err != nil {
		t.Fatalf("AdoptAgentSession: %v", err)
	}
	if sess.State != "running" || !fresh {
		t.Fatalf("adopted session = %+v, fresh = %v", sess, fresh)
	}
	_, fresh, err = m.AdoptAgentSession(a)
	if err != nil {
		t.Fatalf("second AdoptAgentSession: %v", err)
	}
	if fresh {
		t.Error("fresh = true on an already-live session, want false")
	}

	// A session of another kind under the same name is never overwritten.
	other := testAgentRow(t, "billing-orch")
	if err := st.AddSession(store.Session{
		ID: "billing-orch", Kind: "orchestrator", ProjectID: "", RepoID: "",
		Agent: "fake", TmuxName: "billing-orch", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if _, _, err := m.AdoptAgentSession(other); err == nil {
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

	if _, _, err := m.AdoptAgentSession(a); err != nil {
		t.Fatalf("AdoptAgentSession: %v", err)
	}
	if err := m.RetireAgentSession("sre"); err != nil {
		t.Fatalf("RetireAgentSession: %v", err)
	}
	if sess, _ := st.GetSession("sre"); sess.State != "done" {
		t.Fatalf("state after retire = %q, want done", sess.State)
	}

	_, fresh, err := m.AdoptAgentSession(a)
	if err != nil {
		t.Fatalf("re-adopt: %v", err)
	}
	if !fresh {
		t.Error("fresh = false on a revived session, want true")
	}
	sess, err := st.GetSession("sre")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.State != "running" {
		t.Errorf("state after re-adopt = %q, want running", sess.State)
	}
}

func TestAdoptAgentSessionTracksAdapterDetectedFromCommand(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	a := testAgentRow(t, "sre")
	a.Command = "kimi --auto -m kimi-code/k3.1-pro"

	sess, _, err := m.AdoptAgentSession(a)
	if err != nil {
		t.Fatalf("AdoptAgentSession: %v", err)
	}
	if sess.Agent != "kimi-code" {
		t.Errorf("adopted Agent = %q, want kimi-code", sess.Agent)
	}

	stored, err := st.GetSession("sre")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Agent != "kimi-code" {
		t.Errorf("stored Agent = %q, want kimi-code", stored.Agent)
	}
}

func TestAdoptAgentSessionFallsBackToDefaultAgent(t *testing.T) {
	m, st, _, _, _, cfg := testManagerWithConfig(t)
	a := testAgentRow(t, "sre")
	a.Command = "" // hand-made session: nothing to detect from

	if _, _, err := m.AdoptAgentSession(a); err != nil {
		t.Fatalf("AdoptAgentSession: %v", err)
	}
	stored, err := st.GetSession("sre")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Agent != cfg.DefaultAgent {
		t.Errorf("stored Agent = %q, want the configured default %q", stored.Agent, cfg.DefaultAgent)
	}
}

func TestAdoptAgentSessionReconcilesStaleAdapterOnLiveRow(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	a := testAgentRow(t, "sre") // Command: claude

	if _, _, err := m.AdoptAgentSession(a); err != nil {
		t.Fatalf("AdoptAgentSession: %v", err)
	}
	if stored, _ := st.GetSession("sre"); stored.Agent != "claude-code" {
		t.Fatalf("Agent after claude adopt = %q, want claude-code", stored.Agent)
	}

	// The human restarts the session on a different CLI: the registration's
	// command changes, the row stays live. The next adopt must reconcile
	// sessions.agent instead of leaving the old adapter on the row.
	a.Command = "kimi --auto"
	sess, fresh, err := m.AdoptAgentSession(a)
	if err != nil {
		t.Fatalf("re-adopt: %v", err)
	}
	if fresh {
		t.Error("fresh = true on a live session, want false")
	}
	if sess.Agent != "kimi-code" {
		t.Errorf("returned Agent = %q, want kimi-code", sess.Agent)
	}
	stored, err := st.GetSession("sre")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Agent != "kimi-code" {
		t.Errorf("stored Agent = %q, want kimi-code", stored.Agent)
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
