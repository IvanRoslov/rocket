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

// mustAttention reads a thread's attention set, failing the test on error.
func mustAttention(t *testing.T, s *Store, qid int64) []string {
	t.Helper()
	got, err := s.ListAttention(qid)
	if err != nil {
		t.Fatalf("ListAttention: %v", err)
	}
	return got
}

func TestAttentionOnOpenAddressed(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "?", AddressedTo: []string{"cto"}})

	if err := s.AttentionOnOpen(qid, "orch-1", []string{"cto"}, []string{"cto", "human", "orch-1"}); err != nil {
		t.Fatalf("AttentionOnOpen: %v", err)
	}
	if got := mustAttention(t, s, qid); !reflect.DeepEqual(got, []string{"cto"}) {
		t.Errorf("attention = %#v, want [cto]", got)
	}
}

func TestAttentionOnOpenWithoutAddressees(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "?"})

	if err := s.AttentionOnOpen(qid, "orch-1", nil, []string{"cto", "human", "orch-1"}); err != nil {
		t.Fatalf("AttentionOnOpen: %v", err)
	}
	if got := mustAttention(t, s, qid); !reflect.DeepEqual(got, []string{"cto", "human"}) {
		t.Errorf("attention = %#v, want [cto human]", got)
	}
}

func TestAttentionOnEntryAuthorLeavesAndAddresseesJoin(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "?"})

	// Two people owe an answer; one of them replies and names the third.
	if err := s.SetAttention(qid, []string{"cto", "human"}); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
	if err := s.AttentionOnEntry(qid, "cto", []string{"orch-1"}, []string{"cto", "human", "orch-1"}); err != nil {
		t.Fatalf("AttentionOnEntry: %v", err)
	}
	if got := mustAttention(t, s, qid); !reflect.DeepEqual(got, []string{"human", "orch-1"}) {
		t.Errorf("attention = %#v, want [human orch-1]", got)
	}
}

func TestAttentionOnEntryEmptiedPassesTurnToEveryoneElse(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "?", AddressedTo: []string{"cto"}})

	if err := s.SetAttention(qid, []string{"cto"}); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
	// cto answers without naming anyone: the set empties, so the turn goes
	// back to everyone but cto.
	if err := s.AttentionOnEntry(qid, "cto", nil, []string{"cto", "human", "orch-1"}); err != nil {
		t.Fatalf("AttentionOnEntry: %v", err)
	}
	if got := mustAttention(t, s, qid); !reflect.DeepEqual(got, []string{"human", "orch-1"}) {
		t.Errorf("attention = %#v, want [human orch-1]", got)
	}
}

func TestAttentionOnEntryKeepsOthersWaiting(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "human", Body: "?"})

	if err := s.SetAttention(qid, []string{"cto", "orch-1"}); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
	// Only cto has spoken: orch-1 still owes an answer, and the reply must
	// NOT hand the turn back to the human the way the old last-entry rule did.
	if err := s.AttentionOnEntry(qid, "cto", nil, []string{"cto", "human", "orch-1"}); err != nil {
		t.Fatalf("AttentionOnEntry: %v", err)
	}
	if got := mustAttention(t, s, qid); !reflect.DeepEqual(got, []string{"orch-1"}) {
		t.Errorf("attention = %#v, want [orch-1]", got)
	}
}

func TestClearAttention(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "human", Body: "?"})

	if err := s.SetAttention(qid, []string{"cto"}); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
	if err := s.ClearAttention(qid); err != nil {
		t.Fatalf("ClearAttention: %v", err)
	}
	if got := mustAttention(t, s, qid); got != nil {
		t.Errorf("attention = %#v, want nil", got)
	}
}

func TestAttentionCanonicalisesTheHuman(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	qid := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "?"})

	// "" is the legacy spelling of the human and must never reach the table.
	if err := s.SetAttention(qid, []string{"", "cto", "cto"}); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
	if got := mustAttention(t, s, qid); !reflect.DeepEqual(got, []string{"cto", "human"}) {
		t.Errorf("attention = %#v, want [cto human]", got)
	}
}

func TestAttentionOfOpenThreads(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)
	open := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "open?"})
	closed := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "closed?"})

	if err := s.SetAttention(open, []string{"human"}); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
	if err := s.SetAttention(closed, []string{"human"}); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
	if err := s.ResolveQuestion(closed, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	got, err := s.AttentionOfOpenThreads()
	if err != nil {
		t.Fatalf("AttentionOfOpenThreads: %v", err)
	}
	want := map[int64][]string{open: {"human"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AttentionOfOpenThreads = %#v, want %#v", got, want)
	}
}
