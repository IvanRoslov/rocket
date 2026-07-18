package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStaticIndex verifies the embedded dashboard index page is served at
// the root path.
func TestStaticIndex(t *testing.T) {
	d := testDeps(t, nil)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html prefix", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "<title>") {
		t.Errorf("body does not look like html: %s", body)
	}
}

// TestStaticSPAFallback verifies that unmatched client-side routes (paths
// without a file extension, outside /v1) fall back to index.html so the
// SPA router can take over.
func TestStaticSPAFallback(t *testing.T) {
	d := testDeps(t, nil)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/p/billing/tasks/12")
	if err != nil {
		t.Fatalf("GET /p/billing/tasks/12: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "<title>") {
		t.Errorf("body does not look like index.html: %s", body)
	}
}

// TestStaticUnknownV1Returns404JSON verifies /v1 paths that aren't matched
// by any registered route still get the standard JSON 404 error shape, not
// the SPA fallback.
func TestStaticUnknownV1Returns404JSON(t *testing.T) {
	d := testDeps(t, nil)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/definitely-missing")
	if err != nil {
		t.Fatalf("GET /v1/definitely-missing: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json prefix", ct)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", body.Error.Code)
	}
}

// TestStaticMissingAssetReturns404 verifies that a request for a
// nonexistent static asset (with a file extension) returns a plain 404
// rather than falling back to index.html.
func TestStaticMissingAssetReturns404(t *testing.T) {
	d := testDeps(t, nil)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/assets/nope.js")
	if err != nil {
		t.Fatalf("GET /assets/nope.js: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestRealV1RoutesStillWork verifies that registering the static
// catch-all at "/" doesn't shadow real, more specific /v1 routes.
func TestRealV1RoutesStillWork(t *testing.T) {
	d := testDeps(t, nil)
	srv := httptest.NewServer(NewHandler(d))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/health")
	if err != nil {
		t.Fatalf("GET /v1/health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
