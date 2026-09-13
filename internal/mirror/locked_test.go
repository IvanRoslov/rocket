package mirror

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/config"
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

// The git dir is asked of git, never assumed to be <path>/.git. A linked
// worktree's .git is a FILE pointing elsewhere, and reaping the wrong
// directory would leave the real abandoned lock in place — silently turning
// the reaper into a no-op for exactly the repos that have one.
func TestSyncLockedReapsTheRealGitDirOfALinkedWorktree(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	reposDir := reposDirFor(repo)

	linked := filepath.Join(reposDir, "linked")
	// The linked worktree is on its own branch: git refuses to check out
	// main twice, and the reaper is what this test is about, not the
	// fast-forward.
	git(t, repo.Path, "worktree", "add", "-b", "side", linked)
	side := store.Repo{ID: "linked", Path: linked, DefaultBranch: "side"}

	gitDir := git(t, linked, "rev-parse", "--absolute-git-dir")
	if gitDir == filepath.Join(linked, ".git") {
		t.Fatalf("fixture is not a linked worktree: git dir is %s", gitDir)
	}
	lock := filepath.Join(gitDir, "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}

	res, err := SyncLocked(context.Background(), side, lockedOpts(reposDir), OpRepoSync)
	if err != nil {
		t.Fatalf("SyncLocked: %v", err)
	}
	if !res.IndexLockRemoved {
		t.Error("IndexLockRemoved = false — the reaper looked in the wrong directory")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Error("the linked worktree's index.lock survived")
	}
}

// The acceptance test for #3589, in the shape of the incident itself.
//
// What happened: `rocket repo sync --repair` ran against a live daemon. The
// repair's `commit` to rescue/<ts> succeeded and its `checkout <default>`
// lost the race for git's index.lock and failed — leaving 18 mirrors with
// HEAD stranded on rescue/* (a mirror off its default branch is never
// advanced again) and 26 abandoned index.lock files. The repair left the
// mirrors worse than it found them.
//
// So: several dirty mirrors repaired concurrently with a looping background
// sweep, and then the two assertions from the incident report — every mirror
// back on its default branch, and not one index.lock left on disk.
//
// This test earns its keep only if it fails without the lock, and it was
// checked: with the Acquire in withMirrorLock short-circuited, three runs
// produced an abandoned index.lock and a fetch that could not lock
// refs/remotes/origin/main — the incident, reproduced. Do not shrink the
// mirror count, the sweep loop or the bulk of the working tree; with a
// three-file repo the index-writing commands are over before anything can
// collide with them and the test passes unlocked, proving nothing.
func TestConcurrentRepairAndSweepStrandNoMirror(t *testing.T) {
	reposDir := t.TempDir()
	const mirrors = 6

	repos := make([]store.Repo, 0, mirrors)
	for i := range mirrors {
		id := fmt.Sprintf("m%d", i)
		origin, repo := mirrorUnder(t, reposDir, id)
		// Each mirror is in exactly the state the incident found: work
		// nobody committed, sitting on somebody's branch, behind origin.
		commitToOrigin(t, origin, "v2\n", "second")
		git(t, repo.Path, "checkout", "-b", "someones/work")
		// A working tree with some bulk to it. `git status`, `commit` and
		// `checkout` all write the index, and on a three-file repo they are
		// over before anything can collide with them — the race window has
		// to be wide enough for the test to be worth running.
		fillWorktree(t, repo.Path, 400)
		repos = append(repos, repo)
	}

	st := newStore(t, repos...)
	sweeper := NewSyncer(st, &config.Config{
		MirrorSyncInterval:          time.Minute,
		ReposDir:                    reposDir,
		MirrorLockTimeoutBackground: 5 * time.Second,
		MirrorIndexLockMaxAge:       10 * time.Minute,
	})

	// The daemon's sweep, running the whole time the repairs do.
	sweepCtx, stopSweep := context.WithCancel(context.Background())
	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		for sweepCtx.Err() == nil {
			sweeper.SyncOnce(sweepCtx)
		}
	}()

	opts := LockOptions{ReposDir: reposDir, Timeout: 60 * time.Second, IndexLockMaxAge: 10 * time.Minute}
	var wg sync.WaitGroup
	errs := make([]error, len(repos))
	for i, repo := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = RepairLocked(context.Background(), repo, time.Now(), opts, OpRepoSyncRepair)
		}()
	}
	wg.Wait()
	stopSweep()
	<-sweepDone

	for i, err := range errs {
		if err != nil {
			t.Errorf("repair of %s: %v", repos[i].ID, err)
		}
	}

	// The two numbers from the incident report, both of which must be zero.
	stranded, leftoverLocks := 0, 0
	for _, repo := range repos {
		if branch := currentBranchOf(t, repo.Path); branch != repo.DefaultBranch {
			stranded++
			t.Errorf("mirror %s stranded on %q, want %q", repo.ID, branch, repo.DefaultBranch)
		}
		if _, err := os.Stat(filepath.Join(repo.Path, ".git", "index.lock")); err == nil {
			leftoverLocks++
			t.Errorf("mirror %s left an index.lock behind", repo.ID)
		}
		// The rescued work is still there — repair never discards it.
		if out := git(t, repo.Path, "branch", "--list", "rescue/*"); out == "" {
			t.Errorf("mirror %s has no rescue branch: the uncommitted work went nowhere", repo.ID)
		}
	}
	if stranded != 0 || leftoverLocks != 0 {
		t.Errorf("stranded mirrors = %d, abandoned index.lock files = %d, want 0 and 0", stranded, leftoverLocks)
	}
}

// fillWorktree writes n untracked files, giving git's index-writing commands
// enough to do that a concurrent one can actually collide with them.
func fillWorktree(t *testing.T, path string, n int) {
	t.Helper()
	dir := filepath.Join(path, "bulk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("f%03d.txt", i)), strings.Repeat("x", 512)+"\n")
	}
}
