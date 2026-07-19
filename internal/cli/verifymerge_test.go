package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitFixture builds a bare origin with a main branch, an optional feature
// branch, and a local clone at the returned path. Returns the clone path.
func gitFixture(t *testing.T, withBranch bool, branchMerged bool) string {
	t.Helper()
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.git")
	clone := filepath.Join(dir, "clone")

	run := func(cwd string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run(dir, "init", "--bare", "-b", "main", origin)
	run(dir, "clone", origin, clone)
	run(clone, "commit", "--allow-empty", "-m", "init")
	if err := os.WriteFile(filepath.Join(clone, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(clone, "add", "base.txt")
	run(clone, "commit", "-m", "base")
	run(clone, "push", "origin", "main")

	if withBranch {
		run(clone, "checkout", "-b", "feature/x")
		if err := os.WriteFile(filepath.Join(clone, "feat.txt"), []byte("feature\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run(clone, "add", "feat.txt")
		run(clone, "commit", "-m", "feat")
		run(clone, "push", "origin", "feature/x")
		run(clone, "checkout", "main")
		if branchMerged {
			// Имитация сквош-мержа: main получает тот же контент отдельным
			// коммитом; деревья origin/main и origin/feature/x совпадают.
			run(clone, "cherry-pick", "--no-commit", "feature/x")
			run(clone, "commit", "-m", "feat (squash #1)")
			run(clone, "push", "origin", "main")
		}
	}
	return clone
}

func TestRunGitVerifyMerge_IdenticalTrees(t *testing.T) {
	clone := gitFixture(t, true, true)
	res, err := runGitVerifyMerge(context.Background(), clone, "main", "feature/x")
	if err != nil {
		t.Fatalf("runGitVerifyMerge: %v", err)
	}
	if !res.branchExists {
		t.Fatalf("branchExists = false, want true")
	}
	if res.treeDiff != "" {
		t.Errorf("treeDiff = %q, want empty (squash-merged content identical)", res.treeDiff)
	}
	report := formatVerifyMergeReport("main", "feature/x", res)
	if !strings.Contains(report, "✅") {
		t.Errorf("report = %q, want success verdict", report)
	}
}

func TestRunGitVerifyMerge_NotMerged(t *testing.T) {
	clone := gitFixture(t, true, false)
	res, err := runGitVerifyMerge(context.Background(), clone, "main", "feature/x")
	if err != nil {
		t.Fatalf("runGitVerifyMerge: %v", err)
	}
	if res.treeDiff == "" {
		t.Fatalf("treeDiff empty, want difference (branch not merged)")
	}
	if !strings.Contains(res.treeDiff, "feat.txt") || !strings.Contains(res.ownDiff, "feat.txt") {
		t.Errorf("diffs must mention feat.txt: tree=%q own=%q", res.treeDiff, res.ownDiff)
	}
	report := formatVerifyMergeReport("main", "feature/x", res)
	if !strings.Contains(report, "Собственные изменения ветки") || strings.Contains(report, "✅") {
		t.Errorf("report = %q, want non-success verdict with own-changes section", report)
	}
}

func TestRunGitVerifyMerge_BranchDeletedOnOrigin(t *testing.T) {
	clone := gitFixture(t, false, false)
	res, err := runGitVerifyMerge(context.Background(), clone, "main", "feature/x")
	if err != nil {
		t.Fatalf("runGitVerifyMerge: %v", err)
	}
	if res.branchExists {
		t.Fatalf("branchExists = true, want false")
	}
	report := formatVerifyMergeReport("main", "feature/x", res)
	if !strings.Contains(report, "удалена на origin") {
		t.Errorf("report = %q, want deleted-branch explanation", report)
	}
}

func TestVerifyMergeUsage(t *testing.T) {
	cmd := newVerifyMergeCmd()
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected usage error without args")
	}
}
