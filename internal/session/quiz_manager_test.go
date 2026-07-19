package session

import (
	"context"
	"testing"
	"time"
)

const quizManagerPendingJSON = `{"questions":[{"question":"Which color?","header":"Color","multiSelect":false,"options":[{"label":"Red"},{"label":"Green"},{"label":"Blue"}]}],"asked_at":1}`

// noopSleep lets tests skip quizKeySettle in wall-clock time.
func noopSleep(time.Duration) {}

func TestAnswerQuiz_UnknownSessionIsNotFound(t *testing.T) {
	m, _, _, _, _ := testManager(t)
	m.SetQuizTiming(noopSleep, 50*time.Millisecond)

	err := m.AnswerQuiz(context.Background(), "does-not-exist", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}})
	assertValidationCode(t, err, "session_not_found")
}

func TestAnswerQuiz_NoPendingQuizIsConflict(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	m.SetQuizTiming(noopSleep, 50*time.Millisecond)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")

	err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}})
	assertValidationCode(t, err, "no_pending_quiz")
}

func TestAnswerQuiz_InvalidAnswersIsQuizAnswerInvalid(t *testing.T) {
	m, st, _, _, _ := testManager(t)
	m.SetQuizTiming(noopSleep, 50*time.Millisecond)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")
	if err := st.SetPendingQuiz("sess1", quizManagerPendingJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 99, OptionIndices: []int{0}}})
	assertValidationCode(t, err, "quiz_answer_invalid")
}

// TestAnswerQuiz_HappyPathSendsKeysAndWaitsForResolved verifies that a
// valid AnswerQuiz call sends the expected keystrokes to the session's
// tmux handle, and that publishing session.quiz_resolved for that session
// suppresses the unconfirmed timer (no session.quiz_answer_unconfirmed
// event within the (short, test-only) timeout).
func TestAnswerQuiz_HappyPathSendsKeysAndWaitsForResolved(t *testing.T) {
	m, st, b, rt, _ := testManager(t)
	m.SetQuizTiming(noopSleep, 200*time.Millisecond)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")
	if err := st.SetPendingQuiz("sess1", quizManagerPendingJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{1}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}

	// Give the async goroutine time to send keys, then resolve before the
	// unconfirmed timeout fires.
	time.Sleep(30 * time.Millisecond)
	b.Publish("session.quiz_resolved", "sess1", map[string]any{})

	deadline := time.After(2 * time.Second)
	sawUnconfirmed := false
loop:
	for {
		select {
		case ev := <-ch:
			if ev.Type == "session.quiz_answer_unconfirmed" {
				sawUnconfirmed = true
			}
		case <-deadline:
			break loop
		}
	}
	if sawUnconfirmed {
		t.Error("got session.quiz_answer_unconfirmed after a timely quiz_resolved, want none")
	}

	rt.mu.Lock()
	sent := append([]sentKey(nil), rt.sentKeys...)
	rt.mu.Unlock()

	want := []sentKey{
		{handle: "sess1", key: "2", literal: false}, // digit for option index 1 -> "2"
		{handle: "sess1", key: "Enter", literal: false},
	}
	if len(sent) != len(want) {
		t.Fatalf("sentKeys = %+v, want %+v", sent, want)
	}
	for i := range want {
		if sent[i] != want[i] {
			t.Errorf("sentKeys[%d] = %+v, want %+v", i, sent[i], want[i])
		}
	}
}

// TestAnswerQuiz_UnconfirmedFiresWithoutResolved verifies the unconfirmed
// timer publishes session.quiz_answer_unconfirmed if no resolved event
// arrives before the (short, test-only) timeout.
func TestAnswerQuiz_UnconfirmedFiresWithoutResolved(t *testing.T) {
	m, st, b, _, _ := testManager(t)
	m.SetQuizTiming(noopSleep, 50*time.Millisecond)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")
	if err := st.SetPendingQuiz("sess1", quizManagerPendingJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	ch, cancel := b.Subscribe()
	defer cancel()

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}}); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == "session.quiz_answer_unconfirmed" {
				if ev.SessionID != "sess1" {
					t.Errorf("event session = %q, want sess1", ev.SessionID)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for session.quiz_answer_unconfirmed")
		}
	}
}
