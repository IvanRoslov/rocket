package mirror

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// repairClock is the fixed instant every repair test hands to Repair, so the
// rescue branch name is deterministic.
var repairClock = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

const repairBranch = "rescue/2026-03-04-050607"

func TestRepairCommitsDirtyWorktreeToRescueBranch(t *testing.T) {
	origin, repo := newMirror(t)
	writeFile(t, filepath.Join(repo.Path, "file.txt"), "local edit\n")
	advanceOrigin(t, origin)

	res, err := Repair(context.Background(), repo, repairClock)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}

	if res.RescueBranch != repairBranch {
		t.Errorf("RescueBranch = %q, want %q", res.RescueBranch, repairBranch)
	}
	if got := git(t, repo.Path, "show", repairBranch+":file.txt"); got != "local edit" {
		t.Errorf("rescued file.txt = %q, want %q", got, "local edit")
	}
	assertRepaired(t, repo)
}

// assertRepaired checks the post-condition every successful Repair must
// reach: a clean tree, HEAD on the default branch, and fully caught up with
// origin — i.e. Check reports the mirror as neither blocked nor behind.
func assertRepaired(t *testing.T, repo store.Repo) {
	t.Helper()
	fr, err := Check(context.Background(), repo, time.Hour, repairClock)
	if err != nil {
		t.Fatalf("Check after Repair: %v", err)
	}
	if fr.Blocked != "" {
		t.Errorf("Blocked = %q, want empty", fr.Blocked)
	}
	if fr.BehindCommits != 0 {
		t.Errorf("BehindCommits = %d, want 0", fr.BehindCommits)
	}
	if branch := git(t, repo.Path, "symbolic-ref", "--short", "HEAD"); branch != repo.DefaultBranch {
		t.Errorf("HEAD on %q, want %q", branch, repo.DefaultBranch)
	}
	if status := git(t, repo.Path, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Errorf("worktree dirty after Repair:\n%s", status)
	}
}

// advanceOrigin adds one commit to origin so a successful Repair has
// something to fast-forward to.
func advanceOrigin(t *testing.T, origin string) {
	t.Helper()
	writeFile(t, filepath.Join(origin, "upstream.txt"), "from origin\n")
	git(t, origin, "add", ".")
	git(t, origin, "commit", "-m", "upstream commit")
}

func TestRepairReturnsDetachedHeadToDefaultBranchWithoutRescueBranch(t *testing.T) {
	origin, repo := newMirror(t)
	git(t, repo.Path, "checkout", "--detach", "HEAD")
	advanceOrigin(t, origin)

	res, err := Repair(context.Background(), repo, repairClock)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}

	if res.RescueBranch != "" {
		t.Errorf("RescueBranch = %q, want empty: nothing was uncommitted", res.RescueBranch)
	}
	assertRepaired(t, repo)
}

func TestRepairRescuesUntrackedFiles(t *testing.T) {
	origin, repo := newMirror(t)
	writeFile(t, filepath.Join(repo.Path, "scratch.txt"), "untracked\n")
	advanceOrigin(t, origin)

	if _, err := Repair(context.Background(), repo, repairClock); err != nil {
		t.Fatalf("Repair: %v", err)
	}

	if got := git(t, repo.Path, "show", repairBranch+":scratch.txt"); got != "untracked" {
		t.Errorf("rescued scratch.txt = %q, want %q", got, "untracked")
	}
	assertRepaired(t, repo)
}

// A mirror is a clone the daemon made unattended, so it has no user.name or
// user.email of its own and may sit under a global config that has none
// either. The rescue commit must still succeed.
func TestRepairCommitsRescueWithoutConfiguredGitIdentity(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "")
	t.Setenv("GIT_AUTHOR_EMAIL", "")
	t.Setenv("GIT_COMMITTER_NAME", "")
	t.Setenv("GIT_COMMITTER_EMAIL", "")
	t.Setenv("EMAIL", "")

	origin, repo := newMirror(t)
	git(t, repo.Path, "config", "--unset", "user.email")
	git(t, repo.Path, "config", "--unset", "user.name")
	git(t, repo.Path, "config", "user.useConfigOnly", "true")
	writeFile(t, filepath.Join(repo.Path, "file.txt"), "local edit\n")
	advanceOrigin(t, origin)

	if _, err := Repair(context.Background(), repo, repairClock); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	assertRepaired(t, repo)
}

func TestRepairOnHealthyMirrorJustSyncs(t *testing.T) {
	origin, repo := newMirror(t)
	advanceOrigin(t, origin)

	res, err := Repair(context.Background(), repo, repairClock)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}

	if res.RescueBranch != "" {
		t.Errorf("RescueBranch = %q, want empty", res.RescueBranch)
	}
	if branches := git(t, repo.Path, "branch", "--list", "rescue/*"); branches != "" {
		t.Errorf("Repair created a rescue branch on a healthy mirror: %q", branches)
	}
	assertRepaired(t, repo)
}

