package mirror

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// git runs a git command in dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// newMirror builds an origin repo with one commit and a mirror clone of it.
// It returns the origin path and a store.Repo pointing at the mirror.
func newMirror(t *testing.T) (string, store.Repo) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	mirror := filepath.Join(root, "mirror")

	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, origin, "-c", "init.defaultBranch=main", "init")
	git(t, origin, "config", "user.email", "test@example.com")
	git(t, origin, "config", "user.name", "Test")
	writeFile(t, filepath.Join(origin, "file.txt"), "v1\n")
	git(t, origin, "add", ".")
	git(t, origin, "commit", "-m", "initial")

	git(t, root, "clone", origin, mirror)
	git(t, mirror, "config", "user.email", "test@example.com")
	git(t, mirror, "config", "user.name", "Test")

	return origin, store.Repo{ID: "demo", Path: mirror, DefaultBranch: "main"}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// commitToOrigin adds a new commit to the origin repo.
func commitToOrigin(t *testing.T, origin, content, msg string) {
	t.Helper()
	writeFile(t, filepath.Join(origin, "file.txt"), content)
	git(t, origin, "add", ".")
	git(t, origin, "commit", "-m", msg)
}

func headSHA(t *testing.T, dir string) string {
	t.Helper()
	return git(t, dir, "rev-parse", "HEAD")
}

// --- Sync ---------------------------------------------------------------

// Regression test for incident #746: `git fetch` alone moves only
// remote-tracking refs, leaving the mirror's working tree — the thing agents
// actually Read/Grep — on an ancient commit.
func TestSyncFastForwardsWorkingTree(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")

	if _, err := Sync(context.Background(), repo); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := readFile(t, filepath.Join(repo.Path, "file.txt")); got != "v2\n" {
		t.Errorf("mirror working tree not fast-forwarded: file.txt = %q, want %q", got, "v2\n")
	}
	if got, want := headSHA(t, repo.Path), headSHA(t, origin); got != want {
		t.Errorf("mirror HEAD = %s, want %s", got, want)
	}
}

func TestSyncUpToDateIsNoop(t *testing.T) {
	_, repo := newMirror(t)
	before := headSHA(t, repo.Path)

	if _, err := Sync(context.Background(), repo); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if after := headSHA(t, repo.Path); after != before {
		t.Errorf("HEAD moved on an up-to-date mirror: %s -> %s", before, after)
	}
}

func TestSyncSkipsDirtyMirror(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")

	dirty := filepath.Join(repo.Path, "file.txt")
	writeFile(t, dirty, "local edit\n")
	before := headSHA(t, repo.Path)

	if _, err := Sync(context.Background(), repo); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := readFile(t, dirty); got != "local edit\n" {
		t.Errorf("dirty mirror was clobbered: file.txt = %q", got)
	}
	if after := headSHA(t, repo.Path); after != before {
		t.Errorf("dirty mirror HEAD moved: %s -> %s", before, after)
	}

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if fr.Blocked != BlockedDirty {
		t.Errorf("Blocked = %q, want %q", fr.Blocked, BlockedDirty)
	}
	if !fr.Stale {
		t.Error("Stale = false, want true for a blocked mirror")
	}
}

func TestSyncSkipsMirrorOnNonDefaultBranch(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	git(t, repo.Path, "checkout", "-b", "side")
	before := headSHA(t, repo.Path)

	if _, err := Sync(context.Background(), repo); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if got := readFile(t, filepath.Join(repo.Path, "file.txt")); got != "v1\n" {
		t.Errorf("mirror on a side branch was advanced: file.txt = %q", got)
	}
	if after := headSHA(t, repo.Path); after != before {
		t.Errorf("HEAD moved: %s -> %s", before, after)
	}

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !strings.Contains(fr.Blocked, "HEAD") {
		t.Errorf("Blocked = %q, want a message mentioning HEAD", fr.Blocked)
	}
	if !strings.Contains(fr.Blocked, "main") {
		t.Errorf("Blocked = %q, want it to name the default branch", fr.Blocked)
	}
}

func TestSyncSkipsDetachedHead(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	git(t, repo.Path, "checkout", "--detach", "HEAD")
	before := headSHA(t, repo.Path)

	if _, err := Sync(context.Background(), repo); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if after := headSHA(t, repo.Path); after != before {
		t.Errorf("detached mirror was advanced: %s -> %s", before, after)
	}

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if fr.Blocked != BlockedNotOnDefault(repo.DefaultBranch) {
		t.Errorf("Blocked = %q, want %q", fr.Blocked, BlockedNotOnDefault(repo.DefaultBranch))
	}
}

