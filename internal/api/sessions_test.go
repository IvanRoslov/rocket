package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/activity"
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/IvanRoslov/rocket/internal/workspace"
)

// --- minimal fakes for the session.Manager's dependencies ------------------
// These mirror internal/session's own fakes but live here since the api
// package cannot import unexported test types from another package. They're
// intentionally bare-bones: sessions_test.go focuses on HTTP<->code mapping,
// not re-testing manager logic (already covered by internal/session).

type sessFakeRuntime struct{}

func (sessFakeRuntime) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{Name: spec.Name}, nil
}
func (sessFakeRuntime) Inject(ctx context.Context, h runtime.Handle, text string) error { return nil }
func (sessFakeRuntime) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return "", nil
}
func (sessFakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool { return true }
func (sessFakeRuntime) PinWindowSize(ctx context.Context, h runtime.Handle, clientCols, clientRows int) error {
	return nil
}

func (sessFakeRuntime) UnpinWindowSize(ctx context.Context, h runtime.Handle) error { return nil }

func (sessFakeRuntime) Destroy(ctx context.Context, h runtime.Handle) error { return nil }
func (sessFakeRuntime) AttachCommand(h runtime.Handle) []string {
	return []string{"tmux", "attach", "-t", h.Name}
}
func (sessFakeRuntime) List(ctx context.Context) ([]string, error) { return nil, nil }

type sessFakeWorkspace struct{}

func (sessFakeWorkspace) Create(ctx context.Context, repo store.Repo, sessionID, branch string) (workspace.CreateResult, error) {
	return workspace.CreateResult{Path: "/fake/wt/" + sessionID}, nil
}
func (sessFakeWorkspace) Restore(ctx context.Context, repo store.Repo, sessionID, branch string) (string, error) {
	return "/fake/wt/" + sessionID, nil
}
func (sessFakeWorkspace) Destroy(ctx context.Context, repo store.Repo, sessionID string) error {
	return nil
}
func (sessFakeWorkspace) List() ([]workspace.Entry, error) { return nil, nil }

// sessFakeRuntimeErrorOnCreate returns error from Create, used to test spawn
// failure scenarios.
type sessFakeRuntimeErrorOnCreate struct{}

func (sessFakeRuntimeErrorOnCreate) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.Handle, error) {
	return runtime.Handle{}, fmt.Errorf("fake runtime create failed")
}
func (sessFakeRuntimeErrorOnCreate) Inject(ctx context.Context, h runtime.Handle, text string) error {
	return nil
}
func (sessFakeRuntimeErrorOnCreate) Capture(ctx context.Context, h runtime.Handle, lines int) (string, error) {
	return "", nil
}
func (sessFakeRuntimeErrorOnCreate) Alive(ctx context.Context, h runtime.Handle) bool { return true }
func (sessFakeRuntimeErrorOnCreate) PinWindowSize(ctx context.Context, h runtime.Handle, clientCols, clientRows int) error {
	return nil
}

func (sessFakeRuntimeErrorOnCreate) UnpinWindowSize(ctx context.Context, h runtime.Handle) error {
	return nil
}

func (sessFakeRuntimeErrorOnCreate) Destroy(ctx context.Context, h runtime.Handle) error { return nil }
func (sessFakeRuntimeErrorOnCreate) AttachCommand(h runtime.Handle) []string {
	return []string{"tmux", "attach", "-t", h.Name}
}
func (sessFakeRuntimeErrorOnCreate) List(ctx context.Context) ([]string, error) { return nil, nil }

// sessFakeWorkspaceErrorOnCreate returns error from Create, used to test spawn
// failure scenarios.
type sessFakeWorkspaceErrorOnCreate struct{}

func (sessFakeWorkspaceErrorOnCreate) Create(ctx context.Context, repo store.Repo, sessionID, branch string) (workspace.CreateResult, error) {
	return workspace.CreateResult{}, fmt.Errorf("fake workspace create failed")
}
func (sessFakeWorkspaceErrorOnCreate) Restore(ctx context.Context, repo store.Repo, sessionID, branch string) (string, error) {
	return "/fake/wt/" + sessionID, nil
}
func (sessFakeWorkspaceErrorOnCreate) Destroy(ctx context.Context, repo store.Repo, sessionID string) error {
	return nil
}
func (sessFakeWorkspaceErrorOnCreate) List() ([]workspace.Entry, error) { return nil, nil }

