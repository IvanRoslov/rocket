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
	Port               int           `yaml:"port"`
	HeartbeatInterval  time.Duration `yaml:"heartbeat_interval"`
	GithubPollInterval time.Duration `yaml:"github_poll_interval"`
	DefaultAgent       string        `yaml:"default_agent"`
	ReposDir           string        `yaml:"repos_dir"`
	WorktreesDir       string        `yaml:"worktrees_dir"`
	Home               string        `yaml:"-"`
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
		Port:               4477,
		HeartbeatInterval:  5 * time.Minute,
		GithubPollInterval: 2 * time.Minute,
		DefaultAgent:       "claude-code",
		ReposDir:           filepath.Join(home, "repos"),
		WorktreesDir:       filepath.Join(home, "worktrees"),
		Home:               home,
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

	// Restore home since YAML unmarshaling doesn't set it
	cfg.Home = home

	return cfg, nil
}

// SocketPath returns the path to the socket file.
func (c *Config) SocketPath() string {
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

// expandTilde expands ~ in a path to the home directory.
func expandTilde(path, home string) string {
	if len(path) > 0 && path[0] == '~' {
		return filepath.Join(home, path[1:])
	}
	return path
}

// UnmarshalYAML implements yaml.Unmarshaler for duration fields.
// This is handled by yaml.v3 directly for time.Duration fields.
