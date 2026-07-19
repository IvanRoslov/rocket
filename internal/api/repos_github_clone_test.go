package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// initBareRepoWithCommit creates a bare git repo at bareDir (creating its
// parent directory if needed) containing a single commit on branch, by
// committing into a scratch working tree and cloning it --bare. This gives
// the bare repo a resolvable HEAD, so a later `git clone` of it populates
// refs/remotes/origin/HEAD the same way a real GitHub clone would.
func initBareRepoWithCommit(t *testing.T, bareDir, branch string) {
	t.Helper()

	workDir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", branch)
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "init")

	if err := os.MkdirAll(filepath.Dir(bareDir), 0o700); err != nil {
		t.Fatalf("mkdir parent of bare dir: %v", err)
	}
	cmd := exec.Command("git", "clone", "-q", "--bare", workDir, bareDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone --bare: %v\n%s", err, out)
	}
}

func TestPostRepoGithubClonesFromFileURL(t *testing.T) {
	d := reposTestDeps(t)
	reposDir := t.TempDir()
	d.Cfg.ReposDir = reposDir

	cloneRoot := t.TempDir()
	bareDir := filepath.Join(cloneRoot, "acme", "widgets.git")
	initBareRepoWithCommit(t, bareDir, "trunk")
	d.Cfg.GithubCloneBase = "file://" + cloneRoot + "/"

	events, cancel := d.Bus.Subscribe()
	defer cancel()

	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"github": "acme/widgets"})
	if resp.StatusCode != http.StatusCreated {
		body := decodeErr(t, resp)
		t.Fatalf("status = %d, want 201 (err=%+v)", resp.StatusCode, body)
	}
	body := decodeRepo(t, resp)

	if body["id"] != "widgets" {
		t.Errorf("id = %v, want widgets", body["id"])
	}
	wantPath := filepath.Join(reposDir, "acme__widgets")
	if body["path"] != wantPath {
		t.Errorf("path = %v, want %v", body["path"], wantPath)
	}
	if body["default_branch"] != "trunk" {
		t.Errorf("default_branch = %v, want trunk", body["default_branch"])
	}
	if _, err := os.Stat(filepath.Join(wantPath, ".git")); err != nil {
		t.Errorf("cloned repo missing .git: %v", err)
	}

	// The repo must actually be registered in the store.
	stored, err := d.Store.GetRepo("widgets")
	if err != nil {
		t.Fatalf("GetRepo(widgets): %v", err)
	}
	if stored.Path != wantPath {
		t.Errorf("stored path = %v, want %v", stored.Path, wantPath)
	}

	// Event chain: clone_started then clone_done, both tagged with the repo.
	var types []string
	for i := 0; i < 2; i++ {
		select {
		case e := <-events:
			types = append(types, e.Type)
			if e.Data["repo"] != "acme/widgets" {
				t.Errorf("event %s: data[repo] = %v, want acme/widgets", e.Type, e.Data["repo"])
			}
		default:
			t.Fatalf("expected 2 events, only got %d: %v", i, types)
		}
	}
	if len(types) != 2 || types[0] != "repo.clone_started" || types[1] != "repo.clone_done" {
		t.Errorf("event types = %v, want [repo.clone_started repo.clone_done]", types)
	}
}

func TestPostRepoGithubInvalidFormat(t *testing.T) {
	d := reposTestDeps(t)
	d.Cfg.ReposDir = t.TempDir()
	d.Cfg.GithubCloneBase = "file:///unused/"
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"github": "not-owner-slash-name"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "invalid_github_repo" {
		t.Errorf("code = %q, want invalid_github_repo", eb.Error.Code)
	}
}

func TestPostRepoGithubAndPathBothGiven(t *testing.T) {
	d := reposTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"github": "acme/widgets", "path": "/tmp/x"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "bad_request" {
		t.Errorf("code = %q, want bad_request", eb.Error.Code)
	}
}

