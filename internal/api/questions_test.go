package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// questionsTestDeps reuses messagesTestDeps: a real store/bus and a real
// queue.Queue wired to a fake runtime whose Inject always succeeds and whose
// recipients always report "ready", so delivery to the orchestrator can be
// observed end to end via the messages table.
func questionsTestDeps(t *testing.T) Deps {
	t.Helper()
	return messagesTestDeps(t)
}

// setupQuestionTask creates project "proj1", an orchestrator session
// "orch-1", and a root task owned by it. Returns the task id.
func setupQuestionTask(t *testing.T, d Deps) int64 {
	t.Helper()
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")
	taskID, err := d.Store.AddTask(store.Task{Title: "Root", ProjectID: "proj1", SessionID: "orch-1"})
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	return taskID
}

func decodeQuestion(t *testing.T, resp *http.Response) questionResponse {
	t.Helper()
	var q questionResponse
	if err := json.NewDecoder(resp.Body).Decode(&q); err != nil {
		t.Fatalf("decode question: %v", err)
	}
	return q
}

// getQuestions GETs a task's threads as the given caller ("" = the human).
func getQuestions(t *testing.T, srv *httptest.Server, taskID int64, sessionID string) []questionResponse {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if sessionID != "" {
		req.Header.Set(sessionHeader, sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET questions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET questions = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Questions []questionResponse `json:"questions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode questions: %v", err)
	}
	return body.Questions
}

// TestGetTaskQuestions_ParticipantFields covers the additive wire contract: an
// orchestrator-opened thread has the human and the orchestrator as
// participants, waits on the human, and reports your_turn for a human caller.
func TestGetTaskQuestions_ParticipantFields(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?"})
	resp.Body.Close()

	got := getQuestions(t, srv, taskID, "")
	if len(got) != 1 {
		t.Fatalf("got %d questions, want 1", len(got))
	}
	q := got[0]
	if !reflect.DeepEqual(q.Participants, []string{"human", "orch-1"}) {
		t.Errorf("participants = %v, want [human orch-1]", q.Participants)
	}
	if !reflect.DeepEqual(q.WaitingOn, []string{"human"}) {
		t.Errorf("waiting_on = %v, want [human]", q.WaitingOn)
	}
	if !q.YourTurn {
		t.Error("your_turn = false for a human caller, want true")
	}
	if q.WhoseTurn != "user" {
		t.Errorf("whose_turn = %q, want user (compat)", q.WhoseTurn)
	}
}

// TestGetTaskQuestions_YourTurnIsCallerRelative: the same thread reads as
// not-your-turn for the orchestrator that opened it.
func TestGetTaskQuestions_YourTurnIsCallerRelative(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?"})
	resp.Body.Close()

	got := getQuestions(t, srv, taskID, "orch-1")
	if len(got) != 1 {
		t.Fatalf("got %d questions, want 1", len(got))
	}
	if got[0].YourTurn {
		t.Error("your_turn = true for the asker, want false")
	}
}

// TestGetTaskQuestions_HumanOpenedWaitsOnOrchestrator is the no-message case
// from the other side: asked_by is empty, so the human asked and the task's
// orchestrator owes the reply.
func TestGetTaskQuestions_HumanOpenedWaitsOnOrchestrator(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions",
		map[string]any{"body": "Status?"})
	resp.Body.Close()

	got := getQuestions(t, srv, taskID, "")
	if len(got) != 1 {
		t.Fatalf("got %d questions, want 1", len(got))
	}
	if !reflect.DeepEqual(got[0].WaitingOn, []string{"orch-1"}) {
		t.Errorf("waiting_on = %v, want [orch-1]", got[0].WaitingOn)
	}
	if got[0].WhoseTurn != "orchestrator" {
		t.Errorf("whose_turn = %q, want orchestrator (compat)", got[0].WhoseTurn)
	}
	if got[0].YourTurn {
		t.Error("your_turn = true for the human asker, want false")
	}
}

// --- POST /v1/tasks/{id}/questions -------------------------------------

func TestPostTaskQuestions_HappyPath(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?", "context": "some context"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeQuestion(t, resp)
	if q.Body != "Which approach?" || q.Context != "some context" {
		t.Errorf("q = %+v", q)
	}
	if q.Status != "open" {
		t.Errorf("Status = %q, want open", q.Status)
	}
	if q.Ordinal != 1 {
		t.Errorf("Ordinal = %d, want 1", q.Ordinal)
	}
	if q.WhoseTurn != "user" {
		t.Errorf("WhoseTurn = %q, want user", q.WhoseTurn)
	}
	if len(q.Messages) != 0 {
		t.Errorf("Messages = %+v, want empty", q.Messages)
	}

	found := false
	deadline := time.Now().Add(2 * time.Second)
	for !found && time.Now().Before(deadline) {
		select {
		case e := <-ch:
			if e.Type == "task.question_asked" {
				found = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !found {
		t.Fatal("did not observe task.question_asked event")
	}
}

// TestPostTaskQuestions_HumanOpensThreadToOrchestrator covers the new
// direction: a human (no X-Rocket-Session header) opens a question thread
// addressed to the task's orchestrator. It should succeed with AskedBy=="human",
// whose_turn "orchestrator" (nothing has been said back yet), and the
// question body should be injected into the orchestrator's message queue.
func TestPostTaskQuestions_HumanOpensThreadToOrchestrator(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", map[string]any{"body": "What's the status?"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeQuestion(t, resp)
	if q.AskedBy != store.ParticipantHuman {
		t.Errorf("AskedBy = %q, want %q (user-opened)", q.AskedBy, store.ParticipantHuman)
	}
	if q.WhoseTurn != "orchestrator" {
		t.Errorf("WhoseTurn = %q, want orchestrator", q.WhoseTurn)
	}
	if len(q.Messages) != 0 {
		t.Errorf("Messages = %+v, want empty", q.Messages)
	}

	wantPrefix := "[#" + itoa(taskID) + "/Q1 question from human] What's the status?"
	msgs, err := d.Store.ListMessages("orch-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != wantPrefix {
		t.Fatalf("delivered messages = %+v, want single message %q", msgs, wantPrefix)
	}
}

// TestPostTaskQuestions_HumanOpensThreadWithContext verifies the context is
// appended to the injected body.
func TestPostTaskQuestions_HumanOpensThreadWithContext(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions",
		map[string]any{"body": "What's the status?", "context": "extra info"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	wantBody := "[#" + itoa(taskID) + "/Q1 question from human] What's the status?\n\nextra info"
	msgs, err := d.Store.ListMessages("orch-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != wantBody {
		t.Fatalf("delivered messages = %+v, want single message %q", msgs, wantBody)
	}
}

// TestUserOpenedQuestionThread_OrchestratorRepliesNoInjection covers the
// tail of the reverse-direction flow: after a user opens a thread, the
// orchestrator's reply flips whose_turn to "user" and lands thread-only
// (no message re-injected to the orchestrator itself).
func TestUserOpenedQuestionThread_OrchestratorRepliesNoInjection(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", map[string]any{"body": "status?"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	// The initial injection counts as one delivered message; clear it isn't
	// what we're asserting on below, only that no *additional* delivery
	// happens on the orchestrator's reply.
	before, err := d.Store.ListMessages("orch-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}

	replyResp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "orch-1", map[string]any{"body": "all good"})
	if replyResp.StatusCode != http.StatusCreated {
		t.Fatalf("reply status = %d, want 201", replyResp.StatusCode)
	}
	afterReply := decodeQuestion(t, replyResp)
	replyResp.Body.Close()
	if afterReply.WhoseTurn != "user" {
		t.Fatalf("WhoseTurn = %q, want user", afterReply.WhoseTurn)
	}
	if len(afterReply.Messages) != 1 || afterReply.Messages[0].Author != "orch-1" {
		t.Fatalf("messages = %+v", afterReply.Messages)
	}

	after, err := d.Store.ListMessages("orch-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("delivered messages after orch reply = %+v, want unchanged from %+v (thread-only)", after, before)
	}
}

func TestPostTaskQuestions_WorkerForbidden(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	addTestSession(t, d, "worker-1", "worker", "proj1")

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "worker-1", map[string]any{"body": "Q"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
	}
}

func TestPostTaskQuestions_OtherOrchestratorForbidden(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	addTestSession(t, d, "orch-2", "orchestrator", "proj1")

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-2", map[string]any{"body": "Q"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestPostTaskQuestions_NotRootTask(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	rootID := setupQuestionTask(t, d)
	subID, err := d.Store.AddTask(store.Task{Title: "Sub", ProjectID: "proj1", ParentID: rootID, SessionID: "orch-1"})
	if err != nil {
		t.Fatalf("AddTask sub: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(subID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "not_root_task" {
		t.Errorf("code = %q, want not_root_task", eb.Error.Code)
	}
}

func TestPostTaskQuestions_EmptyBody(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": ""})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "empty_body" {
		t.Errorf("code = %q, want empty_body", eb.Error.Code)
	}
}

// --- GET /v1/tasks/{id}/questions ---------------------------------------

func TestGetTaskQuestions_OpenFilter(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	q1resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q1"})
	q1 := decodeQuestion(t, q1resp)
	q1resp.Body.Close()

	q2resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q2"})
	q2 := decodeQuestion(t, q2resp)
	q2resp.Body.Close()

	dismissResp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q1.ID)+"/answer", map[string]any{"dismiss": true})
	dismissResp.Body.Close()

	allResp, err := http.Get(srv.URL + "/v1/tasks/" + itoa(taskID) + "/questions")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer allResp.Body.Close()
	var allBody struct {
		Questions []questionResponse `json:"questions"`
	}
	if err := json.NewDecoder(allResp.Body).Decode(&allBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(allBody.Questions) != 2 {
		t.Fatalf("all len = %d, want 2", len(allBody.Questions))
	}

	openResp, err := http.Get(srv.URL + "/v1/tasks/" + itoa(taskID) + "/questions?status=open")
	if err != nil {
		t.Fatalf("GET open: %v", err)
	}
	defer openResp.Body.Close()
	var openBody struct {
		Questions []questionResponse `json:"questions"`
	}
	if err := json.NewDecoder(openResp.Body).Decode(&openBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(openBody.Questions) != 1 || openBody.Questions[0].ID != q2.ID {
		t.Fatalf("open = %+v, want only id %d", openBody.Questions, q2.ID)
	}
}

// --- Full thread lifecycle: ask -> user reply -> orch reply -> answer --

func TestQuestionThread_FullLifecycle(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	// Ask.
	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()
	if q.WhoseTurn != "user" {
		t.Fatalf("after ask: WhoseTurn = %q, want user", q.WhoseTurn)
	}

	// User replies -> delivered to orchestrator with the expected prefix.
	userReplyResp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", map[string]any{"body": "consider X"})
	if userReplyResp.StatusCode != http.StatusCreated {
		t.Fatalf("user reply status = %d, want 201", userReplyResp.StatusCode)
	}
	afterUserReply := decodeQuestion(t, userReplyResp)
	userReplyResp.Body.Close()
	if afterUserReply.WhoseTurn != "orchestrator" {
		t.Fatalf("after user reply: WhoseTurn = %q, want orchestrator", afterUserReply.WhoseTurn)
	}
	if len(afterUserReply.Messages) != 1 || afterUserReply.Messages[0].Body != "consider X" || afterUserReply.Messages[0].Author != store.ParticipantHuman {
		t.Fatalf("messages after user reply = %+v", afterUserReply.Messages)
	}

	wantUserReplyPrefix := "[#" + itoa(taskID) + "/Q1 reply from human] consider X"
	msgs, err := d.Store.ListMessages("orch-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Body != wantUserReplyPrefix {
		t.Fatalf("delivered messages = %+v, want single message %q", msgs, wantUserReplyPrefix)
	}

	// Orchestrator replies -> thread-only, no new delivery.
	orchReplyResp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "orch-1", map[string]any{"body": "ok, going with X"})
	if orchReplyResp.StatusCode != http.StatusCreated {
		t.Fatalf("orch reply status = %d, want 201", orchReplyResp.StatusCode)
	}
	afterOrchReply := decodeQuestion(t, orchReplyResp)
	orchReplyResp.Body.Close()
	if afterOrchReply.WhoseTurn != "user" {
		t.Fatalf("after orch reply: WhoseTurn = %q, want user", afterOrchReply.WhoseTurn)
	}
	if len(afterOrchReply.Messages) != 2 || afterOrchReply.Messages[1].Author != "orch-1" {
		t.Fatalf("messages after orch reply = %+v", afterOrchReply.Messages)
	}

	msgsAfterOrchReply, err := d.Store.ListMessages("orch-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgsAfterOrchReply) != 1 {
		t.Fatalf("delivered messages after orch reply = %+v, want still 1 (no new delivery)", msgsAfterOrchReply)
	}

	// Human answers -> resolved, delivered to orchestrator with answer prefix.
	answerResp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"body": "final: use X"})
	if answerResp.StatusCode != http.StatusOK {
		t.Fatalf("answer status = %d, want 200", answerResp.StatusCode)
	}
	afterAnswer := decodeQuestion(t, answerResp)
	answerResp.Body.Close()
	if afterAnswer.Status != "resolved" || afterAnswer.Resolution != "answered" {
		t.Fatalf("after answer: status=%q resolution=%q", afterAnswer.Status, afterAnswer.Resolution)
	}
	if afterAnswer.WhoseTurn != "" {
		t.Fatalf("after answer: WhoseTurn = %q, want empty (resolved)", afterAnswer.WhoseTurn)
	}

	wantAnswerPrefix := "[#" + itoa(taskID) + "/Q1 answer from human] final: use X"
	msgsAfterAnswer, err := d.Store.ListMessages("orch-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgsAfterAnswer) != 2 || msgsAfterAnswer[1].Body != wantAnswerPrefix {
		t.Fatalf("delivered messages after answer = %+v, want second message %q", msgsAfterAnswer, wantAnswerPrefix)
	}

	// Task card's open_questions now reflects real store state.
	taskCardResp, err := http.Get(srv.URL + "/v1/tasks/" + itoa(taskID))
	if err != nil {
		t.Fatalf("GET task: %v", err)
	}
	defer taskCardResp.Body.Close()
	var card taskDetailResponse
	if err := json.NewDecoder(taskCardResp.Body).Decode(&card); err != nil {
		t.Fatalf("decode task card: %v", err)
	}
	if card.OpenQuestions != 0 {
		t.Errorf("OpenQuestions = %d, want 0", card.OpenQuestions)
	}
}

// TestQuestionReply_ResolvedConflict pins the human half of replyReopens: a
// resolved DECISION thread is final for the human, with or without a dispute
// flag (subtask #1181 changed only the agent half). Reopening it was never a
// human power, so the dashboard and mobile lose nothing.
func TestQuestionReply_ResolvedConflict(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	dismissResp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"dismiss": true})
	dismissResp.Body.Close()

	resp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", map[string]any{"body": "too late"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "question_resolved" {
		t.Errorf("code = %q, want question_resolved", eb.Error.Code)
	}

	// And the flag buys the human nothing: dispute is an agent's tool.
	forced := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply",
		map[string]any{"body": "передумал", "dispute": true})
	defer forced.Body.Close()
	if forced.StatusCode != http.StatusConflict {
		t.Fatalf("human reply with dispute = %d, want 409", forced.StatusCode)
	}
}

func TestQuestionReply_WorkerForbidden(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	addTestSession(t, d, "worker-1", "worker", "proj1")

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "worker-1", map[string]any{"body": "nope"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
	}
}

func TestQuestionAnswer_AgentForbidden(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "orch-1", map[string]any{"body": "self answer"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
	}
}

func TestQuestionAnswer_Dismiss_NoDeliveryNoMessageRow(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	resp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"dismiss": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.Status != "resolved" || got.Resolution != "dismissed" {
		t.Errorf("got status=%q resolution=%q", got.Status, got.Resolution)
	}
	if len(got.Messages) != 0 {
		t.Errorf("Messages = %+v, want empty (dismiss adds no thread message)", got.Messages)
	}

	msgs, err := d.Store.ListMessages("orch-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("delivered messages = %+v, want none", msgs)
	}
}

// TestQuestionWrite_EchoesTheTarget: a real write confirms where it landed,
// not only a rehearsal. Echoing on dry-run alone would leave the write that
// actually matters — the one that already happened — unconfirmed (task #1023,
// spec v1 §«Подтверждение цели»).
func TestQuestionWrite_EchoesTheTarget(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Какой подход?"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	wantEcho := "→ " + itoa(taskID) + "/Q1 «Какой подход?» (task #" + itoa(taskID) + " \"Root\")"

	reply := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", map[string]any{"body": "уточняю"})
	defer reply.Body.Close()
	if got := decodeQuestion(t, reply); got.Echo != wantEcho {
		t.Errorf("reply echo = %q, want %q", got.Echo, wantEcho)
	}

	answer := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"body": "берём A"})
	defer answer.Body.Close()
	if got := decodeQuestion(t, answer); got.Echo != wantEcho {
		t.Errorf("answer echo = %q, want %q", got.Echo, wantEcho)
	}
}

// TestQuestionAnswer_DismissWithReasonIsRecordedAndDelivered: "close --dismiss
// <почему>" (task #1023, spec v1 §«Глаголы») must not swallow the reason. A
// bare dismiss stays silent as before; a dismiss WITH a reason records it in
// the thread and tells the participants, since otherwise the orchestrator
// waiting on that thread never learns why it died.
func TestQuestionAnswer_DismissWithReasonIsRecordedAndDelivered(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	resp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer",
		map[string]any{"dismiss": true, "body": "уже решили в чате"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.Status != "resolved" || got.Resolution != "dismissed" {
		t.Fatalf("got status=%q resolution=%q, want resolved/dismissed", got.Status, got.Resolution)
	}
	if len(got.Messages) != 1 || got.Messages[0].Body != "уже решили в чате" {
		t.Fatalf("Messages = %+v, want the reason recorded in the thread", got.Messages)
	}
	if got.Messages[0].Kind != "dismiss" {
		t.Errorf("kind = %q, want dismiss — it is not an answer to the question",
			got.Messages[0].Kind)
	}

	msgs, err := d.Store.ListMessages("orch-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0].Body, "уже решили в чате") {
		t.Errorf("delivered = %+v, want the reason delivered to the orchestrator", msgs)
	}
}

func TestQuestionAnswer_AlreadyResolvedConflict(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	first := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"dismiss": true})
	first.Body.Close()

	second := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"body": "too late"})
	defer second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", second.StatusCode)
	}
	if eb := decodeErr(t, second); eb.Error.Code != "question_resolved" {
		t.Errorf("code = %q, want question_resolved", eb.Error.Code)
	}
}

func TestQuestionAnswer_TerminalOrchestrator_SkipsDeliveryButStillResolves(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	// Simulate the orchestrator session going terminal before the answer lands.
	sess, err := d.Store.GetSession("orch-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if err := d.Store.UpdateSessionState(sess.ID, "killed"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"body": "final answer"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.Status != "resolved" || got.Resolution != "answered" {
		t.Errorf("got status=%q resolution=%q, want resolved/answered", got.Status, got.Resolution)
	}

	msgs, err := d.Store.ListMessages("orch-1", 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("delivered messages = %+v, want none (session terminal)", msgs)
	}
}

func TestQuestionAnswer_ConcurrentDoubleAnswer(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	// Two goroutines race to answer the same question.
	ch := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			resp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"body": "answer"})
			ch <- resp.StatusCode
			resp.Body.Close()
		}()
	}

	// Exactly one should succeed (200), the other should get 409.
	codes := []int{<-ch, <-ch}
	successes := 0
	conflicts := 0
	for _, code := range codes {
		if code == http.StatusOK {
			successes++
		} else if code == http.StatusConflict {
			conflicts++
		} else {
			t.Errorf("unexpected status code: %d", code)
		}
	}
	if successes != 1 {
		t.Errorf("successes = %d, want 1", successes)
	}
	if conflicts != 1 {
		t.Errorf("conflicts = %d, want 1", conflicts)
	}

	// Verify exactly ONE answer message row exists.
	msgs, err := d.Store.ListQuestionMessages(q.ID)
	if err != nil {
		t.Fatalf("ListQuestionMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("msgs len = %d, want 1 (exactly one answer from winner)", len(msgs))
	}
	if msgs[0].Kind != "answer" {
		t.Errorf("msgs[0].Kind = %q, want answer", msgs[0].Kind)
	}
}

func TestQuestionReply_WorkerReplyToResolvedGets403NotConflict(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	addTestSession(t, d, "worker-1", "worker", "proj1")

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	// Resolve the question.
	dismissResp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"dismiss": true})
	dismissResp.Body.Close()

	// Worker tries to reply to resolved question.
	// Should get 403 (unauthorized) NOT 409 (conflict/resolved) to avoid information disclosure.
	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "worker-1", map[string]any{"body": "nope"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
	}
}

// TestQuestionReply_OrchestratorReopensResolved: the task's own orchestrator
// disputing a final answer reopens the thread in place — status back to
// open, task.question_reopened published, open_questions counter counts it
// again. The human's reply to a resolved question stays 409 (covered by
// TestQuestionReply_ResolvedConflict above).
func TestQuestionReply_OrchestratorReopensResolved(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Which DB?"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	ansResp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"body": "sqlite"})
	ansResp.Body.Close()

	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "orch-1",
		map[string]any{"body": "Evidence says sqlite cannot work here: no concurrent writers.", "dispute": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (orchestrator reply must reopen)", resp.StatusCode)
	}
	reopened := decodeQuestion(t, resp)
	if reopened.Status != "open" {
		t.Errorf("status after dispute = %q, want open", reopened.Status)
	}

	sawReopened := false
	deadline := time.Now().Add(2 * time.Second)
	for !sawReopened && time.Now().Before(deadline) {
		select {
		case e := <-ch:
			if e.Type == "task.question_reopened" {
				sawReopened = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawReopened {
		t.Errorf("task.question_reopened event not published")
	}

	// The reopened question counts as open again (dashboard badge).
	taskCardResp, err := http.Get(srv.URL + "/v1/tasks/" + itoa(taskID))
	if err != nil {
		t.Fatalf("GET task: %v", err)
	}
	defer taskCardResp.Body.Close()
	var card taskDetailResponse
	if err := json.NewDecoder(taskCardResp.Body).Decode(&card); err != nil {
		t.Fatalf("decode task card: %v", err)
	}
	if card.OpenQuestions != 1 {
		t.Errorf("OpenQuestions = %d, want 1 after reopen", card.OpenQuestions)
	}

	// And the human can now answer again, closing the loop.
	ans2 := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"body": "ок, бери postgres"})
	defer ans2.Body.Close()
	if ans2.StatusCode != http.StatusOK {
		t.Errorf("second answer status = %d, want 200", ans2.StatusCode)
	}
}

// TestGetAllQuestions: the global list enriches each open question with its
// task title, project and orchestrator tmux name.
func TestGetAllQuestions(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	if _, err := d.Store.AddQuestion(store.Question{TaskID: taskID, AskedBy: "orch-1", Body: "open q"}); err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	qid, err := d.Store.AddQuestion(store.Question{TaskID: taskID, AskedBy: "orch-1", Body: "resolved q"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := d.Store.ResolveQuestion(qid, "answered"); err != nil {
		t.Fatalf("ResolveQuestion: %v", err)
	}

	resp := getJSON(t, srv.URL+"/v1/questions")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Questions []struct {
			questionResponse
			TaskTitle        string `json:"task_title"`
			ProjectID        string `json:"project_id"`
			ProjectName      string `json:"project_name"`
			OrchestratorName string `json:"orchestrator_name"`
		} `json:"questions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Questions) != 1 {
		t.Fatalf("len = %d, want 1 (resolved excluded)", len(body.Questions))
	}
	got := body.Questions[0]
	if got.Body != "open q" || got.TaskID != taskID {
		t.Errorf("question mismatch: %+v", got.questionResponse)
	}
	if got.TaskTitle != "Root" || got.ProjectID != "proj1" {
		t.Errorf("task enrichment = %q/%q, want Root/proj1", got.TaskTitle, got.ProjectID)
	}
	if got.OrchestratorName == "" {
		t.Errorf("orchestrator_name empty, want the orch-1 session tmux name")
	}
}

// setupQuestionAgent registers persistent agent "cto" with no live session.
func setupQuestionAgent(t *testing.T, d Deps) {
	t.Helper()
	if err := d.Store.AddAgent(store.Agent{
		ID: "cto", Dir: "/tmp/cto", Command: "claude", Enabled: true,
	}); err != nil {
		t.Fatalf("AddAgent: %v", err)
	}
}

// TestPostTaskQuestions_ToAddsParticipant covers acceptance criterion 2: an
// orchestrator addresses cto, who becomes a participant, is waited on, and is
// notified in its inbox because it has no live session.
func TestPostTaskQuestions_ToAddsParticipant(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Approve the schema?", "to": []string{"cto"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeQuestion(t, resp)

	if !reflect.DeepEqual(q.Participants, []string{"cto", "human", "orch-1"}) {
		t.Errorf("participants = %v, want [cto human orch-1]", q.Participants)
	}
	// --to narrows the turn from the very first entry (spec v2 §2, migration
	// 0010): the human is NOT waited on for a thread addressed to cto.
	if !reflect.DeepEqual(q.WaitingOn, []string{"cto"}) {
		t.Errorf("waiting_on = %v, want [cto]", q.WaitingOn)
	}

	inbox, err := d.Store.ListInboxMessages("cto", store.InboxUnread, 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("cto inbox has %d messages, want 1", len(inbox))
	}
}

// TestPostQuestionAnswer_AgentMayAnswer covers acceptance criterion 1: a
// persistent agent with ROCKET_SESSION_ID set closes the thread itself.
func TestPostQuestionAnswer_AgentMayAnswer(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)
	addLiveAgentSession(t, d, "cto")

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Approve the schema?", "to": []string{"cto"}})
	q := decodeQuestion(t, resp)
	resp.Body.Close()

	ans := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "cto",
		map[string]any{"body": "Approved."})
	defer ans.Body.Close()
	if ans.StatusCode != http.StatusOK {
		t.Fatalf("agent answer = %d, want 200", ans.StatusCode)
	}
	got := decodeQuestion(t, ans)
	if got.Status != "resolved" || got.Resolution != "answered" {
		t.Errorf("status/resolution = %q/%q, want resolved/answered", got.Status, got.Resolution)
	}
	if len(got.WaitingOn) != 0 {
		t.Errorf("waiting_on = %v, want empty for a resolved thread", got.WaitingOn)
	}
}

