package session

import (
	"context"
	"testing"
)

// TestKillClearsPendingQuiz verifies that transitioning a session to a
// terminal state via Kill clears any pending AskUserQuestion quiz, since a
// terminal session can no longer answer it.
func TestKillClearsPendingQuiz(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")

	if err := st.SetPendingQuiz("sess1", `{"q":1}`); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	if err := m.Kill(context.Background(), "sess1", false); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	stored, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.PendingQuiz != "" {
		t.Errorf("PendingQuiz after Kill = %q, want empty", stored.PendingQuiz)
	}
}

// TestCompleteClearsPendingQuiz verifies the same for the Complete path
// (session transitioned to "done").
func TestCompleteClearsPendingQuiz(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")

	if err := st.SetPendingQuiz("sess1", `{"q":1}`); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	if err := m.Complete(context.Background(), "sess1"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	stored, err := st.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.PendingQuiz != "" {
		t.Errorf("PendingQuiz after Complete = %q, want empty", stored.PendingQuiz)
	}
}
