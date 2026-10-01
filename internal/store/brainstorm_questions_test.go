package store

import (
	"errors"
	"testing"
)

// addBrainstorm opens a brainstorm thread with three options recommending the
// second, and returns its id.
func addBrainstorm(t *testing.T, s *Store, taskID int64) int64 {
	t.Helper()
	id, err := s.AddQuestion(Question{
		TaskID: taskID, AskedBy: "orch", Body: "Какую схему?",
		Type: QuestionTypeBrainstorm, Options: []string{"A", "B", "C"}, RecommendedOption: 2,
	})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	return id
}

func TestBrainstormQuestionRoundTrip(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	id := addBrainstorm(t, s, taskID)

	q, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if q.Type != QuestionTypeBrainstorm || q.RecommendedOption != 2 {
		t.Fatalf("type/recommended = %q/%d, want brainstorm/2", q.Type, q.RecommendedOption)
	}
	if q.ChosenOption != 0 || q.AnswerComment != "" || q.AnswerSource != "" || q.Outcome != "" || q.OutcomeOverridden {
		t.Fatalf("unanswered thread carries answer fields: %+v", q)
	}

	// A plain decision thread reads back with no recommendation at all.
	plain, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch", Body: "Q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	q, err = s.GetQuestion(plain)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if q.RecommendedOption != 0 {
		t.Fatalf("recommended = %d, want 0", q.RecommendedOption)
	}
}

func TestBrainstormOutcome(t *testing.T) {
	cases := []struct {
		recommended, chosen int
		want                string
	}{
		{2, 2, OutcomeAccepted},
		{2, 1, OutcomeCorrected},
		{2, 0, OutcomeWrongTurn},
		// No recommendation (a brainstorm thread without options): any own
		// text is a wrong turn, there was nothing to accept.
		{0, 0, OutcomeWrongTurn},
	}
	for _, c := range cases {
		if got := BrainstormOutcome(c.recommended, c.chosen); got != c.want {
			t.Errorf("BrainstormOutcome(%d, %d) = %q, want %q", c.recommended, c.chosen, got, c.want)
		}
	}
}