// TestPostQuestionAnswer_OrchestratorForbidden covers acceptance criterion 5.
func TestPostQuestionAnswer_OrchestratorForbidden(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions",
		map[string]any{"body": "What now?"})
	q := decodeQuestion(t, resp)
	resp.Body.Close()

	ans := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "orch-1",
		map[string]any{"body": "Done."})
	defer ans.Body.Close()
	if ans.StatusCode != http.StatusForbidden {
		t.Fatalf("orchestrator answer = %d, want 403", ans.StatusCode)
	}
	var e errBody
	if err := json.NewDecoder(ans.Body).Decode(&e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if !strings.Contains(e.Error.Message, "reply") {
		t.Errorf("403 message = %q, want it to point at reply", e.Error.Message)
	}
}

// TestPostQuestionReply_HumanDelegatesWithTo covers acceptance criterion 3.
func TestPostQuestionReply_HumanDelegatesWithTo(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?"})
	q := decodeQuestion(t, resp)
	resp.Body.Close()

	rep := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply",
		map[string]any{"body": "cto decides.", "to": []string{"cto"}})
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusCreated {
		t.Fatalf("human reply = %d, want 201", rep.StatusCode)
	}
	got := decodeQuestion(t, rep)
	if !contains(got.Participants, "cto") {
		t.Errorf("participants = %v, want cto included", got.Participants)
	}
	if !reflect.DeepEqual(got.WaitingOn, []string{"cto"}) {
		t.Errorf("waiting_on = %v, want [cto]", got.WaitingOn)
	}
	if !reflect.DeepEqual(got.Messages[0].AddressedTo, []string{"cto"}) {
		t.Errorf("addressed_to = %v, want [cto] on the wire", got.Messages[0].AddressedTo)
	}

	inbox, err := d.Store.ListInboxMessages("cto", store.InboxUnread, 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(inbox) != 1 {
		t.Errorf("cto inbox has %d messages, want 1", len(inbox))
	}
}

