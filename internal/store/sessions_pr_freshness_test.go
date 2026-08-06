package store

import (
	"testing"
	"time"
)

// newPRSession is a helper building a worker session with the given state and
// PR fields.
func newPRSession(id, state string, prNumber int, prState string) Session {
	return Session{
		ID: id, Kind: "worker", ProjectID: "billing", RepoID: "api",
		FeatureSlug: "f1", Agent: "claude-code", Branch: "b-" + id,
		WorktreePath: "/wt/" + id, TmuxName: "t-" + id, State: state,
		PRNumber: prNumber, PRState: prState,
	}
}

func TestMarkSessionPRChecked(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)

	if err := s.AddSession(newPRSession("s1", "running", 42, "open")); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	got, err := s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRCheckedAt != 0 {
		t.Fatalf("fresh session should have zero PRCheckedAt, got %d", got.PRCheckedAt)
	}

	ts := time.Now().Unix()
	if err := s.MarkSessionPRChecked("s1", ts); err != nil {
		t.Fatalf("MarkSessionPRChecked: %v", err)
	}

	got, err = s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession after mark: %v", err)
	}
	if got.PRCheckedAt != ts {
		t.Fatalf("PRCheckedAt = %d, want %d", got.PRCheckedAt, ts)
	}

	// A later successful poll moves the timestamp forward.
	if err := s.MarkSessionPRChecked("s1", ts+60); err != nil {
		t.Fatalf("MarkSessionPRChecked second: %v", err)
	}
	got, err = s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession after second mark: %v", err)
	}
	if got.PRCheckedAt != ts+60 {
		t.Fatalf("PRCheckedAt = %d, want %d", got.PRCheckedAt, ts+60)
	}

	if err := s.MarkSessionPRChecked("nope", ts); err != ErrNotFound {
		t.Fatalf("MarkSessionPRChecked on missing session = %v, want ErrNotFound", err)
	}
}

func TestSessionPRCheckedAt_RoundTripsThroughUpdateSession(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)

	if err := s.AddSession(newPRSession("s1", "running", 42, "open")); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	ts := time.Now().Unix()
	if err := s.MarkSessionPRChecked("s1", ts); err != nil {
		t.Fatalf("MarkSessionPRChecked: %v", err)
	}

	got, err := s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	// UpdateSession rewrites the whole mutable row; it must not silently drop
	// the freshness stamp.
	got.Activity = "busy"
	if err := s.UpdateSession(got); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	got2, err := s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession after UpdateSession: %v", err)
	}
	if got2.PRCheckedAt != ts {
		t.Fatalf("PRCheckedAt lost by UpdateSession: %d, want %d", got2.PRCheckedAt, ts)
	}
}

func TestListSessionsForPRPoll(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)

	sessions := []Session{
		// Live workers are always polled, PR or not.
		newPRSession("live-nopr", "running", 0, ""),
		newPRSession("live-open", "running", 1, "open"),
		newPRSession("spawning", "spawning", 0, ""),
		// A dead worker still holding a non-terminal PR must keep being polled:
		// this is the stale-status bug (task #1087).
		newPRSession("dead-open", "killed", 2, "open"),
		// Dead workers whose PR reached a terminal state are done.
		newPRSession("dead-merged", "killed", 3, "merged"),
		newPRSession("dead-closed", "done", 4, "closed"),
		// A dead worker that never got a PR has nothing to poll.
		newPRSession("dead-nopr", "killed", 0, ""),
	}
	for _, sess := range sessions {
		if err := s.AddSession(sess); err != nil {
			t.Fatalf("AddSession %s: %v", sess.ID, err)
		}
	}
	// An orchestrator session is never polled, even alive.
	orch := newPRSession("orch", "running", 0, "")
	orch.Kind = "orchestrator"
	if err := s.AddSession(orch); err != nil {
		t.Fatalf("AddSession orch: %v", err)
	}

	got, err := s.ListSessionsForPRPoll()
	if err != nil {
		t.Fatalf("ListSessionsForPRPoll: %v", err)
	}

	want := map[string]bool{"live-nopr": true, "live-open": true, "spawning": true, "dead-open": true}
	gotIDs := map[string]bool{}
	for _, sess := range got {
		gotIDs[sess.ID] = true
	}
	for id := range want {
		if !gotIDs[id] {
			t.Errorf("session %s missing from PR poll set", id)
		}
	}
	for id := range gotIDs {
		if !want[id] {
			t.Errorf("session %s unexpectedly in PR poll set", id)
		}
	}
}