type sessFakeAgent struct{}

func (sessFakeAgent) Name() string                                 { return "fake" }
func (sessFakeAgent) Available() error                             { return nil }
func (sessFakeAgent) SetupWorkspace(spec agent.LaunchSpec) error   { return nil }
func (sessFakeAgent) LaunchCommand(spec agent.LaunchSpec) []string { return []string{"fake-agent"} }
func (sessFakeAgent) Env(spec agent.LaunchSpec) map[string]string  { return nil }
func (sessFakeAgent) Activity(ctx context.Context, ref agent.ActivityRef) (activity.State, time.Time, error) {
	return "", time.Time{}, agent.ErrNoSignal
}
func (sessFakeAgent) TranscriptTail(ctx context.Context, ref agent.ActivityRef, cursor string) ([]agent.ChatEntry, string, error) {
	return nil, "", agent.ErrNoSignal
}
func (sessFakeAgent) TranscriptStat(ctx context.Context, ref agent.ActivityRef) (int64, int64, error) {
	return 0, 0, agent.ErrNoSignal
}

func init() {
	agent.Register("fake", func() agent.Agent { return sessFakeAgent{} })
}

// sessionsTestDeps builds Deps with a real store/bus on a temp SQLite db and
// a Manager wired to fake Runtime/Workspace, for tests that exercise the
// /v1/sessions endpoints.
func sessionsTestDeps(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	b := bus.New(st)
	cfg := &config.Config{Home: dir, DefaultAgent: "fake"}
	mgr := session.NewManager(st, b, sessFakeRuntime{}, sessFakeWorkspace{}, cfg)

	d := testDeps(t, nil)
	d.Store = st
	d.Bus = b
	d.Cfg = cfg
	d.Manager = mgr
	return d
}

// sessionsTestDepsWithErrorRuntime builds Deps like sessionsTestDeps but with
// a runtime that errors on Create, for testing spawn failure scenarios.
func sessionsTestDepsWithErrorRuntime(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	b := bus.New(st)
	cfg := &config.Config{Home: dir, DefaultAgent: "fake"}
	mgr := session.NewManager(st, b, sessFakeRuntimeErrorOnCreate{}, sessFakeWorkspace{}, cfg)

	d := testDeps(t, nil)
	d.Store = st
	d.Bus = b
	d.Cfg = cfg
	d.Manager = mgr
	return d
}

// sessionsTestDepsWithErrorWorkspace builds Deps like sessionsTestDeps but with
// a workspace that errors on Create, for testing spawn failure scenarios.
func sessionsTestDepsWithErrorWorkspace(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	b := bus.New(st)
	cfg := &config.Config{Home: dir, DefaultAgent: "fake"}
	mgr := session.NewManager(st, b, sessFakeRuntime{}, sessFakeWorkspaceErrorOnCreate{}, cfg)

	d := testDeps(t, nil)
	d.Store = st
	d.Bus = b
	d.Cfg = cfg
	d.Manager = mgr
	return d
}

// postJSONWithHeader is postJSON plus an optional X-Rocket-Session header
// (skipped when sessionID == "").
func postJSONWithHeader(t *testing.T, url, sessionID string, payload any) *http.Response {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytesReader(b))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set(sessionHeader, sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// seedOrchestratorWithTask seeds project+repo (via addTestProject(id=project)),
// a live orchestrator session on that project/feature, and a root task
// attached to it (status in_progress). Returns (orchestrator session id,
// root task id).
func seedOrchestratorWithTask(t *testing.T, d Deps, project, feature string) (string, int64) {
	t.Helper()
	addTestProject(t, d, project)

	orchID := feature + "-orch"
	sess := store.Session{
		ID: orchID, Kind: "orchestrator", ProjectID: project, RepoID: project + "-repo",
		FeatureSlug: feature, Agent: "fake", Branch: "orch/" + feature,
		TmuxName: orchID, State: "running",
	}
	if err := d.Store.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	taskID, err := d.Store.AddTask(store.Task{
		Title: "Root", ProjectID: project, SessionID: orchID, Status: "in_progress", FeatureSlug: feature,
	})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	return orchID, taskID
}

func TestPostSessionNoHeaderForbidden(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/sessions", map[string]any{"repo": "repo1", "task": "mytask"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "orchestrator_only" {
		t.Errorf("code = %q, want orchestrator_only", eb.Error.Code)
	}
}