// TestPostQuestionReply_NonParticipantWorkerForbidden: participation, not the
// old "human or my own orchestrator" pair, is what grants reply.
func TestPostQuestionReply_NonParticipantWorkerForbidden(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	addTestSession(t, d, "w-9", "worker", "proj1")

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?"})
	q := decodeQuestion(t, resp)
	resp.Body.Close()

	rep := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "w-9",
		map[string]any{"body": "me too"})
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusForbidden {
		t.Errorf("non-participant worker reply = %d, want 403", rep.StatusCode)
	}
}

// TestQuestionThread_OrchestratorAsksAgentAnswers walks acceptance criteria 1,
// 2 and 4 end to end: the orchestrator addresses cto, cto is notified and
// becomes a participant, cto replies (which notifies the orchestrator), and
// cto's answer closes the thread.
func TestQuestionThread_OrchestratorAsksAgentAnswers(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)
	addLiveAgentSession(t, d, "cto")

	ask := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Approve the schema?", "to": []string{"cto"}})
	q := decodeQuestion(t, ask)
	ask.Body.Close()
	if !contains(q.WaitingOn, "cto") {
		t.Fatalf("after ask waiting_on = %v, want cto included", q.WaitingOn)
	}

	ctoMsgs, err := d.Store.ListMessages("cto", 0)
	if err != nil {
		t.Fatalf("ListMessages(cto): %v", err)
	}
	if len(ctoMsgs) != 1 {
		t.Fatalf("cto got %d queued messages after ask, want 1", len(ctoMsgs))
	}

	rep := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "cto",
		map[string]any{"body": "One question first."})
	got := decodeQuestion(t, rep)
	rep.Body.Close()
	if !reflect.DeepEqual(got.WaitingOn, []string{"human", "orch-1"}) {
		t.Errorf("after cto reply waiting_on = %v, want [human orch-1]", got.WaitingOn)
	}

	orchMsgs, err := d.Store.ListMessages("orch-1", 0)
	if err != nil {
		t.Fatalf("ListMessages(orch-1): %v", err)
	}
	if len(orchMsgs) == 0 {
		t.Fatal("the orchestrator was not notified of cto's reply")
	}
	want := "[#" + itoa(taskID) + "/Q1 reply from cto] One question first."
	if last := orchMsgs[len(orchMsgs)-1].Body; last != want {
		t.Errorf("orchestrator body = %q, want %q", last, want)
	}

	ans := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "cto",
		map[string]any{"body": "Approved."})
	final := decodeQuestion(t, ans)
	ans.Body.Close()
	if final.Status != "resolved" || final.Resolution != "answered" {
		t.Fatalf("status/resolution = %q/%q, want resolved/answered", final.Status, final.Resolution)
	}
	if len(final.WaitingOn) != 0 {
		t.Errorf("waiting_on = %v, want empty", final.WaitingOn)
	}
	if final.WhoseTurn != "" {
		t.Errorf("whose_turn = %q, want empty for a resolved thread", final.WhoseTurn)
	}
}

