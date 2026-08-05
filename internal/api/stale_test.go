package api

import (
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// seedAgedThread opens a thread on taskID directly in the store, dated age
// ago and waiting on "cto" — the API has no way to backdate a thread, and
// nothing but a staleness test needs one.
func seedAgedThread(t *testing.T, d Deps, taskID int64, age time.Duration) store.Question {
	t.Helper()
	id, err := d.Store.AddQuestion(store.Question{
		TaskID: taskID, AskedBy: "orch-1", Body: "Which approach?",
		Type: store.QuestionTypeDecision, AskedAt: time.Now().Add(-age).Unix(),
	})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := d.Store.AddParticipants(id, "human", "orch-1", "cto"); err != nil {
		t.Fatalf("AddParticipants: %v", err)
	}
	if err := d.Store.SetAttention(id, []string{"cto"}); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
	q, err := d.Store.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	return q
}

// TestThreadStaleFlag: the human is never messaged about a stale thread, so
// the thread itself has to say it — `stale` is what the dashboard and
// `rocket questions` badge from.
func TestThreadStaleFlag(t *testing.T) {
	d := questionsTestDeps(t)
	_, taskID := attentionTestSetup(t, d)

	fresh, err := buildQuestionResponse(d, nil, seedAgedThread(t, d, taskID, time.Hour))
	if err != nil {
		t.Fatalf("buildQuestionResponse(fresh): %v", err)
	}
	if fresh.Stale {
		t.Error("a thread that moved an hour ago must not be stale")
	}

	old, err := buildQuestionResponse(d, nil, seedAgedThread(t, d, taskID, 30*time.Hour))
	if err != nil {
		t.Fatalf("buildQuestionResponse(old): %v", err)
	}
	if !old.Stale {
		t.Error("a thread idle for 30h with cto on the hook must be stale")
	}
}

// TestThreadStaleFlag_ResolvedIsNeverStale: closing a thread clears the badge,
// whatever its age.
func TestThreadStaleFlag_ResolvedIsNeverStale(t *testing.T) {
	d := questionsTestDeps(t)
	_, taskID := attentionTestSetup(t, d)

	q := seedAgedThread(t, d, taskID, 30*time.Hour)
	if err := d.Store.ResolveQuestion(q.ID, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	resolved, err := d.Store.GetQuestion(q.ID)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}

	resp, err := buildQuestionResponse(d, nil, resolved)
	if err != nil {
		t.Fatalf("buildQuestionResponse: %v", err)
	}
	if resp.Stale {
		t.Error("a resolved thread must never be stale")
	}
}
