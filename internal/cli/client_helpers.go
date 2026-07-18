package cli

import (
	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/IvanRoslov/rocket/internal/config"
)

// loadConfig loads the rocket config from $ROCKET_HOME (or ~/.rocket).
func loadConfig() (*config.Config, error) {
	return config.Load("")
}

// connect loads config and connects a client to the daemon, honoring the
// global --socket override and the given autostart preference. On failure
// to reach a running (or newly autostarted) daemon it returns
// client.ErrDaemonUnavailable, which Execute maps to exit code 2.
func connect(autostart bool) (*client.Client, *config.Config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}
	if flags.Socket == "" {
		c, err := client.Connect(cfg, autostart)
		return c, cfg, err
	}

	// A custom socket was given: connect directly, skipping autostart (we
	// don't know how to launch a daemon bound to an arbitrary socket path).
	c := client.New(flags.Socket)
	var out struct{}
	if err := c.Get("/v1/health", nil, &out); err != nil {
		return nil, cfg, client.ErrDaemonUnavailable
	}
	return c, cfg, nil
}
