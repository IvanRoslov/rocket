package daemon

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
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

// TestRunWithForeignHomeCannotKillLiveSocket reproduces the 2026-08-04
// outage: a daemon started with its own ROCKET_HOME (so claimPidFile sees
// no conflict and lets it through) but with the socket path of a live
// daemon — which is what happens when --socket or the inherited
// $ROCKET_SOCKET points at another instance. The pid file cannot guard
// that; the socket claim must. The intruders have to fail while the
// leader's socket keeps answering.
func TestRunWithForeignHomeCannotKillLiveSocket(t *testing.T) {
	leaderCfg := loadCfg(t, shortHomeDir(t))
	leaderCfg.Port = freeTestPort(t)

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(leaderCfg)
	}()
	waitForHealth(t, leaderCfg, 5*time.Second)

	const intruders = 3
	var wg sync.WaitGroup
	errs := make([]error, intruders)
	for i := 0; i < intruders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Own home (so claimPidFile is happy) and own port (so there
			// is no TCP collision either), but the leader's socket.
			cfg := loadCfg(t, shortHomeDir(t))
			cfg.Port = freeTestPort(t)
			cfg.SocketOverride = leaderCfg.SocketPath()
			errs[i] = Run(cfg)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err == nil {
			t.Errorf("intruder %d: Run returned nil, want a refusal", i)
		}
	}

	// The leader is still serving on its socket — the whole point.
	c := client.New(leaderCfg.SocketPath())
	var health map[string]string
	if err := c.Get("/v1/health", nil, &health); err != nil {
		t.Fatalf("leader health after %d foreign-home starts: %v", intruders, err)
	}
	if health["status"] != "ok" {
		t.Fatalf("leader health = %v, want status ok", health)
	}

	if err := c.Post("/v1/shutdown", nil, nil); err != nil {
		t.Fatalf("POST /v1/shutdown: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("leader Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("leader Run did not exit after shutdown")
	}
}

// loadCfg loads a config rooted at home, the way `rocket daemon run` does,
// so every interval and path is populated.
func loadCfg(t *testing.T, home string) *config.Config {
	t.Helper()
	cfg, err := config.Load(home)
	if err != nil {
		t.Fatalf("config.Load(%s): %v", home, err)
	}
	return cfg
}

func TestReadPid(t *testing.T) {
	home := shortHomeDir(t)
	cfg := &config.Config{Home: home}

	if _, err := ReadPid(cfg); err == nil {
		t.Fatal("expected an error reading a missing pid file")
	} else if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist, got %v", err)
	}

	if err := os.WriteFile(cfg.PidPath(), []byte("4242"), 0600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	pid, err := ReadPid(cfg)
	if err != nil {
		t.Fatalf("ReadPid: %v", err)
	}
	if pid != 4242 {
		t.Errorf("ReadPid = %d, want 4242", pid)
	}

	if err := os.WriteFile(cfg.PidPath(), []byte("not-a-pid"), 0600); err != nil {
		t.Fatalf("write malformed pid file: %v", err)
	}
	if _, err := ReadPid(cfg); err == nil {
		t.Fatal("expected an error parsing a malformed pid file")
	}
}

// TestWaitForExitReturnsOnceProcessExits proves WaitForExit unblocks
// promptly once the pid it's tracking actually dies, rather than always
// waiting out its full timeout — this is what lets `rocket daemon stop`
// return as soon as rocketd's graceful sweep finishes instead of a fixed
// sleep.
func TestWaitForExitReturnsOnceProcessExits(t *testing.T) {
	cmd := exec.Command("sleep", "2")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	pid := cmd.Process.Pid

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	start := time.Now()
	if err := WaitForExit(pid, 5*time.Second); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("WaitForExit took too long to notice process exit: %s", elapsed)
	}
}

// TestWaitForExitTimesOutWhileProcessAlive proves `daemon stop` surfaces a
// clear error rather than hanging forever if the daemon never actually
// exits.
func TestWaitForExitTimesOutWhileProcessAlive(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	if err := WaitForExit(cmd.Process.Pid, 200*time.Millisecond); err == nil {
		t.Fatal("expected a timeout error, got nil")
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
