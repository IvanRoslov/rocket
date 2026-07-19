package daemon

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/IvanRoslov/rocket/internal/config"
)

// shortHomeDir returns a fresh temp directory suitable for use as
// ROCKET_HOME in tests. It deliberately avoids t.TempDir(), whose path
// embeds the (often long) test name: unix domain socket paths are capped at
// ~104 bytes on macOS (sockaddr_un.sun_path), and cfg.SocketPath() easily
// exceeds that under t.TempDir(). os.MkdirTemp with a short prefix keeps the
// resulting rocket.sock path well under the limit.
func shortHomeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rkt")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// freeTestPort finds and returns an available ephemeral TCP port suitable for
// testing. This avoids port collisions with the live daemon or other tests.
func freeTestPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// waitForHealth polls the daemon's health endpoint until it responds ok or
// the timeout elapses, failing the test on timeout.
func waitForHealth(t *testing.T, cfg *config.Config, timeout time.Duration) {
	t.Helper()
	c := client.New(cfg.SocketPath())
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var out struct {
			Status string `json:"status"`
		}
		if err := c.Get("/v1/health", nil, &out); err == nil && out.Status == "ok" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("daemon did not become healthy within %s", timeout)
}

func TestRunServesHealthAndShutsDownCleanly(t *testing.T) {
	home := shortHomeDir(t)
	t.Setenv("ROCKET_HOME", home)
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Port = freeTestPort(t)

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(cfg)
	}()

	waitForHealth(t, cfg, 3*time.Second)

	// PID file should exist and contain our own pid.
	data, err := os.ReadFile(cfg.PidPath())
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("parse pid file: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid file = %d, want %d", pid, os.Getpid())
	}

	// Shut down via the API.
	c := client.New(cfg.SocketPath())
	if err := c.Post("/v1/shutdown", nil, nil); err != nil {
		t.Fatalf("POST /v1/shutdown: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after shutdown")
	}

	if _, err := os.Stat(cfg.PidPath()); !os.IsNotExist(err) {
		t.Errorf("pid file still exists after shutdown: err=%v", err)
	}
}

// TestRunHonorsSocketOverride proves that cfg.SocketOverride (as set by the
// CLI's --socket flag via loadConfig) actually changes where the daemon
// binds, not just what SocketPath() reports.
func TestRunHonorsSocketOverride(t *testing.T) {
	home := shortHomeDir(t)
	t.Setenv("ROCKET_HOME", home)
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Port = freeTestPort(t)

	overrideDir, err := os.MkdirTemp("", "rktsock")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(overrideDir) })
	cfg.SocketOverride = filepath.Join(overrideDir, "override.sock")

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(cfg)
	}()

	waitForHealth(t, cfg, 3*time.Second)

	if _, err := os.Stat(cfg.SocketOverride); err != nil {
		t.Errorf("override socket file not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "rocket.sock")); !os.IsNotExist(err) {
		t.Errorf("default socket path should not exist when override is set, err=%v", err)
	}

	c := client.New(cfg.SocketPath())
	if err := c.Post("/v1/shutdown", nil, nil); err != nil {
		t.Fatalf("POST /v1/shutdown: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after shutdown")
	}
}

func TestRunFailsWhenAlreadyRunning(t *testing.T) {
	home := shortHomeDir(t)
	t.Setenv("ROCKET_HOME", home)
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Port = freeTestPort(t)

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(cfg)
	}()
	waitForHealth(t, cfg, 3*time.Second)
	defer func() {
		c := client.New(cfg.SocketPath())
		_ = c.Post("/v1/shutdown", nil, nil)
		<-errCh
	}()

	if err := Run(cfg); err == nil {
		t.Fatal("expected second Run to fail, got nil")
	}
}

func TestRunRemovesStalePidFile(t *testing.T) {
	home := shortHomeDir(t)
	t.Setenv("ROCKET_HOME", home)
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Port = freeTestPort(t)

	// Write a pid file referencing a pid that (almost certainly) does not
	// exist, simulating a daemon that crashed without cleaning up.
	stalePid := []byte("999999")
	if err := os.WriteFile(cfg.PidPath(), stalePid, 0600); err != nil {
		t.Fatalf("write stale pid file: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(cfg)
	}()
	waitForHealth(t, cfg, 3*time.Second)

	c := client.New(cfg.SocketPath())
	if err := c.Post("/v1/shutdown", nil, nil); err != nil {
		t.Fatalf("POST /v1/shutdown: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after shutdown")
	}
}
