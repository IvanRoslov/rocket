package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// runGit runs git in dir and fails the test on error.
func testGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// setupOriginAndClone creates a bare-ish origin repo (with a commit on
// "main") and a local clone of it, returning the local clone path. The
// local clone has origin/main available for fetch.
func setupOriginAndClone(t *testing.T) (originPath, clonePath string) {
	t.Helper()
	base := t.TempDir()
	originPath = filepath.Join(base, "origin")
	clonePath = filepath.Join(base, "clone")

	if err := os.MkdirAll(originPath, 0o755); err != nil {
		t.Fatal(err)
	}
	testGit(t, originPath, "init", "-b", "main")
	testGit(t, originPath, "config", "user.email", "test@example.com")
	testGit(t, originPath, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(originPath, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, originPath, "add", "README.md")
	testGit(t, originPath, "commit", "-m", "initial commit")

	// Clone the origin so the local repo has an "origin" remote and
	// origin/main ref, but do the actual git operations against clonePath.
	cmd := exec.Command("git", "clone", originPath, clonePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	testGit(t, clonePath, "config", "user.email", "test@example.com")
	testGit(t, clonePath, "config", "user.name", "Test")

	return originPath, clonePath
}

func testRepo(clonePath string) store.Repo {
	return store.Repo{
		ID:            "repo1",
		Path:          clonePath,
		DefaultBranch: "main",
	}
}

func TestCreate_MakesDirAndBranchFromDefault(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)

	repo := testRepo(clonePath)
	res, err := ws.Create(context.Background(), repo, "sess1", "feature-x")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.BranchCollision {
		t.Errorf("expected no branch collision on first create")
	}

	wantPath := filepath.Join(worktreesDir, "repo1", "sess1")
	if res.Path != wantPath {
		t.Errorf("Path = %q, want %q", res.Path, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("worktree dir missing: %v", err)
	}

	// branch exists in clone
	out := testGit(t, clonePath, "branch", "--list", "feature-x")
	if !strings.Contains(out, "feature-x") {
		t.Errorf("branch feature-x not found: %q", out)
	}

	// worktree is checked out on feature-x
	branchOut := strings.TrimSpace(testGit(t, wantPath, "rev-parse", "--abbrev-ref", "HEAD"))
	if branchOut != "feature-x" {
		t.Errorf("worktree HEAD branch = %q, want feature-x", branchOut)
	}
}

func TestCreate_AfterDestroy_BranchCollisionAndReuse(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	ctx := context.Background()

	res1, err := ws.Create(ctx, repo, "sess1", "feature-y")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	// Commit something on the branch inside the first worktree.
	if err := os.WriteFile(filepath.Join(res1.Path, "extra.txt"), []byte("extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, res1.Path, "add", "extra.txt")
	testGit(t, res1.Path, "commit", "-m", "extra commit on feature-y")

	if err := ws.Destroy(ctx, repo, "sess1"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	res2, err := ws.Create(ctx, repo, "sess2", "feature-y")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if !res2.BranchCollision {
		t.Errorf("expected BranchCollision=true on reuse")
	}

	// The extra commit should be visible - i.e. prior commits on the
	// branch are preserved.
	if _, err := os.Stat(filepath.Join(res2.Path, "extra.txt")); err != nil {
		t.Errorf("extra.txt missing in reused worktree: %v", err)
	}
}

func TestCreate_Symlinks(t *testing.T) {
	origin, clonePath := setupOriginAndClone(t)
	_ = origin

	// Add a node_modules dir to the clone's working tree (untracked, just
	// present on disk, simulating installed deps in the main checkout).
	nm := filepath.Join(clonePath, "node_modules")
	if err := os.MkdirAll(filepath.Join(nm, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nm, "pkg", "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	repo.Symlinks = []string{"node_modules", "missing_dir"}

	res, err := ws.Create(context.Background(), repo, "sess1", "feature-sym")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	link := filepath.Join(res.Path, "node_modules")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", link, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to be a symlink", link)
	}
	if _, err := os.Stat(filepath.Join(link, "pkg", "index.js")); err != nil {
		t.Errorf("symlink target content not reachable: %v", err)
	}

	// missing_dir symlink source doesn't exist - should be skipped, no error.
	if _, err := os.Lstat(filepath.Join(res.Path, "missing_dir")); !os.IsNotExist(err) {
		t.Errorf("expected missing_dir to not exist, got err=%v", err)
	}
}

func TestCreate_PostCreateRuns(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	repo.PostCreate = []string{"touch marker"}

	res, err := ws.Create(context.Background(), repo, "sess1", "feature-pc")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "marker")); err != nil {
		t.Errorf("marker file missing: %v", err)
	}
}

func TestCreate_PostCreateFailure_LeavesWorktree(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	repo.PostCreate = []string{"exit 1"}

	res, err := ws.Create(context.Background(), repo, "sess1", "feature-fail")
	if err == nil {
		t.Fatalf("expected Create to fail")
	}

	wantPath := filepath.Join(worktreesDir, "repo1", "sess1")
	if res.Path != "" && res.Path != wantPath {
		t.Errorf("unexpected Path on error: %q", res.Path)
	}
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Errorf("worktree dir should remain for debugging: %v", statErr)
	}
}

func TestDestroy_RemovesDirKeepsBranch(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	ctx := context.Background()

	res, err := ws.Create(ctx, repo, "sess1", "feature-d")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ws.Destroy(ctx, repo, "sess1"); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	if _, err := os.Stat(res.Path); !os.IsNotExist(err) {
		t.Errorf("expected worktree dir removed, got err=%v", err)
	}

	out := testGit(t, clonePath, "branch", "--list", "feature-d")
	if !strings.Contains(out, "feature-d") {
		t.Errorf("branch feature-d should still exist: %q", out)
	}
}

func TestDestroy_Idempotent(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	ctx := context.Background()

	if _, err := ws.Create(ctx, repo, "sess1", "feature-idem"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := ws.Destroy(ctx, repo, "sess1"); err != nil {
		t.Fatalf("first Destroy: %v", err)
	}
	if err := ws.Destroy(ctx, repo, "sess1"); err != nil {
		t.Fatalf("second Destroy should be idempotent: %v", err)
	}
}

func TestRestore_AfterManualRemoval(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	ctx := context.Background()

	res, err := ws.Create(ctx, repo, "sess1", "feature-r")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := os.WriteFile(filepath.Join(res.Path, "restored.txt"), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testGit(t, res.Path, "add", "restored.txt")
	testGit(t, res.Path, "commit", "-m", "commit before manual rm")

	// Simulate manual removal (e.g. disk cleanup) without going through
	// Destroy - the worktree directory is gone but git still thinks it's
	// registered until "worktree prune" runs.
	if err := os.RemoveAll(res.Path); err != nil {
		t.Fatal(err)
	}

	path, err := ws.Restore(ctx, repo, "sess1", "feature-r")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if path != res.Path {
		t.Errorf("Restore path = %q, want %q", path, res.Path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("restored worktree dir missing: %v", err)
	}

	log := testGit(t, path, "log", "--oneline")
	if !strings.Contains(log, "commit before manual rm") {
		t.Errorf("restored worktree missing prior commit: %q", log)
	}
}

func TestRestore_NoOpWhenPresent(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	ctx := context.Background()

	res, err := ws.Create(ctx, repo, "sess1", "feature-noop")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	path, err := ws.Restore(ctx, repo, "sess1", "feature-noop")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if path != res.Path {
		t.Errorf("Restore path = %q, want %q", path, res.Path)
	}
}

func TestValidation_EmptyInputsRejected(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	ctx := context.Background()

	if _, err := ws.Create(ctx, repo, "", "branch"); err == nil {
		t.Errorf("expected error for empty sessionID")
	}
	if _, err := ws.Create(ctx, repo, "sess", ""); err == nil {
		t.Errorf("expected error for empty branch")
	}
	badRepo := repo
	badRepo.ID = ""
	if _, err := ws.Create(ctx, badRepo, "sess", "branch"); err == nil {
		t.Errorf("expected error for empty repo.ID")
	}
	if _, err := ws.Create(ctx, repo, "sess", "-flag-like"); err == nil {
		t.Errorf("expected error for branch starting with -")
	}
	if _, err := ws.Create(ctx, repo, "sess", "has..dotdot"); err == nil {
		t.Errorf("expected error for branch containing ..")
	}
}

func TestValidation_RejectsTraversalIDs(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	ctx := context.Background()

	// Test repo.ID with ".."
	badRepo := repo
	badRepo.ID = ".."
	if _, err := ws.Create(ctx, badRepo, "sess1", "branch"); err == nil {
		t.Errorf("expected error for repo.ID with ..")
	}
	if err := ws.Destroy(ctx, badRepo, "sess1"); err == nil {
		t.Errorf("expected error for Destroy with repo.ID ..")
	}
	if _, err := ws.Restore(ctx, badRepo, "sess1", "branch"); err == nil {
		t.Errorf("expected error for Restore with repo.ID ..")
	}

	// Test sessionID with traversal
	badSess := "../x"
	if _, err := ws.Create(ctx, repo, badSess, "branch"); err == nil {
		t.Errorf("expected error for sessionID ../x")
	}
	if err := ws.Destroy(ctx, repo, badSess); err == nil {
		t.Errorf("expected error for Destroy with sessionID ../x")
	}
	if _, err := ws.Restore(ctx, repo, badSess, "branch"); err == nil {
		t.Errorf("expected error for Restore with sessionID ../x")
	}

	// Test IDs with invalid characters
	badRepo2 := repo
	badRepo2.ID = "repo_underscore"
	if _, err := ws.Create(ctx, badRepo2, "sess1", "branch"); err == nil {
		t.Errorf("expected error for repo.ID with underscore")
	}
}

func TestSymlinks_SkipsTraversalEntries(t *testing.T) {
	_, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)

	// Add a real directory and an evil traversal entry
	nm := filepath.Join(clonePath, "node_modules")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}

	// Include both valid and invalid symlink entries
	repo.Symlinks = []string{
		"node_modules",   // valid
		"../evil",        // traversal
		"/absolute/path", // absolute
	}

	res, err := ws.Create(context.Background(), repo, "sess1", "sym-test")
	if err != nil {
		t.Fatalf("Create should succeed despite invalid entries: %v", err)
	}

	// Verify valid symlink was created
	link := filepath.Join(res.Path, "node_modules")
	info, err := os.Lstat(link)
	if err != nil {
		t.Errorf("expected valid symlink at %s: %v", link, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to be a symlink", link)
	}

	// Verify evil entries were not created
	for _, evil := range []string{"../evil", "/absolute/path"} {
		evilPath := filepath.Join(res.Path, evil)
		if _, err := os.Lstat(evilPath); !os.IsNotExist(err) {
			t.Errorf("evil entry %q should not exist (err=%v)", evil, err)
		}
	}
}

func TestDestroy_RepoPathGone(t *testing.T) {
	origin, clonePath := setupOriginAndClone(t)
	worktreesDir := t.TempDir()
	ws := New(worktreesDir)
	repo := testRepo(clonePath)
	ctx := context.Background()

	res, err := ws.Create(ctx, repo, "sess1", "feature-gone")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Simulate repo being deleted out-of-band (e.g., disk cleanup)
	if err := os.RemoveAll(origin); err != nil {
		t.Fatalf("failed to remove origin: %v", err)
	}

	// Destroy should succeed despite repo.Path being gone and prune failing
	err = ws.Destroy(ctx, repo, "sess1")
	if err != nil {
		t.Errorf("Destroy should succeed when worktree dir is gone: %v", err)
	}

	// Verify worktree dir was actually removed
	if _, statErr := os.Stat(res.Path); !os.IsNotExist(statErr) {
		t.Errorf("worktree dir should be removed, got err=%v", statErr)
	}
}
