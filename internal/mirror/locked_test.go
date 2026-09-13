package mirror

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirrorlock"
	"github.com/IvanRoslov/rocket/internal/store"
)

// lockedOpts is the options a test uses against a repos dir it owns: a long
// enough timeout that a passing test never waits on it, and the production
// index.lock threshold.
func lockedOpts(reposDir string) LockOptions {
	return LockOptions{ReposDir: reposDir, Timeout: 10 * time.Second, IndexLockMaxAge: 10 * time.Minute}
}

// reposDirFor is the repos dir a mirror built by newMirror lives in: the
// mirror's own parent, so LocksDir and StateDir land beside it.
func reposDirFor(repo store.Repo) string { return filepath.Dir(repo.Path) }

func TestSyncLockedFastForwardsAndRecordsTheSync(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	reposDir := reposDirFor(repo)

	res, err := SyncLocked(context.Background(), repo, lockedOpts(reposDir), OpRepoSync)
	if err != nil {
		t.Fatalf("SyncLocked: %v", err)
	}
	if res.Advanced != 1 {
		t.Errorf("Advanced = %d, want 1", res.Advanced)
	}
	if got := readFile(t, filepath.Join(repo.Path, "file.txt")); got != "v2\n" {
		t.Errorf("working tree = %q, want %q", got, "v2\n")
	}

	st, err := ReadState(StateDir(reposDir), repo.ID)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if st.By != OpRepoSync {
		t.Errorf("recorded By = %q, want %q", st.By, OpRepoSync)
	}
}

// The whole point of the lock: a second writer must not race git. It waits,
// and when it cannot wait long enough it says who is holding the mirror
// instead of proceeding.
func TestSyncLockedReportsBusyMirrorWithoutTouchingIt(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	reposDir := reposDirFor(repo)

	held, err := mirrorlock.Acquire(context.Background(), mirrorlock.LocksDir(reposDir), repo.ID, OpSyncer, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	before := headSHA(t, repo.Path)
	opts := lockedOpts(reposDir)
	opts.Timeout = 50 * time.Millisecond

	_, err = SyncLocked(context.Background(), repo, opts, OpRepoSync)
	var busy *mirrorlock.ErrBusy
	if !errors.As(err, &busy) {
		t.Fatalf("err = %v, want *mirrorlock.ErrBusy", err)
	}
	if busy.Holder.Operation != OpSyncer {
		t.Errorf("holder operation = %q, want %q", busy.Holder.Operation, OpSyncer)
	}
	if after := headSHA(t, repo.Path); after != before {
		t.Errorf("a busy mirror was modified: HEAD %s → %s", before, after)
	}
}

// An abandoned index.lock is what left 26 mirrors unusable in #3576. Holding
// the mirror lock already excludes every rocket writer, so an old one is
// removed and the sync proceeds.
func TestSyncLockedReapsStaleIndexLockAndRecordsIt(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	reposDir := reposDirFor(repo)

	lock := filepath.Join(repo.Path, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}

	res, err := SyncLocked(context.Background(), repo, lockedOpts(reposDir), OpRepoSync)
	if err != nil {
		t.Fatalf("SyncLocked: %v", err)
	}
	if !res.IndexLockRemoved {
		t.Error("IndexLockRemoved = false, want true")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Error("stale index.lock survived the sync")
	}
	if res.Advanced != 1 {
		t.Errorf("Advanced = %d, want 1 — the reaped lock should not have stopped the sync", res.Advanced)
	}

	st, err := ReadState(StateDir(reposDir), repo.ID)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if !st.IndexLockRemoved {
		t.Error("sidecar IndexLockRemoved = false, want true")
	}
}

// A fresh index.lock belongs to a git that is still running. Reaping it
// would corrupt exactly the operation we are trying to protect.
func TestSyncLockedLeavesFreshIndexLockAlone(t *testing.T) {
	_, repo := newMirror(t)
	reposDir := reposDirFor(repo)

	lock := filepath.Join(repo.Path, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := SyncLocked(context.Background(), repo, lockedOpts(reposDir), OpRepoSync)
	if err != nil {
		t.Fatalf("SyncLocked: %v", err)
	}
	if res.IndexLockRemoved {
		t.Error("IndexLockRemoved = true for a fresh lock")
	}
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("fresh index.lock must be left alone: %v", err)
	}
}

// The race the feature exists for: several writers of the same mirror at
// once. Every one of them must come back with a consistent mirror, and none
// may fail — they take turns instead of racing git.
func TestSyncLockedSerialisesConcurrentSyncs(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	reposDir := reposDirFor(repo)
	want := headSHA(t, origin)

	const writers = 4
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = SyncLocked(context.Background(), repo, lockedOpts(reposDir), OpRepoSync)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}
	if got := headSHA(t, repo.Path); got != want {
		t.Errorf("mirror HEAD = %s, want %s", got, want)
	}
	if got := readFile(t, filepath.Join(repo.Path, "file.txt")); got != "v2\n" {
		t.Errorf("working tree = %q, want %q", got, "v2\n")
	}
}