// TestGetTaskQuestions_UnrelatedSessionSeesNothing: cross-task snooping is the
// one thing the read gate forbids. The thread is still there for its own
// orchestrator.
func TestGetTaskQuestions_UnrelatedSessionSeesNothing(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	addTestSession(t, d, "orch-2", "orchestrator", "proj1")
	if _, err := d.Store.AddTask(store.Task{
		Title: "Other", ProjectID: "proj1", SessionID: "orch-2",
	}); err != nil {
		t.Fatalf("AddTask other: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which approach?"})
	resp.Body.Close()

	if got := getQuestions(t, srv, taskID, "orch-2"); len(got) != 0 {
		t.Errorf("an unrelated orchestrator saw %d threads, want 0", len(got))
	}
	if got := getQuestions(t, srv, taskID, "orch-1"); len(got) != 1 {
		t.Errorf("the task's own orchestrator saw %d threads, want 1", len(got))
	}
	if got := getQuestions(t, srv, taskID, ""); len(got) != 1 {
		t.Errorf("the human saw %d threads, want 1", len(got))
	}
}

// TestPostTaskQuestions_ToDoesNotBadgeTheHuman is the point of --to on ask
// (spec v2 §2): a thread the orchestrator addressed to cto must not show the
// human an "awaiting you" badge. your_turn is the field the clients bind that
// badge to.
func TestPostTaskQuestions_ToDoesNotBadgeTheHuman(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Approve the schema?", "to": []string{"cto"}})
	resp.Body.Close()

	asHuman := getQuestions(t, srv, taskID, "")
	if len(asHuman) != 1 {
		t.Fatalf("got %d threads, want 1", len(asHuman))
	}
	if asHuman[0].YourTurn {
		t.Error("your_turn = true for the human on a thread addressed to cto, want false")
	}
	if contains(asHuman[0].WaitingOn, "human") {
		t.Errorf("waiting_on = %v, must not include the human", asHuman[0].WaitingOn)
	}
	// The human still SEES the thread — your_turn drives a badge, never hiding.
	if asHuman[0].Body != "Approve the schema?" {
		t.Errorf("the human must still see the thread, got %+v", asHuman[0])
	}
	// And whose_turn no longer collapses to "user" for it.
	if asHuman[0].WhoseTurn != "orchestrator" {
		t.Errorf("whose_turn = %q, want orchestrator", asHuman[0].WhoseTurn)
	}
}

