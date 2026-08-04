package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirror"
)

func TestRepoAddWrongArgCountIsUsageError(t *testing.T) {
	cmd := newRepoAddCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestRepoAddGithubAndPathIsUsageError(t *testing.T) {
	cmd := newRepoAddCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--github", "acme/widgets", "some/path"})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestRepoLsWrongArgCountIsUsageError(t *testing.T) {
	cmd := newRepoLsCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

func TestRepoRmWrongArgCountIsUsageError(t *testing.T) {
	cmd := newRepoRmCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

// TestRenderRepos checks that the repo table is unchanged and that a
// freshness line follows for every mirror. The lines are printed in full,
// not squeezed into a column: the blocked reason is the loudest thing this
// feature has to say and it must never be truncated.
func TestRenderRepos(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	repos := []repoRow{
		{ID: "rocket", Path: "/home/u/.rocket/repos/rocket", DefaultBranch: "main"},
		{ID: "app", Path: "/home/u/.rocket/repos/app", DefaultBranch: "master"},
	}
	mirrors := []mirrorRow{
		{RepoID: "rocket", Fresh: mirror.Freshness{LastFetch: now.Add(-2 * time.Minute)}},
		{RepoID: "app", Fresh: mirror.Freshness{
			BehindCommits: 5, Blocked: mirror.BlockedNoFF, LastFetch: now.Add(-time.Hour), Stale: true,
		}},
	}

	var buf bytes.Buffer
	renderRepos(repos, mirrors, &buf, now)
	out := buf.String()

	for _, want := range []string{
		"ID", "PATH", "BRANCH",
		"/home/u/.rocket/repos/rocket", "master",
		"mirror rocket: свежее (последний fetch 2 мин назад)",
		"mirror app: ПРОТУХЛО — синхронизация не может обновить дерево: fast-forward невозможен",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
	if strings.Index(out, "BRANCH") > strings.Index(out, "mirror rocket") {
		t.Errorf("expected the mirror block after the table, got:\n%s", out)
	}
}

// TestReposWithMirrorJSON checks that --json carries the same freshness the
// human output does: an agent reading the machine format must not be the one
// consumer left in the dark about a stale mirror.
func TestReposWithMirrorJSON(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	raw := []map[string]any{
		{"id": "rocket", "path": "/p/rocket", "default_branch": "main", "auto_cleanup": true},
		{"id": "broken", "path": "/p/broken", "default_branch": "main"},
	}
	mirrors := []mirrorRow{
		{RepoID: "rocket", Fresh: mirror.Freshness{BehindCommits: 3, LastFetch: now.Add(-time.Hour), Stale: true}},
		{RepoID: "broken", Err: errors.New("boom")},
	}

	got := reposWithMirror(raw, mirrors)

	if len(got) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(got))
	}
	if got[0]["auto_cleanup"] != true {
		t.Errorf("expected the original repo fields to survive, got: %v", got[0])
	}
	m, ok := got[0]["mirror"].(mirrorJSON)
	if !ok {
		t.Fatalf("expected a mirror field on the first repo, got: %v", got[0]["mirror"])
	}
	if m.BehindCommits != 3 || !m.Stale {
		t.Errorf("expected behind=3 stale=true, got: %+v", m)
	}
	if m.LastFetch == "" {
		t.Errorf("expected last_fetch to be set, got: %+v", m)
	}
	broken, ok := got[1]["mirror"].(mirrorJSON)
	if !ok {
		t.Fatalf("expected a mirror field on the second repo, got: %v", got[1]["mirror"])
	}
	if broken.Error != "boom" {
		t.Errorf("expected the check error in JSON, got: %+v", broken)
	}
}
