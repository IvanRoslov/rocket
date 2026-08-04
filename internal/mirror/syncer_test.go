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

func TestSyncOnceSyncsEveryRepo(t *testing.T) {
	originA, repoA := newMirror(t)
	originB, repoB := newMirror(t)
	repoA.ID, repoB.ID = "a", "b"
	commitToOrigin(t, originA, "a2\n", "second a")
	commitToOrigin(t, originB, "b2\n", "second b")

	s := NewSyncer(newStore(t, repoA, repoB), &config.Config{MirrorSyncInterval: time.Minute})
	s.SyncOnce(context.Background())

	if got := readFile(t, filepath.Join(repoA.Path, "file.txt")); got != "a2\n" {
		t.Errorf("mirror a working tree = %q, want %q", got, "a2\n")
	}
	if got := readFile(t, filepath.Join(repoB.Path, "file.txt")); got != "b2\n" {
		t.Errorf("mirror b working tree = %q, want %q", got, "b2\n")
	}
}

func TestSyncOnceContinuesAfterFailingRepo(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")

	// "aaa" sorts before the healthy repo's id, so ListRepos (ORDER BY id)
	// hands the broken one to the pass first.
	broken := store.Repo{ID: "aaa-broken", Path: filepath.Join(t.TempDir(), "gone"), DefaultBranch: "main"}
	s := NewSyncer(newStore(t, broken, repo), &config.Config{MirrorSyncInterval: time.Minute})
	s.SyncOnce(context.Background())

	if got := readFile(t, filepath.Join(repo.Path, "file.txt")); got != "v2\n" {
		t.Errorf("healthy mirror not synced after a failing one: %q", got)
	}
}

func TestRunSyncsImmediatelyAndOnEveryTick(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")

	s := NewSyncer(newStore(t, repo), &config.Config{MirrorSyncInterval: 50 * time.Millisecond})
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
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")

	s := NewSyncer(newStore(t, repo), &config.Config{MirrorSyncInterval: 0})

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
