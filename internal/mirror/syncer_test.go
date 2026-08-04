package mirror

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

// newStore opens a throwaway store holding exactly the given repos.
func newStore(t *testing.T, repos ...store.Repo) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "rocket.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	for _, r := range repos {
		if err := st.AddRepo(r); err != nil {
			t.Fatalf("add repo %s: %v", r.ID, err)
		}
	}
	return st
}

// mirrorUnder builds a mirror the way newMirror does, but parks it inside
// reposDir under the given id — where the daemon's own clones live, and so
// where the sweep is allowed to touch it. The clone's origin is an absolute
// path, so relocating the directory keeps it working.
func mirrorUnder(t *testing.T, reposDir, id string) (string, store.Repo) {
	t.Helper()
	origin, repo := newMirror(t)
	dst := filepath.Join(reposDir, id)
	if err := os.Rename(repo.Path, dst); err != nil {
		t.Fatalf("move mirror into repos dir: %v", err)
	}
	repo.Path, repo.ID = dst, id
	return origin, repo
}

func TestSyncOnceSyncsEveryRepo(t *testing.T) {
	reposDir := t.TempDir()
	originA, repoA := mirrorUnder(t, reposDir, "a")
	originB, repoB := mirrorUnder(t, reposDir, "b")
	commitToOrigin(t, originA, "a2\n", "second a")
	commitToOrigin(t, originB, "b2\n", "second b")

	s := NewSyncer(newStore(t, repoA, repoB), &config.Config{
		MirrorSyncInterval: time.Minute,
		ReposDir:           reposDir,
	})
	s.SyncOnce(context.Background())

	if got := readFile(t, filepath.Join(repoA.Path, "file.txt")); got != "a2\n" {
		t.Errorf("mirror a working tree = %q, want %q", got, "a2\n")
	}
	if got := readFile(t, filepath.Join(repoB.Path, "file.txt")); got != "b2\n" {
		t.Errorf("mirror b working tree = %q, want %q", got, "b2\n")
	}
}

// `rocket repo add <path>` registers a human's own working copy, and
// docs/05-state.md promises rocket changes nothing inside it. Only the
// daemon's own clones under ReposDir are mirrors this sweep may advance.
func TestSyncOnceSkipsReposOutsideReposDir(t *testing.T) {
	reposDir := t.TempDir()
	originMirror, mirrorRepo := mirrorUnder(t, reposDir, "mirror")
	// The second repo stays where newMirror put it: a checkout somewhere
	// else on disk, exactly like one registered by `rocket repo add`.
	originLocal, localRepo := newMirror(t)
	localRepo.ID = "local"
	commitToOrigin(t, originMirror, "m2\n", "second mirror")
	commitToOrigin(t, originLocal, "l2\n", "second local")

	localHead := headSHA(t, localRepo.Path)

	s := NewSyncer(newStore(t, mirrorRepo, localRepo), &config.Config{
		MirrorSyncInterval: time.Minute,
		ReposDir:           reposDir,
	})
	s.SyncOnce(context.Background())

	if got := readFile(t, filepath.Join(mirrorRepo.Path, "file.txt")); got != "m2\n" {
		t.Errorf("mirror under ReposDir not synced: %q", got)
	}
	if got := headSHA(t, localRepo.Path); got != localHead {
		t.Errorf("local checkout outside ReposDir was moved: HEAD %s -> %s", localHead, got)
	}
	if got := readFile(t, filepath.Join(localRepo.Path, "file.txt")); got != "v1\n" {
		t.Errorf("local checkout outside ReposDir was modified: %q", got)
	}
}

func TestSyncOnceContinuesAfterFailingRepo(t *testing.T) {
	reposDir := t.TempDir()
	origin, repo := mirrorUnder(t, reposDir, "healthy")
	commitToOrigin(t, origin, "v2\n", "second")

	// "aaa" sorts before the healthy repo's id, so ListRepos (ORDER BY id)
	// hands the broken one to the pass first.
	broken := store.Repo{ID: "aaa-broken", Path: filepath.Join(reposDir, "gone"), DefaultBranch: "main"}
	s := NewSyncer(newStore(t, broken, repo), &config.Config{
		MirrorSyncInterval: time.Minute,
		ReposDir:           reposDir,
	})
	s.SyncOnce(context.Background())

	if got := readFile(t, filepath.Join(repo.Path, "file.txt")); got != "v2\n" {
		t.Errorf("healthy mirror not synced after a failing one: %q", got)
	}
}

func TestRunSyncsImmediatelyAndOnEveryTick(t *testing.T) {
	reposDir := t.TempDir()
	origin, repo := mirrorUnder(t, reposDir, "demo")
	commitToOrigin(t, origin, "v2\n", "second")

	s := NewSyncer(newStore(t, repo), &config.Config{
		MirrorSyncInterval: 50 * time.Millisecond,
		ReposDir:           reposDir,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	// The immediate pass must land the second commit...
	waitForContent(t, filepath.Join(repo.Path, "file.txt"), "v2\n")
	// ...and a later commit must land without restarting anything, which only
	// the ticker can do.
	commitToOrigin(t, origin, "v3\n", "third")
	waitForContent(t, filepath.Join(repo.Path, "file.txt"), "v3\n")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRunDoesNothingWhenIntervalIsZero(t *testing.T) {
	reposDir := t.TempDir()
	origin, repo := mirrorUnder(t, reposDir, "demo")
	commitToOrigin(t, origin, "v2\n", "second")

	s := NewSyncer(newStore(t, repo), &config.Config{
		MirrorSyncInterval: 0,
		ReposDir:           reposDir,
	})

	done := make(chan struct{})
	go func() { s.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run with interval 0 should return immediately")
	}

	if got := readFile(t, filepath.Join(repo.Path, "file.txt")); got != "v1\n" {
		t.Errorf("mirror was synced despite interval 0: %q", got)
	}
}

// waitForContent polls path until it holds want, or fails the test.
func waitForContent(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			got = string(data)
			if got == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s = %q, want %q within timeout", path, got, want)
}
