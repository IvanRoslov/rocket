package cli

import (
	"path/filepath"
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
