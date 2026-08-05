package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// addTestRepo registers a repo with the given id backed by a temp git repo,
// returning its path.
func addTestRepo(t *testing.T, d Deps, id string) string {
	t.Helper()
	repoDir := filepath.Join(t.TempDir(), id)
	initGitRepo(t, repoDir)
	if err := d.Store.AddRepo(store.Repo{ID: id, Path: repoDir, DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo %s: %v", id, err)
	}
	return repoDir
}

func patchJSON(t *testing.T, url string, payload any) *http.Response {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, url, bytesReader(b))
	if err != nil {
		t.Fatalf("build PATCH request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH %s: %v", url, err)
	}
	return resp
}

func deleteReq(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("build DELETE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", url, err)
	}
	return resp
}

func TestPostProjectHappyPath(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")

	resp := postJSON(t, srv.URL+"/v1/projects", map[string]any{"id": "myproj", "name": "My Proj", "main": "myrepo"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	if body["id"] != "myproj" {
		t.Errorf("id = %v, want myproj", body["id"])
	}
	if body["name"] != "My Proj" {
		t.Errorf("name = %v, want My Proj", body["name"])
	}
	if body["main"] != "myrepo" {
		t.Errorf("main = %v, want myrepo", body["main"])
	}
	tasks, ok := body["tasks"].(map[string]any)
	if !ok {
		t.Fatalf("tasks not a map: %v", body["tasks"])
	}
	for _, k := range []string{"backlog", "brainstorm", "in_progress", "review", "done"} {
		if tasks[k] != float64(0) {
			t.Errorf("tasks[%s] = %v, want 0", k, tasks[k])
		}
	}
	if body["live_sessions"] != float64(0) {
		t.Errorf("live_sessions = %v, want 0", body["live_sessions"])
	}
}

func TestPostProjectMissingMainRepo(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/projects", map[string]any{"id": "myproj", "main": "nope"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "repo_not_found" {
		t.Errorf("code = %q, want repo_not_found", eb.Error.Code)
	}
}

func TestPostProjectDuplicateID(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")

	postJSON(t, srv.URL+"/v1/projects", map[string]any{"id": "myproj", "main": "myrepo"}).Body.Close()

	resp := postJSON(t, srv.URL+"/v1/projects", map[string]any{"id": "myproj", "main": "myrepo"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "project_exists" {
		t.Errorf("code = %q, want project_exists", eb.Error.Code)
	}
}

func TestPostProjectLinkedDedupAndDropMain(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	addTestRepo(t, d, "other")

	resp := postJSON(t, srv.URL+"/v1/projects", map[string]any{
		"id":     "myproj",
		"main":   "myrepo",
		"linked": []string{"other", "other", "myrepo"},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	linked, ok := body["linked"].([]any)
	if !ok {
		t.Fatalf("linked not an array: %v", body["linked"])
	}
	if len(linked) != 1 || linked[0] != "other" {
		t.Errorf("linked = %v, want [other]", linked)
	}
}

func TestPostProjectDerivedIDFromName(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")

	resp := postJSON(t, srv.URL+"/v1/projects", map[string]any{"name": "My Cool Project!", "main": "myrepo"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeRepo(t, resp)
	if body["id"] != "my-cool-project" {
		t.Errorf("id = %v, want my-cool-project", body["id"])
	}
}

func TestGetProjectsListAggregates(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	postJSON(t, srv.URL+"/v1/projects", map[string]any{"id": "myproj", "main": "myrepo"}).Body.Close()

	if err := d.Store.AddSession(store.Session{
		ID: "sess1", Kind: "worker", ProjectID: "myproj", RepoID: "myrepo",
		FeatureSlug: "feat", Agent: "claude", Branch: "b", WorktreePath: "/tmp/wt",
		TmuxName: "tmux1", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/projects")
	if err != nil {
		t.Fatalf("GET /v1/projects: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0]["live_sessions"] != float64(1) {
		t.Errorf("live_sessions = %v, want 1", list[0]["live_sessions"])
	}
}

func TestGetProjectShowDetail(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	mainPath := addTestRepo(t, d, "myrepo")
	linkedPath := addTestRepo(t, d, "other")

	postJSON(t, srv.URL+"/v1/projects", map[string]any{
		"id": "myproj", "main": "myrepo", "linked": []string{"other"},
	}).Body.Close()

	resp, err := http.Get(srv.URL + "/v1/projects/myproj")
	if err != nil {
		t.Fatalf("GET /v1/projects/myproj: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	main, ok := body["main"].(map[string]any)
	if !ok {
		t.Fatalf("main not a map: %v", body["main"])
	}
	if main["path"] != mainPath {
		t.Errorf("main.path = %v, want %v", main["path"], mainPath)
	}

	linked, ok := body["linked"].([]any)
	if !ok || len(linked) != 1 {
		t.Fatalf("linked = %v, want 1 entry", body["linked"])
	}
	linkedRepo, ok := linked[0].(map[string]any)
	if !ok {
		t.Fatalf("linked[0] not a map: %v", linked[0])
	}
	if linkedRepo["path"] != linkedPath {
		t.Errorf("linked[0].path = %v, want %v", linkedRepo["path"], linkedPath)
	}
}

func TestGetProjectNotFound(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/projects/nope")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "project_not_found" {
		t.Errorf("code = %q, want project_not_found", eb.Error.Code)
	}
}

func TestPatchProjectRenameOnly(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	postJSON(t, srv.URL+"/v1/projects", map[string]any{"id": "myproj", "name": "Old", "main": "myrepo"}).Body.Close()

	resp := patchJSON(t, srv.URL+"/v1/projects/myproj", map[string]any{"name": "New Name"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	updated, err := d.Store.GetProject("myproj")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("name = %q, want New Name", updated.Name)
	}
	if updated.MainRepo != "myrepo" {
		t.Errorf("main untouched: got %q", updated.MainRepo)
	}
}

func TestPatchProjectRelinkValidation(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	postJSON(t, srv.URL+"/v1/projects", map[string]any{"id": "myproj", "main": "myrepo"}).Body.Close()

	resp := patchJSON(t, srv.URL+"/v1/projects/myproj", map[string]any{"linked": []string{"nope"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "repo_not_found" {
		t.Errorf("code = %q, want repo_not_found", eb.Error.Code)
	}
}

func TestPatchProjectUnknownID(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	resp := patchJSON(t, srv.URL+"/v1/projects/nope", map[string]any{"name": "x"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "project_not_found" {
		t.Errorf("code = %q, want project_not_found", eb.Error.Code)
	}
}

func TestDeleteProjectBusy(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	postJSON(t, srv.URL+"/v1/projects", map[string]any{"id": "myproj", "main": "myrepo"}).Body.Close()

	if err := d.Store.AddSession(store.Session{
		ID: "sess1", Kind: "worker", ProjectID: "myproj", RepoID: "myrepo",
		FeatureSlug: "feat", Agent: "claude", Branch: "b", WorktreePath: "/tmp/wt",
		TmuxName: "tmux1", State: "spawning",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	resp := deleteReq(t, srv.URL+"/v1/projects/myproj")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "project_busy" {
		t.Errorf("code = %q, want project_busy", eb.Error.Code)
	}
}

func TestDeleteProjectOK(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	postJSON(t, srv.URL+"/v1/projects", map[string]any{"id": "myproj", "main": "myrepo"}).Body.Close()

	resp := deleteReq(t, srv.URL+"/v1/projects/myproj")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if _, err := d.Store.GetProject("myproj"); err != store.ErrNotFound {
		t.Errorf("GetProject after delete: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteProjectNotFound(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	resp := deleteReq(t, srv.URL+"/v1/projects/nope")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "project_not_found" {
		t.Errorf("code = %q, want project_not_found", eb.Error.Code)
	}
}
