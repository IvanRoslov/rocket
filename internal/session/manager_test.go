package session

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/IvanRoslov/rocket/internal/workspace"
)

// --- fakes ---------------------------------------------------------------

type fakeRuntime struct {
	mu        sync.Mutex
	created   []runtime.CreateSpec
	destroyed []string
	aliveMap  map[string]bool
	listNames []string
}

func (f *fakeRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, spec)
	if f.aliveMap == nil {
		f.aliveMap = map[string]bool{}
	}
	f.aliveMap[spec.Name] = true
	return runtime.Handle{Name: spec.Name}, nil
}

func (f *fakeRuntime) Inject(ctx context.Context, h runtime.Handle, text string) error { return nil }

func (f *fakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return "fake output", nil
}

func (f *fakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.aliveMap[h.Name]
}

func (f *fakeRuntime) Destroy(ctx context.Context, h runtime.Handle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.destroyed = append(f.destroyed, h.Name)
	delete(f.aliveMap, h.Name)
	return nil
}

func (f *fakeRuntime) AttachCommand(h runtime.Handle) []string {
	return []string{"tmux", "attach", "-t", h.Name}
}

func (f *fakeRuntime) List(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listNames, nil
}

type workspaceCreateCall struct {
	repoID, sessionID, branch string
}

type fakeWorkspace struct {
	createErr    error
	createResult workspace.CreateResult
	createCalls  []workspaceCreateCall

	restorePath string
	restoreErr  error

	destroyed []string
}

func (f *fakeWorkspace) Create(ctx context.Context, repo store.Repo, sessionID, branch string) (workspace.CreateResult, error) {
	f.createCalls = append(f.createCalls, workspaceCreateCall{repo.ID, sessionID, branch})
	if f.createErr != nil {
		return workspace.CreateResult{}, f.createErr
	}
	res := f.createResult
	if res.Path == "" {
		res.Path = "/fake/wt/" + sessionID
	}
	return res, nil
}

func (f *fakeWorkspace) Restore(ctx context.Context, repo store.Repo, sessionID, branch string) (string, error) {
	if f.restoreErr != nil {
		return "", f.restoreErr
	}
	if f.restorePath != "" {
		return f.restorePath, nil
	}
	return "/fake/wt/" + sessionID, nil
}

func (f *fakeWorkspace) Destroy(ctx context.Context, repo store.Repo, sessionID string) error {
	f.destroyed = append(f.destroyed, sessionID)
	return nil
}

type fakeAgent struct {
	availableErr error
	setupErr     error
	setupCalls   []agent.LaunchSpec
	launchCalls  []agent.LaunchSpec
	envOverrides map[string]string // optional env overrides to merge with defaults
}

func (a *fakeAgent) Name() string { return "fake" }

func (a *fakeAgent) Available() error { return a.availableErr }

func (a *fakeAgent) SetupWorkspace(spec agent.LaunchSpec) error {
	a.setupCalls = append(a.setupCalls, spec)
	return a.setupErr
}

func (a *fakeAgent) LaunchCommand(spec agent.LaunchSpec) []string {
	a.launchCalls = append(a.launchCalls, spec)
	return []string{"fake-agent", "--session", spec.SessionID, "--msg", spec.FirstMessage}
}

func (a *fakeAgent) Env(spec agent.LaunchSpec) map[string]string {
	env := map[string]string{"FAKE_ENV": "1", "ROCKET_SESSION_ID": spec.SessionID}
	if a.envOverrides != nil {
		for k, v := range a.envOverrides {
			env[k] = v
		}
	}
	return env
}

// testFakeAgent is the instance returned by the "fake" agent builder
// registered below. Tests reassign it before calling Manager methods to
// control/inspect its behavior; tests in this package run sequentially so
// this package-level indirection is safe.
var testFakeAgent *fakeAgent

func init() {
	agent.Register("fake", func() agent.Agent {
		return testFakeAgent
	})
}

// --- test scaffolding ------------------------------------------------------

func testManager(t *testing.T) (*Manager, *store.Store, *bus.Bus, *fakeRuntime, *fakeWorkspace) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	b := bus.New(st)
	rt := &fakeRuntime{}
	ws := &fakeWorkspace{}
	cfg := &config.Config{Home: dir, DefaultAgent: "fake"}

	testFakeAgent = &fakeAgent{}

	m := NewManager(st, b, rt, ws, cfg)
	return m, st, b, rt, ws
}

