package store

import (
	"errors"
	"testing"
)

func mustAddQuestionTask(t *testing.T, s *Store) int64 {
	t.Helper()
	mustAddTaskSession(t, s, "orch-1")
	id, err := s.AddTask(Task{Title: "Root", ProjectID: "billing", SessionID: "orch-1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	return id
}

func TestAddQuestion_Defaults(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	id, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Which approach?"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	got, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if got.Status != "open" {
		t.Errorf("Status = %q, want open", got.Status)
	}
	if got.AskedAt == 0 {
		t.Errorf("AskedAt not set")
	}
	if got.ResolvedAt != 0 {
		t.Errorf("ResolvedAt = %d, want 0", got.ResolvedAt)
	}
	if got.Body != "Which approach?" || got.TaskID != taskID || got.AskedBy != "orch-1" {
		t.Errorf("field mismatch: %+v", got)
	}
}

func TestGetQuestion_NotFound(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetQuestion(999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListQuestions_OpenOnlyFilter(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	id1, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q1"})
	if err != nil {
		t.Fatalf("AddQuestion 1: %v", err)
	}
	id2, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q2"})
	if err != nil {
		t.Fatalf("AddQuestion 2: %v", err)
	}

	if err := s.ResolveQuestion(id1, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	all, err := s.ListQuestions(taskID, false)
	if err != nil {
		t.Fatalf("ListQuestions all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all len = %d, want 2", len(all))
	}

	open, err := s.ListQuestions(taskID, true)
	if err != nil {
		t.Fatalf("ListQuestions open: %v", err)
	}
	if len(open) != 1 || open[0].ID != id2 {
		t.Fatalf("open = %+v, want only id %d", open, id2)
	}
}

func TestResolveQuestion_SetsResolutionAndTimestamp(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	id, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	if err := s.ResolveQuestion(id, "dismissed"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	got, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if got.Status != "resolved" {
		t.Errorf("Status = %q, want resolved", got.Status)
	}
	if got.Resolution != "dismissed" {
		t.Errorf("Resolution = %q, want dismissed", got.Resolution)
	}
	if got.ResolvedAt == 0 {
		t.Errorf("ResolvedAt not set")
	}
}

func TestResolveQuestion_AlreadyResolvedErrors(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	id, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := s.ResolveQuestion(id, "answered"); err != nil {
		t.Fatalf("first ResolveQuestion: %v", err)
	}

	if err := s.ResolveQuestion(id, "answered"); err == nil {
		t.Fatal("second ResolveQuestion: want error, got nil")
	} else if !errors.Is(err, ErrQuestionResolved) {
		t.Fatalf("expected ErrQuestionResolved, got: %v", err)
	}
}

func TestResolveQuestion_NotFound(t *testing.T) {
	s := openTestStore(t)
	if err := s.ResolveQuestion(999, "answered"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAddQuestionMessage_AndList(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	qid, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	if _, err := s.AddQuestionMessage(QuestionMessage{QuestionID: qid, Author: "", Kind: "reply", Body: "user reply"}); err != nil {
		t.Fatalf("AddQuestionMessage reply: %v", err)
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{QuestionID: qid, Author: "orch-1", Kind: "reply", Body: "orch reply"}); err != nil {
		t.Fatalf("AddQuestionMessage orch reply: %v", err)
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{QuestionID: qid, Kind: "answer", Body: "final answer"}); err != nil {
		t.Fatalf("AddQuestionMessage answer: %v", err)
	}

	msgs, err := s.ListQuestionMessages(qid)
	if err != nil {
		t.Fatalf("ListQuestionMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	if msgs[0].Body != "user reply" || msgs[0].Author != "" {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Body != "orch reply" || msgs[1].Author != "orch-1" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
	if msgs[2].Kind != "answer" || msgs[2].Body != "final answer" {
		t.Errorf("msgs[2] = %+v", msgs[2])
	}
}

func TestCountOpenQuestions(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	n, err := s.CountOpenQuestions(taskID)
	if err != nil {
		t.Fatalf("CountOpenQuestions: %v", err)
	}
	if n != 0 {
		t.Fatalf("n = %d, want 0", n)
	}

	id1, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q1"})
	if err != nil {
		t.Fatalf("AddQuestion 1: %v", err)
	}
	if _, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q2"}); err != nil {
		t.Fatalf("AddQuestion 2: %v", err)
	}

	n, err = s.CountOpenQuestions(taskID)
	if err != nil {
		t.Fatalf("CountOpenQuestions: %v", err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}

	if err := s.ResolveQuestion(id1, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	n, err = s.CountOpenQuestions(taskID)
	if err != nil {
		t.Fatalf("CountOpenQuestions: %v", err)
	}
	if n != 1 {
		t.Fatalf("n = %d, want 1", n)
	}
}

func TestQuestionOrdinal(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	id1, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q1"})
	if err != nil {
		t.Fatalf("AddQuestion 1: %v", err)
	}
	id2, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q2"})
	if err != nil {
		t.Fatalf("AddQuestion 2: %v", err)
	}
	id3, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q3"})
	if err != nil {
		t.Fatalf("AddQuestion 3: %v", err)
	}

	for i, id := range []int64{id1, id2, id3} {
		q, err := s.GetQuestion(id)
		if err != nil {
			t.Fatalf("GetQuestion: %v", err)
		}
		ord, err := s.QuestionOrdinal(q)
		if err != nil {
			t.Fatalf("QuestionOrdinal: %v", err)
		}
		if ord != i+1 {
			t.Errorf("QuestionOrdinal(%d) = %d, want %d", id, ord, i+1)
		}
	}
}

func TestReopenQuestion(t *testing.T) {
	st := openTestStore(t)
	taskID := mustAddQuestionTask(t, st)

	qid, err := st.AddQuestion(Question{TaskID: taskID, Body: "Q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	// Reopening an OPEN question is an error.
	if err := st.ReopenQuestion(qid); !errors.Is(err, ErrQuestionOpen) {
		t.Fatalf("reopen open question: err = %v, want ErrQuestionOpen", err)
	}

	if err := st.ResolveQuestion(qid, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}
	if err := st.ReopenQuestion(qid); err != nil {
		t.Fatalf("ReopenQuestion: %v", err)
	}

	q, err := st.GetQuestion(qid)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if q.Status != "open" || q.Resolution != "" || q.ResolvedAt != 0 {
		t.Errorf("after reopen: %+v, want open with cleared resolution/resolved_at", q)
	}

	// Unknown id → ErrNotFound.
	if err := st.ReopenQuestion(99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("reopen unknown: err = %v, want ErrNotFound", err)
	}
}
