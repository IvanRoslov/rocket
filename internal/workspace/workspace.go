// Package workspace manages per-session git worktrees carved out of a
// repo's main checkout. It is a correctness-critical component: it
// manipulates users' git repositories on disk. The iron rule throughout
// this package is that we NEVER delete a branch — only worktree
// directories are ever removed; branch refs are left for the user (or a
// future Restore) to recover.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/IvanRoslov/rocket/internal/store"
)

// CreateResult is returned by Create.
type CreateResult struct {
	// Path is the absolute path to the created worktree.
	Path string
	// BranchCollision is true when the requested branch already existed
	// (e.g. survived a prior Destroy) and Create reused it rather than
	// creating a new branch.
	BranchCollision bool
}

// Workspace manages git worktrees for agent sessions.
type Workspace interface {
	// Create sets up a new git worktree for sessionID on the given
	// branch, based off repo. If the branch already exists it is reused
	// (CreateResult.BranchCollision=true) rather than treated as an
	// error. Symlinks and post_create commands configured on repo are
	// applied after the worktree is created; a post_create failure fails
	// Create but leaves the worktree directory in place for debugging.
	Create(ctx context.Context, repo store.Repo, sessionID, branch string) (CreateResult, error)
	// Restore recreates a worktree that has gone missing on disk (e.g.
	// deleted out-of-band), reusing the existing branch. If the worktree
	// directory is already present, Restore is a no-op and simply
	// returns its path.
	Restore(ctx context.Context, repo store.Repo, sessionID, branch string) (string, error)
	// Destroy removes the worktree directory for sessionID. It never
	// deletes the underlying branch. Destroying a session whose worktree
	// is already gone is not an error (idempotent).
	Destroy(ctx context.Context, repo store.Repo, sessionID string) error
}

// New returns a Workspace that creates worktrees under
// <worktreesDir>/<repo-id>/<session-id>/.
func New(worktreesDir string) Workspace {
	return &gitWorkspace{worktreesDir: worktreesDir}
}

type gitWorkspace struct {
	worktreesDir string
}

func (w *gitWorkspace) worktreePath(repo store.Repo, sessionID string) string {
	return filepath.Join(w.worktreesDir, repo.ID, sessionID)
}

// idPattern validates that an ID contains only lowercase letters, digits, and hyphens.
var idPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

func validateInputs(repo store.Repo, sessionID, branch string) error {
	if repo.ID == "" {
		return errors.New("repo.ID must not be empty")
	}
	if !idPattern.MatchString(repo.ID) {
		return fmt.Errorf("invalid repo.ID %q: must contain only lowercase letters, digits, and hyphens", repo.ID)
	}
	if sessionID == "" {
		return errors.New("sessionID must not be empty")
	}
	if !idPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid sessionID %q: must contain only lowercase letters, digits, and hyphens", sessionID)
	}
	if branch == "" {
		return errors.New("branch must not be empty")
	}
	if strings.Contains(branch, "..") {
		return fmt.Errorf("invalid branch %q: must not contain \"..\"", branch)
	}
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch %q: must not start with \"-\"", branch)
	}
	return nil
}

