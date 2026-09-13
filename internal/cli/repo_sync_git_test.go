package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestMirror builds an origin with two commits and a clone stuck on the
// first one, so a sync has something real to fast-forward.
func newTestMirror(t *testing.T) repoRow {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	clone := filepath.Join(root, "mirror")

	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInTest(t, origin, "-c", "init.defaultBranch=main", "init")
	writeTestFile(t, filepath.Join(origin, "file.txt"), "v1\n")
	gitInTest(t, origin, "add", ".")
	gitInTest(t, origin, "commit", "-m", "one")
	gitInTest(t, root, "clone", origin, clone)

	writeTestFile(t, filepath.Join(origin, "file.txt"), "v2\n")
	gitInTest(t, origin, "commit", "-am", "two")

	return repoRow{ID: "demo", Path: clone, DefaultBranch: "main"}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSyncMirrorsAdvancesRealMirror runs the whole command path — real git,
// real mirror.Sync — over a clone that is one commit behind.
func TestSyncMirrorsAdvancesRealMirror(t *testing.T) {
	m := newTestMirror(t)

	got := syncMirrors(context.Background(), []repoRow{m}, realSyncOps(""), false, time.Now())

	if len(got) != 1 {
		t.Fatalf("outcomes = %+v", got)
	}
	if got[0].Err != nil || got[0].Advanced != 1 || got[0].Blocked != "" {
		t.Fatalf("outcome = %+v, want advanced by 1", got[0])
	}
	if content := readTestFile(t, filepath.Join(m.Path, "file.txt")); content != "v2\n" {
		t.Fatalf("working tree content = %q, want %q", content, "v2\n")
	}
}

// TestSyncMirrorsRepairsDirtyMirrorEndToEnd is the acceptance criterion for
// --repair, over real git: a dirty mirror on a foreign branch is reported
// blocked without the flag, and with it the uncommitted work lands on a
// named rescue branch while the mirror itself catches up.
func TestSyncMirrorsRepairsDirtyMirrorEndToEnd(t *testing.T) {
	m := newTestMirror(t)
	gitInTest(t, m.Path, "checkout", "-b", "feature/someones-work")
	writeTestFile(t, filepath.Join(m.Path, "scratch.txt"), "uncommitted\n")

	without := syncMirrors(context.Background(), []repoRow{m}, realSyncOps(""), false, time.Now())
	if without[0].Blocked == "" {
		t.Fatalf("dirty mirror reported as fine without --repair: %+v", without[0])
	}
	if without[0].Repaired {
		t.Fatalf("mirror repaired without --repair: %+v", without[0])
	}

	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	with := syncMirrors(context.Background(), []repoRow{m}, realSyncOps(""), true, now)
	o := with[0]

	if o.Err != nil {
		t.Fatalf("repair failed: %v", o.Err)
	}
	if o.RescueBranch != "rescue/2026-09-13-120000" {
		t.Fatalf("RescueBranch = %q, want rescue/2026-09-13-120000", o.RescueBranch)
	}
	if o.Blocked != "" || o.Behind != 0 {
		t.Fatalf("outcome = %+v, want unblocked and caught up", o)
	}

	// The rescued work is on the rescue branch, and the mirror is back on
	// its default branch holding origin's content.
	rescued := gitOutput(t, m.Path, "show", o.RescueBranch+":scratch.txt")
	if rescued != "uncommitted" {
		t.Fatalf("rescued content = %q, want %q", rescued, "uncommitted")
	}
	if branch := gitOutput(t, m.Path, "symbolic-ref", "--short", "HEAD"); branch != "main" {
		t.Fatalf("HEAD is on %q, want main", branch)
	}
	if content := readTestFile(t, filepath.Join(m.Path, "file.txt")); content != "v2\n" {
		t.Fatalf("working tree content = %q, want %q", content, "v2\n")
	}
	// The branch someone was working on is left exactly where it was.
	if gitOutput(t, m.Path, "rev-parse", "--verify", "refs/heads/feature/someones-work") == "" {
		t.Fatal("the foreign branch was deleted")
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGitLocal(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}