func TestPostSessionUnknownSession(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", "nope", map[string]any{"repo": "repo1", "task": "mytask"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "unknown_session" {
		t.Errorf("code = %q, want unknown_session", eb.Error.Code)
	}
}

func TestPostSessionWorkerCallerForbidden(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "worker-1", "worker", "proj1")

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", "worker-1", map[string]any{"repo": "proj1-repo", "task": "mytask"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "orchestrator_only" {
		t.Errorf("code = %q, want orchestrator_only", eb.Error.Code)
	}
}

func TestPostSessionOrchestratorWithoutTaskConflict(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", "orch-1", map[string]any{"repo": "proj1-repo", "task": "mytask"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "no_task" {
		t.Errorf("code = %q, want no_task", eb.Error.Code)
	}
}

func TestPostSessionRepoNotInProject(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")
	addTestRepo(t, d, "other")

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", orchID, map[string]any{"repo": "other", "task": "mytask"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "repo_not_in_project" {
		t.Errorf("code = %q, want repo_not_in_project", eb.Error.Code)
	}
}

func TestPostSessionInvalidTask(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", orchID, map[string]any{"repo": "proj1-repo", "task": "Not Valid!"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "invalid_task" {
		t.Errorf("code = %q, want invalid_task", eb.Error.Code)
	}
}

func TestPostSessionHappyPathAutoSubtask(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, rootID := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", orchID,
		map[string]any{"repo": "proj1-repo", "task": "mytask", "prompt": "go do it"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body := decodeErr(t, resp)
		t.Fatalf("status = %d, want 201 (%+v)", resp.StatusCode, body)
	}
	body := decodeRepo(t, resp)
	if body["id"] != "myfeat-mytask" {
		t.Errorf("id = %v, want myfeat-mytask", body["id"])
	}
	if body["branch"] != "feature/myfeat/mytask" {
		t.Errorf("branch = %v, want feature/myfeat/mytask", body["branch"])
	}
	subID, ok := body["subtask_id"].(float64)
	if !ok || subID == 0 {
		t.Fatalf("subtask_id missing/invalid: %+v", body)
	}

	sub, err := d.Store.GetTask(int64(subID))
	if err != nil {
		t.Fatalf("GetTask(subtask): %v", err)
	}
	if sub.ParentID != rootID {
		t.Errorf("subtask ParentID = %d, want %d", sub.ParentID, rootID)
	}
	if sub.Title != "mytask" {
		t.Errorf("subtask Title = %q, want mytask", sub.Title)
	}
	if sub.SessionID != "myfeat-mytask" {
		t.Errorf("subtask SessionID = %q, want myfeat-mytask", sub.SessionID)
	}
	if sub.Status != "in_progress" {
		t.Errorf("subtask Status = %q, want in_progress", sub.Status)
	}
	if sub.CreatedBy != "orchestrator" {
		t.Errorf("subtask CreatedBy = %q, want orchestrator", sub.CreatedBy)
	}

	workerSess, err := d.Store.GetSession("myfeat-mytask")
	if err != nil {
		t.Fatalf("GetSession(worker): %v", err)
	}
	if workerSess.ParentID != orchID {
		t.Errorf("worker ParentID = %q, want %q", workerSess.ParentID, orchID)
	}
	if workerSess.Kind != "worker" {
		t.Errorf("worker Kind = %q, want worker", workerSess.Kind)
	}

	logs, err := d.Store.ListTaskLog(rootID, "status")
	if err != nil {
		t.Fatalf("ListTaskLog: %v", err)
	}
	found := false
	for _, e := range logs {
		if e.Body == "spawned worker myfeat-mytask for subtask #"+itoa(sub.ID) {
			found = true
		}
	}
	if !found {
		t.Errorf("root task log missing spawn entry: %+v", logs)
	}
}

