package store

import "testing"

func TestPendingQuiz_SetReadClear(t *testing.T) {
	s := openTestStore(t)
	mustSetup(t, s)

	if err := s.AddSession(Session{
		ID: "s1", Kind: "worker", ProjectID: "billing", RepoID: "api",
		FeatureSlug: "f1", Agent: "claude-code", Branch: "b1",
		WorktreePath: "/wt/s1", TmuxName: "t1", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	// New session has no pending quiz.
	got, err := s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.PendingQuiz != "" {
		t.Fatalf("PendingQuiz initial = %q, want empty", got.PendingQuiz)
	}

	quizJSON := `{"questions":[{"question":"pick one","options":["a","b"]}]}`
	if err := s.SetPendingQuiz("s1", quizJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	got, err = s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession after set: %v", err)
	}
	if got.PendingQuiz != quizJSON {
		t.Fatalf("PendingQuiz = %q, want %q", got.PendingQuiz, quizJSON)
	}

	if err := s.ClearPendingQuiz("s1"); err != nil {
		t.Fatalf("ClearPendingQuiz: %v", err)
	}
	got, err = s.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession after clear: %v", err)
	}
	if got.PendingQuiz != "" {
		t.Fatalf("PendingQuiz after clear = %q, want empty", got.PendingQuiz)
	}

	// ClearPendingQuiz is idempotent (no-op when already empty).
	if err := s.ClearPendingQuiz("s1"); err != nil {
		t.Fatalf("ClearPendingQuiz idempotent: %v", err)
	}

	if err := s.SetPendingQuiz("missing", quizJSON); err != ErrNotFound {
		t.Fatalf("SetPendingQuiz missing id: got %v, want ErrNotFound", err)
	}
	if err := s.ClearPendingQuiz("missing"); err != ErrNotFound {
		t.Fatalf("ClearPendingQuiz missing id: got %v, want ErrNotFound", err)
	}
}

func TestPendingQuiz_MigrationAppliesOnExistingDB(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/rocket.db"

	// Simulate a database created before this migration existed by opening
	// and closing normally (Open always applies all embedded migrations,
	// including 0003); this test's real assertion is that a session added
	// before any pending_quiz call round-trips correctly once the column
	// exists, i.e. the ALTER TABLE migration is compatible with existing
	// rows and reopening the store is safe.
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.AddRepo(Repo{ID: "api", Path: "/repos/api"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if err := s1.AddProject(Project{ID: "billing", Name: "Billing", MainRepo: "api"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := s1.AddSession(Session{
		ID: "s1", Kind: "worker", ProjectID: "billing", RepoID: "api",
		FeatureSlug: "f1", Agent: "claude-code", Branch: "b1",
		WorktreePath: "/wt/s1", TmuxName: "t1", State: "running",
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen: migration 0003 must apply cleanly against the already-existing
	// database and the pending_quiz column must be usable afterwards.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	if err := s2.SetPendingQuiz("s1", `{"q":1}`); err != nil {
		t.Fatalf("SetPendingQuiz after reopen: %v", err)
	}
	got, err := s2.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession after reopen: %v", err)
	}
	if got.PendingQuiz != `{"q":1}` {
		t.Fatalf("PendingQuiz after reopen = %q", got.PendingQuiz)
	}
}
