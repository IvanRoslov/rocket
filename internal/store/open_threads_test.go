package store

import (
	"reflect"
	"testing"
)

// TestListOpenThreads: the raw material the API layer needs to derive
// waiting_on for every open thread in one pass — the question, its last
// message (with addressed_to), and its participants. Resolved threads are
// excluded.
func TestListOpenThreads(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	seedAgentForQuestions(t, s, "cto")

	// A task thread with two messages; only the last one is reported.
	q1, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q1"})
	if err != nil {
		t.Fatalf("AddQuestion q1: %v", err)
	}
	if err := s.AddParticipants(q1, "human", "orch-1", "cto"); err != nil {
		t.Fatalf("AddParticipants q1: %v", err)
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{QuestionID: q1, Author: "human", Body: "first"}); err != nil {
		t.Fatalf("AddQuestionMessage q1 first: %v", err)
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{
		QuestionID: q1, Author: "cto", Body: "last", AddressedTo: []string{"orch-1"},
	}); err != nil {
		t.Fatalf("AddQuestionMessage q1 last: %v", err)
	}

	// A role thread with no messages at all.
	q2, err := s.AddQuestion(Question{RoleID: "cto", AskedBy: "", Body: "Q2"})
	if err != nil {
		t.Fatalf("AddQuestion q2: %v", err)
	}
	if err := s.AddParticipants(q2, "human", "cto"); err != nil {
		t.Fatalf("AddParticipants q2: %v", err)
	}

	// A resolved thread: excluded.
	q3, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q3"})
	if err != nil {
		t.Fatalf("AddQuestion q3: %v", err)
	}
	if err := s.ResolveQuestion(q3, "answered"); err != nil {
		t.Fatalf("ResolveQuestion q3: %v", err)
	}

	got, err := s.ListOpenThreads()
	if err != nil {
		t.Fatalf("ListOpenThreads: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("threads = %+v, want the two open ones", got)
	}

	first := got[0]
	if first.Question.ID != q1 || first.Question.TaskID != taskID || first.Question.AskedBy != "orch-1" {
		t.Errorf("first question = %+v, want q1 on task %d", first.Question, taskID)
	}
	if first.LastMessage == nil {
		t.Fatalf("first thread has no last message, want the cto one")
	}
	if first.LastMessage.Author != "cto" || first.LastMessage.Body != "last" {
		t.Errorf("last message = %+v, want the cto 'last' one", *first.LastMessage)
	}
	if !reflect.DeepEqual(first.LastMessage.AddressedTo, []string{"orch-1"}) {
		t.Errorf("addressed_to = %v, want [orch-1]", first.LastMessage.AddressedTo)
	}
	if !reflect.DeepEqual(first.Participants, []string{"cto", "human", "orch-1"}) {
		t.Errorf("participants = %v, want [cto human orch-1]", first.Participants)
	}

	second := got[1]
	if second.Question.ID != q2 || second.Question.RoleID != "cto" {
		t.Errorf("second question = %+v, want q2 on role cto", second.Question)
	}
	if second.LastMessage != nil {
		t.Errorf("last message = %+v, want nil for a thread with no messages", *second.LastMessage)
	}
	if !reflect.DeepEqual(second.Participants, []string{"cto", "human"}) {
		t.Errorf("participants = %v, want [cto human]", second.Participants)
	}
}

// TestListOpenThreadsCarriesType: the thread's type travels with it. The
// heartbeat's staleness sweep must skip fyi notes, and it reads them from
// this aggregate rather than re-querying every thread.
func TestListOpenThreadsCarriesType(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	q, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q", Type: QuestionTypeDecision})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	got, err := s.ListOpenThreads()
	if err != nil {
		t.Fatalf("ListOpenThreads: %v", err)
	}
	if len(got) != 1 || got[0].Question.ID != q {
		t.Fatalf("threads = %+v, want the one open thread", got)
	}
	if got[0].Question.Type != QuestionTypeDecision {
		t.Errorf("Question.Type = %q, want %q", got[0].Question.Type, QuestionTypeDecision)
	}
}
