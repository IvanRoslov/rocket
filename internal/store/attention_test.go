package store

import (
	"reflect"
	"strings"
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

// backfillAttentionSQL returns the backfill section of migration 0011 — the
// statements between the BACKFILL-BEGIN/END markers — so the test exercises
// exactly the SQL that ships, not a paraphrase of it.
func backfillAttentionSQL(t *testing.T) string {
	t.Helper()
	raw, err := migrationsFS.ReadFile("migrations/0011_attention.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	_, after, ok := strings.Cut(string(raw), "-- BACKFILL-BEGIN")
	if !ok {
		t.Fatal("migration 0011 lost its BACKFILL-BEGIN marker")
	}
	body, _, ok := strings.Cut(after, "-- BACKFILL-END")
	if !ok {
		t.Fatal("migration 0011 lost its BACKFILL-END marker")
	}
	return body
}

// TestBackfillAttentionMatchesLegacyWaitingOn builds threads in the shapes a
// pre-0011 database holds them in, wipes the attention table, re-runs the
// migration's backfill and checks it seeds exactly the turn the old
// last-entry derivation produced.
func TestBackfillAttentionMatchesLegacyWaitingOn(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	// 1. Addressed question, no messages yet: the turn is its addressees.
	addressed := mustAddOpenQuestion(t, s, taskID, Question{
		AskedBy: "orch-1", Body: "?", AddressedTo: []string{"cto"},
	})
	// 2. Human-opened thread stored with the legacy empty asked_by.
	byHuman := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "", Body: "?"})
	// 3. Last message from cto, addressed to nobody: everyone but cto.
	replied := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "?"})
	// 4. Last message addressed to two people: exactly those two.
	narrowed := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "?"})
	// 5. Resolved thread: waits on nobody.
	resolved := mustAddOpenQuestion(t, s, taskID, Question{AskedBy: "orch-1", Body: "?"})

	for _, qid := range []int64{addressed, byHuman, replied, narrowed, resolved} {
		if err := s.AddParticipants(qid, ParticipantHuman, "orch-1", "cto"); err != nil {
			t.Fatalf("AddParticipants: %v", err)
		}
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{QuestionID: replied, Author: "cto", Body: "here"}); err != nil {
		t.Fatalf("AddQuestionMessage: %v", err)
	}
	if _, err := s.AddQuestionMessage(QuestionMessage{
		QuestionID: narrowed, Author: "cto", Body: "here", AddressedTo: []string{"human", "orch-1"},
	}); err != nil {
		t.Fatalf("AddQuestionMessage: %v", err)
	}
	if err := s.ResolveQuestion(resolved, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	if _, err := s.db.Exec(`DELETE FROM question_attention`); err != nil {
		t.Fatalf("wipe attention: %v", err)
	}
	if _, err := s.db.Exec(backfillAttentionSQL(t)); err != nil {
		t.Fatalf("run backfill: %v", err)
	}

	for _, tc := range []struct {
		name string
		qid  int64
		want []string
	}{
		{"addressed question", addressed, []string{"cto"}},
		{"human-opened thread", byHuman, []string{"cto", "orch-1"}},
		{"reply by cto", replied, []string{"human", "orch-1"}},
		{"reply addressed to two", narrowed, []string{"human", "orch-1"}},
		{"resolved thread", resolved, nil},
	} {
		if got := mustAttention(t, s, tc.qid); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: attention = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}
