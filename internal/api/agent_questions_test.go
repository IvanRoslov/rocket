package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

	msgs, err := d.Store.ListInboxMessages("sre", store.InboxUnread, 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("inbox = %+v, want one message", msgs)
	}
	for _, want := range []string{"[role sre Q1 question from human]", "почему упал деплой?"} {
		if !strings.Contains(msgs[0].Body, want) {
			t.Errorf("body = %q, missing %q", msgs[0].Body, want)
		}
	}
}

func TestPostAgentQuestion_FromRoleInstanceAwaitsUser(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "sre", "agent", "platform")

	resp := postJSONWithHeader(t, srv.URL+"/v1/agents/sre/questions", "sre",
		map[string]any{"body": "нужно ваше решение", "context": "детали"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeAgentQuestion(t, resp)
	if q.AskedBy != "sre" || q.WhoseTurn != "user" || q.Context != "детали" {
		t.Fatalf("question = %+v", q)
	}

	msgs, err := d.Store.ListInboxMessages("sre", "", 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("an agent-opened question must not be delivered back to it: %+v", msgs)
	}
	sent, err := d.Store.ListMessages("sre", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(sent) != 0 {
		t.Fatalf("an agent-opened question must not be delivered back to it: %+v", sent)
	}
}

// TestPostAgentQuestion_ForeignOrchestratorForbidden: an ephemeral session
// that is not the role itself still may not open a role thread.
func TestPostAgentQuestion_ForeignOrchestratorForbidden(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "platform-orch", "orchestrator", "platform")

	resp := postJSONWithHeader(t, srv.URL+"/v1/agents/sre/questions", "platform-orch",
		map[string]any{"body": "чужой"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TestPostAgentQuestion_AnotherAgentMayAsk is the rule spec v1 §3 changed: a
// persistent agent may open a thread on ANY role, not only its own. This is
// the point of the participant model — agents reach each other in threads
// instead of writing into each other's terminals with rocket send.
func TestPostAgentQuestion_AnotherAgentMayAsk(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "triage", "agent", "platform")

	resp := postJSONWithHeader(t, srv.URL+"/v1/agents/sre/questions", "triage",
		map[string]any{"body": "посмотри деплой"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeAgentQuestion(t, resp)
	if !contains(q.Participants, "triage") {
		t.Errorf("participants = %v, want the asking agent included", q.Participants)
	}
	if !reflect.DeepEqual(q.WaitingOn, []string{"human", "sre"}) {
		t.Errorf("waiting_on = %v, want [human sre]", q.WaitingOn)
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
	addTestSession(t, d, "sre", "agent", "platform")

	q := decodeAgentQuestion(t, postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "вопрос"}))
	qid := itoa(q.ID)

	// The role replies in-thread: no new inbox event, turn flips to the human.
	resp := postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+qid+"/reply", "sre",
		map[string]any{"body": "разбираюсь"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("role reply status = %d, want 201", resp.StatusCode)
	}
	q = decodeAgentQuestion(t, resp)
	if len(q.Messages) != 1 || q.WhoseTurn != "user" {
		t.Fatalf("after role reply: %+v", q)
	}
	if sent, _ := d.Store.ListMessages("sre", 0); len(sent) != 1 {
		t.Fatalf("an agent reply must not be delivered back to it: %+v", sent)
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
	sent, _ := d.Store.ListMessages("sre", 0)
	if len(sent) != 2 || !strings.Contains(sent[1].Body, "[role sre Q1 reply from human]") {
		t.Fatalf("a human reply must reach the live agent: %+v", sent)
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
	if sent, _ := d.Store.ListMessages("sre", 0); len(sent) != 3 {
		t.Fatalf("an answer must reach the live agent: %+v", sent)
	}

	// A second human answer is a conflict.
	resp = postJSON(t, srv.URL+"/v1/agent-questions/"+qid+"/answer", map[string]any{"dismiss": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("double answer status = %d, want 409", resp.StatusCode)
	}

	// The role may dispute a resolved thread: its reply reopens it.
	resp = postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+qid+"/reply", "sre",
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

// TestAgentQuestion_AnswerRejectsOrchestrators: answer is the human's and a
// persistent agent's alone. An orchestrator is told to use reply instead —
// spec v1 §3 reversed the old "any agent gets 403" rule for kind=agent
// callers, but ephemeral sessions are still refused.
func TestAgentQuestion_AnswerRejectsOrchestrators(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	qid, err := d.Store.AddAgentQuestion(store.AgentQuestion{RoleID: "sre", Body: "q"})
	if err != nil {
		t.Fatalf("AddAgentQuestion: %v", err)
	}

	resp := postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+itoa(qid)+"/answer", "orch-1",
		map[string]any{"body": "не моё дело"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	var e errBody
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if !strings.Contains(e.Error.Message, "reply") {
		t.Errorf("403 message = %q, want it to point at reply", e.Error.Message)
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

func TestAgentQuestion_HumanEntriesReachTheLiveSession(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "sre", "agent", "platform")

	q := decodeAgentQuestion(t, postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "почему упал деплой?"}))
	postJSON(t, srv.URL+"/v1/agent-questions/"+itoa(q.ID)+"/answer",
		map[string]any{"body": "перезапусти воркер"}).Body.Close()

	sent, err := d.Store.ListMessages("sre", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("messages = %+v, want the question and the answer", sent)
	}
	if !strings.Contains(sent[0].Body, "[role sre Q1 question from human]") ||
		!strings.Contains(sent[1].Body, "[role sre Q1 answer from human]") {
		t.Errorf("bodies = %q / %q", sent[0].Body, sent[1].Body)
	}

	// A live agent is delivered to, not inboxed.
	inbox, err := d.Store.ListInboxMessages("sre", "", 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("a live agent must not be inboxed: %+v", inbox)
	}
}

func TestAgentQuestion_NoLiveSessionInboxesTheEntry(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)
	addTestSession(t, d, "sre", "agent", "platform")
	if err := d.Store.UpdateSessionState("sre", "killed"); err != nil {
		t.Fatalf("UpdateSessionState: %v", err)
	}

	q := decodeAgentQuestion(t, postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "вопрос в пустоту"}))
	if q.ID == 0 {
		t.Fatal("question was not recorded")
	}

	msgs, err := d.Store.ListMessages("sre", 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("a terminal instance must not be delivered to: %+v", msgs)
	}
	if inbox, _ := d.Store.ListInboxMessages("sre", store.InboxUnread, 0); len(inbox) != 1 {
		t.Fatalf("a dead agent must find the entry in its inbox: %+v", inbox)
	}
}

// TestAgentQuestion_ParticipantFields: a role thread carries the same additive
// fields as a task thread, with the role's own whose_turn vocabulary.
func TestAgentQuestion_ParticipantFields(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")

	q := decodeAgentQuestion(t, postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "Status?"}))

	if !reflect.DeepEqual(q.Participants, []string{"human", "sre"}) {
		t.Errorf("participants = %v, want [human sre]", q.Participants)
	}
	if !reflect.DeepEqual(q.WaitingOn, []string{"sre"}) {
		t.Errorf("waiting_on = %v, want [sre]", q.WaitingOn)
	}
	if q.WhoseTurn != "role" {
		t.Errorf("whose_turn = %q, want role (compat)", q.WhoseTurn)
	}
	if q.YourTurn {
		t.Error("your_turn = true for the human asker, want false")
	}
}

// TestAgentQuestion_OrchestratorMayReplyWhenAddressed: an orchestrator named in
// "to" joins a role thread and may reply, but still may not answer it.
func TestAgentQuestion_OrchestratorMayReplyWhenAddressed(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")
	addTestProject(t, d, "proj1")
	addTestSession(t, d, "orch-1", "orchestrator", "proj1")

	q := decodeAgentQuestion(t, postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "Status?", "to": []string{"orch-1"}}))

	if !contains(q.Participants, "orch-1") {
		t.Fatalf("participants = %v, want orch-1 joined via to", q.Participants)
	}

	rep := postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+itoa(q.ID)+"/reply", "orch-1",
		map[string]any{"body": "green"})
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusCreated {
		t.Fatalf("addressed orchestrator reply = %d, want 201", rep.StatusCode)
	}

	ans := postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+itoa(q.ID)+"/answer", "orch-1",
		map[string]any{"body": "done"})
	defer ans.Body.Close()
	if ans.StatusCode != http.StatusForbidden {
		t.Errorf("orchestrator answer = %d, want 403", ans.StatusCode)
	}
}

// TestAgentQuestion_RoleMayAnswerItsOwnThread: a persistent agent resolves a
// thread the human opened to it.
func TestAgentQuestion_RoleMayAnswerItsOwnThread(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := newTestServer(t, d)
	createTestAgent(t, srv, "sre")
	addLiveAgentSession(t, d, "sre")

	q := decodeAgentQuestion(t, postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "Status?"}))

	ans := postJSONWithHeader(t, srv.URL+"/v1/agent-questions/"+itoa(q.ID)+"/answer", "sre",
		map[string]any{"body": "all green"})
	got := decodeAgentQuestion(t, ans)
	if got.Status != "resolved" || got.Resolution != "answered" {
		t.Errorf("status/resolution = %q/%q, want resolved/answered", got.Status, got.Resolution)
	}
	if len(got.WaitingOn) != 0 {
		t.Errorf("waiting_on = %v, want empty", got.WaitingOn)
	}
}
