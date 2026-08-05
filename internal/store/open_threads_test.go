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

// TestThreadOrdinalsMatchPerQuestionOrdinal: the bulk ordinal pass must agree
// with the single-thread ordinals local refs are built from today, including
// across a resolved thread — otherwise the inbox would label a thread with a
// ref that its own detail view disagrees with.
func TestThreadOrdinalsMatchPerQuestionOrdinal(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	seedAgentForQuestions(t, s, "cto")

	var taskQs []int64
	for i := 0; i < 3; i++ {
		id, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "q"})
		if err != nil {
			t.Fatalf("AddQuestion task %d: %v", i, err)
		}
		taskQs = append(taskQs, id)
	}
	if err := s.ResolveQuestion(taskQs[1], "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	roleQ, err := s.AddQuestion(Question{RoleID: "cto", Body: "r"})
	if err != nil {
		t.Fatalf("AddQuestion role: %v", err)
	}

	got, err := s.ThreadOrdinals()
	if err != nil {
		t.Fatalf("ThreadOrdinals: %v", err)
	}
	for i, id := range taskQs {
		q, err := s.GetQuestion(id)
		if err != nil {
			t.Fatalf("GetQuestion %d: %v", id, err)
		}
		want, err := s.QuestionOrdinal(q)
		if err != nil {
			t.Fatalf("QuestionOrdinal %d: %v", id, err)
		}
		if want != i+1 {
			t.Fatalf("QuestionOrdinal %d = %d, want %d — test setup is wrong", id, want, i+1)
		}
		if got[id] != want {
			t.Errorf("ThreadOrdinals[%d] = %d, want %d", id, got[id], want)
		}
	}
	if got[roleQ] != 1 {
		t.Errorf("ThreadOrdinals[role q] = %d, want 1 — role threads count separately", got[roleQ])
	}
}

// TestListThreadsIncludesResolved: the unified inbox (rocket questions --all)
// needs closed threads too, with their participants and their type/options —
// none of which the open-only listing had to carry.
func TestListThreadsIncludesResolved(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	open, err := s.AddQuestion(Question{
		TaskID: taskID, AskedBy: "orch-1", Body: "open one",
		Options: []string{"A", "B"},
	})
	if err != nil {
		t.Fatalf("AddQuestion open: %v", err)
	}
	if err := s.AddParticipants(open, "human", "orch-1"); err != nil {
		t.Fatalf("AddParticipants open: %v", err)
	}
	if err := s.SetAttention(open, []string{"human"}); err != nil {
		t.Fatalf("SetAttention open: %v", err)
	}

	closed, err := s.AddQuestion(Question{
		TaskID: taskID, AskedBy: "orch-1", Body: "closed one", Type: QuestionTypeFYI,
	})
	if err != nil {
		t.Fatalf("AddQuestion closed: %v", err)
	}
	if err := s.AddParticipants(closed, "human", "orch-1"); err != nil {
		t.Fatalf("AddParticipants closed: %v", err)
	}
	if err := s.ResolveQuestion(closed, QuestionResolutionFYI); err != nil {
		t.Fatalf("ResolveQuestion closed: %v", err)
	}

	openOnly, err := s.ListThreads(false)
	if err != nil {
		t.Fatalf("ListThreads(false): %v", err)
	}
	if len(openOnly) != 1 || openOnly[0].Question.ID != open {
		t.Fatalf("ListThreads(false) = %+v, want only the open thread", openOnly)
	}
	if !reflect.DeepEqual(openOnly[0].Question.Options, []string{"A", "B"}) {
		t.Errorf("options = %v, want [A B] — the inbox renders them", openOnly[0].Question.Options)
	}
	if !reflect.DeepEqual(openOnly[0].Attention, []string{"human"}) {
		t.Errorf("attention = %v, want [human]", openOnly[0].Attention)
	}

	all, err := s.ListThreads(true)
	if err != nil {
		t.Fatalf("ListThreads(true): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListThreads(true) = %+v, want both threads", all)
	}
	got := all[1]
	if got.Question.ID != closed || got.Question.Status != "resolved" {
		t.Fatalf("second thread = %+v, want the resolved one", got.Question)
	}
	if got.Question.Type != QuestionTypeFYI || got.Question.Resolution != QuestionResolutionFYI {
		t.Errorf("type/resolution = %q/%q, want fyi/fyi",
			got.Question.Type, got.Question.Resolution)
	}
	if !reflect.DeepEqual(got.Participants, []string{"human", "orch-1"}) {
		t.Errorf("participants = %v, want [human orch-1] on a resolved thread", got.Participants)
	}
	if len(got.Attention) != 0 {
		t.Errorf("attention = %v, want empty on a resolved thread", got.Attention)
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
