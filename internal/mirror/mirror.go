// Package mirror keeps the shared repo mirrors under ~/.rocket/repos/ fresh
// and reports how fresh they are.
//
// The bug this package exists for: `git fetch` moves only remote-tracking
// refs. The mirror's own working tree — the files an agent actually reads
// with Read/Grep/Glob — stays on whatever commit it was cloned at, forever.
// So a mirror can have a perfectly current origin/main and still hand out
// weeks-old file content. Sync closes that gap by advancing the working tree
// too.
//
// The iron rule here is the mirror of workspace's: we NEVER clobber. The
// advance is strictly `git merge --ff-only`, guarded by a clean-tree check
// and a HEAD-on-default-branch check. There is no reset --hard, no
// checkout -f, no clean anywhere in this package. A mirror that cannot be
// advanced safely is left exactly as it is — and the reason is observable
// through Check().Blocked, so a stale mirror can never be read silently.
package mirror

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// Reasons why Sync cannot advance a mirror's working tree. They are shown to
// humans verbatim by the CLI, hence Russian.
const (
	// BlockedDirty: the mirror has uncommitted local changes. Nobody is
	// supposed to edit a mirror, but if someone did we would rather report
	// it than silently overwrite their work.
	BlockedDirty = "локальные изменения в зеркале"
	// BlockedNoFF: HEAD is not an ancestor of origin/<default>, so the
	// update would not be a fast-forward.
	BlockedNoFF = "fast-forward невозможен"
)

// BlockedNotOnDefault builds the Blocked reason for a mirror whose HEAD is
// not on its default branch (including a detached HEAD).
func BlockedNotOnDefault(defaultBranch string) string {
	return fmt.Sprintf("HEAD не на ветке %s", defaultBranch)
}

// Freshness describes one mirror's state. It is computed entirely from local
// data — Check never touches the network.
type Freshness struct {
	// RepoID is the store repo this mirror belongs to.
	RepoID string
	// BehindCommits is how many commits origin/<default_branch> is ahead of
	// the mirror's HEAD.
	BehindCommits int
	// LastFetch is the mtime of the mirror's FETCH_HEAD, i.e. when it last
	// talked to origin. Zero when the mirror has never been fetched.
	LastFetch time.Time
	// Blocked explains why Sync cannot advance the working tree; empty when
	// nothing is in the way.
	Blocked string
	// Stale is true when the mirror must not be trusted as a view of
	// origin: it is behind, blocked, or has not been fetched recently
	// enough.
	Stale bool
}

// Check computes a mirror's freshness. It makes no network calls and no
// changes: it runs the same guards as Sync in read-only form. staleAfter is
// the age of LastFetch beyond which the mirror counts as stale (callers pass
// roughly twice their sync interval); now is injected so callers — and
// tests — control the clock.
func Check(ctx context.Context, repo store.Repo, staleAfter time.Duration, now time.Time) (Freshness, error) {
	if err := validate(repo); err != nil {
		return Freshness{}, err
	}

	fr := Freshness{RepoID: repo.ID}

	if _, err := runGit(ctx, repo.Path, "rev-parse", "--git-dir"); err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: not a git repository at %s: %w", repo.ID, repo.Path, err)
	}

	upstream := "origin/" + repo.DefaultBranch
	if _, err := runGit(ctx, repo.Path, "rev-parse", "--verify", "--quiet", upstream+"^{commit}"); err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: %s not found: %w", repo.ID, upstream, err)
	}

	behindOut, err := runGit(ctx, repo.Path, "rev-list", "--count", "HEAD.."+upstream)
	if err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: count commits behind %s: %w", repo.ID, upstream, err)
	}
	behind, err := strconv.Atoi(strings.TrimSpace(behindOut))
	if err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: parse behind count %q: %w", repo.ID, strings.TrimSpace(behindOut), err)
	}
	fr.BehindCommits = behind

	lastFetch, err := lastFetchTime(ctx, repo.Path)
	if err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: %w", repo.ID, err)
	}
	fr.LastFetch = lastFetch

	fr.Blocked, err = blockedReason(ctx, repo, upstream)
	if err != nil {
		return Freshness{}, fmt.Errorf("mirror %s: %w", repo.ID, err)
	}

	fr.Stale = fr.BehindCommits > 0 ||
		fr.Blocked != "" ||
		fr.LastFetch.IsZero() ||
		now.Sub(fr.LastFetch) > staleAfter

	return fr, nil
}

