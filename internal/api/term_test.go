package api

import (
	"net/http"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

func TestParseControlResize(t *testing.T) {
	c, ok := parseControl([]byte(`{"type":"resize","cols":100,"rows":40}`))
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if c.Type != "resize" || c.Cols != 100 || c.Rows != 40 {
		t.Errorf("got %+v", c)
	}
}

func TestParseControlPing(t *testing.T) {
	c, ok := parseControl([]byte(`{"type":"ping"}`))
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if c.Type != "ping" {
		t.Errorf("got %+v", c)
	}
}

func TestParseControlGarbage(t *testing.T) {
	if _, ok := parseControl([]byte(`not json`)); ok {
		t.Fatalf("expected ok=false for garbage")
	}
	if _, ok := parseControl([]byte(`{}`)); ok {
		t.Fatalf("expected ok=false for missing type")
	}
	if _, ok := parseControl([]byte(`{"type":"bogus"}`)); ok {
		t.Fatalf("expected ok=false for unknown type")
	}
}

func TestSessionTermUnknownSession(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/sessions/nope/term")
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

func TestSessionTermDeadSession(t *testing.T) {
	d := sessionsTestDeps(t)
	srv := newTestServer(t, d)

	addTestRepo(t, d, "myrepo")
	if err := d.Store.AddProject(store.Project{ID: "myproj", Name: "myproj", MainRepo: "myrepo"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := d.Store.AddSession(store.Session{
		ID:           "dead-sess",
		Kind:         "worker",
		ProjectID:    "myproj",
		RepoID:       "myrepo",
		FeatureSlug:  "dead",
		Agent:        "fake",
		Branch:       "feature/dead/dead",
		WorktreePath: "/fake/wt/dead-sess",
		TmuxName:     "rocket-dead-sess",
		State:        "killed",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/sessions/dead-sess/term")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	eb := decodeErr(t, resp)
	if eb.Error.Code != "session_not_live" {
		t.Errorf("code = %q, want session_not_live", eb.Error.Code)
	}
}
