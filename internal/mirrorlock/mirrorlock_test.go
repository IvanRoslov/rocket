package mirrorlock

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	l, err := Acquire(context.Background(), dir, "rocket", "syncer", time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("second Release must be a no-op: %v", err)
	}
}

// A second Acquire in the SAME process must also block: flock is per open
// file description, so two separate opens contend even in one process.
func TestAcquireBusyReportsHolder(t *testing.T) {
	dir := t.TempDir()
	first, err := Acquire(context.Background(), dir, "rocket", "repo-sync-repair", time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer func() { _ = first.Release() }()

	_, err = Acquire(context.Background(), dir, "rocket", "syncer", 100*time.Millisecond)
	var busy *ErrBusy
	if !errors.As(err, &busy) {
		t.Fatalf("want *ErrBusy, got %v", err)
	}
	if busy.Holder.Operation != "repo-sync-repair" {
		t.Errorf("holder operation = %q, want repo-sync-repair", busy.Holder.Operation)
	}
	if busy.Holder.PID != os.Getpid() {
		t.Errorf("holder pid = %d, want %d", busy.Holder.PID, os.Getpid())
	}
	if busy.RepoID != "rocket" {
		t.Errorf("busy.RepoID = %q, want rocket", busy.RepoID)
	}
}

func TestAcquireWaitsUntilReleased(t *testing.T) {
	dir := t.TempDir()
	first, err := Acquire(context.Background(), dir, "rocket", "syncer", time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	go func() { time.Sleep(150 * time.Millisecond); _ = first.Release() }()

	second, err := Acquire(context.Background(), dir, "rocket", "repo-sync", 5*time.Second)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if second.Waited() < 100*time.Millisecond {
		t.Errorf("Waited() = %v, want >= 100ms", second.Waited())
	}
	_ = second.Release()
}

func TestAcquireHonoursContext(t *testing.T) {
	dir := t.TempDir()
	first, err := Acquire(context.Background(), dir, "rocket", "syncer", time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer func() { _ = first.Release() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx, dir, "rocket", "repo-sync", time.Minute); err == nil {
		t.Fatal("want error on cancelled context")
	}
}

func TestAcquireCreatesLocksDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist-yet")
	l, err := Acquire(context.Background(), dir, "rocket", "syncer", time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	_ = l.Release()
	if _, err := os.Stat(filepath.Join(dir, "rocket.lock")); err != nil {
		t.Errorf("lock file not created: %v", err)
	}
}

func TestLocksDir(t *testing.T) {
	if got, want := LocksDir("/repos"), filepath.Join("/repos", ".locks"); got != want {
		t.Errorf("LocksDir = %q, want %q", got, want)
	}
}

// The helper below runs in a separate process, started by
// TestAcquireContendsAcrossProcesses via a re-exec of the test binary.
func TestHelperHoldsLock(t *testing.T) {
	dir := os.Getenv("MIRRORLOCK_HELPER_DIR")
	if dir == "" {
		t.Skip("helper process only")
	}
	l, err := Acquire(context.Background(), dir, "rocket", "repo-sync-repair", 5*time.Second)
	if err != nil {
		t.Fatalf("helper Acquire: %v", err)
	}
	time.Sleep(2 * time.Second)
	_ = l.Release()
}

func TestAcquireContendsAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperHoldsLock")
	cmd.Env = append(os.Environ(), "MIRRORLOCK_HELPER_DIR="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	defer func() { _ = cmd.Wait() }()

	waitForLockFile(t, filepath.Join(dir, "rocket.lock"))

	_, err := Acquire(context.Background(), dir, "rocket", "syncer", 200*time.Millisecond)
	var busy *ErrBusy
	if !errors.As(err, &busy) {
		t.Fatalf("want *ErrBusy while another process holds the lock, got %v", err)
	}
	if busy.Holder.PID == os.Getpid() {
		t.Errorf("holder pid = %d, want the helper process", busy.Holder.PID)
	}
}

// waitForLockFile polls until the lock file exists and its holder JSON parses,
// i.e. the helper process has really taken the lock.
func waitForLockFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h, err := readHolder(path); err == nil && h.PID != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("helper never took the lock at %s", path)
}
