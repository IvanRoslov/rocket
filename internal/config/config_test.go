package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ROCKET_HOME", "")

	cfg, err := Load(tempDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Port != 4477 {
		t.Errorf("expected Port 4477, got %d", cfg.Port)
	}
	if cfg.HeartbeatInterval != 5*time.Minute {
		t.Errorf("expected HeartbeatInterval 5m, got %v", cfg.HeartbeatInterval)
	}
	if cfg.GithubPollInterval != 2*time.Minute {
		t.Errorf("expected GithubPollInterval 2m, got %v", cfg.GithubPollInterval)
	}
	if cfg.DefaultAgent != "claude-code" {
		t.Errorf("expected DefaultAgent 'claude-code', got %q", cfg.DefaultAgent)
	}
	if cfg.ReposDir != filepath.Join(tempDir, "repos") {
		t.Errorf("expected ReposDir %s, got %s", filepath.Join(tempDir, "repos"), cfg.ReposDir)
	}
	if cfg.WorktreesDir != filepath.Join(tempDir, "worktrees") {
		t.Errorf("expected WorktreesDir %s, got %s", filepath.Join(tempDir, "worktrees"), cfg.WorktreesDir)
	}
	if cfg.Home != tempDir {
		t.Errorf("expected Home %s, got %s", tempDir, cfg.Home)
	}
}

func TestLoadWithConfig(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ROCKET_HOME", "")

	// Create config.yaml
	configContent := `port: 9999
heartbeat_interval: 10m
github_poll_interval: 3m
default_agent: custom-agent
repos_dir: /tmp/repos
worktrees_dir: /tmp/worktrees
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(tempDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Port != 9999 {
		t.Errorf("expected Port 9999, got %d", cfg.Port)
	}
	if cfg.HeartbeatInterval != 10*time.Minute {
		t.Errorf("expected HeartbeatInterval 10m, got %v", cfg.HeartbeatInterval)
	}
	if cfg.GithubPollInterval != 3*time.Minute {
		t.Errorf("expected GithubPollInterval 3m, got %v", cfg.GithubPollInterval)
	}
	if cfg.DefaultAgent != "custom-agent" {
		t.Errorf("expected DefaultAgent 'custom-agent', got %q", cfg.DefaultAgent)
	}
	if cfg.ReposDir != "/tmp/repos" {
		t.Errorf("expected ReposDir /tmp/repos, got %s", cfg.ReposDir)
	}
	if cfg.WorktreesDir != "/tmp/worktrees" {
		t.Errorf("expected WorktreesDir /tmp/worktrees, got %s", cfg.WorktreesDir)
	}
}

func TestTildeExpansion(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := tempDir
	t.Setenv("ROCKET_HOME", "")

	// Create config.yaml with ~ in paths
	configContent := `repos_dir: ~/my_repos
worktrees_dir: ~/my_worktrees
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(homeDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	expectedRepos := filepath.Join(homeDir, "my_repos")
	expectedWorktrees := filepath.Join(homeDir, "my_worktrees")

	if cfg.ReposDir != expectedRepos {
		t.Errorf("expected ReposDir %s, got %s", expectedRepos, cfg.ReposDir)
	}
	if cfg.WorktreesDir != expectedWorktrees {
		t.Errorf("expected WorktreesDir %s, got %s", expectedWorktrees, cfg.WorktreesDir)
	}
}

func TestRocketHomeEnv(t *testing.T) {
	tempDir := t.TempDir()
	rocketHomeDir := filepath.Join(tempDir, "rocket_home")
	t.Setenv("ROCKET_HOME", rocketHomeDir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Home != rocketHomeDir {
		t.Errorf("expected Home %s, got %s", rocketHomeDir, cfg.Home)
	}
}

func TestDirectoryCreation(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "new_home")
	t.Setenv("ROCKET_HOME", "")

	cfg, err := Load(homeDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Check home directory was created
	info, err := os.Stat(homeDir)
	if err != nil {
		t.Errorf("home directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("home is not a directory")
	}
	mode := info.Mode().Perm()
	if mode != 0700 {
		t.Errorf("expected home dir perms 0700, got %#o", mode)
	}

	// Check logs directory was created
	logsDir := filepath.Join(homeDir, "logs")
	info, err = os.Stat(logsDir)
	if err != nil {
		t.Errorf("logs directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("logs is not a directory")
	}
	mode = info.Mode().Perm()
	if mode != 0700 {
		t.Errorf("expected logs dir perms 0700, got %#o", mode)
	}

	if cfg.Home != homeDir {
		t.Errorf("expected Home %s, got %s", homeDir, cfg.Home)
	}
}

func TestPaths(t *testing.T) {
	tempDir := t.TempDir()
	cfg, err := Load(tempDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	expectedSocket := filepath.Join(tempDir, "rocket.sock")
	if cfg.SocketPath() != expectedSocket {
		t.Errorf("expected SocketPath %s, got %s", expectedSocket, cfg.SocketPath())
	}

	expectedDB := filepath.Join(tempDir, "rocket.db")
	if cfg.DBPath() != expectedDB {
		t.Errorf("expected DBPath %s, got %s", expectedDB, cfg.DBPath())
	}

	expectedPid := filepath.Join(tempDir, "rocketd.pid")
	if cfg.PidPath() != expectedPid {
		t.Errorf("expected PidPath %s, got %s", expectedPid, cfg.PidPath())
	}

	expectedLog := filepath.Join(tempDir, "logs", "rocketd.log")
	if cfg.LogPath() != expectedLog {
		t.Errorf("expected LogPath %s, got %s", expectedLog, cfg.LogPath())
	}
}

func TestSocketOverride(t *testing.T) {
	tempDir := t.TempDir()
	cfg, err := Load(tempDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Default: SocketPath is under Home.
	if got, want := cfg.SocketPath(), filepath.Join(tempDir, "rocket.sock"); got != want {
		t.Errorf("SocketPath() = %s, want %s", got, want)
	}

	// With SocketOverride set, it takes precedence.
	cfg.SocketOverride = "/tmp/custom.sock"
	if got, want := cfg.SocketPath(), "/tmp/custom.sock"; got != want {
		t.Errorf("SocketPath() with override = %s, want %s", got, want)
	}
}

func TestMalformedYAML(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ROCKET_HOME", "")

	// Create malformed config.yaml
	configContent := `port: 9999
heartbeat_interval: 10m
invalid: [unclosed bracket
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := Load(tempDir)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}
}