func TestResolveBrainstormQuestion(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	id := addBrainstorm(t, s, taskID)

	outcome, err := s.ResolveBrainstormQuestion(id, BrainstormAnswer{
		ChosenOption: 1, Comment: "лучше A", Source: AnswerSourceTerminal,
	})
	if err != nil {
		t.Fatalf("ResolveBrainstormQuestion: %v", err)
	}
	if outcome != OutcomeCorrected {
		t.Fatalf("outcome = %q, want corrected", outcome)
	}
	q, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if q.Status != "resolved" || q.Resolution != "answered" || q.ResolvedAt == 0 {
		t.Fatalf("status/resolution/resolved_at = %q/%q/%d", q.Status, q.Resolution, q.ResolvedAt)
	}
	if q.ChosenOption != 1 || q.AnswerComment != "лучше A" || q.AnswerSource != AnswerSourceTerminal || q.Outcome != OutcomeCorrected {
		t.Fatalf("answer fields = %+v", q)
	}

	if _, err := s.ResolveBrainstormQuestion(id, BrainstormAnswer{ChosenOption: 2, Source: AnswerSourceUI}); !errors.Is(err, ErrQuestionResolved) {
		t.Fatalf("second resolve err = %v, want ErrQuestionResolved", err)
	}
	if _, err := s.ResolveBrainstormQuestion(99999, BrainstormAnswer{Source: AnswerSourceUI}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

func TestResolveBrainstormQuestion_OwnTextIsWrongTurn(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	id := addBrainstorm(t, s, taskID)

	outcome, err := s.ResolveBrainstormQuestion(id, BrainstormAnswer{Comment: "всё не то", Source: AnswerSourceUI})
	if err != nil {
		t.Fatalf("ResolveBrainstormQuestion: %v", err)
	}
	if outcome != OutcomeWrongTurn {
		t.Fatalf("outcome = %q, want wrong_turn", outcome)
	}
	q, _ := s.GetQuestion(id)
	if q.ChosenOption != 0 {
		t.Fatalf("chosen = %d, want 0 (NULL)", q.ChosenOption)
	}
}

func TestSetQuestionOutcome(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	id := addBrainstorm(t, s, taskID)
	if _, err := s.ResolveBrainstormQuestion(id, BrainstormAnswer{ChosenOption: 2, Source: AnswerSourceUI}); err != nil {
		t.Fatalf("ResolveBrainstormQuestion: %v", err)
	}

	if err := s.SetQuestionOutcome(id, OutcomeCorrected); err != nil {
		t.Fatalf("SetQuestionOutcome: %v", err)
	}
	q, _ := s.GetQuestion(id)
	if q.Outcome != OutcomeCorrected || !q.OutcomeOverridden {
		t.Fatalf("outcome/overridden = %q/%v, want corrected/true", q.Outcome, q.OutcomeOverridden)
	}
	if err := s.SetQuestionOutcome(99999, OutcomeCorrected); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

func TestReopenBrainstormQuestion_ClearsAnswerKeepsType(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	id := addBrainstorm(t, s, taskID)
	if _, err := s.ResolveBrainstormQuestion(id, BrainstormAnswer{ChosenOption: 2, Comment: "да", Source: AnswerSourceUI}); err != nil {
		t.Fatalf("ResolveBrainstormQuestion: %v", err)
	}
	if err := s.SetQuestionOutcome(id, OutcomeCorrected); err != nil {
		t.Fatalf("SetQuestionOutcome: %v", err)
	}

	if err := s.ReopenQuestion(id); err != nil {
		t.Fatalf("ReopenQuestion: %v", err)
	}
	q, _ := s.GetQuestion(id)
	if q.Status != "open" || q.Type != QuestionTypeBrainstorm {
		t.Fatalf("status/type = %q/%q, want open/brainstorm", q.Status, q.Type)
	}
	if q.RecommendedOption != 2 {
		t.Fatalf("recommended = %d, want kept 2", q.RecommendedOption)
	}
	if q.ChosenOption != 0 || q.AnswerComment != "" || q.AnswerSource != "" || q.Outcome != "" || q.OutcomeOverridden {
		t.Fatalf("reopen kept answer fields: %+v", q)
	}
}

func TestReopenFYIStillBecomesDecision(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	id, err := s.AddQuestion(Question{
		TaskID: taskID, AskedBy: "orch", Body: "note", Type: QuestionTypeFYI,
		Status: "resolved", Resolution: QuestionResolutionFYI, ResolvedAt: 1,
	})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := s.ReopenQuestion(id); err != nil {
		t.Fatalf("ReopenQuestion: %v", err)
	}
	q, _ := s.GetQuestion(id)
	if q.Type != QuestionTypeDecision {
		t.Fatalf("type = %q, want decision", q.Type)
	}
}

func TestAnswerAuthors(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	a := addBrainstorm(t, s, taskID)
	b := addBrainstorm(t, s, taskID)
	c := addBrainstorm(t, s, taskID)
	for _, m := range []QuestionMessage{
		{QuestionID: a, Author: "orch", Kind: "reply", Body: "r"},
		{QuestionID: a, Author: "", Kind: "answer", Body: "x"},
		{QuestionID: a, Author: "orch", Kind: "reply", Body: "принял"},
		{QuestionID: b, Author: "cto", Kind: "answer", Body: "y"},
		{QuestionID: c, Author: "human", Kind: "dismiss", Body: "не нужно"},
	} {
		if _, err := s.AddQuestionMessage(m); err != nil {
			t.Fatalf("AddQuestionMessage: %v", err)
		}
	}

	got, err := s.AnswerAuthors()
	if err != nil {
		t.Fatalf("AnswerAuthors: %v", err)
	}
	if got[a] != ParticipantHuman || got[b] != "cto" {
		t.Fatalf("authors = %v, want a=human b=cto", got)
	}
	if _, ok := got[c]; ok {
		t.Fatalf("a dismissal is not an answer: %v", got)
	}
}
