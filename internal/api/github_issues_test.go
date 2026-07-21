package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/store"
)

// githubIssuesStubDeps builds Deps whose GH factory points at an httptest
// stub serving the issues list endpoint.
func githubIssuesStubDeps(t *testing.T, body string) Deps {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(stub.Close)

	d := testDeps(t, nil)
	d.GH = func() (*github.Client, error) {
		return github.New(stub.URL, "test-token"), nil
	}
	return d
}

func TestGetGithubIssuesHappyPath(t *testing.T) {
	d := githubIssuesStubDeps(t, `[
		{"number":1,"title":"bug one","body":"repro","html_url":"https://github.com/acme/widgets/issues/1","state":"open","updated_at":"2026-01-01T00:00:00Z","labels":[{"name":"bug"}]},
		{"number":2,"title":"a pr","html_url":"https://github.com/acme/widgets/pull/2","state":"open","pull_request":{"url":"x"}}
	]`)
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/github/issues?repo=acme/widgets")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Issues []issueResponse `json:"issues"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1 (PR should be filtered)", len(body.Issues))
	}
	got := body.Issues[0]
	if got.Number != 1 || got.Title != "bug one" || len(got.Labels) != 1 || got.Labels[0] != "bug" {
		t.Errorf("unexpected issue: %+v", got)
	}
}

func TestGetGithubIssuesNoToken(t *testing.T) {
	d := testDeps(t, nil)
	d.GH = func() (*github.Client, error) {
		return nil, github.ErrNoToken
	}
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/github/issues?repo=acme/widgets")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "no_token" {
		t.Errorf("code = %q, want no_token", eb.Error.Code)
	}
}

func TestGetGithubIssuesNilGH(t *testing.T) {
	d := testDeps(t, nil)
	d.GH = nil
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/github/issues?repo=acme/widgets")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "no_token" {
		t.Errorf("code = %q, want no_token", eb.Error.Code)
	}
}

func TestGetGithubIssuesMissingRepoParam(t *testing.T) {
	d := githubIssuesStubDeps(t, `[]`)
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/github/issues")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "bad_request" {
		t.Errorf("code = %q, want bad_request", eb.Error.Code)
	}
}

func TestGetGithubIssuesBadRepoFormat(t *testing.T) {
	d := githubIssuesStubDeps(t, `[]`)
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/github/issues?repo=not-owner-slash-name")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetGithubIssuesUnknownRepoID(t *testing.T) {
	d := reposTestDeps(t)
	d.GH = func() (*github.Client, error) {
		return github.New("https://api.example.com", "test-token"), nil
	}
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/github/issues?repo_id=nope")
	if err != nil {
		t.Fatalf("GET: %v", err)
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

func TestGetGithubIssuesRepoIDResolvesFromOrigin(t *testing.T) {
	d := reposTestDeps(t)

	dir := t.TempDir()
	initGitRepo(t, dir)
	remote := filepath.Join(t.TempDir(), "bare.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", "https://github.com/acme/widgets.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}

	if err := d.Store.AddRepo(store.Repo{ID: "r1", Path: dir}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	var gotPath string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	t.Cleanup(stub.Close)
	d.GH = func() (*github.Client, error) {
		return github.New(stub.URL, "test-token"), nil
	}

	srv := newTestServer(t, d)
	resp, err := http.Get(srv.URL + "/v1/github/issues?repo_id=r1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if gotPath != "/repos/acme/widgets/issues" {
		t.Fatalf("stub path = %q, want /repos/acme/widgets/issues", gotPath)
	}
}
