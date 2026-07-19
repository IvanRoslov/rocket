package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExcludeFromGitLinkedWorktree(t *testing.T) {
	// Create a main repository
	mainDir := t.TempDir()
	if err := exec.Command("git", "-C", mainDir, "init").Run(); err != nil {
		t.Fatalf("git init main repo failed: %v", err)
	}
	// Set git config for testing
	if err := exec.Command("git", "-C", mainDir, "config", "user.email", "test@example.com").Run(); err != nil {
		t.Fatalf("git config user.email failed: %v", err)
	}
	if err := exec.Command("git", "-C", mainDir, "config", "user.name", "Test User").Run(); err != nil {
		t.Fatalf("git config user.name failed: %v", err)
	}

	// Create and commit a file to have a valid commit
	testFile := filepath.Join(mainDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0o644); err != nil {
		t.Fatalf("write test file failed: %v", err)
	}
	if err := exec.Command("git", "-C", mainDir, "add", "test.txt").Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	if err := exec.Command("git", "-C", mainDir, "commit", "-m", "initial commit").Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Create a linked worktree
	worktreeDir := filepath.Join(t.TempDir(), "linked-tree")
	if err := exec.Command("git", "-C", mainDir, "worktree", "add", worktreeDir, "-b", "test-branch").Run(); err != nil {
		t.Fatalf("git worktree add failed: %v", err)
	}
	defer func() {
		// Clean up worktree
		exec.Command("git", "-C", mainDir, "worktree", "remove", worktreeDir).Run()
	}()

	// Call ExcludeFromGit with some paths
	pathsToExclude := []string{".rocket/", "somefile.md"}
	ExcludeFromGit(worktreeDir, pathsToExclude)

	// Resolve the exclude file path using git
	excludePathOut, err := exec.Command("git", "-C", worktreeDir, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		t.Fatalf("git rev-parse --git-path failed: %v", err)
	}
	excludePathRel := strings.TrimSpace(string(excludePathOut))
	excludePath := excludePathRel
	if !filepath.IsAbs(excludePath) {
		excludePath = filepath.Join(worktreeDir, excludePathRel)
	}

	// Read the exclude file and verify entries
	excludeContent, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude file failed: %v", err)
	}

	excludeText := string(excludeContent)
	for _, path := range pathsToExclude {
		if !strings.Contains(excludeText, path) {
			t.Errorf("path %q not found in exclude file. content:\n%s", path, excludeText)
		}
	}

	// Call ExcludeFromGit again to verify idempotency (no duplicates)
	ExcludeFromGit(worktreeDir, pathsToExclude)

	excludeContentAfter, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude file after second call failed: %v", err)
	}

	if string(excludeContentAfter) != excludeText {
		t.Errorf("exclude file changed after second call (duplicates added).\nBefore:\n%s\nAfter:\n%s", excludeText, string(excludeContentAfter))
	}
}