func TestPostRepoGithubDuplicateID(t *testing.T) {
	d := reposTestDeps(t)
	reposDir := t.TempDir()
	d.Cfg.ReposDir = reposDir

	if err := d.Store.AddRepo(store.Repo{ID: "widgets", Path: "/tmp/existing", DefaultBranch: "main"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	cloneRoot := t.TempDir()
	bareDir := filepath.Join(cloneRoot, "acme", "widgets.git")
	initBareRepoWithCommit(t, bareDir, "main")
	d.Cfg.GithubCloneBase = "file://" + cloneRoot + "/"

	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"github": "acme/widgets"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "repo_exists" {
		t.Errorf("code = %q, want repo_exists", eb.Error.Code)
	}

	// Duplicate is caught before cloning: the target dir must not exist.
	if _, err := os.Stat(filepath.Join(reposDir, "acme__widgets")); !os.IsNotExist(err) {
		t.Errorf("target dir should not have been created, stat err = %v", err)
	}
}

func TestPostRepoGithubCloneFailure(t *testing.T) {
	d := reposTestDeps(t)
	reposDir := t.TempDir()
	d.Cfg.ReposDir = reposDir
	// Points at a base with nothing cloneable at acme/widgets.git.
	d.Cfg.GithubCloneBase = "file://" + t.TempDir() + "/"

	events, cancel := d.Bus.Subscribe()
	defer cancel()

	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"github": "acme/widgets"})
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	var eb struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&eb); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if eb.Error.Code != "clone_failed" {
		t.Errorf("code = %q, want clone_failed", eb.Error.Code)
	}

	if _, err := d.Store.GetRepo("widgets"); err != store.ErrNotFound {
		t.Errorf("repo should not have been registered, GetRepo err = %v", err)
	}

	select {
	case e := <-events:
		if e.Type != "repo.clone_started" {
			t.Errorf("first event type = %q, want repo.clone_started", e.Type)
		}
	default:
		t.Fatal("expected repo.clone_started event")
	}
	select {
	case e := <-events:
		if e.Type != "repo.clone_failed" {
			t.Errorf("second event type = %q, want repo.clone_failed", e.Type)
		}
		if msg, _ := e.Data["error"].(string); msg == "" {
			t.Error("repo.clone_failed event missing error data")
		}
	default:
		t.Fatal("expected repo.clone_failed event")
	}
}

func TestBuildCloneCmdKeepsTokenOutOfArgs(t *testing.T) {
	const token = "ghp_supersecrettoken123"

	cmd := buildCloneCmd(context.Background(), "", "acme", "widgets", "/tmp/target", token)

	for _, a := range cmd.Args {
		if strings.Contains(a, token) {
			t.Fatalf("cmd.Args contains token: %v", cmd.Args)
		}
	}
	wantURL := "https://github.com/acme/widgets.git"
	found := false
	for _, a := range cmd.Args {
		if a == wantURL {
			found = true
		}
	}
	if !found {
		t.Errorf("cmd.Args = %v, want to contain credential-free URL %q", cmd.Args, wantURL)
	}

	// The credential must instead travel via the process environment, not argv.
	var gotHeader string
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "GIT_CONFIG_VALUE_0=") {
			gotHeader = strings.TrimPrefix(e, "GIT_CONFIG_VALUE_0=")
		}
	}
	if !strings.HasPrefix(gotHeader, "Authorization: Basic ") {
		t.Fatalf("GIT_CONFIG_VALUE_0 = %q, want Authorization: Basic ...", gotHeader)
	}
	if strings.Contains(gotHeader, token) {
		t.Errorf("env header contains raw token unexpectedly encoded: %q", gotHeader)
	}
}

func TestBuildCloneCmdSkipsAuthForCloneBase(t *testing.T) {
	cmd := buildCloneCmd(context.Background(), "file:///tmp/bares/", "acme", "widgets", "/tmp/target", "ghp_sometoken")

	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "GIT_CONFIG_KEY_0=") || strings.HasPrefix(e, "GIT_CONFIG_VALUE_0=") {
			t.Errorf("cmd.Env should carry no auth header in clone-base test mode, got %v", cmd.Env)
		}
	}
}

func TestPostRepoGithubCloneFailureCleansUpPartialDir(t *testing.T) {
	d := reposTestDeps(t)
	reposDir := t.TempDir()
	d.Cfg.ReposDir = reposDir
	// Points at a base with nothing cloneable at acme/widgets.git.
	d.Cfg.GithubCloneBase = "file://" + t.TempDir() + "/"

	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/repos", map[string]string{"github": "acme/widgets"})
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	resp.Body.Close()

	targetDir := filepath.Join(reposDir, "acme__widgets")
	if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
		t.Errorf("target dir should have been removed after failed clone, stat err = %v", err)
	}
}

func TestSanitizeCloneErrorStripsToken(t *testing.T) {
	const token = "ghp_supersecrettoken123"
	raw := "fatal: unable to access 'https://x-access-token:" + token + "@github.com/acme/widgets.git/': Could not resolve host: github.com"

	got := sanitizeCloneError(raw, token)

	if strings.Contains(got, token) {
		t.Errorf("sanitized error still contains token: %q", got)
	}
	if !strings.Contains(got, "x-access-token:***@") {
		t.Errorf("sanitized error = %q, want to contain masked credential", got)
	}
}
