package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the rocket daemon configuration.
type Config struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
	// TLSPort is the https/HTTP/2 listener port (same host, same handler).
	// Browsers cap cleartext HTTP/1.1 at ~6 connections per host, which a
	// dashboard full of SSE streams exhausts (page loads then hang);
	// HTTP/2 — which browsers only speak over TLS — multiplexes everything
	// over one connection. 0 disables the listener. The certificate lives
	// in <home>/tls/ (auto-generated self-signed; replace with an
	// mkcert-issued pair to avoid the browser trust warning).
	TLSPort              int           `yaml:"tls_port"`
	HeartbeatInterval    time.Duration `yaml:"heartbeat_interval"`
	GithubPollInterval   time.Duration `yaml:"github_poll_interval"`
	DefaultAgent         string        `yaml:"default_agent"`
	ReposDir             string        `yaml:"repos_dir"`
	WorktreesDir         string        `yaml:"worktrees_dir"`
	AttachmentsDir       string        `yaml:"attachments_dir"`
	ActivityPollInterval time.Duration `yaml:"activity_poll_interval"`
	ReadyToIdle          time.Duration `yaml:"ready_to_idle"`
	QueueTimeout         time.Duration `yaml:"queue_timeout"`
	// LargeMessageThreshold is the body-size cutoff (in bytes) above which
	// deliver() writes the full message to a file in the recipient's
	// worktree inbox and injects a short pointer instead of the full body
	// (see internal/queue's deliver doc comment for why: injecting large
	// pastes directly into the TUI intermittently loses them). Messages to
	// recipients without a worktree always inject the full body.
	LargeMessageThreshold     int           `yaml:"large_message_threshold"`
	WorkerStallThreshold      time.Duration `yaml:"worker_stall_threshold"`
	QuestionReminderThreshold time.Duration `yaml:"question_reminder_threshold"`
	GithubAPIBase             string        `yaml:"github_api_base"`
	GithubCloneBase           string        `yaml:"github_clone_base"`
	MergeGrace                time.Duration `yaml:"merge_grace"`
	// AgentIdleTimeout bounds how long a role instance (session kind=agent)
	// may sit without activity before the wake engine kills it. It is the
	// safety net behind `rocket agent done`.
	AgentIdleTimeout time.Duration `yaml:"agent_idle_timeout"`
	// AgentWakeDebounce is the window the wake engine waits after the first
	// queued inbox event of a role before spawning an instance, so a burst
	// of events becomes one run.
	AgentWakeDebounce time.Duration `yaml:"agent_wake_debounce"`
	Home              string        `yaml:"-"`

	// SocketOverride, when non-empty, takes precedence over the default
	// <home>/rocket.sock path returned by SocketPath. It is populated from
	// the CLI's global --socket flag and is never persisted to config.yaml.
	SocketOverride string `yaml:"-"`
}

// Load loads the configuration from <home>/config.yaml.
// If home is empty, uses $ROCKET_HOME or ~/.rocket.
// Missing config.yaml is not an error; malformed yaml is.
func Load(home string) (*Config, error) {
	// Determine home directory
	if home == "" {
		home = os.Getenv("ROCKET_HOME")
		if home == "" {
			userHome, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get user home: %w", err)
			}
			home = filepath.Join(userHome, ".rocket")
		}
	}

	// Create home directory with 0700 perms if it doesn't exist
	if err := os.MkdirAll(home, 0700); err != nil {
		return nil, fmt.Errorf("failed to create home directory: %w", err)
	}

	// Create logs directory with 0700 perms if it doesn't exist
	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Set defaults
	cfg := &Config{
		Port:                      4477,
		Host:                      "127.0.0.1",
		TLSPort:                   4478,
		HeartbeatInterval:         5 * time.Minute,
		GithubPollInterval:        2 * time.Minute,
		DefaultAgent:              "claude-code",
		ReposDir:                  filepath.Join(home, "repos"),
		WorktreesDir:              filepath.Join(home, "worktrees"),
		AttachmentsDir:            filepath.Join(home, "attachments"),
		ActivityPollInterval:      5 * time.Second,
		ReadyToIdle:               5 * time.Minute,
		QueueTimeout:              30 * time.Minute,
		LargeMessageThreshold:     2048,
		WorkerStallThreshold:      15 * time.Minute,
		QuestionReminderThreshold: 30 * time.Minute,
		GithubAPIBase:             "https://api.github.com",
		GithubCloneBase:           "",
		MergeGrace:                5 * time.Minute,
		AgentIdleTimeout:          15 * time.Minute,
		AgentWakeDebounce:         30 * time.Second,
		Home:                      home,
	}

	// Try to load config.yaml
	configPath := filepath.Join(home, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing config.yaml is not an error
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Expand ~ in ReposDir
	if cfg.ReposDir != "" {
		cfg.ReposDir = expandTilde(cfg.ReposDir, home)
	} else {
		cfg.ReposDir = filepath.Join(home, "repos")
	}

	// Expand ~ in WorktreesDir
	if cfg.WorktreesDir != "" {
		cfg.WorktreesDir = expandTilde(cfg.WorktreesDir, home)
	} else {
		cfg.WorktreesDir = filepath.Join(home, "worktrees")
	}

	// Expand ~ in AttachmentsDir
	if cfg.AttachmentsDir != "" {
		cfg.AttachmentsDir = expandTilde(cfg.AttachmentsDir, home)
	} else {
		cfg.AttachmentsDir = filepath.Join(home, "attachments")
	}

	// An explicit empty host would bind all interfaces; keep the safe default.
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}

	// Restore home since YAML unmarshaling doesn't set it
	cfg.Home = home

	return cfg, nil
}

// SocketPath returns the path to the socket file. If SocketOverride is set
// (typically from the CLI's global --socket flag), it takes precedence over
// the default <home>/rocket.sock path.
func (c *Config) SocketPath() string {
	if c.SocketOverride != "" {
		return c.SocketOverride
	}
	return filepath.Join(c.Home, "rocket.sock")
}

// DBPath returns the path to the database file.
func (c *Config) DBPath() string {
	return filepath.Join(c.Home, "rocket.db")
}

// PidPath returns the path to the PID file.
func (c *Config) PidPath() string {
	return filepath.Join(c.Home, "rocketd.pid")
}

// LogPath returns the path to the log file.
func (c *Config) LogPath() string {
	return filepath.Join(c.Home, "logs", "rocketd.log")
}

// ConfigPath returns the path to the config.yaml file.
func (c *Config) ConfigPath() string {
	return filepath.Join(c.Home, "config.yaml")
}

// expandTilde expands ~ in a path to the home directory.
func expandTilde(path, home string) string {
	if len(path) > 0 && path[0] == '~' {
		return filepath.Join(home, path[1:])
	}
	return path
}

// UnmarshalYAML implements yaml.Unmarshaler for duration fields.
// This is handled by yaml.v3 directly for time.Duration fields.
