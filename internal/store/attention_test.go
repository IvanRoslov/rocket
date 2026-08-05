package store

import (
	"reflect"
	"testing"
)

// mustAddOpenQuestion opens a thread on a task and seeds its participants,
// mirroring what internal/api does on every ask.
func mustAddOpenQuestion(t *testing.T, s *Store, taskID int64, q Question) int64 {
	t.Helper()
	q.TaskID = taskID
	id, err := s.AddQuestion(q)
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	return id
}

func TestQuestionTypeAndOptionsRoundTrip(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	id := mustAddOpenQuestion(t, s, taskID, Question{
		AskedBy: "orch-1",
		Body:    "Which approach?",
		Type:    QuestionTypeFYI,
		Options: []string{"A", "B"},
	})

	got, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if got.Type != QuestionTypeFYI {
		t.Errorf("Type = %q, want %q", got.Type, QuestionTypeFYI)
	}
	if !reflect.DeepEqual(got.Options, []string{"A", "B"}) {
		t.Errorf("Options = %#v, want [A B]", got.Options)
	}
}

func TestQuestionTypeDefaultsToDecision(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	id := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "?"})
	got, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if got.Type != QuestionTypeDecision {
		t.Errorf("Type = %q, want %q", got.Type, QuestionTypeDecision)
	}
	if got.Options != nil {
		t.Errorf("Options = %#v, want nil", got.Options)
	}
}

func TestAgentQuestionTypeAndOptionsRoundTrip(t *testing.T) {
	s := openTestStore(t)
	seedAgentForQuestions(t, s, "cto")

	id, err := s.AddAgentQuestion(AgentQuestion{
		RoleID: "cto", Body: "?", Type: QuestionTypeFYI, Options: []string{"yes", "no"},
	})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	got, err := s.GetAgentQuestion(id)
	if err != nil {
		t.Fatalf("GetAgentQuestion: %v", err)
	}
	if got.Type != QuestionTypeFYI {
		t.Errorf("Type = %q, want fyi", got.Type)
	}
	if !reflect.DeepEqual(got.Options, []string{"yes", "no"}) {
		t.Errorf("Options = %#v, want [yes no]", got.Options)
	}
}