// Two repairs within the same second must not collide: the timestamp alone
// is not unique, and losing the second rescue would be exactly the data loss
// Repair exists to avoid.
func TestRepairSuffixesRescueBranchWhenNameIsTaken(t *testing.T) {
	origin, repo := newMirror(t)
	advanceOrigin(t, origin)

	writeFile(t, filepath.Join(repo.Path, "first.txt"), "first\n")
	first, err := Repair(context.Background(), repo, repairClock)
	if err != nil {
		t.Fatalf("first Repair: %v", err)
	}
	writeFile(t, filepath.Join(repo.Path, "second.txt"), "second\n")
	second, err := Repair(context.Background(), repo, repairClock)
	if err != nil {
		t.Fatalf("second Repair: %v", err)
	}

	if first.RescueBranch != repairBranch {
		t.Errorf("first RescueBranch = %q, want %q", first.RescueBranch, repairBranch)
	}
	if second.RescueBranch == first.RescueBranch {
		t.Fatalf("second Repair reused rescue branch %q", second.RescueBranch)
	}
	if got := git(t, repo.Path, "show", first.RescueBranch+":first.txt"); got != "first" {
		t.Errorf("first rescue lost: first.txt = %q", got)
	}
	if got := git(t, repo.Path, "show", second.RescueBranch+":second.txt"); got != "second" {
		t.Errorf("second rescue lost: second.txt = %q", got)
	}
	assertRepaired(t, repo)
}

func TestRepairReturnsHeadToDefaultAndKeepsTheForeignBranch(t *testing.T) {
	origin, repo := newMirror(t)
	git(t, repo.Path, "checkout", "-b", "orch/some-feature")
	writeFile(t, filepath.Join(repo.Path, "work.txt"), "orchestrator work\n")
	git(t, repo.Path, "add", ".")
	git(t, repo.Path, "commit", "-m", "work in progress")
	advanceOrigin(t, origin)

	res, err := Repair(context.Background(), repo, repairClock)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}

	if !res.Repaired {
		t.Error("Repaired = false, want true: HEAD was moved back to the default branch")
	}
	if res.Blocked != "" {
		t.Errorf("Blocked = %q, want empty", res.Blocked)
	}
	if git(t, repo.Path, "branch", "--list", "orch/some-feature") == "" {
		t.Error("Repair deleted the foreign branch orch/some-feature")
	}
	if got := git(t, repo.Path, "show", "orch/some-feature:work.txt"); got != "orchestrator work" {
		t.Errorf("foreign branch lost its commit: work.txt = %q", got)
	}
	assertRepaired(t, repo)
}

func TestRepairOnCleanUpToDateMirrorChangesNothing(t *testing.T) {
	_, repo := newMirror(t)
	before := git(t, repo.Path, "rev-parse", "HEAD")

	res, err := Repair(context.Background(), repo, repairClock)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}

	if res.Repaired {
		t.Error("Repaired = true, want false: the mirror was already syncable and up to date")
	}
	if res.RescueBranch != "" {
		t.Errorf("RescueBranch = %q, want empty", res.RescueBranch)
	}
	if res.Blocked != "" {
		t.Errorf("Blocked = %q, want empty", res.Blocked)
	}
	if after := git(t, repo.Path, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD moved from %s to %s", before, after)
	}
}

// A default branch that has diverged from origin is not routine drift —
// something wrote history into the mirror. Repair reports it and leaves it
// alone; rewinding it would be the clobber this package refuses to do.
func TestRepairReportsBlockedNoFFInsteadOfRewindingTheDefaultBranch(t *testing.T) {
	origin, repo := newMirror(t)
	writeFile(t, filepath.Join(repo.Path, "local.txt"), "local history\n")
	git(t, repo.Path, "add", ".")
	git(t, repo.Path, "commit", "-m", "local commit on main")
	local := git(t, repo.Path, "rev-parse", "HEAD")
	advanceOrigin(t, origin)

	res, err := Repair(context.Background(), repo, repairClock)
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}

	if res.Blocked != BlockedNoFF {
		t.Errorf("Blocked = %q, want %q", res.Blocked, BlockedNoFF)
	}
	if head := git(t, repo.Path, "rev-parse", "HEAD"); head != local {
		t.Errorf("Repair moved the diverged default branch from %s to %s", local, head)
	}
}

// Several real mirrors have a default_branch in the registry that does not
// match the repository. Repair must fail cleanly there, not half-repair:
// above all it must not strand the local changes on a rescue branch it then
// cannot return from.
func TestRepairFailsCleanlyWhenUpstreamBranchDoesNotResolve(t *testing.T) {
	_, repo := newMirror(t)
	repo.DefaultBranch = "master"
	writeFile(t, filepath.Join(repo.Path, "file.txt"), "local edit\n")

	res, err := Repair(context.Background(), repo, repairClock)
	if err == nil {
		t.Fatalf("Repair returned no error, result %+v", res)
	}
	if branches := git(t, repo.Path, "branch", "--list", "rescue/*"); branches != "" {
		t.Errorf("Repair created a rescue branch despite failing: %q", branches)
	}
	if got := git(t, repo.Path, "show", "HEAD:file.txt"); got != "v1" {
		t.Errorf("HEAD:file.txt = %q, want v1: nothing should have been committed", got)
	}
}
