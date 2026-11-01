package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// fileSecrets maps an environment variable to the file under the rocket home
// that holds its value. The daemon's environment is whatever its starter had:
// `make restart` races agents' CLI calls, and the first one autostarts the
// daemon with that agent's environment, which may lack the key. Reading the
// file at startup makes the key independent of who started the daemon.
var fileSecrets = map[string]string{
	"DEEPSEEK_API_KEY": "deepseek-api-key",
}

// loadFileSecrets sets each variable in fileSecrets that is empty in the
// environment from its file under home. A missing file is not an error. The
// value is never logged.
func loadFileSecrets(home string) {
	for env, name := range fileSecrets {
		if os.Getenv(env) != "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(home, name))
		if err != nil {
			if !os.IsNotExist(err) {
				slog.Warn("daemon: cannot read secret file", "env", env, "error", err)
			}
			continue
		}
		v := strings.TrimSpace(string(b))
		if v == "" {
			continue
		}
		if err := os.Setenv(env, v); err != nil {
			slog.Warn("daemon: cannot set secret from file", "env", env, "error", err)
			continue
		}
		slog.Info("daemon: secret loaded from file", "env", env)
	}
}
