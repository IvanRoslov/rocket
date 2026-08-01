package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// agentQuestionsTestDeps reuses messagesTestDeps (real store/bus plus a real
// queue wired to a fake runtime) so delivery into a live role instance can be
// observed end to end via the messages table, and registers the project every
// role belongs to.
func agentQuestionsTestDeps(t *testing.T) Deps {
	t.Helper()
	d := messagesTestDeps(t)
	addTestProject(t, d, "platform")
	return d
}

func decodeAgentQuestion(t *testing.T, resp *http.Response) agentQuestionResponse {
	t.Helper()
	defer resp.Body.Close()
	var q agentQuestionResponse
	if err := json.NewDecoder(resp.Body).Decode(&q); err != nil {
		t.Fatalf("decode agent question: %v", err)
	}
	return q
}

func decodeAgentQuestions(t *testing.T, resp *http.Response) []agentQuestionResponse {
	t.Helper()
	defer resp.Body.Close()
	var body struct {
		Questions []agentQuestionResponse `json:"questions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode agent questions: %v", err)
	}
	return body.Questions
}

// setupRoleForQuestions creates role "sre" and returns the test server.
func setupRoleForQuestions(t *testing.T, d Deps) *httptest.Server {
	t.Helper()
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")
	return srv
}

// --- whoseTurnAgent ------------------------------------------------------

func TestWhoseTurnAgent_HumanOpenedNoMessages(t *testing.T) {
	q := store.AgentQuestion{Status: "open", AskedBy: ""}
	if got := whoseTurnAgent(q, nil); got != "role" {
		t.Errorf("whoseTurnAgent = %q, want role", got)
	}
}

func TestWhoseTurnAgent_RoleOpenedNoMessages(t *testing.T) {
	q := store.AgentQuestion{Status: "open", AskedBy: "sre-run-1"}
	if got := whoseTurnAgent(q, nil); got != "user" {
		t.Errorf("whoseTurnAgent = %q, want user", got)
	}
}

func TestWhoseTurnAgent_ResolvedHasNoTurn(t *testing.T) {
	q := store.AgentQuestion{Status: "resolved", AskedBy: "sre-run-1"}
	if got := whoseTurnAgent(q, nil); got != "" {
		t.Errorf("whoseTurnAgent = %q, want empty", got)
	}
}

// --- POST /v1/agents/{id}/questions --------------------------------------

func TestPostAgentQuestion_FromHumanEnqueuesInboxEvent(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)

	resp := postJSON(t, srv.URL+"/v1/agents/sre/questions", map[string]any{"body": "почему упал деплой?"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeAgentQuestion(t, resp)
	if q.RoleID != "sre" || q.AskedBy != "" || q.Ordinal != 1 || q.WhoseTurn != "role" {
		t.Fatalf("question = %+v", q)
	}

	events, err := d.Store.QueuedInboxEvents("sre")
	if err != nil {
		t.Fatalf("QueuedInboxEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "question" {
		t.Fatalf("inbox events = %+v", events)
	}
	for _, want := range []string{`"entry":"question"`, `"ordinal":1`, "почему упал деплой?"} {
		if !strings.Contains(events[0].Payload, want) {
			t.Errorf("payload = %s, missing %q", events[0].Payload, want)
		}
	}
}

func TestPostAgentQuestion_FromRoleInstanceAwaitsUser(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "sre-run-1", "agent", "platform")

	resp := postJSONWithHeader(t, srv.URL+"/v1/agents/sre/questions", "sre-run-1",
		map[string]any{"body": "нужно ваше решение", "context": "детали"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeAgentQuestion(t, resp)
	if q.AskedBy != "sre-run-1" || q.WhoseTurn != "user" || q.Context != "детали" {
		t.Fatalf("question = %+v", q)
	}

	events, err := d.Store.QueuedInboxEvents("sre")
	if err != nil {
		t.Fatalf("QueuedInboxEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("role-opened question must not wake the role: %+v", events)
	}
}

func TestPostAgentQuestion_ForeignSessionForbidden(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "triage-run-1", "agent", "platform")
	addTestSession(t, d, "platform-orch", "orchestrator", "platform")

	for _, sessionID := range []string{"triage-run-1", "platform-orch"} {
		resp := postJSONWithHeader(t, srv.URL+"/v1/agents/sre/questions", sessionID,
			map[string]any{"body": "чужой"})
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s status = %d, want 403", sessionID, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestPostAgentQuestion_EmptyBody(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)

	resp := postJSON(t, srv.URL+"/v1/agents/sre/questions", map[string]any{"body": ""})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if eb := decodeErr(t, resp); eb.Error.Code != "empty_body" {
		t.Errorf("code = %q, want empty_body", eb.Error.Code)
	}
}

func TestPostAgentQuestion_UnknownRole(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/agents/ghost/questions", map[string]any{"body": "?"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// --- GET /v1/agents/{id}/questions ---------------------------------------

func TestGetAgentQuestions_FiltersOpen(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)

	first, err := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", Body: "q1"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	if _, err := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", Body: "q2"}); err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	if err := d.Store.ResolveAgentQuestion(first, "answered"); err != nil {
		t.Fatalf("ResolveAgentQuestion: %v", err)
	}

	all := decodeAgentQuestions(t, getJSON(t, srv.URL+"/v1/agents/sre/questions"))
	if len(all) != 2 {
		t.Fatalf("all = %+v", all)
	}

	open := decodeAgentQuestions(t, getJSON(t, srv.URL+"/v1/agents/sre/questions?status=open"))
	if len(open) != 1 || open[0].Body != "q2" || open[0].Ordinal != 2 {
		t.Fatalf("open = %+v", open)
	}
}

// --- reply / answer ------------------------------------------------------

func TestAgentQuestion_ReplyAnswerAndReopen(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "sre-run-1", "agent", "platform")

	q := decodeAgentQuestion(t, postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "вопрос"}))
	qid := itoa(q.ID)

	// The role replies in-thread: no new inbox event, turn flips to the human.
	resp := postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+qid+"/reply", "sre-run-1",
		map[string]any{"body": "разбираюсь"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("role reply status = %d, want 201", resp.StatusCode)
	}
	q = decodeAgentQuestion(t, resp)
	if len(q.Messages) != 1 || q.WhoseTurn != "user" {
		t.Fatalf("after role reply: %+v", q)
	}
	if events, _ := d.Store.QueuedInboxEvents("sre"); len(events) != 1 {
		t.Fatalf("role reply must not enqueue: %+v", events)
	}

	// The human replies: a second inbox event lands.
	resp = postJSON(t, srv.URL+"/v1/agent-questions/"+qid+"/reply", map[string]any{"body": "жду"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("human reply status = %d, want 201", resp.StatusCode)
	}
	q = decodeAgentQuestion(t, resp)
	if q.WhoseTurn != "role" {
		t.Fatalf("after human reply: %+v", q)
	}
	events, _ := d.Store.QueuedInboxEvents("sre")
	if len(events) != 2 || !strings.Contains(events[1].Payload, `"entry":"reply"`) {
		t.Fatalf("human reply must enqueue a reply event: %+v", events)
	}

	// The human closes the thread.
	resp = postJSON(t, srv.URL+"/v1/agent-questions/"+qid+"/answer", map[string]any{"body": "делай вариант Б"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer status = %d, want 200", resp.StatusCode)
	}
	q = decodeAgentQuestion(t, resp)
	if q.Status != "resolved" || q.Resolution != "answered" || q.WhoseTurn != "" {
		t.Fatalf("after answer: %+v", q)
	}
	if events, _ := d.Store.QueuedInboxEvents("sre"); len(events) != 3 {
		t.Fatalf("answer must enqueue: %+v", events)
	}

	// A second human answer is a conflict.
	resp = postJSON(t, srv.URL+"/v1/agent-questions/"+qid+"/answer", map[string]any{"dismiss": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("double answer status = %d, want 409", resp.StatusCode)
	}

	// The role may dispute a resolved thread: its reply reopens it.
	resp = postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+qid+"/reply", "sre-run-1",
		map[string]any{"body": "вариант Б не сработает"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("reopen status = %d, want 201", resp.StatusCode)
	}
	q = decodeAgentQuestion(t, resp)
	if q.Status != "open" || q.Resolution != "" {
		t.Fatalf("after reopen: %+v", q)
	}
}

func TestAgentQuestion_HumanReplyToResolvedIsConflict(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)

	qid, err := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", Body: "q"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}
	if err := d.Store.ResolveAgentQuestion(qid, "answered"); err != nil {
		t.Fatalf("ResolveAgentQuestion: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/agent-questions/"+itoa(qid)+"/reply", map[string]any{"body": "ещё"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

func TestAgentQuestion_AnswerRejectsAgents(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "sre-run-1", "agent", "platform")

	qid, err := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", Body: "q"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+itoa(qid)+"/answer", "sre-run-1",
		map[string]any{"body": "сам себе"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestAgentQuestion_DismissResolvesWithoutMessage(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)

	q := decodeAgentQuestion(t, postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "вопрос"}))

	resp := postJSON(t, srv.URL+"/v1/agent-questions/"+itoa(q.ID)+"/answer", map[string]any{"dismiss": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	q = decodeAgentQuestion(t, resp)
	if q.Status != "resolved" || q.Resolution != "dismissed" || len(q.Messages) != 0 {
		t.Fatalf("after dismiss: %+v", q)
	}
}

func TestAgentQuestion_NotFound(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := newTestServer(t, d)

	for _, path := range []string{"/v1/agent-questions/999/reply", "/v1/agent-questions/nope/reply"} {
		resp := postJSON(t, srv.URL+path, map[string]any{"body": "x"})
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// --- delivery into a live instance ---------------------------------------

func TestAgentQuestion_DeliversToLiveInstance(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "sre-run-1", "agent", "platform")

	q := decodeAgentQuestion(t, postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "почему упал деплой?"}))
	postJSON(t, srv.URL+"/v1/agent-questions/"+itoa(q.ID)+"/answer",
		map[string]any{"body": "перезапусти воркер"}).Body.Close()

	msgs, err := d.Store.ListMessages("sre-run-1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %+v, want 2", msgs)
	}
	if !strings.HasPrefix(msgs[0].Body, "[role sre Q1 question] ") {
		t.Errorf("question message = %q", msgs[0].Body)
	}
	if !strings.HasPrefix(msgs[1].Body, "[role sre Q1 answer] ") {
		t.Errorf("answer message = %q", msgs[1].Body)
	}
}

func TestAgentQuestion_NoLiveInstanceStillRecords(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "sre-run-1", "agent", "platform")
	if err := d.Store.UpdateSessionState("sre-run-1", "killed"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	q := decodeAgentQuestion(t, postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "вопрос в пустоту"}))
	if q.ID == 0 {
		t.Fatal("question was not recorded")
	}

	msgs, err := d.Store.ListMessages("sre-run-1", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("a terminal instance must not be delivered to: %+v", msgs)
	}
	if events, _ := d.Store.QueuedInboxEvents("sre"); len(events) != 1 {
		t.Fatalf("inbox event must still be queued: %+v", events)
	}
}
