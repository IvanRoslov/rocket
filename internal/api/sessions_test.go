package api

import (
	"context"
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
func (sessFakeRuntime) Alive(ctx context.Context, h runtime.Handle) bool    { return true }
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

type sessFakeAgent struct{}

func (sessFakeAgent) Name() string                                 { return "fake" }
func (sessFakeAgent) Available() error                             { return nil }
func (sessFakeAgent) SetupWorkspace(spec agent.LaunchSpec) error   { return nil }
func (sessFakeAgent) LaunchCommand(spec agent.LaunchSpec) []string { return []string{"fake-agent"} }
func (sessFakeAgent) Env(spec agent.LaunchSpec) map[string]string  { return nil }
func (sessFakeAgent) Activity(ctx context.Context, ref agent.ActivityRef) (activity.State, time.Time, error) {
	return "", time.Time{}, agent.ErrNoSignal
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

func TestPostSessionProjectNotFound(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/sessions", map[string]any{
		"project": "nope", "repo": "repo1", "task": "mytask",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "project_not_found" {
		t.Errorf("code = %q, want project_not_found", eb.Error.Code)
	}
}

func TestPostSessionRepoNotInProject(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	addTestRepo(t, d, "other")
	if err := d.Store.AddProject(store.Project{ID: "myproj", Name: "myproj", MainRepo: "myrepo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/sessions", map[string]any{
		"project": "myproj", "repo": "other", "task": "mytask",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "repo_not_in_project" {
		t.Errorf("code = %q, want repo_not_in_project", eb.Error.Code)
	}
}

func TestPostSessionInvalidTask(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	if err := d.Store.AddProject(store.Project{ID: "myproj", Name: "myproj", MainRepo: "myrepo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/sessions", map[string]any{
		"project": "myproj", "repo": "myrepo", "task": "Not Valid!",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "invalid_task" {
		t.Errorf("code = %q, want invalid_task", eb.Error.Code)
	}
}

func TestPostSessionHappyPath(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	if err := d.Store.AddProject(store.Project{ID: "myproj", Name: "myproj", MainRepo: "myrepo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/sessions", map[string]any{
		"project": "myproj", "repo": "myrepo", "task": "mytask", "feature": "myfeat",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	if body["id"] != "myfeat-mytask" {
		t.Errorf("id = %v, want myfeat-mytask", body["id"])
	}
	if body["branch"] != "feature/myfeat/mytask" {
		t.Errorf("branch = %v, want feature/myfeat/mytask", body["branch"])
	}
	if body["worktree_path"] != "/fake/wt/myfeat-mytask" {
		t.Errorf("worktree_path = %v", body["worktree_path"])
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

	addTestRepo(t, d, "myrepo")
	if err := d.Store.AddProject(store.Project{ID: "myproj", Name: "myproj", MainRepo: "myrepo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	spawnResp := postJSON(t, srv.URL+"/v1/sessions", map[string]any{
		"project": "myproj", "repo": "myrepo", "task": "mytask",
	})
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
