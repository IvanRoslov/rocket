// Package mirrorlock guards a git mirror against concurrent writers.
//
// Every process that writes a mirror under <ReposDir> — the daemon's Syncer,
// `rocket repo sync`, `rocket repo sync --repair`, and workspace clone — takes
// the exclusive flock for that mirror first. The lock file doubles as holder
// metadata: whoever loses the race can say who is holding the mirror and for
// how long, instead of racing git and leaving the mirror half-repaired.
package mirrorlock

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Holder is who currently owns a mirror lock, as recorded in the lock file.
type Holder struct {
	PID       int       `json:"pid"`
	Operation string    `json:"operation"` // "syncer" | "repo-sync" | "repo-sync-repair" | "workspace-clone"
	Since     time.Time `json:"since"`
}

// Lock is a held mirror lock. Release is idempotent.
type Lock struct {
	f        *os.File
	waited   time.Duration
	released bool
}

// Release unlocks and closes the lock file. Calling it twice is a no-op.
//
// The lock file itself is deliberately left on disk: removing it would race a
// waiter that already holds the old inode open, and two processes would then
// think they own the mirror.
func (l *Lock) Release() error {
	if l == nil || l.released {
		return nil
	}
	l.released = true
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

// Waited reports how long Acquire blocked before the lock was taken.
func (l *Lock) Waited() time.Duration {
	if l == nil {
		return 0
	}
	return l.waited
}

// ErrBusy is returned when the lock could not be taken within the timeout.
// It carries the holder read from the lock file, when readable.
type ErrBusy struct {
	RepoID string
	Holder Holder // zero value when the file could not be read
	Waited time.Duration
}

func (e *ErrBusy) Error() string {
	if e.Holder.PID == 0 {
		return fmt.Sprintf("mirror %s is locked by another process (waited %s)", e.RepoID, e.Waited)
	}
	return fmt.Sprintf("mirror %s is locked by %s (pid %d, since %s; waited %s)",
		e.RepoID, e.Holder.Operation, e.Holder.PID, e.Holder.Since.Format(time.RFC3339), e.Waited)
}

// LocksDir is the conventional lock directory for a repos dir.
func LocksDir(reposDir string) string { return filepath.Join(reposDir, ".locks") }

// LockPath is the lock file guarding one mirror.
func LockPath(locksDir, repoID string) string {
	return filepath.Join(locksDir, repoID+".lock")
}

// Acquire takes the exclusive lock for repoID under locksDir, waiting up to
// timeout. It returns *ErrBusy on timeout and honours ctx cancellation.
func Acquire(ctx context.Context, locksDir, repoID, operation string, timeout time.Duration) (*Lock, error) {
	if err := os.MkdirAll(locksDir, 0o755); err != nil {
		return nil, fmt.Errorf("create locks dir %s: %w", locksDir, err)
	}
	path := LockPath(locksDir, repoID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}

	started := time.Now()
	// The blocking flock cannot be interrupted, so it runs in its own
	// goroutine. If it succeeds after we have already given up, that
	// goroutine releases it — otherwise the mirror would stay locked by
	// nobody until the process exits.
	locked := make(chan error, 1)
	go func() {
		locked <- syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	}()

	abandon := func() {
		go func() {
			if err := <-locked; err == nil {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			}
			_ = f.Close()
		}()
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-locked:
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("flock %s: %w", path, err)
		}
	case <-ctx.Done():
		abandon()
		return nil, ctx.Err()
	case <-timer.C:
		abandon()
		waited := time.Since(started)
		busy := &ErrBusy{RepoID: repoID, Waited: waited}
		if h, err := readHolder(path); err == nil {
			busy.Holder = h
		}
		return nil, busy
	}

	l := &Lock{f: f, waited: time.Since(started)}
	if err := writeHolder(f, Holder{PID: os.Getpid(), Operation: operation, Since: time.Now()}); err != nil {
		_ = l.Release()
		return nil, err
	}
	return l, nil
}

// writeHolder replaces the lock file's contents with the holder record. The
// file is only ever written while the flock is held, so there is no reader
// racing a partial write of a lock that is actually owned.
func writeHolder(f *os.File, h Holder) error {
	data, err := json.Marshal(h)
	if err != nil {
		return fmt.Errorf("marshal holder: %w", err)
	}
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncate lock file: %w", err)
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		return fmt.Errorf("write holder: %w", err)
	}
	return f.Sync()
}

// readHolder reads the holder record of a lock file. A malformed or empty
// file is reported as an error so the caller falls back to the zero Holder.
func readHolder(path string) (Holder, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Holder{}, err
	}
	var h Holder
	if err := json.Unmarshal(data, &h); err != nil {
		return Holder{}, err
	}
	return h, nil
}
