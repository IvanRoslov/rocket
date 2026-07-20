package api

import (
	"encoding/json"
	"net/http"
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

func TestPostTaskQuestions_HumanForbidden(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSON(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", map[string]any{"body": "Q"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", eb.Error.Code)
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
	if len(afterUserReply.Messages) != 1 || afterUserReply.Messages[0].Body != "consider X" || afterUserReply.Messages[0].Author != "" {
		t.Fatalf("messages after user reply = %+v", afterUserReply.Messages)
	}

	wantUserReplyPrefix := "[task #" + itoa(taskID) + " Q1 reply] consider X"
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

	wantAnswerPrefix := "[task #" + itoa(taskID) + " Q1 answer] final: use X"
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
		map[string]any{"body": "Evidence says sqlite cannot work here: no concurrent writers."})
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