func seedProjectRepo(t *testing.T, st *store.Store, projectID, repoID string) {
	t.Helper()
	if err := st.AddRepo(store.Repo{ID: repoID, Path: "/tmp/" + repoID, DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := st.AddProject(store.Project{ID: projectID, Name: projectID, MainRepo: repoID}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
}

// drainEvents collects events currently buffered on ch, without blocking.
func drainEvents(ch <-chan store.Event) []store.Event {
	var out []store.Event
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		case <-time.After(50 * time.Millisecond):
			return out
		}
	}
}

// --- Spawn -----------------------------------------------------------------

func TestSpawnHappyPath(t *testing.T) {
	m, st, b, rt, ws := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	ch, cancel := b.Subscribe()
	defer cancel()

	sess, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat",
		Prompt: "hello", AgentName: "fake",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if sess.ID != "myfeat-mytask" {
		t.Errorf("ID = %q, want myfeat-mytask", sess.ID)
	}
	if sess.Branch != "feature/myfeat/mytask" {
		t.Errorf("Branch = %q, want feature/myfeat/mytask", sess.Branch)
	}
	if sess.State != "running" {
		t.Errorf("State = %q, want running", sess.State)
	}
	if sess.WorktreePath != "/fake/wt/myfeat-mytask" {
		t.Errorf("WorktreePath = %q", sess.WorktreePath)
	}

	stored, err := st.GetSession("myfeat-mytask")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.State != "running" {
		t.Errorf("stored State = %q, want running", stored.State)
	}

	if len(ws.createCalls) != 1 {
		t.Fatalf("workspace.Create calls = %d, want 1", len(ws.createCalls))
	}
	if ws.createCalls[0].sessionID != "myfeat-mytask" || ws.createCalls[0].branch != "feature/myfeat/mytask" {
		t.Errorf("unexpected workspace.Create call: %+v", ws.createCalls[0])
	}

	if len(rt.created) != 1 {
		t.Fatalf("runtime.Create calls = %d, want 1", len(rt.created))
	}
	if rt.created[0].Name != "myfeat-mytask" {
		t.Errorf("runtime.Create Name = %q", rt.created[0].Name)
	}
	if rt.created[0].Env["FAKE_ENV"] != "1" {
		t.Errorf("runtime.Create Env missing agent env: %+v", rt.created[0].Env)
	}

	events := drainEvents(ch)
	var types []string
	for _, e := range events {
		types = append(types, e.Type)
	}
	wantOrder := []string{"session.spawned", "session.state_changed"}
	if len(types) != len(wantOrder) {
		t.Fatalf("event types = %v, want %v", types, wantOrder)
	}
	for i, want := range wantOrder {
		if types[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, types[i], want)
		}
	}
	if events[1].Data["from"] != "spawning" || events[1].Data["to"] != "running" {
		t.Errorf("state_changed data = %+v", events[1].Data)
	}
}

func TestSpawnNameCollisionInStoreGetsSuffix(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	if err := st.AddSession(store.Session{
		ID: "myfeat-mytask", Kind: "worker", ProjectID: "proj1", RepoID: "repo1",
		FeatureSlug: "myfeat", Agent: "fake", Branch: "feature/myfeat/mytask",
		WorktreePath: "/tmp/wt", TmuxName: "myfeat-mytask", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	sess, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat", AgentName: "fake",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if sess.ID != "myfeat-mytask-2" {
		t.Errorf("ID = %q, want myfeat-mytask-2", sess.ID)
	}
}

func TestSpawnNameCollisionInTmuxGetsSuffix(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	rt.listNames = []string{"myfeat-mytask"}

	sess, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat", AgentName: "fake",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if sess.ID != "myfeat-mytask-2" {
		t.Errorf("ID = %q, want myfeat-mytask-2", sess.ID)
	}
}

func TestSpawnConcurrentRaceRetriesOnErrExists(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	// Pre-seed the store with "feat-task" to simulate a concurrent spawn
	// winning the race between reserveName check and AddSession.
	if err := st.AddSession(store.Session{
		ID: "feat-task", Kind: "worker", ProjectID: "proj1", RepoID: "repo1",
		FeatureSlug: "feat", Agent: "fake", Branch: "feature/feat/task",
		WorktreePath: "/tmp/wt", TmuxName: "feat-task", State: "running",
	}); err != nil {
		t.Fatalf("pre-seed AddSession: %v", err)
	}

	// Spawn should retry and pick "feat-task-2" instead.
	sess, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "task", Feature: "feat", AgentName: "fake",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if sess.ID != "feat-task-2" {
		t.Errorf("ID = %q, want feat-task-2 (retried after collision)", sess.ID)
	}
}

