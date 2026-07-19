package store

import "testing"

func TestSessionPRFields_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)

	sess := Session{
		ID: "s1", Kind: "worker", ProjectID: "billing", RepoID: "api",
		FeatureSlug: "f1", Agent: "claude-code", Branch: "b1",
		WorktreePath: "/wt/s1", TmuxName: "t1", State: "running",
		PRNumber: 42, PRState: "open", CIState: "pending",
	}
	if err := s.AddSession(sess); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	got, err := s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRNumber != 42 || got.PRState != "open" || got.CIState != "pending" {
		t.Fatalf("PR fields round-trip mismatch: %+v", got)
	}

	// zero-value PR fields should round-trip as zero values (NULL columns).
	if err := s.AddSession(Session{
		ID: "s2", Kind: "worker", ProjectID: "billing", RepoID: "api",
		FeatureSlug: "f1", Agent: "claude-code", Branch: "b2",
		WorktreePath: "/wt/s2", TmuxName: "t2", State: "running",
	}); err != nil {
		t.Fatalf("AddSession s2: %v", err)
	}
	got2, err := s.GetSession("s2")
	if err != nil {
		t.Fatalf("GetSession s2: %v", err)
	}
	if got2.PRNumber != 0 || got2.PRState != "" || got2.CIState != "" {
		t.Fatalf("expected zero PR fields, got %+v", got2)
	}

	// UpdateSession also round-trips PR fields.
	got.PRNumber = 43
	got.PRState = "merged"
	got.CIState = "passing"
	if err := s.UpdateSession(got); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	got3, err := s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession after UpdateSession: %v", err)
	}
	if got3.PRNumber != 43 || got3.PRState != "merged" || got3.CIState != "passing" {
		t.Fatalf("PR fields not updated via UpdateSession: %+v", got3)
	}
}

func TestUpdateSessionPR(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)

	if err := s.AddSession(Session{
		ID: "s1", Kind: "worker", ProjectID: "billing", RepoID: "api",
		FeatureSlug: "f1", Agent: "claude-code", Branch: "b1",
		WorktreePath: "/wt/s1", TmuxName: "t1", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	if err := s.UpdateSessionPR("s1", 7, "open", "failing"); err != nil {
		t.Fatalf("UpdateSessionPR: %v", err)
	}

	got, err := s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PRNumber != 7 || got.PRState != "open" || got.CIState != "failing" {
		t.Fatalf("UpdateSessionPR mismatch: %+v", got)
	}
	if got.UpdatedAt < got.CreatedAt {
		t.Fatalf("expected UpdatedAt >= CreatedAt, got %d < %d", got.UpdatedAt, got.CreatedAt)
	}

	if err := s.UpdateSessionPR("nope", 1, "open", "pending"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
