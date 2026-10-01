package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// stderrLogPath is where a detached daemon's stderr goes, next to its log.
func stderrLogPath(logPath string) string {
	return filepath.Join(filepath.Dir(logPath), "rocketd.stderr.log")
}

// captureStderr points fd 2 at stderrLogPath unless stderr is a terminal.
// The autostarted daemon's stderr is /dev/null, and that is exactly where the
// Go runtime writes what matters most when the daemon is stuck: a panic, and
// the goroutine dump of `kill -QUIT <pid>`. A daemon run in the foreground
// keeps its terminal.
func captureStderr(logPath string) error {
	if term.IsTerminal(int(os.Stderr.Fd())) {
		return nil
	}
	path := stderrLogPath(logPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}
	return redirectFD(int(os.Stderr.Fd()), path)
}

// redirectFD makes fd write to path (appending), after a header line that
// separates this process's output from earlier runs'.
func redirectFD(fd int, path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open stderr log: %w", err)
	}
	defer f.Close() // fd keeps its own reference after Dup2

	fmt.Fprintf(f, "=== rocketd stderr, pid %d, started %s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	if err := unix.Dup2(int(f.Fd()), fd); err != nil {
		return fmt.Errorf("redirect stderr: %w", err)
	}
	return nil
}
