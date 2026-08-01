package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/roles"
	"github.com/IvanRoslov/rocket/internal/store"
)

// roleTestManager is testManager plus the config (role worktree paths are
// derived from cfg.WorktreesDir) and a workspace fake that materializes the
// directories it reports, so the "reuse an existing worktree" branch is
// exercised for real.
func roleTestManager(t *testing.T) (*Manager, *store.Store, *bus.Bus, *fakeRuntime, *fakeWorkspace, *config.Config) {
	t.Helper()
	m, st, b, rt, ws, cfg := testManagerWithConfig(t)
	// Report (and materialize) the same path the manager derives from the
	// config, so the create-once/reuse-afterwards decision is real.
	rolePath := filepath.Join(cfg.WorktreesDir, "repo1", "sre-agent")
	ws.createMkdir = true
	ws.createResult.Path = rolePath
	ws.restorePath = rolePath
	return m, st, b, rt, ws, cfg
}

// seedRole registers a project, its main repo and an enabled role with a
// prompt file on disk.
func seedRole(t *testing.T, st *store.Store, cfg *config.Config, roleID string) store.Agent {
	t.Helper()
	seedProjectRepo(t, st, "platform", "repo1")

	promptPath, err := roles.Ensure(cfg.Home, roleID, "TRIAGE POLICY", true)
	if err != nil {
		t.Fatalf("roles.Ensure: %v", err)
	}

	role := store.Agent{
		ID:         roleID,
		ProjectID:  "platform",
		PromptPath: promptPath,
		Agent:      "fake",
		Enabled:    true,
	}
	if err := st.AddAgent(role); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
	return role
}

func TestSpawnRoleCreatesInstanceInPersistentWorktree(t *testing.T) {
	m, st, _, rt, ws, cfg := roleTestManager(t)
	role := seedRole(t, st, cfg, "sre")

	sess, err := m.SpawnRole(context.Background(), role, "BRIEFING BODY")
	if err != nil {
		t.Fatalf("SpawnRole: %v", err)
	}

	if sess.ID != "sre-run-1" {
		t.Errorf("ID = %q, want sre-run-1", sess.ID)
	}
	if sess.Kind != "agent" {
		t.Errorf("Kind = %q, want agent", sess.Kind)
	}
	if sess.Branch != "agent/sre" {
		t.Errorf("Branch = %q, want agent/sre", sess.Branch)
	}
	if sess.FeatureSlug != "sre" {
		t.Errorf("FeatureSlug = %q, want sre", sess.FeatureSlug)
	}
	if sess.State != "running" {
		t.Errorf("State = %q, want running", sess.State)
	}

	if len(ws.createCalls) != 1 {
		t.Fatalf("workspace.Create calls = %d, want 1", len(ws.createCalls))
	}
	if got := ws.createCalls[0].sessionID; got != "sre-agent" {
		t.Errorf("worktree name = %q, want sre-agent", got)
	}
	if got := ws.createCalls[0].branch; got != "agent/sre" {
		t.Errorf("worktree branch = %q, want agent/sre", got)
	}
	wantPath := filepath.Join(cfg.WorktreesDir, "repo1", "sre-agent")
	if sess.WorktreePath != wantPath {
		t.Errorf("WorktreePath = %q, want %q", sess.WorktreePath, wantPath)
	}

	if len(testFakeAgent.launchCalls) != 1 {
		t.Fatalf("LaunchCommand calls = %d, want 1", len(testFakeAgent.launchCalls))
	}
	spec := testFakeAgent.launchCalls[0]
	if spec.FirstMessage != "BRIEFING BODY" {
		t.Errorf("FirstMessage = %q, want the briefing", spec.FirstMessage)
	}
	if !strings.Contains(spec.SystemPrompt, "TRIAGE POLICY") {
		t.Errorf("system prompt does not carry the role prompt:\n%s", spec.SystemPrompt)
	}
	if !strings.Contains(spec.SystemPrompt, "sre-run-1") {
		t.Errorf("system prompt does not carry the session id:\n%s", spec.SystemPrompt)
	}
	if len(rt.created) != 1 || rt.created[0].Name != "sre-run-1" {
		t.Fatalf("runtime.Create = %+v, want one sre-run-1", rt.created)
	}
}

