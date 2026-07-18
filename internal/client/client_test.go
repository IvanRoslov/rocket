package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
)

func TestConnectAutostartFalseOnDeadSocketReturnsErrDaemonUnavailable(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Home: dir}
	// No daemon listening on cfg.SocketPath(); autostart=false must fail fast.
	_, err := Connect(cfg, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrDaemonUnavailable) {
		t.Fatalf("expected ErrDaemonUnavailable, got %v", err)
	}
}

func TestAPIErrorFromEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code":    "bad_input",
				"message": "field X is required",
			},
		})
	}))
	defer srv.Close()

	sockPath := filepath.Join(t.TempDir(), "test.sock")
	_ = os.Remove(sockPath) // client doesn't need the socket for this test; we hit srv.URL directly via a plain http client wrapper.

	c := &Client{baseURL: srv.URL, http: srv.Client()}
	var out struct{}
	err := c.Get("/whatever", nil, &out)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "bad_input" {
		t.Errorf("code = %q, want bad_input", apiErr.Code)
	}
	if apiErr.Message != "field X is required" {
		t.Errorf("message = %q, want %q", apiErr.Message, "field X is required")
	}
}

// TestRequestTimeoutSpawnAndRestoreGetLongTimeout proves POST /v1/sessions
// (spawn) and POST .../restore requests get the long spawn-like timeout,
// since their handlers synchronously run `git fetch` + `worktree add` and
// can outlast the default 10s budget, while everything else keeps the
// default.
func TestRequestTimeoutSpawnAndRestoreGetLongTimeout(t *testing.T) {
	cases := []struct {
		method, path string
		want         time.Duration
	}{
		{http.MethodPost, "/v1/sessions", spawnRequestTimeout},
		{http.MethodPost, "/v1/sessions/myfeat-mytask/restore", spawnRequestTimeout},
		{http.MethodGet, "/v1/sessions", defaultRequestTimeout},
		{http.MethodPost, "/v1/sessions/myfeat-mytask/kill", defaultRequestTimeout},
		{http.MethodPost, "/v1/sessions/myfeat-mytask/kill?cleanup=true", defaultRequestTimeout},
		{http.MethodGet, "/v1/sessions/myfeat-mytask/attach", defaultRequestTimeout},
		{http.MethodPost, "/v1/repos", defaultRequestTimeout},
		{http.MethodPost, "/v1/projects", defaultRequestTimeout},
	}
	for _, c := range cases {
		got := requestTimeout(c.method, c.path)
		if got != c.want {
			t.Errorf("requestTimeout(%s, %q) = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}