// blockedReason runs Sync's three guards read-only, in the same order Sync
// applies them, and returns the first one that would stop the fast-forward.
func blockedReason(ctx context.Context, repo store.Repo, upstream string) (string, error) {
	dirty, err := isDirty(ctx, repo.Path)
	if err != nil {
		return "", err
	}
	if dirty {
		return BlockedDirty, nil
	}

	branch, err := currentBranch(ctx, repo.Path)
	if err != nil {
		return "", err
	}
	if branch != repo.DefaultBranch {
		return BlockedNotOnDefault(repo.DefaultBranch), nil
	}

	// `merge-base --is-ancestor` exits 1 (without a message) when HEAD is
	// not an ancestor of upstream, i.e. when the merge would not be a
	// fast-forward.
	if _, err := runGit(ctx, repo.Path, "merge-base", "--is-ancestor", "HEAD", upstream); err != nil {
		return BlockedNoFF, nil
	}

	return "", nil
}

// Sync brings a mirror up to date with origin: it fetches (the only network
// call in this package) and then strictly fast-forwards the working tree to
// origin/<default_branch>.
//
// A mirror that is dirty, off its default branch, or not fast-forwardable is
// left untouched; Sync logs a warning and returns nil, because there is
// nothing the caller can do about it and the reason is separately observable
// through Check. A failing fetch is not fatal either: Sync warns, still
// attempts the fast-forward with the refs already on disk (so an offline
// mirror can at least catch up to its last fetch), and returns the fetch
// error afterwards.
func Sync(ctx context.Context, repo store.Repo) error {
	if err := validate(repo); err != nil {
		return err
	}

	var fetchErr error
	if out, err := runGit(ctx, repo.Path, "fetch", "origin", "--prune"); err != nil {
		fetchErr = fmt.Errorf("mirror %s: fetch origin --prune: %w", repo.ID, err)
		slog.Warn("mirror: fetch failed, continuing with local refs",
			"repo", repo.ID, "path", repo.Path, "error", err, "output", strings.TrimSpace(out))
	}

	dirty, err := isDirty(ctx, repo.Path)
	if err != nil {
		slog.Warn("mirror: cannot determine worktree state, leaving mirror untouched",
			"repo", repo.ID, "path", repo.Path, "error", err)
		return fetchErr
	}
	if dirty {
		slog.Warn("mirror: skipping fast-forward",
			"repo", repo.ID, "path", repo.Path, "blocked", BlockedDirty)
		return fetchErr
	}

	branch, err := currentBranch(ctx, repo.Path)
	if err != nil {
		slog.Warn("mirror: cannot determine current branch, leaving mirror untouched",
			"repo", repo.ID, "path", repo.Path, "error", err)
		return fetchErr
	}
	if branch != repo.DefaultBranch {
		slog.Warn("mirror: skipping fast-forward",
			"repo", repo.ID, "path", repo.Path, "branch", branch,
			"blocked", BlockedNotOnDefault(repo.DefaultBranch))
		return fetchErr
	}

	upstream := "origin/" + repo.DefaultBranch
	if out, err := runGit(ctx, repo.Path, "merge", "--ff-only", upstream); err != nil {
		slog.Warn("mirror: skipping fast-forward",
			"repo", repo.ID, "path", repo.Path, "blocked", BlockedNoFF,
			"error", err, "output", strings.TrimSpace(out))
		return fetchErr
	}

	return fetchErr
}

func validate(repo store.Repo) error {
	if repo.Path == "" {
		return errors.New("mirror: repo.Path must not be empty")
	}
	if repo.DefaultBranch == "" {
		return fmt.Errorf("mirror %s: repo.DefaultBranch must not be empty", repo.ID)
	}
	return nil
}

// isDirty reports whether the mirror has uncommitted changes or untracked
// files.
func isDirty(ctx context.Context, path string) (bool, error) {
	out, err := runGit(ctx, path, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status --porcelain: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

// currentBranch returns the checked-out branch name, or "" for a detached
// HEAD (which `symbolic-ref` reports as an error, not a failure of ours).
func currentBranch(ctx context.Context, path string) (string, error) {
	out, err := runGit(ctx, path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// lastFetchTime returns the mtime of the mirror's FETCH_HEAD, or the zero
// time when the mirror has never been fetched.
func lastFetchTime(ctx context.Context, path string) (time.Time, error) {
	gitDir, err := runGit(ctx, path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return time.Time{}, fmt.Errorf("resolve git dir: %w", err)
	}
	info, err := os.Stat(filepath.Join(strings.TrimSpace(gitDir), "FETCH_HEAD"))
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("stat FETCH_HEAD: %w", err)
	}
	return info.ModTime(), nil
}

// runGit runs `git -C path <args...>`, no shell involved. It returns the
// combined output either way, so callers can log what git actually said.
func runGit(ctx context.Context, path string, args ...string) (string, error) {
	fullArgs := append([]string{"-C", path}, args...)
	out, err := exec.CommandContext(ctx, "git", fullArgs...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
