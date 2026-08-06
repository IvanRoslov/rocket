package ghpoller

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

// TestTick_PollsKilledWorkerWithOpenPR is the regression test for the
// stale-status bug (task #1087): a killed worker whose PR was still open kept
// its pr_state frozen forever, because the tick only looked at live sessions.
func TestTick_PollsKilledWorkerWithOpenPR(t *testing.T) {
	st, b := setupEnv(t)
	addWorker(t, st, "w1", "feature-branch")
	if err := st.UpdateSessionPR("w1", 5, "open", "passing"); err != nil {
		t.Fatalf("UpdateSessionPR: %v", err)
	}
	if err := st.UpdateSessionState("w1", "killed"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	mock := newMockGitHubServer()
	mock.prState = "closed"
	mock.merged = true
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := st.GetSession("w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRState != "merged" {
		t.Fatalf("pr_state = %q, want merged (killed worker must keep being polled)", got.PRState)
	}
}

// TestTick_SkipsDeadWorkerWithTerminalPR guards the other side: once the PR is
// merged and the session is gone, we must stop burning GitHub API calls on it.
func TestTick_SkipsDeadWorkerWithTerminalPR(t *testing.T) {
	st, b := setupEnv(t)
	addWorker(t, st, "w1", "feature-branch")
	if err := st.UpdateSessionPR("w1", 5, "merged", "passing"); err != nil {
		t.Fatalf("UpdateSessionPR: %v", err)
	}
	if err := st.UpdateSessionState("w1", "done"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	// Park the freshness stamp at a known old value; a poll would move it.
	if err := st.MarkSessionPRChecked("w1", 1); err != nil {
		t.Fatalf("MarkSessionPRChecked: %v", err)
	}

	mock := newMockGitHubServer()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := st.GetSession("w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRCheckedAt != 1 {
		t.Fatalf("merged PR of a dead session should not be polled, got pr_checked_at=%d", got.PRCheckedAt)
	}
}

func TestTick_StampsPRCheckedAtOnSuccessfulPoll(t *testing.T) {
	st, b := setupEnv(t)
	addWorker(t, st, "w1", "feature-branch")

	mock := newMockGitHubServer()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	before := time.Now().Unix()
	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := st.GetSession("w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRCheckedAt < before {
		t.Fatalf("pr_checked_at = %d, want >= %d", got.PRCheckedAt, before)
	}

	// A second tick that changes nothing still refreshes the stamp: freshness
	// is about when we last looked, not when something last changed.
	if err := st.MarkSessionPRChecked("w1", 1); err != nil {
		t.Fatalf("MarkSessionPRChecked: %v", err)
	}
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	got, err = st.GetSession("w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRCheckedAt < before {
		t.Fatalf("pr_checked_at not refreshed by a no-change poll: %d", got.PRCheckedAt)
	}
}

// TestTick_NoStampOnFailedPoll: a session whose poll errored must not look
// fresh — that would be exactly the lie this feature exists to prevent.
func TestTick_NoStampOnFailedPoll(t *testing.T) {
	st, b := setupEnv(t)
	addWorker(t, st, "w1", "feature-branch")
	if err := st.UpdateSessionPR("w1", 5, "open", ""); err != nil {
		t.Fatalf("UpdateSessionPR: %v", err)
	}

	// The mock only knows PR #5 under /pulls/5; point the session at a PR the
	// server 404s on so GetPR fails.
	if err := st.UpdateSessionPR("w1", 77, "open", ""); err != nil {
		t.Fatalf("UpdateSessionPR: %v", err)
	}

	// Park the stamp at a known old value: after a failed poll it must still
	// read as old, otherwise a stale pr_state would look freshly confirmed.
	if err := st.MarkSessionPRChecked("w1", 1); err != nil {
		t.Fatalf("MarkSessionPRChecked: %v", err)
	}

	mock := newMockGitHubServer()
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	p := New(st, b, ghFactory(srv.URL), testConfig(), &fakeNotifier{})
	if err := p.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := st.GetSession("w1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRCheckedAt != 1 {
		t.Fatalf("failed poll must not stamp pr_checked_at, got %d", got.PRCheckedAt)
	}
}