func TestSyncSkipsDivergedMirror(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")

	// A local commit on the mirror's default branch makes the update a
	// non-fast-forward.
	writeFile(t, filepath.Join(repo.Path, "local.txt"), "local\n")
	git(t, repo.Path, "add", ".")
	git(t, repo.Path, "commit", "-m", "local work")
	before := headSHA(t, repo.Path)

	if _, err := Sync(context.Background(), repo); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if after := headSHA(t, repo.Path); after != before {
		t.Errorf("diverged mirror was advanced: %s -> %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(repo.Path, "local.txt")); err != nil {
		t.Errorf("local work lost: %v", err)
	}

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if fr.Blocked != BlockedNoFF {
		t.Errorf("Blocked = %q, want %q", fr.Blocked, BlockedNoFF)
	}
}

func TestSyncReturnsFetchErrorButLeavesMirrorIntact(t *testing.T) {
	_, repo := newMirror(t)
	git(t, repo.Path, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "nope"))
	before := headSHA(t, repo.Path)

	_, err := Sync(context.Background(), repo)
	if err == nil {
		t.Fatal("Sync: want an error when fetch fails, got nil")
	}
	if after := headSHA(t, repo.Path); after != before {
		t.Errorf("HEAD moved despite failed fetch: %s -> %s", before, after)
	}
	if got := readFile(t, filepath.Join(repo.Path, "file.txt")); got != "v1\n" {
		t.Errorf("working tree changed despite failed fetch: %q", got)
	}
}

// --- Check --------------------------------------------------------------

func TestCheckBehindCommitsCountsUnmergedOriginCommits(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	commitToOrigin(t, origin, "v3\n", "third")
	git(t, repo.Path, "fetch", "origin", "--prune")

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if fr.BehindCommits != 2 {
		t.Errorf("BehindCommits = %d, want 2", fr.BehindCommits)
	}
	if fr.RepoID != repo.ID {
		t.Errorf("RepoID = %q, want %q", fr.RepoID, repo.ID)
	}
	if !fr.Stale {
		t.Error("Stale = false, want true when behind")
	}
}

func TestCheckNeverFetchedHasZeroLastFetchAndIsStale(t *testing.T) {
	_, repo := newMirror(t)
	// A fresh `git clone` writes FETCH_HEAD; remove it to model a mirror
	// that has never been fetched since it was created.
	if err := os.Remove(filepath.Join(repo.Path, ".git", "FETCH_HEAD")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !fr.LastFetch.IsZero() {
		t.Errorf("LastFetch = %v, want zero", fr.LastFetch)
	}
	if !fr.Stale {
		t.Error("Stale = false, want true when never fetched")
	}
}

func TestCheckFreshlySyncedMirrorIsNotStale(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	if _, err := Sync(context.Background(), repo); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if fr.Stale {
		t.Errorf("Stale = true, want false: %+v", fr)
	}
	if fr.BehindCommits != 0 {
		t.Errorf("BehindCommits = %d, want 0", fr.BehindCommits)
	}
	if fr.Blocked != "" {
		t.Errorf("Blocked = %q, want empty", fr.Blocked)
	}
	if fr.LastFetch.IsZero() {
		t.Error("LastFetch is zero, want the FETCH_HEAD mtime")
	}
}

func TestCheckStaleWhenLastFetchOlderThanThreshold(t *testing.T) {
	_, repo := newMirror(t)

	fr, err := Check(context.Background(), repo, time.Hour, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !fr.Stale {
		t.Errorf("Stale = false, want true when LastFetch is older than staleAfter: %+v", fr)
	}
}

func TestCheckMakesNoNetworkCalls(t *testing.T) {
	_, repo := newMirror(t)
	// Point origin at a path that does not exist: any network/remote access
	// would fail, so a successful Check proves it stayed local.
	git(t, repo.Path, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "nope"))

	if _, err := Check(context.Background(), repo, time.Hour, time.Now()); err != nil {
		t.Fatalf("Check must work offline: %v", err)
	}
}

func TestCheckRejectsNonRepoPath(t *testing.T) {
	repo := store.Repo{ID: "demo", Path: t.TempDir(), DefaultBranch: "main"}
	if _, err := Check(context.Background(), repo, time.Hour, time.Now()); err == nil {
		t.Fatal("want an error for a path that is not a git repo, got nil")
	}
}

func TestBlockedNotOnDefaultNamesTheBranch(t *testing.T) {
	if got := BlockedNotOnDefault("main"); !strings.Contains(got, "main") || !strings.Contains(got, "HEAD") {
		t.Errorf("BlockedNotOnDefault(\"main\") = %q, want it to mention HEAD and main", got)
	}
}

// --- Check: the fields the CLI's repo status table renders ---------------

// TestCheckReportsHeadAndUpstream: `rocket repo status` shows both commit
// ids side by side, which is how a human confirms at a glance that the
// mirror really is the commit they think they are reading.
func TestCheckReportsHeadAndUpstream(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")
	git(t, repo.Path, "fetch", "origin")

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if want := headSHA(t, repo.Path); fr.Head != want {
		t.Fatalf("Head = %q, want %q", fr.Head, want)
	}
	if want := git(t, repo.Path, "rev-parse", "origin/main"); fr.Upstream != want {
		t.Fatalf("Upstream = %q, want %q", fr.Upstream, want)
	}
	if fr.Head == fr.Upstream {
		t.Fatal("Head and Upstream are equal, fixture was supposed to be behind")
	}
}

func TestCheckReportsCheckedOutBranch(t *testing.T) {
	_, repo := newMirror(t)

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if fr.Branch != "main" {
		t.Fatalf("Branch = %q, want %q", fr.Branch, "main")
	}
}

// TestCheckReportsForeignBranch: 22 of 77 measured mirrors sat on some
// orchestrator's feature branch, and the branch name is what tells the human
// whose it is.
func TestCheckReportsForeignBranch(t *testing.T) {
	_, repo := newMirror(t)
	git(t, repo.Path, "checkout", "-b", "feature/whatever")

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if fr.Branch != "feature/whatever" {
		t.Fatalf("Branch = %q, want %q", fr.Branch, "feature/whatever")
	}
}

// TestCheckDetachedHeadHasEmptyBranch: the package does not invent a name
// for a detached HEAD; naming it is the CLI's job.
func TestCheckDetachedHeadHasEmptyBranch(t *testing.T) {
	_, repo := newMirror(t)
	git(t, repo.Path, "checkout", "--detach", "HEAD")

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if fr.Branch != "" {
		t.Fatalf("Branch = %q, want %q for a detached HEAD", fr.Branch, "")
	}
}

func TestCheckReportsCleanTreeAsNotDirty(t *testing.T) {
	_, repo := newMirror(t)

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if fr.Dirty {
		t.Fatal("Dirty = true for a freshly cloned mirror")
	}
}

func TestCheckReportsUncommittedChangesAsDirty(t *testing.T) {
	_, repo := newMirror(t)
	writeFile(t, filepath.Join(repo.Path, "file.txt"), "local edit\n")

	fr, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !fr.Dirty {
		t.Fatal("Dirty = false for a mirror with uncommitted changes")
	}
}

// newEmptyRemoteMirror builds a clone of an origin that has no branches at
// all — the state of a GitHub repository created and never pushed to.
func newEmptyRemoteMirror(t *testing.T) store.Repo {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	mirror := filepath.Join(root, "mirror")

	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, origin, "-c", "init.defaultBranch=main", "init")
	git(t, root, "clone", origin, mirror)

	return store.Repo{ID: "landing", Path: mirror, DefaultBranch: "main"}
}