func TestSpawnRoleReusesWorktreeAndIncrementsRunNumber(t *testing.T) {
	m, st, _, _, ws, cfg := roleTestManager(t)
	role := seedRole(t, st, cfg, "sre")

	first, err := m.SpawnRole(context.Background(), role, "one")
	if err != nil {
		t.Fatalf("SpawnRole #1: %v", err)
	}
	if err := m.Kill(context.Background(), first.ID, false); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	second, err := m.SpawnRole(context.Background(), role, "two")
	if err != nil {
		t.Fatalf("SpawnRole #2: %v", err)
	}

	if second.ID != "sre-run-2" {
		t.Errorf("second run id = %q, want sre-run-2", second.ID)
	}
	if len(ws.createCalls) != 1 {
		t.Errorf("workspace.Create calls = %d, want 1 (worktree must be reused)", len(ws.createCalls))
	}
	if ws.restoreCalls != 1 {
		t.Errorf("workspace.Restore calls = %d, want 1", ws.restoreCalls)
	}
	if second.WorktreePath != first.WorktreePath {
		t.Errorf("worktree changed between runs: %q vs %q", first.WorktreePath, second.WorktreePath)
	}
	if len(ws.destroyed) != 0 {
		t.Errorf("role worktree must survive a kill, destroyed: %v", ws.destroyed)
	}
}

func TestSpawnRoleRefusesSecondLiveInstance(t *testing.T) {
	m, st, _, _, _, cfg := roleTestManager(t)
	role := seedRole(t, st, cfg, "sre")

	if _, err := m.SpawnRole(context.Background(), role, "one"); err != nil {
		t.Fatalf("SpawnRole #1: %v", err)
	}

	_, err := m.SpawnRole(context.Background(), role, "two")
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "instance_live" {
		t.Fatalf("SpawnRole #2 error = %v, want ValidationError instance_live", err)
	}
}

func TestSpawnRoleRejectsDisabledRole(t *testing.T) {
	m, st, _, _, _, cfg := roleTestManager(t)
	role := seedRole(t, st, cfg, "sre")
	role.Enabled = false

	_, err := m.SpawnRole(context.Background(), role, "hi")
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != "agent_disabled" {
		t.Fatalf("SpawnRole error = %v, want ValidationError agent_disabled", err)
	}
}

func TestLiveRoleInstanceFindsRunningSession(t *testing.T) {
	m, st, _, _, _, cfg := roleTestManager(t)
	role := seedRole(t, st, cfg, "sre")

	if _, ok, err := m.LiveRoleInstance("sre"); err != nil || ok {
		t.Fatalf("LiveRoleInstance before spawn = (%v, %v), want (false, nil)", ok, err)
	}

	sess, err := m.SpawnRole(context.Background(), role, "hi")
	if err != nil {
		t.Fatalf("SpawnRole: %v", err)
	}

	live, ok, err := m.LiveRoleInstance("sre")
	if err != nil || !ok || live.ID != sess.ID {
		t.Fatalf("LiveRoleInstance = (%+v, %v, %v), want %s", live, ok, err, sess.ID)
	}

	if err := m.Kill(context.Background(), sess.ID, false); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, ok, err := m.LiveRoleInstance("sre"); err != nil || ok {
		t.Fatalf("LiveRoleInstance after kill = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestRoleWorktreeNameAndBranch(t *testing.T) {
	if got := RoleWorktreeName("sre"); got != "sre-agent" {
		t.Errorf("RoleWorktreeName = %q, want sre-agent", got)
	}
	if got := RoleBranch("sre"); got != "agent/sre" {
		t.Errorf("RoleBranch = %q, want agent/sre", got)
	}
}

// ensure the role worktree directory really is created by the fake, so the
// reuse branch above is not vacuously true.
func TestRoleFakeWorkspaceMaterializesPath(t *testing.T) {
	m, st, _, _, _, cfg := roleTestManager(t)
	role := seedRole(t, st, cfg, "sre")

	if _, err := m.SpawnRole(context.Background(), role, "hi"); err != nil {
		t.Fatalf("SpawnRole: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.WorktreesDir, "repo1", "sre-agent")); err != nil {
		t.Fatalf("role worktree dir not created: %v", err)
	}
}
