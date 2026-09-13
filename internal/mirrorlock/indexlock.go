package mirrorlock

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// ReapIndexLock removes gitDir/index.lock when it is older than maxAge.
// The caller MUST already hold the mirror's lock. It returns whether a lock
// file was removed.
//
// Age is the only criterion: holding the mirror lock already excludes every
// rocket process, and probing for a live git process is deliberately out of
// scope. A maxAge of zero or less disables reaping entirely.
func ReapIndexLock(gitDir string, maxAge time.Duration, now time.Time) (removed bool, err error) {
	if maxAge <= 0 {
		return false, nil
	}
	path := filepath.Join(gitDir, "index.lock")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	age := now.Sub(info.ModTime())
	if age <= maxAge {
		return false, nil
	}
	slog.Warn("mirror: removing stale index.lock", "path", path, "age", age)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("remove %s: %w", path, err)
	}
	return true, nil
}
