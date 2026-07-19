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

func TestValidResizeBounds(t *testing.T) {
	cases := []struct {
		name       string
		cols, rows int
		want       bool
	}{
		{"min valid", 1, 1, true},
		{"max valid", 4096, 4096, true},
		{"typical", 100, 40, true},
		{"zero cols", 0, 40, false},
		{"zero rows", 100, 0, false},
		{"negative cols", -1, 40, false},
		{"negative rows", 100, -1, false},
		{"cols too large", 4097, 40, false},
		{"rows too large", 100, 4097, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validResize(tc.cols, tc.rows); got != tc.want {
				t.Errorf("validResize(%d, %d) = %v, want %v", tc.cols, tc.rows, got, tc.want)
			}
		})
	}
}

func TestParseControlResizeOutOfBounds(t *testing.T) {
	// parseControl only validates JSON shape/type; bounds checking is done
	// separately via validResize so out-of-range resize frames still parse
	// ok=true and are rejected downstream instead of killing the connection.
	c, ok := parseControl([]byte(`{"type":"resize","cols":-1,"rows":999999}`))
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if validResize(c.Cols, c.Rows) {
		t.Errorf("expected validResize to reject cols=%d rows=%d", c.Cols, c.Rows)
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

// TestTermClaimsRefcount verifies the pin-refcount semantics: with two
// writer terminals on the same session, closing one must NOT release the
// window pin (the survivor still owns the size); only the last close
// releases. Sessions are counted independently.
func TestTermClaimsRefcount(t *testing.T) {
	c := newTermClaims()

	c.claim("a")
	c.claim("a")
	c.claim("b")

	if c.release("a") {
		t.Fatalf("first of two releases for 'a' must not be last")
	}
	if !c.release("a") {
		t.Fatalf("second release for 'a' must be last")
	}
	if !c.release("b") {
		t.Fatalf("sole release for 'b' must be last")
	}
	// Releasing beyond zero (defensive) still reports last and does not
	// underflow into negative counts.
	if !c.release("a") {
		t.Fatalf("release of unclaimed session must report last")
	}
	c.claim("a")
	if !c.release("a") {
		t.Fatalf("claim after over-release must behave normally")
	}
}
