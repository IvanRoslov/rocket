package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirror"
	"github.com/IvanRoslov/rocket/internal/store"
)

// blockingChecker returns a mirrorChecker that blocks until its context is
// cancelled for the named repo and answers instantly for every other one.
// Blocking on the context is what a wedged git command does; sleeping a real
// timeout would make this test as slow as the bug it pins.
func blockingChecker(blockID string) mirrorChecker {
	return func(ctx context.Context, repo store.Repo, staleAfter time.Duration, now time.Time) (mirror.Freshness, error) {
		if repo.ID == blockID {
			<-ctx.Done()
			return mirror.Freshness{}, ctx.Err()
		}
		return mirror.Freshness{RepoID: repo.ID, Head: "deadbeefdeadbeef"}, nil
	}
}

// TestCheckMirrorsBlockedMirrorDoesNotPoisonLaterRows pins the fleet bug: a
// single wedged mirror used to burn the whole sweep's budget, so every row
// after it rendered as an error. Each mirror now gets its own budget.
func TestCheckMirrorsBlockedMirrorDoesNotPoisonLaterRows(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	repos := []repoRow{
		{ID: "wedged", Path: "/nonexistent", DefaultBranch: "main"},
		{ID: "fine-a", Path: "/nonexistent", DefaultBranch: "main"},
		{ID: "fine-b", Path: "/nonexistent", DefaultBranch: "main"},
	}

	rows := checkMirrorsWith(t.Context(), blockingChecker("wedged"),
		repos, 10*time.Millisecond, time.Minute, 10*time.Minute, now)

	if len(rows) != len(repos) {
		t.Fatalf("expected %d rows, got %d", len(repos), len(rows))
	}
	if rows[0].Err == nil {
		t.Errorf("wedged mirror: expected an error row, got %+v", rows[0].Fresh)
	}
	for _, row := range rows[1:] {
		if row.Err != nil {
			t.Errorf("mirror %s: poisoned by the wedged mirror: %v", row.RepoID, row.Err)
		}
	}
}

// TestCheckMirrorsTimeoutIsNamedAsATimeout guards the wording: a mirror we
// did not wait long enough for must not be indistinguishable from a broken
// repository.
func TestCheckMirrorsTimeoutIsNamedAsATimeout(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	repos := []repoRow{{ID: "wedged", Path: "/nonexistent", DefaultBranch: "main"}}

	rows := checkMirrorsWith(t.Context(), blockingChecker("wedged"),
		repos, 10*time.Millisecond, time.Minute, 10*time.Minute, now)

	if !errors.Is(rows[0].Err, errMirrorCheckTimeout) {
		t.Fatalf("expected errMirrorCheckTimeout, got %v", rows[0].Err)
	}
	line := mirrorLine(rows[0], now)
	if !strings.Contains(line, "превышено время проверки") {
		t.Errorf("line does not read as a timeout: %q", line)
	}
	if strings.Contains(line, "signal: killed") {
		t.Errorf("line still reads as a raw git failure: %q", line)
	}
}

// TestCheckMirrorsOverallCeilingStillBounded verifies the sweep gives up as
// a whole when every mirror is wedged: the per-mirror budget must not make
// the ceiling unbounded on a wedged filesystem.
func TestCheckMirrorsOverallCeilingStillBounded(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	repos := []repoRow{
		{ID: "a", Path: "/nonexistent", DefaultBranch: "main"},
		{ID: "b", Path: "/nonexistent", DefaultBranch: "main"},
		{ID: "c", Path: "/nonexistent", DefaultBranch: "main"},
	}
	block := func(ctx context.Context, repo store.Repo, _ time.Duration, _ time.Time) (mirror.Freshness, error) {
		<-ctx.Done()
		return mirror.Freshness{}, ctx.Err()
	}

	start := time.Now()
	rows := checkMirrorsWith(t.Context(), block, repos,
		time.Second, 30*time.Millisecond, 10*time.Minute, now)
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Fatalf("sweep ignored its overall ceiling: took %s", elapsed)
	}
	if len(rows) != len(repos) {
		t.Fatalf("expected %d rows, got %d", len(repos), len(rows))
	}
	for _, row := range rows {
		if !errors.Is(row.Err, errMirrorCheckTimeout) {
			t.Errorf("mirror %s: expected a timeout row, got %v", row.RepoID, row.Err)
		}
	}
}

// TestMirrorSweepTimeoutScalesWithFleet pins the ceiling's shape: 83 mirrors
// must get a bigger budget than 2, which is what the flat 15s failed to do.
func TestMirrorSweepTimeoutScalesWithFleet(t *testing.T) {
	small, large := mirrorSweepTimeout(2), mirrorSweepTimeout(83)
	if large <= small {
		t.Errorf("sweep budget does not scale: 2 mirrors = %s, 83 mirrors = %s", small, large)
	}
	if got := mirrorSweepTimeout(83); got < 83*mirrorCheckTimeout/10 {
		t.Errorf("83 mirrors get only %s, too little to finish a real sweep", got)
	}
}