func TestPostSessionExplicitSubtaskHappyPath(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, rootID := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	subID, err := d.Store.AddTask(store.Task{Title: "presub", ParentID: rootID, ProjectID: "proj1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", orchID,
		map[string]any{"repo": "proj1-repo", "task": "mytask", "subtask_id": subID})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body := decodeErr(t, resp)
		t.Fatalf("status = %d, want 201 (%+v)", resp.StatusCode, body)
	}
	body := decodeRepo(t, resp)
	if int64(body["subtask_id"].(float64)) != subID {
		t.Errorf("subtask_id = %v, want %d", body["subtask_id"], subID)
	}

	sub, err := d.Store.GetTask(subID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if sub.Status != "in_progress" {
		t.Errorf("subtask Status = %q, want in_progress (was backlog)", sub.Status)
	}
	if sub.SessionID == "" {
		t.Errorf("subtask SessionID not set")
	}
}

func TestPostSessionSubtaskWrongParent(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	// A root task belonging to a *different* orchestrator, with its own
	// subtask — not a child of orchID's root task.
	otherID, otherRootID := seedOrchestratorWithTask(t, d, "proj2", "otherfeat")
	_ = otherID
	subID, err := d.Store.AddTask(store.Task{Title: "other-sub", ParentID: otherRootID, ProjectID: "proj2"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", orchID,
		map[string]any{"repo": "proj1-repo", "task": "mytask", "subtask_id": subID})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "subtask_wrong_parent" {
		t.Errorf("code = %q, want subtask_wrong_parent", eb.Error.Code)
	}
}

func TestPostSessionSubtaskTaken(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, rootID := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	// A subtask already bound to a live worker session.
	if err := d.Store.AddSession(store.Session{
		ID: "myfeat-taken", Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
		FeatureSlug: "myfeat", Agent: "fake", Branch: "feature/myfeat/taken",
		TmuxName: "myfeat-taken", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	subID, err := d.Store.AddTask(store.Task{
		Title: "taken", ParentID: rootID, ProjectID: "proj1", SessionID: "myfeat-taken", Status: "in_progress",
	})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", orchID,
		map[string]any{"repo": "proj1-repo", "task": "mytask", "subtask_id": subID})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "subtask_taken" {
		t.Errorf("code = %q, want subtask_taken", eb.Error.Code)
	}
}

func TestPostSessionSubtaskNotFound(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", orchID,
		map[string]any{"repo": "proj1-repo", "task": "mytask", "subtask_id": 999999})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "subtask_not_found" {
		t.Errorf("code = %q, want subtask_not_found", eb.Error.Code)
	}
}

func TestKillSessionNotFound(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/sessions/nope/kill", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "session_not_found" {
		t.Errorf("code = %q, want session_not_found", eb.Error.Code)
	}
}

func TestRestoreSessionNotFound(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/sessions/nope/restore", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "session_not_found" {
		t.Errorf("code = %q, want session_not_found", eb.Error.Code)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/sessions/nope")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "session_not_found" {
		t.Errorf("code = %q, want session_not_found", eb.Error.Code)
	}
}

func TestKillThenRestoreSession(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	orchID, _ := seedOrchestratorWithTask(t, d, "myproj", "myfeat")

	spawnResp := postJSONWithHeader(t, srv.URL+"/v1/sessions", orchID,
		map[string]any{"repo": "myproj-repo", "task": "mytask"})
	body := decodeRepo(t, spawnResp)
	id := body["id"].(string)

	killResp := postJSON(t, srv.URL+"/v1/sessions/"+id+"/kill", nil)
	defer killResp.Body.Close()
	if killResp.StatusCode != http.StatusOK {
		t.Fatalf("kill status = %d, want 200", killResp.StatusCode)
	}

	restoreResp := postJSON(t, srv.URL+"/v1/sessions/"+id+"/restore", nil)
	defer restoreResp.Body.Close()
	if restoreResp.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d, want 200", restoreResp.StatusCode)
	}

	updated, err := d.Store.GetSession(id)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if updated.State != "running" {
		t.Errorf("State = %q, want running", updated.State)
	}
}

