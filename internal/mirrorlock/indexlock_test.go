package mirrorlock

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReapIndexLockRemovesStale(t *testing.T) {
	gitDir := t.TempDir()
	lock := filepath.Join(gitDir, "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}

	removed, err := ReapIndexLock(gitDir, 10*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("ReapIndexLock: %v", err)
	}
	if !removed {
		t.Error("removed = false, want true")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Error("index.lock still present")
	}
}

func TestReapIndexLockKeepsFresh(t *testing.T) {
	gitDir := t.TempDir()
	lock := filepath.Join(gitDir, "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := ReapIndexLock(gitDir, 10*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("ReapIndexLock: %v", err)
	}
	if removed {
		t.Error("removed = true, want false for a fresh lock")
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("fresh index.lock must be left alone: %v", err)
	}
}

func TestReapIndexLockNoLockFile(t *testing.T) {
	removed, err := ReapIndexLock(t.TempDir(), 10*time.Minute, time.Now())
	if err != nil {
		t.Fatalf("ReapIndexLock: %v", err)
	}
	if removed {
		t.Error("removed = true with no index.lock present")
	}
}

// maxAge <= 0 disables reaping: an operator who zeroes the threshold in the
// config must not get an aggressive reaper instead of none at all.
func TestReapIndexLockDisabled(t *testing.T) {
	gitDir := t.TempDir()
	lock := filepath.Join(gitDir, "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}

	removed, err := ReapIndexLock(gitDir, 0, time.Now())
	if err != nil {
		t.Fatalf("ReapIndexLock: %v", err)
	}
	if removed {
		t.Error("removed = true, want false when reaping is disabled")
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("index.lock must be left alone when reaping is disabled: %v", err)
	}
}
