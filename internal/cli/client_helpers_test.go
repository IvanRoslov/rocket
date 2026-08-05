package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadConfigAppliesSocketOverride proves the global --socket flag
// reaches cfg.SocketOverride (and thus cfg.SocketPath()) via loadConfig,
// the single place both the client path and `daemon run` derive cfg from.
func TestLoadConfigAppliesSocketOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ROCKET_HOME", home)

	prev := flags.Socket
	t.Cleanup(func() { flags.Socket = prev })

	override := filepath.Join(home, "custom.sock")
	flags.Socket = override

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.SocketOverride != override {
		t.Errorf("cfg.SocketOverride = %q, want %q", cfg.SocketOverride, override)
	}
	if cfg.SocketPath() != override {
		t.Errorf("cfg.SocketPath() = %q, want %q", cfg.SocketPath(), override)
	}
}

// TestLoadConfigNoOverrideWhenSocketFlagEmpty proves that an empty --socket
// flag leaves cfg.SocketPath() at its default <home>/rocket.sock value.
func TestLoadConfigNoOverrideWhenSocketFlagEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ROCKET_HOME", home)
	// loadConfig falls back to $ROCKET_SOCKET when --socket is empty, and
	// that var is exported into every rocket agent session — so without
	// clearing it this test fails whenever it is run from inside one.
	t.Setenv("ROCKET_SOCKET", "")

	prev := flags.Socket
	t.Cleanup(func() { flags.Socket = prev })
	flags.Socket = ""

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.SocketOverride != "" {
		t.Errorf("cfg.SocketOverride = %q, want empty", cfg.SocketOverride)
	}
	want := filepath.Join(home, "rocket.sock")
	if cfg.SocketPath() != want {
		t.Errorf("cfg.SocketPath() = %q, want %q", cfg.SocketPath(), want)
	}
}

// TestLoadConfigFallsBackToRocketSocketEnv proves that when neither --socket
// nor ROCKET_HOME point at the right daemon instance, the ROCKET_SOCKET env
// var (already exported into every orchestrator/worker session by
// claudecode.Env) is honored as the socket override. Without this, agent
// sessions launched with a non-default ROCKET_HOME can never reach the
// daemon that spawned them via the `rocket` CLI, because loadConfig only
// ever resolves the *default* <ROCKET_HOME or ~/.rocket>/rocket.sock path
// and tmux sessions don't inherit ROCKET_HOME set after the tmux server
// started.
func TestLoadConfigFallsBackToRocketSocketEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ROCKET_HOME", home)

	prev := flags.Socket
	t.Cleanup(func() { flags.Socket = prev })
	flags.Socket = ""

	sockEnv := filepath.Join(home, "session.sock")
	t.Setenv("ROCKET_SOCKET", sockEnv)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.SocketPath() != sockEnv {
		t.Errorf("cfg.SocketPath() = %q, want %q (from ROCKET_SOCKET env)", cfg.SocketPath(), sockEnv)
	}
}

// TestLoadConfigSocketFlagBeatsRocketSocketEnv proves the explicit --socket
// flag still wins over the ROCKET_SOCKET env fallback.
func TestLoadConfigSocketFlagBeatsRocketSocketEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ROCKET_HOME", home)

	prev := flags.Socket
	t.Cleanup(func() { flags.Socket = prev })

	flagSock := filepath.Join(home, "flag.sock")
	flags.Socket = flagSock
	t.Setenv("ROCKET_SOCKET", filepath.Join(home, "env.sock"))

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.SocketPath() != flagSock {
		t.Errorf("cfg.SocketPath() = %q, want %q (--socket flag)", cfg.SocketPath(), flagSock)
	}
}

// TestApiPathEscapesTraversal proves apiPath escapes a crafted id like
// "../shutdown?" so it cannot be used to rewrite the request to a different
// endpoint (e.g. turning POST /v1/sessions/<id>/restore into
// POST /v1/shutdown).
func TestApiPathEscapesTraversal(t *testing.T) {
	got := apiPath("v1", "sessions", "../shutdown?", "restore")
	want := "/v1/sessions/..%2Fshutdown%3F/restore"
	if got != want {
		t.Errorf("apiPath = %q, want %q", got, want)
	}
	if strings.Contains(got, "../") {
		t.Fatalf("apiPath result still contains a raw path traversal segment: %q", got)
	}
}

// TestApiPathPlainIDsUnchanged proves apiPath leaves ordinary ids untouched
// so normal usage still produces the expected request path.
func TestApiPathPlainIDsUnchanged(t *testing.T) {
	got := apiPath("v1", "sessions", "myfeat-mytask", "kill")
	want := "/v1/sessions/myfeat-mytask/kill"
	if got != want {
		t.Errorf("apiPath = %q, want %q", got, want)
	}
}