// TestTaskQuestion_HumanIsCanonicalOnTheWire pins the post-#736 contract: the
// human is spelled store.ParticipantHuman everywhere the API names a party —
// asked_by on a human-opened thread and messages[].author on a human reply.
// The legacy empty spelling is gone.
func TestTaskQuestion_HumanIsCanonicalOnTheWire(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions",
		map[string]any{"body": "Status?"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()
	if q.AskedBy != store.ParticipantHuman {
		t.Errorf("asked_by = %q, want %q", q.AskedBy, store.ParticipantHuman)
	}

	replyResp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply",
		map[string]any{"body": "and one more thing"})
	after := decodeQuestion(t, replyResp)
	replyResp.Body.Close()
	if after.AskedBy != store.ParticipantHuman {
		t.Errorf("asked_by after reply = %q, want %q", after.AskedBy, store.ParticipantHuman)
	}
	if len(after.Messages) != 1 {
		t.Fatalf("messages = %+v, want one", after.Messages)
	}
	if after.Messages[0].Author != store.ParticipantHuman {
		t.Errorf("messages[0].author = %q, want %q", after.Messages[0].Author, store.ParticipantHuman)
	}

	// The listing endpoint renders the same thread the same way.
	listed := getQuestions(t, srv, taskID, "")
	if len(listed) != 1 {
		t.Fatalf("got %d questions, want 1", len(listed))
	}
	if listed[0].AskedBy != store.ParticipantHuman {
		t.Errorf("listed asked_by = %q, want %q", listed[0].AskedBy, store.ParticipantHuman)
	}
	if listed[0].Messages[0].Author != store.ParticipantHuman {
		t.Errorf("listed messages[0].author = %q, want %q", listed[0].Messages[0].Author, store.ParticipantHuman)
	}
}

