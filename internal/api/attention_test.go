package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// attentionTestSetup wires a server, a task owned by orchestrator "orch-1" and
// two persistent-agent sessions: "cto", who gets pulled into threads, and
// "sre", who never does — the outsider the non-participant guard is about.
func attentionTestSetup(t *testing.T, d Deps) (*httptest.Server, int64) {
	t.Helper()
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	addTestSession(t, d, "cto", session.AgentSessionKind, "proj1")
	addTestSession(t, d, "sre", session.AgentSessionKind, "proj1")
	return srv, taskID
}

// ask opens a thread on taskID as sessionID ("" = the human) and returns the
// decoded response, failing the test unless the status matches want.
func ask(t *testing.T, srv *httptest.Server, taskID int64, sessionID string, payload any, want int) questionResponse {
	t.Helper()
	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", sessionID, payload)
	defer resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("POST questions = %d, want %d", resp.StatusCode, want)
	}
	return decodeQuestion(t, resp)
}

func TestAskAddressedSetsAttention(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "", map[string]any{"body": "Which approach?", "to": []string{"cto"}}, http.StatusCreated)

	if !reflect.DeepEqual(q.Attention, []string{"cto"}) {
		t.Errorf("attention = %#v, want [cto]", q.Attention)
	}
	if !reflect.DeepEqual(q.WaitingOn, []string{"cto"}) {
		t.Errorf("waiting_on = %#v, want [cto]", q.WaitingOn)
	}
	if q.Type != store.QuestionTypeDecision {
		t.Errorf("type = %q, want decision", q.Type)
	}
	if want := itoa(taskID) + "/Q1"; q.LocalRef != want {
		t.Errorf("local_ref = %q, want %q", q.LocalRef, want)
	}
}

func TestAskWithoutAddresseesWaitsOnEveryoneElse(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "orch-1", map[string]any{"body": "Which approach?"}, http.StatusCreated)

	if !reflect.DeepEqual(q.Attention, []string{"human"}) {
		t.Errorf("attention = %#v, want [human]", q.Attention)
	}
}

// TestReplyLeavesAttentionAndPassesTheTurn is acceptance criterion 1: the
// addressee replying without naming anyone empties the set, which hands the
// turn to everybody else — not back to the whole thread including itself.
func TestReplyLeavesAttentionAndPassesTheTurn(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "", map[string]any{"body": "Which approach?", "to": []string{"cto"}}, http.StatusCreated)

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "cto", map[string]any{"body": "This one"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reply = %d, want 201", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if !reflect.DeepEqual(got.Attention, []string{"human", "orch-1"}) {
		t.Errorf("attention = %#v, want [human orch-1]", got.Attention)
	}
}

// TestReplyKeepsOtherAddresseesWaiting is the fix the whole task is about: with
// two people on the hook, one answering must not clear the other.
func TestReplyKeepsOtherAddresseesWaiting(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "", map[string]any{
		"body": "Who owns this?", "to": []string{"cto", "orch-1"},
	}, http.StatusCreated)

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "cto", map[string]any{"body": "Not me"})
	defer resp.Body.Close()
	got := decodeQuestion(t, resp)
	if !reflect.DeepEqual(got.Attention, []string{"orch-1"}) {
		t.Errorf("attention = %#v, want [orch-1]", got.Attention)
	}
}

func TestAnswerClearsAttention(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "orch-1", map[string]any{"body": "Which approach?"}, http.StatusCreated)

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "", map[string]any{"body": "This one"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer = %d, want 200", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.Attention != nil {
		t.Errorf("attention = %#v, want empty", got.Attention)
	}
	if got.WaitingOn != nil {
		t.Errorf("waiting_on = %#v, want empty", got.WaitingOn)
	}
}

// TestAnswerChoosesOption covers --choose: the client sends a 1-based index
// into the thread's options and the server substitutes the option's text.
func TestAnswerChoosesOption(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "orch-1", map[string]any{
		"body": "Which approach?", "options": []string{"rewrite", "patch"},
	}, http.StatusCreated)
	if !reflect.DeepEqual(q.Options, []string{"rewrite", "patch"}) {
		t.Fatalf("options = %#v, want [rewrite patch]", q.Options)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "", map[string]any{"choose": 2})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer = %d, want 200", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.Resolution != "answered" {
		t.Errorf("resolution = %q, want answered", got.Resolution)
	}
	last := got.Messages[len(got.Messages)-1]
	if last.Body != "patch" {
		t.Errorf("answer body = %q, want %q", last.Body, "patch")
	}
}

func TestAnswerRejectsOutOfRangeChoice(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "orch-1", map[string]any{
		"body": "Which approach?", "options": []string{"rewrite", "patch"},
	}, http.StatusCreated)

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "", map[string]any{"choose": 5})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("answer = %d, want 400", resp.StatusCode)
	}
}