func TestCheckEmptyRemoteIsReportedAsSuch(t *testing.T) {
	repo := newEmptyRemoteMirror(t)

	_, err := Check(context.Background(), repo, time.Hour, time.Now())
	if !errors.Is(err, ErrEmptyRemote) {
		t.Fatalf("Check error = %v, want one matching ErrEmptyRemote", err)
	}
}

func TestCheckUnbornLocalBranchIsNotAnEmptyRemote(t *testing.T) {
	_, repo := newMirror(t)
	// The `app` case: origin/main exists, but the local branch has no
	// commits. That is a real defect and must keep erroring on its own.
	git(t, repo.Path, "update-ref", "-d", "refs/heads/main")

	_, err := Check(context.Background(), repo, time.Hour, time.Now())
	if err == nil {
		t.Fatal("Check succeeded, want an error for an unborn local branch")
	}
	if errors.Is(err, ErrEmptyRemote) {
		t.Fatalf("Check error = %v, want it distinguishable from ErrEmptyRemote", err)
	}
}

// --- SyncResult ---------------------------------------------------------

// Regression test for the bug found in #3576: a failed `git merge --ff-only`
// was logged and swallowed, so a mirror silently stayed behind while Sync
// reported success. A fresh .git/index.lock is the cheapest way to make the
// merge fail for a reason that is not one of Sync's own guards.
func TestSyncReportsMergeError(t *testing.T) {
	origin, repo := newMirror(t)
	commitToOrigin(t, origin, "v2\n", "second")

	lock := filepath.Join(repo.Path, ".git", "index.lock")
	writeFile(t, lock, "")
	defer func() { _ = os.Remove(lock) }()

	res, err := Sync(context.Background(), repo)
	if err == nil {
		t.Fatal("Sync returned nil; the merge failure must reach the caller")
	}
	if res.MergeErr == nil {
		t.Error("SyncResult.MergeErr is nil, want the failed merge --ff-only")
	}
	if res.Blocked != "" {
		t.Errorf("Blocked = %q; a failing merge is an error, not a guard refusal", res.Blocked)
	}
}