func TestRepairLockedRepairsAndRecordsUnderTheLock(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	reposDir := reposDirFor(repo)
	writeFile(t, filepath.Join(repo.Path, "dirt.txt"), "local\n")

	res, err := RepairLocked(context.Background(), repo, time.Now(), lockedOpts(reposDir), OpRepoSyncRepair)
	if err != nil {
		t.Fatalf("RepairLocked: %v", err)
	}
	if res.RescueBranch == "" {
		t.Error("RescueBranch is empty, want the branch the local changes went to")
	}
	if res.Blocked != "" {
		t.Errorf("Blocked = %q, want the mirror repaired", res.Blocked)
	}

	st, err := ReadState(StateDir(reposDir), repo.ID)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if st.By != OpRepoSyncRepair {
		t.Errorf("recorded By = %q, want %q", st.By, OpRepoSyncRepair)
	}
}

// A repair on a busy mirror is the #3576 failure verbatim: the commit landed
// and the checkout lost the race, leaving 18 mirrors on rescue/*. It must
// not start at all.
func TestRepairLockedRefusesABusyMirror(t *testing.T) {
	_, repo := newMirror(t)
	reposDir := reposDirFor(repo)
	writeFile(t, filepath.Join(repo.Path, "dirt.txt"), "local\n")

	held, err := mirrorlock.Acquire(context.Background(), mirrorlock.LocksDir(reposDir), repo.ID, OpSyncer, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	opts := lockedOpts(reposDir)
	opts.Timeout = 50 * time.Millisecond

	_, err = RepairLocked(context.Background(), repo, time.Now(), opts, OpRepoSyncRepair)
	var busy *mirrorlock.ErrBusy
	if !errors.As(err, &busy) {
		t.Fatalf("err = %v, want *mirrorlock.ErrBusy", err)
	}
	if branch := currentBranchOf(t, repo.Path); branch != "main" {
		t.Errorf("a refused repair moved HEAD to %q", branch)
	}
}

// WithLock is the entry point for the one writer that is not a sync: the
// workspace clone, which runs `git worktree add` inside the mirror.
func TestWithLockRunsTheWorkUnderTheMirrorLock(t *testing.T) {
	_, repo := newMirror(t)
	reposDir := reposDirFor(repo)

	var lockedDuringWork bool
	err := WithLock(context.Background(), repo, lockedOpts(reposDir), OpWorkspaceClone, func(ctx context.Context) error {
		probe, err := mirrorlock.Acquire(ctx, mirrorlock.LocksDir(reposDir), repo.ID, "probe", 20*time.Millisecond)
		if err == nil {
			probe.Release()
			return nil
		}
		lockedDuringWork = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock: %v", err)
	}
	if !lockedDuringWork {
		t.Error("the mirror was not locked while the work ran")
	}
}

func TestWithLockReturnsBusyWithoutRunningTheWork(t *testing.T) {
	_, repo := newMirror(t)
	reposDir := reposDirFor(repo)

	held, err := mirrorlock.Acquire(context.Background(), mirrorlock.LocksDir(reposDir), repo.ID, OpSyncer, time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	opts := lockedOpts(reposDir)
	opts.Timeout = 50 * time.Millisecond

	ran := false
	err = WithLock(context.Background(), repo, opts, OpWorkspaceClone, func(context.Context) error {
		ran = true
		return nil
	})
	var busy *mirrorlock.ErrBusy
	if !errors.As(err, &busy) {
		t.Fatalf("err = %v, want *mirrorlock.ErrBusy", err)
	}
	if ran {
		t.Error("the work ran on a mirror we do not hold")
	}
}

// WithLock must hand the caller's own error back unchanged: the clone's
// failure is the clone's, not the lock's.
func TestWithLockPropagatesTheWorkError(t *testing.T) {
	_, repo := newMirror(t)
	want := errors.New("worktree add failed")

	err := WithLock(context.Background(), repo, lockedOpts(reposDirFor(repo)), OpWorkspaceClone,
		func(context.Context) error { return want })
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

// A caller with no repos dir has nowhere to put a lock file. It still syncs —
// that is how the tests and any host without a mirror root work — rather
// than failing over bookkeeping it never asked for.
func TestSyncLockedWithoutReposDirStillSyncs(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")

	res, err := SyncLocked(context.Background(), repo, LockOptions{Timeout: time.Second}, OpRepoSync)
	if err != nil {
		t.Fatalf("SyncLocked: %v", err)
	}
	if res.Advanced != 1 {
		t.Errorf("Advanced = %d, want 1", res.Advanced)
	}
}

// currentBranchOf is the checked-out branch of a mirror, for the tests that
// assert a refused operation moved nothing.
func currentBranchOf(t *testing.T, path string) string {
	t.Helper()
	return git(t, path, "rev-parse", "--abbrev-ref", "HEAD")
}