// TestFYIThreadIsBornResolved covers the fyi type: a status note nobody owes
// an answer to, and therefore no badge.
func TestFYIThreadIsBornResolved(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "orch-1", map[string]any{
		"body": "Deployed to staging", "type": "fyi",
	}, http.StatusCreated)

	if q.Status != "resolved" || q.Resolution != store.QuestionResolutionFYI {
		t.Errorf("status/resolution = %q/%q, want resolved/fyi", q.Status, q.Resolution)
	}
	if q.Attention != nil {
		t.Errorf("attention = %#v, want empty", q.Attention)
	}
	if q.Type != store.QuestionTypeFYI {
		t.Errorf("type = %q, want fyi", q.Type)
	}

	counts, err := taskQuestionCounts(d)
	if err != nil {
		t.Fatalf("taskQuestionCounts: %v", err)
	}
	if c := counts[taskID]; c.Open != 0 || c.AwaitingUser != 0 {
		t.Errorf("counts = %+v, want zero", c)
	}
}

// TestReplyReopensFYIAsDecision: somebody did care after all, so the status
// note becomes an ordinary thread waiting for a turn.
func TestReplyReopensFYIAsDecision(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "orch-1", map[string]any{
		"body": "Deployed to staging", "type": "fyi",
	}, http.StatusCreated)

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "", map[string]any{"body": "Roll it back"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reply = %d, want 201", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.Status != "open" {
		t.Errorf("status = %q, want open", got.Status)
	}
	if got.Type != store.QuestionTypeDecision {
		t.Errorf("type = %q, want decision", got.Type)
	}
	if !reflect.DeepEqual(got.Attention, []string{"orch-1"}) {
		t.Errorf("attention = %#v, want [orch-1]", got.Attention)
	}
}

// TestNonParticipantReplyNeedsJoin is acceptance criterion 2: a persistent
// agent writing into a thread it has nothing to do with is the misdelivery the
// guard exists to catch.
func TestNonParticipantReplyNeedsJoin(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "orch-1", map[string]any{"body": "Which approach?"}, http.StatusCreated)

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "sre", map[string]any{"body": "wrong thread"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("reply = %d, want 403", resp.StatusCode)
	}
	var body errBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Error.Code != "not_a_participant" {
		t.Errorf("code = %q, want not_a_participant", body.Error.Code)
	}
	// The error must carry enough context to see the miss: the thread's ref
	// and what it is about.
	if !strings.Contains(body.Error.Message, itoa(taskID)+"/Q1") ||
		!strings.Contains(body.Error.Message, "Which approach?") ||
		!strings.Contains(body.Error.Message, "join") {
		t.Errorf("message lacks thread context or join hint: %q", body.Error.Message)
	}
}

func TestNonParticipantReplyWithJoinSucceeds(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "orch-1", map[string]any{"body": "Which approach?"}, http.StatusCreated)

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "sre",
		map[string]any{"body": "I know this one", "join": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reply = %d, want 201", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if !contains(got.Participants, "sre") {
		t.Errorf("participants = %#v, want sre joined", got.Participants)
	}
}

// TestNonParticipantAnswerNeedsJoin closes the same hole on the answer path,
// which used to let any persistent agent resolve any thread.
func TestNonParticipantAnswerNeedsJoin(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "orch-1", map[string]any{"body": "Which approach?"}, http.StatusCreated)

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "sre", map[string]any{"body": "done"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("answer = %d, want 403", resp.StatusCode)
	}
}

// TestDryRunReplyWritesNothing: the target echo without the write, so a
// misaddressed reply can be caught before it is sent.
func TestDryRunReplyWritesNothing(t *testing.T) {
	d := questionsTestDeps(t)
	srv, taskID := attentionTestSetup(t, d)

	q := ask(t, srv, taskID, "orch-1", map[string]any{"body": "Which approach?"}, http.StatusCreated)

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "",
		map[string]any{"body": "This one", "dry_run": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dry-run reply = %d, want 200", resp.StatusCode)
	}
	var got struct {
		questionResponse
		DryRun bool   `json:"dry_run"`
		Echo   string `json:"echo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.DryRun {
		t.Error("dry_run = false, want true")
	}
	if !strings.Contains(got.Echo, itoa(taskID)+"/Q1") || !strings.Contains(got.Echo, "Which approach?") {
		t.Errorf("echo = %q, want the thread's ref and subject", got.Echo)
	}
	if len(got.Messages) != 0 {
		t.Errorf("messages = %d, want 0 — a dry run writes nothing", len(got.Messages))
	}

	msgs, err := d.Store.ListQuestionMessages(q.ID)
	if err != nil {
		t.Fatalf("ListQuestionMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("stored messages = %d, want 0", len(msgs))
	}
}
