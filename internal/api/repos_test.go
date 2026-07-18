package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// newTestServer starts an httptest.Server backed by NewHandler(d), closed
// automatically at the end of the test.
func newTestServer(t *testing.T, d Deps) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewHandler(d))
	t.Cleanup(srv.Close)
	return srv
}

func bytesReader(b []byte) io.Reader {
	return bytes.NewReader(b)
}

// reposTestDeps builds Deps with a real store on a temp SQLite db, for tests
// that exercise the /v1/repos endpoints.
func reposTestDeps(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	d := testDeps(t, nil)
	d.Store = st
	return d
}

// initGitRepo creates a bare-minimum git repository at dir (via `git init`)
// so path validation (".git must exist") succeeds.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", dir, err, out)
	}
}

func decodeRepo(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func decodeErr(t *testing.T, resp *http.Response) errBody {
	t.Helper()
	defer resp.Body.Close()
	var body errBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return body
}

func postJSON(t *testing.T, url string, payload any) *http.Response {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytesReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func TestPostRepoHappyPathDefaultBranchFallback(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	repoDir := filepath.Join(t.TempDir(), "myrepo")
	initGitRepo(t, repoDir)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"path": repoDir})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeRepo(t, resp)

	if body["id"] != "myrepo" {
		t.Errorf("id = %v, want myrepo", body["id"])
	}
	if body["path"] != repoDir {
		t.Errorf("path = %v, want %v", body["path"], repoDir)
	}
	if body["default_branch"] != "main" {
		t.Errorf("default_branch = %v, want main (fallback)", body["default_branch"])
	}
}

func TestPostRepoIDNormalizedFromDirName(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	repoDir := filepath.Join(t.TempDir(), "My Repo_Name!!")
	initGitRepo(t, repoDir)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"path": repoDir})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeRepo(t, resp)

	if body["id"] != "my-repo-name" {
		t.Errorf("id = %v, want my-repo-name", body["id"])
	}
}

func TestPostRepoExplicitInvalidID(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	repoDir := filepath.Join(t.TempDir(), "myrepo")
	initGitRepo(t, repoDir)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"path": repoDir, "id": "Not Valid!"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "invalid_id" {
		t.Errorf("code = %q, want invalid_id", eb.Error.Code)
	}
}

func TestPostRepoDerivedIDEmpty(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	repoDir := filepath.Join(t.TempDir(), "___")
	initGitRepo(t, repoDir)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"path": repoDir})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "invalid_id" {
		t.Errorf("code = %q, want invalid_id", eb.Error.Code)
	}
}

func TestPostRepoNonexistentPath(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"path": filepath.Join(t.TempDir(), "nope")})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "repo_path_invalid" {
		t.Errorf("code = %q, want repo_path_invalid", eb.Error.Code)
	}
}

func TestPostRepoPathWithoutGit(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	dir := t.TempDir()
	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"path": dir})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "repo_path_invalid" {
		t.Errorf("code = %q, want repo_path_invalid", eb.Error.Code)
	}
}

func TestPostRepoDuplicateID(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	repoDir := filepath.Join(t.TempDir(), "myrepo")
	initGitRepo(t, repoDir)

	resp1 := postJSON(t, srv.URL+"/v1/repos", map[string]string{"path": repoDir})
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first POST status = %d, want 201", resp1.StatusCode)
	}
	resp1.Body.Close()

	repoDir2 := filepath.Join(t.TempDir(), "myrepo")
	initGitRepo(t, repoDir2)

	resp2 := postJSON(t, srv.URL+"/v1/repos", map[string]string{"path": repoDir2})
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp2.StatusCode)
	}
	eb := decodeErr(t, resp2)
	if eb.Error.Code != "repo_exists" {
		t.Errorf("code = %q, want repo_exists", eb.Error.Code)
	}
}

func TestGetReposList(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	repoDir := filepath.Join(t.TempDir(), "myrepo")
	initGitRepo(t, repoDir)
	postJSON(t, srv.URL+"/v1/repos", map[string]string{"path": repoDir}).Body.Close()

	resp, err := http.Get(srv.URL + "/v1/repos")
	if err != nil {
		t.Fatalf("GET /v1/repos: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var repos []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("len(repos) = %d, want 1", len(repos))
	}
	if repos[0]["id"] != "myrepo" {
		t.Errorf("id = %v, want myrepo", repos[0]["id"])
	}
}

func TestPatchRepoPartialUpdate(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	if err := d.Store.AddRepo(store.Repo{
		ID:            "myrepo",
		Path:          "/tmp/myrepo",
		DefaultBranch: "main",
		Symlinks:      []string{"config.yaml"},
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/v1/repos/myrepo",
		bytesReader([]byte(`{"env":{"FOO":"bar"}}`)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	updated, err := d.Store.GetRepo("myrepo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if updated.Env["FOO"] != "bar" {
		t.Errorf("env[FOO] = %q, want bar", updated.Env["FOO"])
	}
	if len(updated.Symlinks) != 1 || updated.Symlinks[0] != "config.yaml" {
		t.Errorf("symlinks = %v, want untouched [config.yaml]", updated.Symlinks)
	}
}

func TestPatchRepoUnknownID(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/v1/repos/nope",
		bytesReader([]byte(`{"env":{"FOO":"bar"}}`)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "repo_not_found" {
		t.Errorf("code = %q, want repo_not_found", eb.Error.Code)
	}
}

func TestDeleteRepoInUse(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	if err := d.Store.AddRepo(store.Repo{ID: "myrepo", Path: "/tmp/myrepo", DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := d.Store.AddProject(store.Project{ID: "proj1", Name: "proj1", MainRepo: "myrepo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/v1/repos/myrepo", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "repo_in_use" {
		t.Errorf("code = %q, want repo_in_use", eb.Error.Code)
	}
}

func TestDeleteRepoOK(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	if err := d.Store.AddRepo(store.Repo{ID: "myrepo", Path: "/tmp/myrepo", DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/v1/repos/myrepo", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "deleted" {
		t.Errorf("status = %q, want deleted", body.Status)
	}

	if _, err := d.Store.GetRepo("myrepo"); err != store.ErrNotFound {
		t.Errorf("GetRepo after delete: err = %v, want ErrNotFound", err)
	}
}
