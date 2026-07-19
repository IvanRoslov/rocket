package cli

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/client"
	"github.com/IvanRoslov/rocket/internal/config"
)

// apiPath joins parts into a "/"-separated request path, url.PathEscape-ing
// each part first. Every place a user-supplied id (session id, repo id,
// project id, ...) is embedded in a request path MUST go through this
// helper: an unescaped id like "../shutdown?" would otherwise let a crafted
// id rewrite the request to a different endpoint (e.g. turning
// POST /v1/sessions/<id>/restore into POST /v1/shutdown).
func apiPath(parts ...string) string {
	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = url.PathEscape(p)
	}
	return "/" + strings.Join(escaped, "/")
}

// loadConfig loads the rocket config from $ROCKET_HOME (or ~/.rocket),
// applying the global --socket override (if given) to cfg.SocketOverride
// so every consumer of cfg — the HTTP client, daemon autostart, and
// `daemon run` itself — agrees on the socket path.
//
// Falls back to $ROCKET_SOCKET when --socket is not given. This env var is
// exported into every orchestrator/worker session (claudecode.Env) but
// tmux sessions don't inherit $ROCKET_HOME set after the tmux server first
// started, so without this fallback a `rocket` CLI call made from inside an
// agent session can silently resolve to the wrong (or no) daemon whenever
// ROCKET_HOME isn't the default ~/.rocket.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}
	if flags.Socket != "" {
		cfg.SocketOverride = flags.Socket
	} else if sock := os.Getenv("ROCKET_SOCKET"); sock != "" {
		cfg.SocketOverride = sock
	}
	return cfg, nil
}

// connect loads config and connects a client to the daemon, honoring the
// global --socket override and the given autostart preference. On failure
// to reach a running (or newly autostarted) daemon it returns
// client.ErrDaemonUnavailable, which Execute maps to exit code 2.
//
// NOTE: Under go test, connect() is disabled to prevent CLI tests from
// accidentally connecting to the live daemon. Tests should stub the client
// or test at ParseFlags level instead (2026-07-19 incident).
func connect(autostart bool) (*client.Client, *config.Config, error) {
	if testing.Testing() {
		return nil, nil, fmt.Errorf("cli: connect() is disabled under go test (a test almost hit the live daemon); stub the client or test at ParseFlags level")
	}

	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}
	c, err := client.Connect(cfg, autostart)
	return c, cfg, err
}
