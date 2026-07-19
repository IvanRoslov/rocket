package client

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func TestPidAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Error("expected the current process to report alive")
	}
	if pidAlive(999999) {
		t.Error("expected a made-up high pid to report dead")
	}
}

func TestReadAlivePid(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Home: dir}

	if _, ok := readAlivePid(cfg); ok {
		t.Error("expected ok=false when the pid file is missing")
	}

	if err := os.WriteFile(cfg.PidPath(), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	if pid, ok := readAlivePid(cfg); !ok || pid != os.Getpid() {
		t.Errorf("readAlivePid = (%d, %v), want (%d, true)", pid, ok, os.Getpid())
	}

	// A pid file naming a dead process must report ok=false, the same as a
	// missing one: this is what lets Connect fall through to spawning a
	// replacement daemon once the old one is actually gone.
	if err := os.WriteFile(cfg.PidPath(), []byte("999999"), 0600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	if _, ok := readAlivePid(cfg); ok {
		t.Error("expected ok=false for a pid file naming a dead process")
	}
}

// TestWaitForHealthyOrDeadReturnsFalseOncePidDies proves Connect's
// stale-daemon retry loop doesn't wait out its full timeout once the
// mid-shutdown daemon it's tracking has actually exited: it should notice
// promptly and let the caller move on to spawning a replacement.
func TestWaitForHealthyOrDeadReturnsFalseOncePidDies(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "nonexistent.sock"))

	cmd := exec.Command("sleep", "0.2")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()

	start := time.Now()
	if waitForHealthyOrDead(c, pid, 5*time.Second) {
		t.Fatal("expected false: nothing ever serves health on this socket")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("waitForHealthyOrDead took too long to notice pid death: %s", elapsed)
	}
}

// TestWaitForHealthyOrDeadReturnsTrueOnceHealthy proves the retry loop
// keeps polling (rather than giving up on the first failed health check)
// until the socket starts responding — the case where a mid-shutdown
// daemon finishes cleanly and a fresh one isn't needed at all.
func TestWaitForHealthyOrDeadReturnsTrueOnceHealthy(t *testing.T) {
	// os.MkdirTemp (not t.TempDir()) keeps the path short: unix domain
	// socket paths are capped at ~104 bytes on macOS (sockaddr_un.sun_path)
	// and t.TempDir()'s path embeds the (often long) test name, which
	// silently overflows that limit and makes net.Listen fail.
	dir, err := os.MkdirTemp("", "rktcli")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "test.sock")
	c := New(sockPath)

	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	go func() {
		time.Sleep(150 * time.Millisecond)
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			return
		}
		defer ln.Close()
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})}
		_ = srv.Serve(ln)
	}()

	if !waitForHealthyOrDead(c, cmd.Process.Pid, 5*time.Second) {
		t.Fatal("expected true once the socket starts responding healthy")
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