// TestQuestionReply_AckDoesNotReopen is the point of subtask #1181: a plain
// "принял, работаю" from the orchestrator is not a dispute. Without
// `dispute: true` the reply lands in the history and nothing else moves —
// the thread stays resolved, attention stays empty and no reopen event is
// published, so the answer does not come back to the human's badge.
func TestQuestionReply_AckDoesNotReopen(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	askResp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Which DB?"})
	q := decodeQuestion(t, askResp)
	askResp.Body.Close()

	ansResp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"body": "sqlite"})
	ansResp.Body.Close()

	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "orch-1",
		map[string]any{"body": "принял, делаю"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (an ack is recorded, not rejected)", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.Status != "resolved" {
		t.Errorf("status after ack = %q, want resolved", got.Status)
	}
	// The stored attention set must not move: a resolved thread waits on
	// nobody, and an ack must not pull the acking orchestrator (or anyone
	// else) back in.
	if len(got.Attention) != 0 || len(got.WaitingOn) != 0 || got.YourTurn {
		t.Errorf("attention after ack = %#v/%#v, your_turn = %v; want untouched and empty",
			got.Attention, got.WaitingOn, got.YourTurn)
	}
	if len(got.Messages) == 0 || got.Messages[len(got.Messages)-1].Body != "принял, делаю" {
		t.Errorf("ack must be recorded in the thread: %+v", got.Messages)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case e := <-ch:
			if e.Type == "task.question_reopened" {
				t.Fatalf("an ack must not publish task.question_reopened")
			}
		case <-time.After(50 * time.Millisecond):
		}
	}

	// And the dashboard badge stays down: the question is still resolved.
	taskCardResp, err := http.Get(srv.URL + "/v1/tasks/" + itoa(taskID))
	if err != nil {
		t.Fatalf("GET task: %v", err)
	}
	defer taskCardResp.Body.Close()
	var card taskDetailResponse
	if err := json.NewDecoder(taskCardResp.Body).Decode(&card); err != nil {
		t.Fatalf("decode task card: %v", err)
	}
	if card.OpenQuestions != 0 {
		t.Errorf("OpenQuestions = %d, want 0 after an ack", card.OpenQuestions)
	}
}

