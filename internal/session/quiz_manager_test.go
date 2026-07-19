package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

const quizManagerPendingJSON = `{"questions":[{"question":"Which color?","header":"Color","multiSelect":false,"options":[{"label":"Red"},{"label":"Green"},{"label":"Blue"}]}],"asked_at":1}`

// quizManagerMultiPendingJSON is a 4-option multi-select question, giving
// quizKeySequence enough steps (4 digits + Tab + final Enter = 6) that a
// mid-injection abort has room to demonstrably stop early.
const quizManagerMultiPendingJSON = `{"questions":[{"question":"Which fruits?","header":"Fruits","multiSelect":true,"options":[{"label":"Apple"},{"label":"Banana"},{"label":"Cherry"},{"label":"Date"}]}],"asked_at":1}`

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

// TestAnswerQuiz_SecondCallWhileInFlightIs409ThenClearsAfterResolved
// verifies the in-flight guard (HIGH finding #1): a second AnswerQuiz call
// for a session whose first injection hasn't finished yet is rejected with
// "quiz_answer_in_flight", and once session.quiz_resolved arrives for the
// first injection, the flag clears and a new quiz can be answered again.
func TestAnswerQuiz_SecondCallWhileInFlightIs409ThenClearsAfterResolved(t *testing.T) {
	m, st, b, _, _ := testManager(t)
	// A real (short) sleep between keystrokes, so the first injection is
	// still in flight when we fire the second AnswerQuiz call right after.
	m.SetQuizTiming(func(time.Duration) { time.Sleep(20 * time.Millisecond) }, 2*time.Second)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")
	if err := st.SetPendingQuiz("sess1", quizManagerPendingJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	if err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}}); err != nil {
		t.Fatalf("first AnswerQuiz: %v", err)
	}

	err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{1}}})
	assertValidationCode(t, err, "quiz_answer_in_flight")

	// Resolve the first injection so its in-flight flag clears.
	b.Publish("session.quiz_resolved", "sess1", map[string]any{})

	// Poll AnswerQuiz until it stops returning quiz_answer_in_flight,
	// proving the flag was cleared once the injection goroutine observed
	// the resolved event and returned.
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := m.AnswerQuiz(context.Background(), "sess1", []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}})
		if err == nil {
			return // in-flight cleared, new quiz answer accepted
		}
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != "quiz_answer_in_flight" {
			t.Fatalf("unexpected error while polling: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for in-flight flag to clear after resolved")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAnswerQuiz_ResolveMidInjectionAbortsRemainingKeys verifies MEDIUM
// finding #3 (and, by relying on the subscription being established before
// the first keystroke, MEDIUM finding #2): if session.quiz_resolved
// arrives for the session while sendQuizKeys is still working through the
// sequence, injection stops sending further keys rather than running the
// full sequence to completion.
func TestAnswerQuiz_ResolveMidInjectionAbortsRemainingKeys(t *testing.T) {
	m, st, b, rt, _ := testManager(t)
	// 20ms between keystrokes gives the watcher goroutine plenty of time to
	// observe quiz_resolved and close the abort channel before the next
	// step's abort check runs.
	m.SetQuizTiming(func(time.Duration) { time.Sleep(20 * time.Millisecond) }, 2*time.Second)
	seedProjectRepo(t, st, "proj1", "repo1")
	seedRunningSession(t, st, "sess1")
	if err := st.SetPendingQuiz("sess1", quizManagerMultiPendingJSON); err != nil {
		t.Fatalf("SetPendingQuiz: %v", err)
	}

	// Selecting all 4 options produces 6 steps (4 digits + Tab + final
	// Enter, per quizKeySequence's multi-select shape).
	answer := []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0, 1, 2, 3}}}
	if err := m.AnswerQuiz(context.Background(), "sess1", answer); err != nil {
		t.Fatalf("AnswerQuiz: %v", err)
	}

	// Publish resolved as soon as the first keystroke has landed, well
	// before the sequence would otherwise finish.
	go func() {
		for {
			rt.mu.Lock()
			n := len(rt.sentKeys)
			rt.mu.Unlock()
			if n >= 1 {
				b.Publish("session.quiz_resolved", "sess1", map[string]any{})
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Give the injection goroutine time to abort and return; the full
	// unaborted sequence would take 6*20ms=120ms, so 500ms is comfortably
	// long enough to distinguish "aborted early" from "ran to completion"
	// without being flaky.
	time.Sleep(500 * time.Millisecond)

	rt.mu.Lock()
	sent := len(rt.sentKeys)
	rt.mu.Unlock()
	if sent >= 6 {
		t.Errorf("sentKeys count = %d, want < 6 (injection should have aborted mid-sequence after quiz_resolved)", sent)
	}
	if sent == 0 {
		t.Error("sentKeys count = 0, want at least 1 (the key sent before resolved was published)")
	}
}

// TestAnswerQuiz_UnconfirmedSkippedWhenPendingQuizAlreadyCleared verifies
// the belt-and-braces half of MEDIUM finding #2: if the unconfirmed timer
// fires but the session's pending_quiz was already cleared (as
// internal_quiz.go does before publishing session.quiz_resolved — the
// resolved event itself just raced this call's bus subscription and was
// missed), no session.quiz_answer_unconfirmed is published.
func TestAnswerQuiz_UnconfirmedSkippedWhenPendingQuizAlreadyCleared(t *testing.T) {
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

	// Simulate the resolved event racing (and losing to) this call's
	// subscription: clear pending_quiz directly, exactly as
	// internal_quiz.go does, without ever publishing session.quiz_resolved.
	if err := st.ClearPendingQuiz("sess1"); err != nil {
		t.Fatalf("ClearPendingQuiz: %v", err)
	}

	// Wait comfortably longer than the 50ms unconfirmed timeout, watching
	// for a (wrongly) published unconfirmed event.
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case ev := <-ch:
			if ev.Type == "session.quiz_answer_unconfirmed" {
				t.Fatal("got session.quiz_answer_unconfirmed despite pending_quiz already cleared")
			}
		case <-deadline:
			return
		}
	}
}
