package store

import (
	"errors"
	"strings"
	"sync"
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

// TestAddQuestion_UserOpenedRoundTrips verifies a user-opened question
// (AskedBy == "", the convention for a human-authored entry) round-trips
// through the NOT NULL asked_by column without error.
func TestAddQuestion_UserOpenedRoundTrips(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	id, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "", Body: "What's the status?"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	got, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if got.AskedBy != "" {
		t.Errorf("AskedBy = %q, want empty", got.AskedBy)
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
	// Written with the legacy empty author, read back canonicalised.
	if msgs[0].Body != "user reply" || msgs[0].Author != ParticipantHuman {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}
	if msgs[1].Body != "orch reply" || msgs[1].Author != "orch-1" {
		t.Errorf("msgs[1] = %+v", msgs[1])
	}
	if msgs[2].Kind != "answer" || msgs[2].Body != "final answer" {
		t.Errorf("msgs[2] = %+v", msgs[2])
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

func TestListAllOpenQuestions(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	q1, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "open"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	q2, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "closed"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := s.ResolveQuestion(q2, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	got, err := s.ListAllOpenQuestions()
	if err != nil {
		t.Fatalf("ListAllOpenQuestions: %v", err)
	}
	if len(got) != 1 || got[0].ID != q1 {
		t.Fatalf("got %+v, want exactly q1(%d)", got, q1)
	}
}

// --- unified threads (#722) -------------------------------------------------

func TestAddQuestionMessage_RoundTripsAddressedTo(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Which approach?"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	if _, err := s.AddQuestionMessage(QuestionMessage{
		QuestionID: qid, Author: "orch-1", Body: "addressed", AddressedTo: []string{"cto", "human"},
	}); err != nil {
		t.Fatalf("AddQuestionMessage addressed: %v", err)
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{
		QuestionID: qid, Author: "orch-1", Body: "broadcast",
	}); err != nil {
		t.Fatalf("AddQuestionMessage broadcast: %v", err)
	}

	msgs, err := s.ListQuestionMessages(qid)
	if err != nil {
		t.Fatalf("ListQuestionMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if got := strings.Join(msgs[0].AddressedTo, ","); got != "cto,human" {
		t.Errorf("AddressedTo = %q, want \"cto,human\"", got)
	}
	if len(msgs[1].AddressedTo) != 0 {
		t.Errorf("broadcast AddressedTo = %v, want empty", msgs[1].AddressedTo)
	}
}

// A message written with the legacy empty author — the convention every
// current caller in internal/api uses for the human — must read back as the
// canonical participant id.
func TestAddQuestionMessage_NormalisesTheHumanAuthor(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Which approach?"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{QuestionID: qid, Author: "", Body: "from the human"}); err != nil {
		t.Fatalf("AddQuestionMessage: %v", err)
	}

	msgs, err := s.ListQuestionMessages(qid)
	if err != nil {
		t.Fatalf("ListQuestionMessages: %v", err)
	}
	if msgs[0].Author != ParticipantHuman {
		t.Errorf("Author = %q, want %q", msgs[0].Author, ParticipantHuman)
	}
	if !IsHuman(msgs[0].Author) {
		t.Errorf("IsHuman(%q) = false, want true", msgs[0].Author)
	}
}

func TestIsHuman_AcceptsBothTheLegacyAndCanonicalForms(t *testing.T) {
	for _, id := range []string{"", ParticipantHuman} {
		if !IsHuman(id) {
			t.Errorf("IsHuman(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"cto", "orch-1"} {
		if IsHuman(id) {
			t.Errorf("IsHuman(%q) = true, want false", id)
		}
	}
}

// A thread may be bound to a role instead of a task, or to neither.
func TestAddQuestion_RoleBoundThreadRoundTrips(t *testing.T) {
	s := openTestStore(t)
	seedAgentForQuestions(t, s, "cto")

	id, err := s.AddQuestion(Question{RoleID: "cto", AskedBy: "cto-run-1", Body: "Role question"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	got, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if got.RoleID != "cto" {
		t.Errorf("RoleID = %q, want cto", got.RoleID)
	}
	if got.TaskID != 0 {
		t.Errorf("TaskID = %d, want 0", got.TaskID)
	}
}

func TestAddQuestion_UnboundThreadRoundTrips(t *testing.T) {
	s := openTestStore(t)

	id, err := s.AddQuestion(Question{AskedBy: "orch-1", Body: "Unbound"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	got, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if got.TaskID != 0 || got.RoleID != "" {
		t.Errorf("got %+v, want no task and no role", got)
	}
}

func TestAddParticipants_IsIdempotent(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	if err := s.AddParticipants(qid, "human", "cto"); err != nil {
		t.Fatalf("AddParticipants first: %v", err)
	}
	// Re-adding an existing participant is a no-op, not an error, and does
	// not duplicate the row.
	if err := s.AddParticipants(qid, "cto", "orch-1"); err != nil {
		t.Fatalf("AddParticipants second: %v", err)
	}

	got, err := s.ListParticipants(qid)
	if err != nil {
		t.Fatalf("ListParticipants: %v", err)
	}
	if strings.Join(got, ",") != "cto,human,orch-1" {
		t.Errorf("participants = %v, want [cto human orch-1]", got)
	}
}

func TestAddParticipants_NoIdsIsANoOp(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	if err := s.AddParticipants(qid); err != nil {
		t.Fatalf("AddParticipants: %v", err)
	}
	got, err := s.ListParticipants(qid)
	if err != nil {
		t.Fatalf("ListParticipants: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("participants = %v, want none", got)
	}
}

func TestListQuestionsForParticipant_OpenOnly(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	add := func(body string) int64 {
		t.Helper()
		id, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: body})
		if err != nil {
			t.Fatalf("AddQuestion %s: %v", body, err)
		}
		return id
	}
	openWithCTO := add("open, cto participates")
	resolvedWithCTO := add("resolved, cto participates")
	openWithoutCTO := add("open, cto absent")

	if err := s.AddParticipants(openWithCTO, "human", "cto"); err != nil {
		t.Fatalf("AddParticipants: %v", err)
	}
	if err := s.AddParticipants(resolvedWithCTO, "human", "cto"); err != nil {
		t.Fatalf("AddParticipants: %v", err)
	}
	if err := s.AddParticipants(openWithoutCTO, "human", "orch-1"); err != nil {
		t.Fatalf("AddParticipants: %v", err)
	}
	if err := s.ResolveQuestion(resolvedWithCTO, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	got, err := s.ListQuestionsForParticipant("cto", true)
	if err != nil {
		t.Fatalf("ListQuestionsForParticipant: %v", err)
	}
	if len(got) != 1 || got[0].ID != openWithCTO {
		t.Fatalf("open threads for cto = %+v, want only %d", got, openWithCTO)
	}

	all, err := s.ListQuestionsForParticipant("cto", false)
	if err != nil {
		t.Fatalf("ListQuestionsForParticipant all: %v", err)
	}
	if len(all) != 2 || all[0].ID != openWithCTO || all[1].ID != resolvedWithCTO {
		t.Errorf("all threads for cto = %+v, want %d and %d ascending", all, openWithCTO, resolvedWithCTO)
	}
}

// Task threads and role threads now share one table, so each aggregate must
// exclude the other's rows instead of silently counting them.
func TestThreadAggregates_DoNotLeakAcrossSubjects(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	seedAgentForQuestions(t, s, "cto")

	if _, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "task thread"}); err != nil {
		t.Fatalf("AddQuestion task: %v", err)
	}
	if _, err := s.AddQuestion(Question{RoleID: "cto", AskedBy: "cto-run-1", Body: "role thread"}); err != nil {
		t.Fatalf("AddQuestion role: %v", err)
	}

	threads, err := s.ListOpenThreads()
	if err != nil {
		t.Fatalf("ListOpenThreads: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("ListOpenThreads = %+v, want both threads", threads)
	}
	if threads[0].Question.TaskID != taskID || threads[0].Question.RoleID != "" {
		t.Errorf("first thread = %+v, want the task one", threads[0].Question)
	}
	if threads[1].Question.RoleID != "cto" || threads[1].Question.TaskID != 0 {
		t.Errorf("second thread = %+v, want the role one", threads[1].Question)
	}

	all, err := s.ListAllOpenQuestions()
	if err != nil {
		t.Fatalf("ListAllOpenQuestions: %v", err)
	}
	if len(all) != 1 || all[0].TaskID != taskID {
		t.Errorf("ListAllOpenQuestions = %+v, want only the task thread", all)
	}
}

// Acceptance criterion 4: concurrent AddParticipants calls must not error and
// must not duplicate rows. The UNIQUE constraint plus INSERT OR IGNORE is what
// makes this safe; without them the racing inserts would double up.
func TestAddParticipants_IsSafeUnderConcurrency(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch-1", Body: "Q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}

	const writers = 8
	errs := make(chan error, writers)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < writers; i++ {
		go func() {
			start.Wait()
			errs <- s.AddParticipants(qid, "human", "cto", "orch-1")
		}()
	}
	start.Done()
	for i := 0; i < writers; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent AddParticipants: %v", err)
		}
	}

	got, err := s.ListParticipants(qid)
	if err != nil {
		t.Fatalf("ListParticipants: %v", err)
	}
	if strings.Join(got, ",") != "cto,human,orch-1" {
		t.Errorf("participants = %v, want each exactly once", got)
	}
}