func TestSpawnWorkspaceErrorMarksErrored(t *testing.T) {
	m, st, b, _, ws := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	ws.createErr = errors.New("boom")

	ch, cancel := b.Subscribe()
	defer cancel()

	_, err := m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", AgentName: "fake",
	})
	if err == nil {
		t.Fatal("Spawn: want error, got nil")
	}

	stored, gerr := st.GetSession("mytask-mytask")
	if gerr != nil {
		t.Fatalf("GetSession: %v", gerr)
	}
	if stored.State != "errored" {
		t.Errorf("State = %q, want errored", stored.State)
	}

	events := drainEvents(ch)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (spawned, state_changed)", len(events))
	}
	if events[1].Type != "session.state_changed" || events[1].Data["to"] != "errored" {
		t.Errorf("unexpected second event: %+v", events[1])
	}
}

func TestSpawnProjectNotFound(t *testing.T) {
	m, _, _, _, _ := testManager(t)
	_, err := m.Spawn(context.Background(), SpawnReq{Project: "nope", Repo: "repo1", Task: "mytask"})
	assertValidationCode(t, err, "project_not_found")
}

func TestSpawnRepoNotInProject(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	if err := st.AddRepo(store.Repo{ID: "other", Path: "/tmp/other", DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	_, err := m.Spawn(context.Background(), SpawnReq{Project: "proj1", Repo: "other", Task: "mytask"})
	assertValidationCode(t, err, "repo_not_in_project")
}

func TestSpawnInvalidTask(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	_, err := m.Spawn(context.Background(), SpawnReq{Project: "proj1", Repo: "repo1", Task: "Not Valid!"})
	assertValidationCode(t, err, "invalid_task")
}

func TestSpawnInvalidKind(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	_, err := m.Spawn(context.Background(), SpawnReq{Project: "proj1", Repo: "repo1", Task: "mytask", Kind: "orchestrator"})
	assertValidationCode(t, err, "invalid_kind")
}

func TestSpawnAgentUnknown(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	_, err := m.Spawn(context.Background(), SpawnReq{Project: "proj1", Repo: "repo1", Task: "mytask", AgentName: "nope"})
	assertValidationCode(t, err, "agent_unknown")
}

func TestSpawnAgentUnavailable(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	testFakeAgent.availableErr = errors.New("not installed")
	_, err := m.Spawn(context.Background(), SpawnReq{Project: "proj1", Repo: "repo1", Task: "mytask", AgentName: "fake"})
	assertValidationCode(t, err, "agent_unavailable")
}

func TestSpawnEnvMergeAgentOverridesRepo(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")

	// Set repo env with K=repo and R=repo.
	repo, err := st.GetRepo("repo1")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	repo.Env = map[string]string{"K": "repo", "R": "repo"}
	if err := st.UpdateRepo(repo); err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}

	// Create agent that overrides K with "agent" value to test merge.
	testFakeAgent = &fakeAgent{
		envOverrides: map[string]string{"K": "agent"},
	}

	_, err = m.Spawn(context.Background(), SpawnReq{
		Project: "proj1", Repo: "repo1", Task: "mytask", Feature: "myfeat",
		AgentName: "fake",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if len(rt.created) != 1 {
		t.Fatalf("runtime.Create calls = %d, want 1", len(rt.created))
	}

	env := rt.created[0].Env
	if env["K"] != "agent" {
		t.Errorf("env[K] = %q, want agent (agent should override repo)", env["K"])
	}
	if env["R"] != "repo" {
		t.Errorf("env[R] = %q, want repo (repo-only key should be present)", env["R"])
	}
	if env["FAKE_ENV"] != "1" {
		t.Errorf("env[FAKE_ENV] = %q, want 1 (agent env should be present)", env["FAKE_ENV"])
	}
}

func assertValidationCode(t *testing.T, err error, code string) {
	t.Helper()
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if verr.Code != code {
		t.Errorf("code = %q, want %q", verr.Code, code)
	}
}

// --- Kill --------------------------------------------------------------

func seedRunningSession(t *testing.T, st *store.Store, id string) store.Session {
	t.Helper()
	sess := store.Session{
		ID: id, Kind: "worker", ProjectID: "proj1", RepoID: "repo1",
		FeatureSlug: "myfeat", Agent: "fake", Branch: "feature/myfeat/mytask",
		WorktreePath: "/fake/wt/" + id, TmuxName: id, State: "running",
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	return sess
}

func TestKillWithoutCleanup(t *testing.T) {
	m, st, b, rt, ws := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Kill(context.Background(), "sess1", false); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	stored, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.State != "killed" {
		t.Errorf("State = %q, want killed", stored.State)
	}
	if len(rt.destroyed) != 1 || rt.destroyed[0] != "sess1" {
		t.Errorf("runtime.Destroy calls = %v", rt.destroyed)
	}
	if len(ws.destroyed) != 0 {
		t.Errorf("workspace.Destroy should not have been called: %v", ws.destroyed)
	}

	events := drainEvents(ch)
	if len(events) != 1 || events[0].Type != "session.killed" {
		t.Errorf("events = %v, want [session.killed]", events)
	}
}

func TestKillWithCleanup(t *testing.T) {
	m, st, b, rt, ws := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Kill(context.Background(), "sess1", true); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	if len(rt.destroyed) != 1 {
		t.Errorf("runtime.Destroy calls = %v", rt.destroyed)
	}
	if len(ws.destroyed) != 1 || ws.destroyed[0] != "sess1" {
		t.Errorf("workspace.Destroy calls = %v", ws.destroyed)
	}

	events := drainEvents(ch)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Type != "session.killed" || events[1].Type != "workspace.cleanup" {
		t.Errorf("unexpected event types: %v, %v", events[0].Type, events[1].Type)
	}
}

func TestKillIdempotentOnAlreadyKilled(t *testing.T) {
	m, st, b, rt, ws := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")

	if err := m.Kill(context.Background(), "sess1", false); err != nil {
		t.Fatalf("first Kill: %v", err)
	}

	ch, cancel := b.Subscribe()
	defer cancel()

	destroyedBefore := len(rt.destroyed)

	if err := m.Kill(context.Background(), "sess1", true); err != nil {
		t.Fatalf("second Kill: %v", err)
	}

	stored, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.State != "killed" {
		t.Errorf("State = %q, want killed (unchanged)", stored.State)
	}
	if len(rt.destroyed) != destroyedBefore {
		t.Errorf("runtime.Destroy should not be called again: %v", rt.destroyed)
	}
	if len(ws.destroyed) != 1 || ws.destroyed[0] != "sess1" {
		t.Errorf("cleanup should still run on idempotent kill: %v", ws.destroyed)
	}

	events := drainEvents(ch)
	if len(events) != 1 || events[0].Type != "workspace.cleanup" {
		t.Errorf("events = %v, want only [workspace.cleanup] (no repeated session.killed)", events)
	}
}

func TestKillUnknownSession(t *testing.T) {
	m, _, _, _, _ := testManager(t)
	err := m.Kill(context.Background(), "nope", false)
	assertValidationCode(t, err, "session_not_found")
}

// --- Restore -----------------------------------------------------------

func TestRestoreFromErrored(t *testing.T) {
	m, st, b, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	sess := seedRunningSession(t, st, "sess1")
	sess.State = "errored"
	if err := st.UpdateSession(sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.Restore(context.Background(), "sess1"); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	stored, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.State != "running" {
		t.Errorf("State = %q, want running", stored.State)
	}

	if len(rt.created) != 1 {
		t.Fatalf("runtime.Create calls = %d, want 1", len(rt.created))
	}

	events := drainEvents(ch)
	if len(events) != 1 || events[0].Type != "session.restored" {
		t.Errorf("events = %v, want [session.restored]", events)
	}

	if len(testFakeAgent.launchCalls) == 0 {
		t.Fatal("expected LaunchCommand to be called")
	}
	if testFakeAgent.launchCalls[len(testFakeAgent.launchCalls)-1].FirstMessage != "" {
		t.Errorf("restore should launch without FirstMessage")
	}
}

func TestRestoreRejectedWhenRunningAndAlive(t *testing.T) {
	m, st, _, rt, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")
	rt.mu.Lock()
	if rt.aliveMap == nil {
		rt.aliveMap = map[string]bool{}
	}
	rt.aliveMap["sess1"] = true
	rt.mu.Unlock()

	err := m.Restore(context.Background(), "sess1")
	assertValidationCode(t, err, "restore_not_allowed")
}

func TestRestoreUnknownSession(t *testing.T) {
	m, _, _, _, _ := testManager(t)
	err := m.Restore(context.Background(), "nope")
	assertValidationCode(t, err, "session_not_found")
}
