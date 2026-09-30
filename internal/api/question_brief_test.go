package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

const testBrief = "**Проблема:** staging и prod в одной сети.\n\n**Варианты:** развести или оставить.\n\n**Рекомендация:** развести."

// TestPostTaskQuestions_BriefRoundTrip: the brief is stored as given and comes
// back apart from the body, so a client can show it first.
func TestPostTaskQuestions_BriefRoundTrip(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"title": "Какой CIDR?", "brief": testBrief, "body": "детали"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeQuestion(t, resp)
	if q.Brief != testBrief {
		t.Fatalf("brief = %q, want %q", q.Brief, testBrief)
	}
	if q.Body != "детали" {
		t.Fatalf("body = %q, want it untouched", q.Body)
	}

	list := getQuestions(t, srv, taskID, "")
	if len(list) != 1 || list[0].Brief != testBrief {
		t.Fatalf("GET questions = %+v, want one thread carrying the brief", list)
	}
}

// TestPostAgentQuestions_BriefRoundTrip: role threads carry the brief too.
func TestPostAgentQuestions_BriefRoundTrip(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)

	resp := postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"brief": testBrief, "body": "детали"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var q agentQuestionResponse
	if err := json.NewDecoder(resp.Body).Decode(&q); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if q.Brief != testBrief {
		t.Fatalf("brief = %q, want %q", q.Brief, testBrief)
	}
}

// TestThreadInbox_CarriesBrief: the inbox listing carries the brief, so the
// queue can show the plain-language version without opening each thread.
func TestThreadInbox_CarriesBrief(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"brief": testBrief, "body": "детали"})
	resp.Body.Close()

	getResp, err := http.Get(srv.URL + "/v1/threads")
	if err != nil {
		t.Fatalf("GET /v1/threads: %v", err)
	}
	defer getResp.Body.Close()
	var body struct {
		Threads []threadInboxEntry `json:"threads"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode threads: %v", err)
	}
	if len(body.Threads) != 1 || body.Threads[0].Brief != testBrief {
		t.Fatalf("threads = %+v, want one carrying the brief", body.Threads)
	}
}
