package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/mirror"
	"github.com/IvanRoslov/rocket/internal/store"
)

// TestWorkspaceSeesCommitPushedAfterClone is the end-to-end regression guard
// for the staleness bug: a mirror cloned before a commit landed in origin
// used to hand out pre-commit file content forever, and the worktree carved
// out of it branched off that stale point. Nothing in this test ever runs a
// manual `git pull`: freshness must come from mirror.Sync alone.
func TestWorkspaceSeesCommitPushedAfterClone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	base := t.TempDir()
	originPath := filepath.Join(base, "origin.git")
	seedPath := filepath.Join(base, "seed")
	mirrorPath := filepath.Join(base, "mirror")
	worktreesDir := filepath.Join(base, "worktrees")

	// 1. A real local bare repo playing the role of origin, seeded through
	//    a throwaway clone.
	testGit(t, base, "init", "--bare", "-b", "main", originPath)
	testGit(t, base, "clone", originPath, seedPath)
	testGit(t, seedPath, "config", "user.email", "test@example.com")
	testGit(t, seedPath, "config", "user.name", "Test")
	writeFile(t, filepath.Join(seedPath, "VERSION"), "v1\n")
	testGit(t, seedPath, "add", "VERSION")
	testGit(t, seedPath, "commit", "-m", "v1")
	testGit(t, seedPath, "push", "origin", "main")

	// 2. The mirror is cloned at v1 — this is ~/.rocket/repos/<repo>.
	testGit(t, base, "clone", originPath, mirrorPath)

	// 3. A new commit lands in origin *after* the mirror was cloned.
	writeFile(t, filepath.Join(seedPath, "VERSION"), "v2\n")
	testGit(t, seedPath, "add", "VERSION")
	testGit(t, seedPath, "commit", "-m", "v2")
	testGit(t, seedPath, "push", "origin", "main")
	wantSHA := strings.TrimSpace(testGit(t, seedPath, "rev-parse", "HEAD"))

	repo := store.Repo{ID: "freshness", Path: mirrorPath, DefaultBranch: "main"}

	// 4. Sync the mirror, then carve out a workspace — exactly what the
	//    daemon does on spawn.
	if _, err := mirror.Sync(ctx, repo); err != nil {
		t.Fatalf("mirror.Sync: %v", err)
	}
	res, err := New(worktreesDir).Create(ctx, repo, "sess", "feature/x")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 5a. The mirror's own working tree — the files agents read directly —
	//     must be at the new commit.
	if got := readFile(t, filepath.Join(mirrorPath, "VERSION")); got != "v2\n" {
		t.Errorf("mirror working tree stale: VERSION = %q, want %q", got, "v2\n")
	}
	if got := strings.TrimSpace(testGit(t, mirrorPath, "rev-parse", "HEAD")); got != wantSHA {
		t.Errorf("mirror HEAD = %s, want %s", got, wantSHA)
	}

	// 5b. And the workspace carved out of it must contain the new commit.
	if got := readFile(t, filepath.Join(res.Path, "VERSION")); got != "v2\n" {
		t.Errorf("workspace stale: VERSION = %q, want %q", got, "v2\n")
	}
	if got := strings.TrimSpace(testGit(t, res.Path, "rev-parse", "HEAD")); got != wantSHA {
		t.Errorf("workspace HEAD = %s, want %s", got, wantSHA)
	}
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