// TestReplyReopens is the whole rule in one table: who may reopen a resolved
// thread, with what, and who gets a conflict instead (subtask #1181).
func TestReplyReopens(t *testing.T) {
	agent := &store.Session{ID: "orch-1"}
	tests := []struct {
		name         string
		caller       *store.Session
		status       string
		threadType   string
		dispute      bool
		wantReopen   bool
		wantConflict bool
	}{
		{"открытый тред", agent, "open", store.QuestionTypeDecision, false, false, false},
		{"ack агента", agent, "resolved", store.QuestionTypeDecision, false, false, false},
		{"оспаривание агента", agent, "resolved", store.QuestionTypeDecision, true, true, false},
		{"человек в resolved", nil, "resolved", store.QuestionTypeDecision, false, false, true},
		{"человек в fyi без флага", nil, "resolved", store.QuestionTypeFYI, false, true, false},
		{"агент в fyi без флага", agent, "resolved", store.QuestionTypeFYI, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reopen, conflict := replyReopens(tt.caller, tt.status, tt.threadType, tt.dispute)
			if reopen != tt.wantReopen || conflict != tt.wantConflict {
				t.Errorf("replyReopens = (%v, %v), want (%v, %v)",
					reopen, conflict, tt.wantReopen, tt.wantConflict)
			}
		})
	}
}
