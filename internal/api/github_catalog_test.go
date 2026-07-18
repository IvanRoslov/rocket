package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/IvanRoslov/rocket/internal/github"
)

// githubStubDeps builds Deps whose GH factory points at an httptest stub
// serving /user/repos, and returns a pointer to a counter incremented on
// every request the stub receives (used to assert cache hits/misses).
func githubStubDeps(t *testing.T, repos []github.Repo) (Deps, *int32) {
	t.Helper()
	resetCatalogCache()
	t.Cleanup(resetCatalogCache)

	var requests int32
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(repos)
	}))
	t.Cleanup(stub.Close)

	d := testDeps(t, nil)
	d.GH = func() (*github.Client, error) {
		return github.New(stub.URL, "test-token"), nil
	}
	return d, &requests
}

func TestGetGithubReposHappyPath(t *testing.T) {
	d, _ := githubStubDeps(t, []github.Repo{
		{FullName: "acme/one", Private: false, DefaultBranch: "main"},
		{FullName: "acme/two", Private: true, DefaultBranch: "trunk"},
	})
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/github/repos")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Repos []catalogRepoResponse `json:"repos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Repos) != 2 {
		t.Fatalf("len(repos) = %d, want 2", len(body.Repos))
	}
	if body.Repos[0].FullName != "acme/one" || body.Repos[1].DefaultBranch != "trunk" {
		t.Errorf("unexpected repos: %+v", body.Repos)
	}
}

func TestGetGithubReposCachesWithinTTL(t *testing.T) {
	d, requests := githubStubDeps(t, []github.Repo{{FullName: "acme/one"}})
	srv := newTestServer(t, d)

	for i := 0; i < 2; i++ {
		resp, err := http.Get(srv.URL + "/v1/github/repos")
		if err != nil {
			t.Fatalf("GET #%d: %v", i, err)
		}
		resp.Body.Close()
	}

	if got := atomic.LoadInt32(requests); got != 1 {
		t.Errorf("stub request count = %d, want 1 (second call should hit cache)", got)
	}
}

func TestGetGithubReposFilterQ(t *testing.T) {
	d, _ := githubStubDeps(t, []github.Repo{
		{FullName: "acme/widgets"},
		{FullName: "acme/gadgets"},
		{FullName: "other/widgets"},
	})
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/github/repos?q=ACME/w")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Repos []catalogRepoResponse `json:"repos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Repos) != 1 || body.Repos[0].FullName != "acme/widgets" {
		t.Errorf("filtered repos = %+v, want [acme/widgets]", body.Repos)
	}
}

func TestGetGithubReposNoToken(t *testing.T) {
	resetCatalogCache()
	t.Cleanup(resetCatalogCache)

	d := testDeps(t, nil)
	d.GH = func() (*github.Client, error) {
		return nil, github.ErrNoToken
	}
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/github/repos")
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
