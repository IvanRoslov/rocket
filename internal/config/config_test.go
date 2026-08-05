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
	if cfg.Host != "127.0.0.1" {
		t.Errorf("expected Host 127.0.0.1, got %q", cfg.Host)
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
	if cfg.ActivityPollInterval != 5*time.Second {
		t.Errorf("expected ActivityPollInterval 5s, got %v", cfg.ActivityPollInterval)
	}
	if cfg.ReadyToIdle != 5*time.Minute {
		t.Errorf("expected ReadyToIdle 5m, got %v", cfg.ReadyToIdle)
	}
	if cfg.QueueTimeout != 30*time.Minute {
		t.Errorf("expected QueueTimeout 30m, got %v", cfg.QueueTimeout)
	}
	if cfg.LargeMessageThreshold != 2048 {
		t.Errorf("expected LargeMessageThreshold 2048, got %d", cfg.LargeMessageThreshold)
	}
	if cfg.GithubAPIBase != "https://api.github.com" {
		t.Errorf("expected GithubAPIBase https://api.github.com, got %q", cfg.GithubAPIBase)
	}
	if cfg.GithubCloneBase != "" {
		t.Errorf("expected GithubCloneBase empty, got %q", cfg.GithubCloneBase)
	}
	if cfg.MergeGrace != 5*time.Minute {
		t.Errorf("expected MergeGrace 5m, got %v", cfg.MergeGrace)
	}
	if cfg.Home != tempDir {
		t.Errorf("expected Home %s, got %s", tempDir, cfg.Home)
	}
	if cfg.InputStallThreshold != 10*time.Minute {
		t.Errorf("expected InputStallThreshold 10m, got %v", cfg.InputStallThreshold)
	}
	if cfg.QuestionStaleAfter != DefaultQuestionStaleAfter {
		t.Errorf("expected QuestionStaleAfter %v, got %v", DefaultQuestionStaleAfter, cfg.QuestionStaleAfter)
	}
}

func TestLoadWithConfig(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ROCKET_HOME", "")

	// Create config.yaml
	configContent := `port: 9999
host: 0.0.0.0
heartbeat_interval: 10m
github_poll_interval: 3m
default_agent: custom-agent
repos_dir: /tmp/repos
worktrees_dir: /tmp/worktrees
activity_poll_interval: 10s
ready_to_idle: 10m
queue_timeout: 1h
github_api_base: https://ghe.example.com/api/v3
merge_grace: 10m
input_stall_threshold: 3m
question_stale_after: 6h
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
	if cfg.Host != "0.0.0.0" {
		t.Errorf("expected Host 0.0.0.0, got %q", cfg.Host)
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
	if cfg.ActivityPollInterval != 10*time.Second {
		t.Errorf("expected ActivityPollInterval 10s, got %v", cfg.ActivityPollInterval)
	}
	if cfg.ReadyToIdle != 10*time.Minute {
		t.Errorf("expected ReadyToIdle 10m, got %v", cfg.ReadyToIdle)
	}
	if cfg.QueueTimeout != 1*time.Hour {
		t.Errorf("expected QueueTimeout 1h, got %v", cfg.QueueTimeout)
	}
	if cfg.GithubAPIBase != "https://ghe.example.com/api/v3" {
		t.Errorf("expected GithubAPIBase https://ghe.example.com/api/v3, got %q", cfg.GithubAPIBase)
	}
	if cfg.MergeGrace != 10*time.Minute {
		t.Errorf("expected MergeGrace 10m, got %v", cfg.MergeGrace)
	}
	if cfg.InputStallThreshold != 3*time.Minute {
		t.Errorf("expected InputStallThreshold 3m, got %v", cfg.InputStallThreshold)
	}
	if cfg.QuestionStaleAfter != 6*time.Hour {
		t.Errorf("expected QuestionStaleAfter 6h, got %v", cfg.QuestionStaleAfter)
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

func TestAgentRuntimeDefaults(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AgentNotifyInterval != 5*time.Minute {
		t.Errorf("AgentNotifyInterval = %v, want 5m", cfg.AgentNotifyInterval)
	}
}

func TestAgentRuntimeOverrides(t *testing.T) {
	home := t.TempDir()
	yaml := "agent_notify_interval: 90s\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AgentNotifyInterval != 90*time.Second {
		t.Errorf("AgentNotifyInterval = %v, want 90s", cfg.AgentNotifyInterval)
	}
}

func TestMirrorSyncIntervalDefault(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MirrorSyncInterval != 5*time.Minute {
		t.Errorf("MirrorSyncInterval = %v, want 5m", cfg.MirrorSyncInterval)
	}
}

func TestMirrorSyncIntervalOverride(t *testing.T) {
	home := t.TempDir()
	yaml := "mirror_sync_interval: 30s\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MirrorSyncInterval != 30*time.Second {
		t.Errorf("MirrorSyncInterval = %v, want 30s", cfg.MirrorSyncInterval)
	}
}

// A zero interval must survive Load untouched: it is how a user disables
// background mirror syncing, not a missing value to be defaulted. It is
// spelled "0s" — yaml.v3 parses time.Duration from a string, so a bare 0 is
// a parse error here exactly as it is for every other duration key.
func TestMirrorSyncIntervalZeroDisables(t *testing.T) {
	home := t.TempDir()
	yaml := "mirror_sync_interval: 0s\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MirrorSyncInterval != 0 {
		t.Errorf("MirrorSyncInterval = %v, want 0", cfg.MirrorSyncInterval)
	}
}