// runGit runs `git -C repoPath <args...>`, no shell involved.
func runGit(ctx context.Context, repoPath string, args ...string) (string, error) {
	fullArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// verifyRef reports whether ref resolves to a valid object in repoPath.
func verifyRef(ctx context.Context, repoPath, ref string) bool {
	_, err := runGit(ctx, repoPath, "rev-parse", "--verify", ref)
	return err == nil
}

func (w *gitWorkspace) Create(ctx context.Context, repo store.Repo, sessionID, branch string) (CreateResult, error) {
	if err := validateInputs(repo, sessionID, branch); err != nil {
		return CreateResult{}, err
	}

	path := w.worktreePath(repo, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CreateResult{}, fmt.Errorf("create parent dir: %w", err)
	}

	// 1. Fetch is best-effort: offline operation must still succeed.
	if _, err := runGit(ctx, repo.Path, "fetch", "origin"); err != nil {
		slog.Warn("workspace: git fetch origin failed, continuing offline", "repo", repo.ID, "error", err)
	}

	// 2. Determine base ref.
	base := "origin/" + repo.DefaultBranch
	if !verifyRef(ctx, repo.Path, base) {
		if verifyRef(ctx, repo.Path, repo.DefaultBranch) {
			base = repo.DefaultBranch
		} else {
			return CreateResult{}, fmt.Errorf("base branch not found: neither origin/%s nor %s exist", repo.DefaultBranch, repo.DefaultBranch)
		}
	}

	// 3. Branch collision check.
	result := CreateResult{Path: path}
	branchExists := verifyRef(ctx, repo.Path, "refs/heads/"+branch)
	if branchExists {
		result.BranchCollision = true
		if _, err := runGit(ctx, repo.Path, "worktree", "add", path, branch); err != nil {
			return CreateResult{}, fmt.Errorf("worktree add (existing branch): %w", err)
		}
	} else {
		if _, err := runGit(ctx, repo.Path, "worktree", "add", "-b", branch, path, base); err != nil {
			return CreateResult{}, fmt.Errorf("worktree add (new branch): %w", err)
		}
	}

	// 4. Symlinks.
	for _, rel := range repo.Symlinks {
		// Reject traversal attempts and absolute paths.
		if strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			slog.Warn("workspace: symlink entry rejected (traversal/absolute)", "repo", repo.ID, "path", rel)
			continue
		}

		src := filepath.Join(repo.Path, rel)
		dst := filepath.Join(path, rel)

		if _, err := os.Lstat(src); err != nil {
			slog.Warn("workspace: symlink source missing, skipping", "repo", repo.ID, "path", rel, "error", err)
			continue
		}
		if _, err := os.Lstat(dst); err == nil {
			slog.Warn("workspace: symlink destination already exists, skipping", "repo", repo.ID, "path", rel)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return CreateResult{Path: path}, fmt.Errorf("create symlink parent dir for %q: %w", rel, err)
		}
		if err := os.Symlink(src, dst); err != nil {
			return CreateResult{Path: path}, fmt.Errorf("symlink %q: %w", rel, err)
		}
	}

	// 5. post_create commands.
	for _, command := range repo.PostCreate {
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir = path
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			// Leave the worktree in place for debugging; do not attempt
			// cleanup here.
			return CreateResult{Path: path}, fmt.Errorf("post_create command %q failed: %w (output: %s)", command, err, strings.TrimSpace(string(out)))
		}
	}

	return result, nil
}

func (w *gitWorkspace) Destroy(ctx context.Context, repo store.Repo, sessionID string) error {
	if repo.ID == "" {
		return errors.New("repo.ID must not be empty")
	}
	if !idPattern.MatchString(repo.ID) {
		return fmt.Errorf("invalid repo.ID %q: must contain only lowercase letters, digits, and hyphens", repo.ID)
	}
	if sessionID == "" {
		return errors.New("sessionID must not be empty")
	}
	if !idPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid sessionID %q: must contain only lowercase letters, digits, and hyphens", sessionID)
	}

	path := w.worktreePath(repo, sessionID)

	_, err := runGit(ctx, repo.Path, "worktree", "remove", "--force", path)
	if err != nil {
		slog.Warn("workspace: git worktree remove failed, falling back to manual cleanup", "repo", repo.ID, "session", sessionID, "error", err)
		if rmErr := os.RemoveAll(path); rmErr != nil {
			return fmt.Errorf("remove worktree dir: %w", rmErr)
		}
	}

	// Always prune, so git's own bookkeeping is idempotent regardless of
	// which path above was taken (this call never touches branches).
	if _, err := runGit(ctx, repo.Path, "worktree", "prune"); err != nil {
		// If the worktree dir is already gone, prune failure is non-fatal.
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			slog.Warn("workspace: git worktree prune failed but worktree dir already removed", "repo", repo.ID, "session", sessionID, "error", err)
			return nil
		}
		return fmt.Errorf("worktree prune: %w", err)
	}

	return nil
}

func (w *gitWorkspace) Restore(ctx context.Context, repo store.Repo, sessionID, branch string) (string, error) {
	if err := validateInputs(repo, sessionID, branch); err != nil {
		return "", err
	}

	path := w.worktreePath(repo, sessionID)

	if _, err := runGit(ctx, repo.Path, "worktree", "prune"); err != nil {
		return "", fmt.Errorf("worktree prune: %w", err)
	}

	if _, err := runGit(ctx, repo.Path, "fetch", "origin"); err != nil {
		slog.Warn("workspace: git fetch origin failed during restore, continuing offline", "repo", repo.ID, "error", err)
	}

	if _, err := os.Stat(path); err == nil {
		// Already present: no-op.
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat worktree dir: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create parent dir: %w", err)
	}

	if !verifyRef(ctx, repo.Path, "refs/heads/"+branch) {
		return "", fmt.Errorf("branch %q does not exist, cannot restore", branch)
	}

	if _, err := runGit(ctx, repo.Path, "worktree", "add", path, branch); err != nil {
		return "", fmt.Errorf("worktree add (restore): %w", err)
	}

	return path, nil
}
