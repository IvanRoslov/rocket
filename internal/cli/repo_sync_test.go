package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirror"
	"github.com/IvanRoslov/rocket/internal/store"
)

// TestSelectMirrorsAll: with no ids given, every mirror is selected and
// nothing is reported as unknown.
func TestSelectMirrorsAll(t *testing.T) {
	mirrors := []repoRow{{ID: "rocket"}, {ID: "app"}}

	got, unknown := selectMirrors(mirrors, nil)

	if !reflect.DeepEqual(got, mirrors) {
		t.Fatalf("selected = %v, want %v", got, mirrors)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
}

// TestSelectMirrorsByID keeps the requested mirrors in the order the user
// named them, so the report reads back in the order it was asked for.
func TestSelectMirrorsByID(t *testing.T) {
	mirrors := []repoRow{{ID: "rocket"}, {ID: "app"}, {ID: "web"}}

	got, unknown := selectMirrors(mirrors, []string{"web", "rocket"})

	want := []repoRow{{ID: "web"}, {ID: "rocket"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected = %v, want %v", got, want)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown = %v, want none", unknown)
	}
}

// TestSelectMirrorsUnknownID: an id that is not a mirror is reported back
// rather than silently skipped — a typo that syncs nothing must say so.
func TestSelectMirrorsUnknownID(t *testing.T) {
	mirrors := []repoRow{{ID: "rocket"}}

	got, unknown := selectMirrors(mirrors, []string{"rocket", "nope"})

	if len(got) != 1 || got[0].ID != "rocket" {
		t.Fatalf("selected = %v, want [rocket]", got)
	}
	if !reflect.DeepEqual(unknown, []string{"nope"}) {
		t.Fatalf("unknown = %v, want [nope]", unknown)
	}
}

// TestSyncLineAdvanced: the ordinary outcome names how far the working tree
// moved, because "synced" alone does not tell a reader whether the mirror
// they were about to read was days behind.
func TestSyncLineAdvanced(t *testing.T) {
	got := syncLine(syncOutcome{RepoID: "rocket", Advanced: 5})
	want := "mirror rocket: обновлено на 5 коммитов"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

func TestSyncLineAlreadyCurrent(t *testing.T) {
	got := syncLine(syncOutcome{RepoID: "rocket"})
	want := "mirror rocket: уже актуально"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

// TestSyncLineBlocked carries the mirror package's reason verbatim: a
// blocked mirror is a normal, reportable outcome, not a failure.
func TestSyncLineBlocked(t *testing.T) {
	got := syncLine(syncOutcome{RepoID: "app", Blocked: mirror.BlockedDirty})
	want := "mirror app: заблокировано: локальные изменения в зеркале"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

// TestSyncLineRepairedNamesRescueBranch: the one thing a user must never
// have to go looking for is where their uncommitted work went.
func TestSyncLineRepairedNamesRescueBranch(t *testing.T) {
	got := syncLine(syncOutcome{
		RepoID: "app", Advanced: 3, Repaired: true, RescueBranch: "rescue/2026-09-13-120000",
	})
	want := "mirror app: починено (изменения сохранены в ветке rescue/2026-09-13-120000), обновлено на 3 коммита"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

// TestSyncLineRepairedButStillBlocked: Repair deliberately does not fix a
// diverged default branch, so say so instead of implying success.
func TestSyncLineRepairedButStillBlocked(t *testing.T) {
	got := syncLine(syncOutcome{
		RepoID: "web", Repaired: true, RescueBranch: "rescue/2026-09-13-120000", Blocked: mirror.BlockedNoFF,
	})
	want := "mirror web: починено (изменения сохранены в ветке rescue/2026-09-13-120000), но заблокировано: fast-forward невозможен"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

func TestSyncLineError(t *testing.T) {
	got := syncLine(syncOutcome{RepoID: "landing", Err: errors.New("origin/main not found")})
	want := "mirror landing: ошибка — origin/main not found"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

// TestRenderSyncReportsUnknownIDs: an id that matched no mirror is printed,
// never swallowed.
func TestRenderSyncReportsUnknownIDs(t *testing.T) {
	var buf bytes.Buffer
	renderSync([]syncOutcome{{RepoID: "rocket", Advanced: 1}}, []string{"nope"}, &buf)
	out := buf.String()

	for _, want := range []string{
		"mirror rocket: обновлено на 1 коммит",
		"неизвестное зеркало: nope",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderSync output missing %q:\n%s", want, out)
		}
	}
}

// fakeOps is a syncOps whose git is scripted, so the flow through sync,
// blocked-detection and repair is testable without a git repository.
func fakeOps() *syncOps {
	return &syncOps{
		head:  func(context.Context, string) (string, error) { return "before", nil },
		count: func(context.Context, string, string, string) (int, error) { return 0, nil },
		sync:  func(context.Context, store.Repo) (mirror.SyncResult, error) { return mirror.SyncResult{}, nil },
		check: func(context.Context, store.Repo) (mirror.Freshness, error) { return mirror.Freshness{}, nil },
		repair: func(context.Context, store.Repo, time.Time) (mirror.RepairResult, error) {
			return mirror.RepairResult{}, nil
		},
	}
}

var testNow = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

// TestSyncMirrorsCountsTheAdvance: the number comes from HEAD before and
// after, because Sync fetches and merges in one step and the pre-sync behind
// count would miss whatever that fetch brought in.
func TestSyncMirrorsCountsTheAdvance(t *testing.T) {
	ops := fakeOps()
	heads := []string{"aaa", "bbb"}
	ops.head = func(context.Context, string) (string, error) {
		h := heads[0]
		if len(heads) > 1 {
			heads = heads[1:]
		}
		return h, nil
	}
	ops.count = func(_ context.Context, _, from, to string) (int, error) {
		if from != "aaa" || to != "bbb" {
			t.Fatalf("counted %s..%s, want aaa..bbb", from, to)
		}
		return 7, nil
	}

	got := syncMirrors(context.Background(), []repoRow{{ID: "rocket"}}, ops, false, testNow)

	if len(got) != 1 || got[0].Advanced != 7 {
		t.Fatalf("outcomes = %+v, want one advanced by 7", got)
	}
}

// TestSyncMirrorsReportsBlockedWithoutRepairing: without --repair a blocked
// mirror is reported and left alone.
func TestSyncMirrorsReportsBlockedWithoutRepairing(t *testing.T) {
	ops := fakeOps()
	ops.check = func(context.Context, store.Repo) (mirror.Freshness, error) {
		return mirror.Freshness{Blocked: mirror.BlockedDirty}, nil
	}
	ops.repair = func(context.Context, store.Repo, time.Time) (mirror.RepairResult, error) {
		t.Fatal("repair called without --repair")
		return mirror.RepairResult{}, nil
	}

	got := syncMirrors(context.Background(), []repoRow{{ID: "app"}}, ops, false, testNow)

	if len(got) != 1 || got[0].Blocked != mirror.BlockedDirty || got[0].Repaired {
		t.Fatalf("outcomes = %+v, want one blocked and unrepaired", got)
	}
}

// TestSyncMirrorsRepairsBlockedMirror: with --repair the rescue branch comes
// back in the outcome, so the report can name where the work went.
func TestSyncMirrorsRepairsBlockedMirror(t *testing.T) {
	ops := fakeOps()
	ops.check = func(context.Context, store.Repo) (mirror.Freshness, error) {
		return mirror.Freshness{Blocked: mirror.BlockedDirty}, nil
	}
	ops.repair = func(context.Context, store.Repo, time.Time) (mirror.RepairResult, error) {
		return mirror.RepairResult{RescueBranch: "rescue/2026-09-13-120000", Repaired: true}, nil
	}

	got := syncMirrors(context.Background(), []repoRow{{ID: "app"}}, ops, true, testNow)

	if len(got) != 1 {
		t.Fatalf("outcomes = %+v", got)
	}
	o := got[0]
	if !o.Repaired || o.RescueBranch != "rescue/2026-09-13-120000" || o.Blocked != "" {
		t.Fatalf("outcome = %+v, want repaired onto a named rescue branch and unblocked", o)
	}
}

// TestSyncMirrorsKeepsBlockedAfterRepair: Repair deliberately refuses a
// diverged default branch, and the report must keep saying so.
func TestSyncMirrorsKeepsBlockedAfterRepair(t *testing.T) {
	ops := fakeOps()
	ops.check = func(context.Context, store.Repo) (mirror.Freshness, error) {
		return mirror.Freshness{Blocked: mirror.BlockedNoFF}, nil
	}
	ops.repair = func(context.Context, store.Repo, time.Time) (mirror.RepairResult, error) {
		return mirror.RepairResult{Blocked: mirror.BlockedNoFF}, nil
	}

	got := syncMirrors(context.Background(), []repoRow{{ID: "web"}}, ops, true, testNow)

	if len(got) != 1 || got[0].Blocked != mirror.BlockedNoFF {
		t.Fatalf("outcomes = %+v, want still blocked", got)
	}
}

// TestSyncMirrorsCarriesPerMirrorErrors: one mirror we cannot even resolve
// HEAD in must not stop the others.
func TestSyncMirrorsCarriesPerMirrorErrors(t *testing.T) {
	ops := fakeOps()
	ops.head = func(_ context.Context, path string) (string, error) {
		if path == "/broken" {
			return "", errors.New("not a git repository")
		}
		return "aaa", nil
	}

	got := syncMirrors(context.Background(),
		[]repoRow{{ID: "landing", Path: "/broken"}, {ID: "rocket"}}, ops, false, testNow)

	if len(got) != 2 {
		t.Fatalf("outcomes = %+v, want two", got)
	}
	if got[0].Err == nil {
		t.Fatalf("broken mirror reported no error: %+v", got[0])
	}
	if got[1].Err != nil {
		t.Fatalf("healthy mirror spoiled by its neighbour: %+v", got[1])
	}
}

// TestSyncExitErrorOnlyWhenNothingCouldBeProcessed: an individual block is a
// normal, reportable outcome, not a failure of the command.
func TestSyncExitErrorOnlyWhenNothingCouldBeProcessed(t *testing.T) {
	blocked := []syncOutcome{{RepoID: "app", Blocked: mirror.BlockedDirty}}
	if err := syncExitError(blocked); err != nil {
		t.Fatalf("blocked mirror must not fail the command, got %v", err)
	}

	mixed := []syncOutcome{{RepoID: "landing", Err: errors.New("boom")}, {RepoID: "rocket"}}
	if err := syncExitError(mixed); err != nil {
		t.Fatalf("one broken mirror out of two must not fail the command, got %v", err)
	}

	allBroken := []syncOutcome{{RepoID: "landing", Err: errors.New("boom")}}
	if syncExitError(allBroken) == nil {
		t.Fatal("no mirror could be processed, want a non-nil error")
	}

	if syncExitError(nil) == nil {
		t.Fatal("no mirrors at all, want a non-nil error")
	}
}

// TestSyncLineReportsFetchFailure: Sync fast-forwards from the refs already
// on disk when fetch fails, so the mirror may well have advanced — but it
// advanced to a stale origin, and the line has to say so.
func TestSyncLineReportsFetchFailure(t *testing.T) {
	got := syncLine(syncOutcome{RepoID: "rocket", Advanced: 2, FetchErr: errors.New("host unreachable")})
	want := "mirror rocket: обновлено на 2 коммита (fetch не удался: host unreachable)"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

// TestRepoStatusWrongArgCountIsUsageError: status takes no arguments.
func TestRepoStatusWrongArgCountIsUsageError(t *testing.T) {
	cmd := newRepoStatusCmd()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})
	if err := cmd.Execute(); exitCode(err) != 3 {
		t.Fatalf("exitCode = %d, want 3 (err=%v)", exitCode(err), err)
	}
}

// TestRepoCmdHasStatusAndSync: both subcommands are reachable from the group.
func TestRepoCmdHasStatusAndSync(t *testing.T) {
	have := map[string]bool{}
	for _, c := range newRepoCmd().Commands() {
		have[c.Name()] = true
	}
	for _, want := range []string{"status", "sync"} {
		if !have[want] {
			t.Fatalf("repo group is missing the %q subcommand", want)
		}
	}
}

// TestRepoSyncHasRepairFlag: without --repair nothing is ever repaired, so
// the flag is the whole opt-in.
func TestRepoSyncHasRepairFlag(t *testing.T) {
	if newRepoSyncCmd().Flags().Lookup("repair") == nil {
		t.Fatal("repo sync has no --repair flag")
	}
}

// TestGitHeadAndCountCommits exercises the two real git calls the advance
// count rests on, against an actual repository.
func TestGitHeadAndCountCommits(t *testing.T) {
	dir := t.TempDir()
	gitInTest(t, dir, "-c", "init.defaultBranch=main", "init")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInTest(t, dir, "add", ".")
	gitInTest(t, dir, "commit", "-m", "one")
	first, err := gitHead(context.Background(), dir)
	if err != nil {
		t.Fatalf("gitHead: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInTest(t, dir, "commit", "-am", "two")
	second, err := gitHead(context.Background(), dir)
	if err != nil {
		t.Fatalf("gitHead: %v", err)
	}

	if first == second {
		t.Fatal("HEAD did not move between commits")
	}
	n, err := gitCountCommits(context.Background(), dir, first, second)
	if err != nil {
		t.Fatalf("gitCountCommits: %v", err)
	}
	if n != 1 {
		t.Fatalf("gitCountCommits = %d, want 1", n)
	}
}

func gitInTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestSyncLineStillBehindWithoutBlocking is the bug a live run turned up:
// mirror.Sync reports only the fetch error, so a `merge --ff-only` that died
// on a stale index.lock left nothing blocked and nothing advanced — and the
// line read "уже актуально" about a mirror three commits behind. A mirror
// that is still behind afterwards has to say so.
func TestSyncLineStillBehindWithoutBlocking(t *testing.T) {
	got := syncLine(syncOutcome{RepoID: "rocket", Behind: 3})
	want := "mirror rocket: не обновлено, отстаёт на 3 коммита"
	if got != want {
		t.Fatalf("syncLine = %q, want %q", got, want)
	}
}

// TestSyncMirrorsRecordsRemainingBehind carries the post-sync behind count
// into the outcome, which is the only honest signal that the fast-forward
// did not happen.
func TestSyncMirrorsRecordsRemainingBehind(t *testing.T) {
	ops := fakeOps()
	ops.check = func(context.Context, store.Repo) (mirror.Freshness, error) {
		return mirror.Freshness{BehindCommits: 3}, nil
	}

	got := syncMirrors(context.Background(), []repoRow{{ID: "rocket"}}, ops, false, testNow)

	if len(got) != 1 || got[0].Behind != 3 {
		t.Fatalf("outcomes = %+v, want one still 3 commits behind", got)
	}
}
