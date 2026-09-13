package mirror

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// rescueBranchLayout is the timestamp layout for rescue branch names. It is
// UTC, sortable, and free of the characters git refuses in a ref name.
const rescueBranchLayout = "2006-01-02-150405"

// The rescue commit's message and identity. The identity is spelled out
// because a mirror is a clone the daemon made unattended: it has no
// user.name or user.email of its own, and may well sit under a global config
// that has none either. An identity git has to guess is one it can also
// refuse to guess.
const (
	rescueCommitMessage = "rescue: uncommitted mirror changes"
	rescueUserName      = "rocket"
	rescueUserEmail     = "rocket@localhost"
)

// maxRescueBranchAttempts bounds the search for a free rescue branch name.
// Reaching it means something is wrong with the mirror, not that repairs are
// genuinely that frequent.
const maxRescueBranchAttempts = 100

// RepairResult describes what Repair did and what it could not do, so a
// caller can tell the user where their work went and whether the mirror is
// usable now.
type RepairResult struct {
	// RescueBranch is the branch the mirror's uncommitted changes were
	// committed to, or "" when there was nothing to rescue.
	RescueBranch string
	// Repaired is true when Repair changed anything at all: a rescue
	// commit, a move back to the default branch, or a fast-forward.
	Repaired bool
	// Blocked is why the mirror still cannot be advanced, empty when it
	// can. It carries the same reasons Check reports.
	Blocked string
	// Sync is what the closing Sync observed. It carries the fetch and
	// merge errors that used to exist only as a log line, so a repair that
	// fixed the branch but could not fast-forward is not reported as a
	// clean success.
	Sync SyncResult
}

// Repair brings a mirror that Sync refuses to advance back into a syncable
// state without losing anything. Sync's own refusal is deliberate — see this
// package's doc — so Repair is the explicit, human-triggered way out of it,
// and it clobbers just as little as Sync does:
//
//  1. uncommitted changes (untracked files included) are committed to a
//     rescue/<timestamp> branch, never discarded;
//  2. HEAD returns to the default branch, and whatever branch was checked
//     out stays exactly where it was — it is never deleted or moved;
//  3. Sync fast-forwards the mirror as usual;
//  4. anything still in the way is reported in Blocked rather than forced.
//
// Step 4 is the whole reason BlockedNoFF is not repaired here. Making a
// diverged default branch fast-forwardable means moving it off commits it
// holds, and a mirror whose default branch has its own history is not
// routine drift: something wrote into it, and that wants a human, not an
// automatic rewind.
//
// now is injected so the rescue branch name is predictable; Repair never
// reads the clock itself.
func Repair(ctx context.Context, repo store.Repo, now time.Time) (res RepairResult, err error) {
	if err := validate(repo); err != nil {
		return res, err
	}

	upstream := "origin/" + repo.DefaultBranch

	// Everything below either commits to a rescue branch or moves HEAD, and
	// both are pointless — worse, a half-repair that strands the local
	// changes on a branch we cannot return from — if there is no upstream to
	// come back to. Several real mirrors are registered with a
	// default_branch the repository does not have, so this is checked
	// before anything is touched, not after.
	if _, err := runGit(ctx, repo.Path, "rev-parse", "--verify", "--quiet", upstream+"^{commit}"); err != nil {
		return res, fmt.Errorf("mirror %s: %s not found: %w", repo.ID, upstream, err)
	}

	headBefore, err := revParseHead(ctx, repo.Path)
	if err != nil {
		return res, fmt.Errorf("mirror %s: %w", repo.ID, err)
	}

	dirty, err := isDirty(ctx, repo.Path)
	if err != nil {
		return res, fmt.Errorf("mirror %s: %w", repo.ID, err)
	}
	if dirty {
		branch, err := freeRescueBranch(ctx, repo.Path, now)
		if err != nil {
			return res, fmt.Errorf("mirror %s: %w", repo.ID, err)
		}
		if _, err := runGit(ctx, repo.Path, "checkout", "-b", branch); err != nil {
			return res, fmt.Errorf("mirror %s: create rescue branch %s: %w", repo.ID, branch, err)
		}
		if _, err := runGit(ctx, repo.Path, "add", "-A"); err != nil {
			return res, fmt.Errorf("mirror %s: stage rescued changes: %w", repo.ID, err)
		}
		if _, err := runGitEnv(ctx, repo.Path, rescueIdentityEnv(),
			"commit", "-m", rescueCommitMessage); err != nil {
			return res, fmt.Errorf("mirror %s: commit rescued changes: %w", repo.ID, err)
		}
		res.RescueBranch = branch
		res.Repaired = true
	}

	// Plain `checkout <branch>`, never -f: the tree is clean by now, so a
	// checkout that would still need forcing means something we have not
	// understood, and refusing is the right answer.
	branch, err := currentBranch(ctx, repo.Path)
	if err != nil {
		return res, fmt.Errorf("mirror %s: %w", repo.ID, err)
	}
	if branch != repo.DefaultBranch {
		if _, err := runGit(ctx, repo.Path, "checkout", repo.DefaultBranch); err != nil {
			return res, fmt.Errorf("mirror %s: return HEAD to %s: %w", repo.ID, repo.DefaultBranch, err)
		}
		res.Repaired = true
	}

	syncRes, syncErr := Sync(ctx, repo)
	res.Sync = syncRes

	headAfter, err := revParseHead(ctx, repo.Path)
	if err != nil {
		return res, fmt.Errorf("mirror %s: %w", repo.ID, err)
	}
	if headAfter != headBefore {
		res.Repaired = true
	}

	// Sync leaves a mirror it cannot advance exactly as it was and says so
	// only in the log, so ask the same guards Check asks and hand the reason
	// back to the caller.
	res.Blocked, err = blockedReason(ctx, repo, upstream)
	if err != nil {
		return res, fmt.Errorf("mirror %s: %w", repo.ID, err)
	}

	return res, syncErr
}

// freeRescueBranch picks a rescue branch name that does not exist yet.
// Second-resolution timestamps collide — two repairs in the same second are
// entirely possible — and a collision would abort the second rescue, losing
// exactly the work Repair exists to preserve. So a taken name gets a -2, -3,
// ... suffix.
func freeRescueBranch(ctx context.Context, path string, now time.Time) (string, error) {
	base := "rescue/" + now.UTC().Format(rescueBranchLayout)
	for n := 1; n <= maxRescueBranchAttempts; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		// rev-parse --verify exits non-zero precisely when the ref does not
		// resolve, which is the free name we are looking for.
		if _, err := runGit(ctx, path, "rev-parse", "--verify", "--quiet", "refs/heads/"+name); err != nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("no free rescue branch name after %d attempts at %s", maxRescueBranchAttempts, base)
}

// rescueIdentityEnv is the author and committer the rescue commit is made
// under. It goes in the environment rather than in `-c user.name=...`
// because GIT_AUTHOR_*/GIT_COMMITTER_* outrank every config source,
// including a hostile empty value inherited from the daemon's environment.
func rescueIdentityEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=" + rescueUserName,
		"GIT_AUTHOR_EMAIL=" + rescueUserEmail,
		"GIT_COMMITTER_NAME=" + rescueUserName,
		"GIT_COMMITTER_EMAIL=" + rescueUserEmail,
	}
}

// revParseHead resolves HEAD to a commit id, which is how Repair tells
// whether it actually changed anything.
func revParseHead(ctx context.Context, path string) (string, error) {
	out, err := runGit(ctx, path, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}