func TestKillCascadeKillsOrchestratorAndWorkers(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	for _, wid := range []string{"myfeat-w1", "myfeat-w2"} {
		if err := d.Store.AddSession(store.Session{
			ID: wid, Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
			FeatureSlug: "myfeat", ParentID: orchID, Agent: "fake",
			Branch: "feature/myfeat/" + wid, TmuxName: wid, State: "running",
		}); err != nil {
			t.Fatalf("AddSession %s: %v", wid, err)
		}
	}
	// An unrelated running session must survive the cascade.
	if err := d.Store.AddSession(store.Session{
		ID: "unrelated", Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
		FeatureSlug: "other", Agent: "fake", Branch: "feature/other/x",
		TmuxName: "unrelated", State: "running",
	}); err != nil {
		t.Fatalf("AddSession unrelated: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/sessions/"+orchID+"/kill?cascade=true", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	for _, id := range []string{orchID, "myfeat-w1", "myfeat-w2"} {
		sess, err := d.Store.GetSession(id)
		if err != nil {
			t.Fatalf("GetSession(%s): %v", id, err)
		}
		if sess.State != "killed" {
			t.Errorf("session %s state = %q, want killed", id, sess.State)
		}
	}

	unrelated, err := d.Store.GetSession("unrelated")
	if err != nil {
		t.Fatalf("GetSession(unrelated): %v", err)
	}
	if unrelated.State != "running" {
		t.Errorf("unrelated session state = %q, want running (unaffected by cascade)", unrelated.State)
	}
}

func TestKillSessionWorkerCannotKillSibling(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	for _, wid := range []string{"myfeat-w1", "myfeat-w2"} {
		if err := d.Store.AddSession(store.Session{
			ID: wid, Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
			FeatureSlug: "myfeat", ParentID: orchID, Agent: "fake",
			Branch: "feature/myfeat/" + wid, TmuxName: wid, State: "running",
		}); err != nil {
			t.Fatalf("AddSession %s: %v", wid, err)
		}
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions/myfeat-w2/kill", "myfeat-w1", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
	}

	sess, err := d.Store.GetSession("myfeat-w2")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.State != "running" {
		t.Errorf("state = %q, want running (kill must not have happened)", sess.State)
	}
}

func TestKillSessionWorkerCannotKillOrchestrator(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	if err := d.Store.AddSession(store.Session{
		ID: "myfeat-w1", Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
		FeatureSlug: "myfeat", ParentID: orchID, Agent: "fake",
		Branch: "feature/myfeat/w1", TmuxName: "myfeat-w1", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions/"+orchID+"/kill", "myfeat-w1", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
	}
}

func TestKillSessionOrchestratorCanKillOwnWorker(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	if err := d.Store.AddSession(store.Session{
		ID: "myfeat-w1", Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
		FeatureSlug: "myfeat", ParentID: orchID, Agent: "fake",
		Branch: "feature/myfeat/w1", TmuxName: "myfeat-w1", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions/myfeat-w1/kill", orchID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	sess, err := d.Store.GetSession("myfeat-w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.State != "killed" {
		t.Errorf("state = %q, want killed", sess.State)
	}
}

func TestKillSessionWorkerCanKillSelf(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	if err := d.Store.AddSession(store.Session{
		ID: "myfeat-w1", Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
		FeatureSlug: "myfeat", ParentID: orchID, Agent: "fake",
		Branch: "feature/myfeat/w1", TmuxName: "myfeat-w1", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions/myfeat-w1/kill", "myfeat-w1", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	sess, err := d.Store.GetSession("myfeat-w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.State != "killed" {
		t.Errorf("state = %q, want killed", sess.State)
	}
}

func TestKillSessionWorkerCannotCascadeKillSelf(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	if err := d.Store.AddSession(store.Session{
		ID: "myfeat-w1", Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
		FeatureSlug: "myfeat", ParentID: orchID, Agent: "fake",
		Branch: "feature/myfeat/w1", TmuxName: "myfeat-w1", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions/myfeat-w1/kill?cascade=true", "myfeat-w1", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestKillSessionOrchestratorCanCascadeKillSelf(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions/"+orchID+"/kill?cascade=true", orchID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRestoreSessionWorkerCannotRestoreSibling(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	for _, wid := range []string{"myfeat-w1", "myfeat-w2"} {
		if err := d.Store.AddSession(store.Session{
			ID: wid, Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
			FeatureSlug: "myfeat", ParentID: orchID, Agent: "fake",
			Branch: "feature/myfeat/" + wid, TmuxName: wid, State: "killed",
		}); err != nil {
			t.Fatalf("AddSession %s: %v", wid, err)
		}
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions/myfeat-w2/restore", "myfeat-w1", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestRestoreSessionOrchestratorCanRestoreOwnWorker(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)
	orchID, _ := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	if err := d.Store.AddSession(store.Session{
		ID: "myfeat-w1", Kind: "worker", ProjectID: "proj1", RepoID: "proj1-repo",
		FeatureSlug: "myfeat", ParentID: orchID, Agent: "fake",
		Branch: "feature/myfeat/w1", TmuxName: "myfeat-w1", State: "killed",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions/myfeat-w1/restore", orchID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestPostSessionSpawnFailureCancelsAutoSubtask(t *testing.T) {
	d := sessionsTestDepsWithErrorWorkspace(t)
	srv := newTestServer(t, d)
	orchID, rootID := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	// Spawn without subtask_id: auto-creates a subtask.
	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", orchID,
		map[string]any{"repo": "proj1-repo", "task": "mytask"})
	defer resp.Body.Close()

	// Spawn fails because workspace.Create errors.
	if resp.StatusCode < 400 {
		t.Fatalf("status = %d, want 4xx/5xx", resp.StatusCode)
	}

	// Verify the auto-created subtask was cancelled.
	tasks, err := d.Store.ListTasks(store.TaskFilter{ParentSet: true, Parent: rootID})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 auto-created subtask, got %d", len(tasks))
	}
	sub := tasks[0]
	if sub.Status != "cancelled" {
		t.Errorf("auto-created subtask status = %q, want cancelled", sub.Status)
	}
	if sub.SessionID != "" {
		t.Errorf("auto-created subtask SessionID should be empty but got %q", sub.SessionID)
	}
}

func TestPostSessionSpawnFailureLeavesExistingSubtask(t *testing.T) {
	d := sessionsTestDepsWithErrorWorkspace(t)
	srv := newTestServer(t, d)
	orchID, rootID := seedOrchestratorWithTask(t, d, "proj1", "myfeat")

	// Create a pre-existing subtask in backlog state.
	subID, err := d.Store.AddTask(store.Task{
		Title:     "presub",
		ParentID:  rootID,
		ProjectID: "proj1",
		Status:    "backlog",
	})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	// Spawn with subtask_id: uses the existing subtask.
	resp := postJSONWithHeader(t, srv.URL+"/v1/sessions", orchID,
		map[string]any{"repo": "proj1-repo", "task": "mytask", "subtask_id": subID})
	defer resp.Body.Close()

	// Spawn fails because workspace.Create errors.
	if resp.StatusCode < 400 {
		t.Fatalf("status = %d, want 4xx/5xx", resp.StatusCode)
	}

	// Verify the existing subtask was left untouched.
	sub, err := d.Store.GetTask(subID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if sub.Status != "backlog" {
		t.Errorf("subtask status = %q, want backlog (unchanged)", sub.Status)
	}
	if sub.SessionID != "" {
		t.Errorf("subtask SessionID should remain empty but got %q", sub.SessionID)
	}
}
