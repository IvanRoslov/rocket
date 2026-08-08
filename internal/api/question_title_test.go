package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// TestPostTaskQuestions_TitleFromRequest: a title passed in wins over anything
// the server could derive.
func TestPostTaskQuestions_TitleFromRequest(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "## Какой CIDR выставить\n\nдетали", "title": "Свой заголовок"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeQuestion(t, resp)
	if q.Title != "Свой заголовок" {
		t.Fatalf("title = %q, want %q", q.Title, "Свой заголовок")
	}
	if q.Body != "## Какой CIDR выставить\n\nдетали" {
		t.Fatalf("body = %q, want it untouched", q.Body)
	}
}

// TestPostTaskQuestions_TitleDerived: no title in the request, so the server
// derives one from the body.
func TestPostTaskQuestions_TitleDerived(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "## Какой CIDR выставить\n\nдетали"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeQuestion(t, resp)
	if q.Title != "Какой CIDR выставить" {
		t.Fatalf("title = %q, want it derived from the body", q.Title)
	}
}

// TestPostTaskQuestions_ContextGoesIntoBody: the deprecated context is appended
// to the body with the canonical separator and comes back empty.
func TestPostTaskQuestions_ContextGoesIntoBody(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Что выставить?", "context": "детали контекста"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	q := decodeQuestion(t, resp)
	want := "Что выставить?" + store.ContextSeparator + "детали контекста"
	if q.Body != want {
		t.Fatalf("body = %q, want %q", q.Body, want)
	}
	if q.Context != "" {
		t.Fatalf("context = %q, want empty", q.Context)
	}
	if q.Title != "Что выставить?" {
		t.Fatalf("title = %q, want it derived from the body proper", q.Title)
	}
}

// TestPostAgentQuestions_TitleAndContext: role threads behave exactly the same.
func TestPostAgentQuestions_TitleAndContext(t *testing.T) {
	d := agentQuestionsTestDeps(t)
	srv := setupRoleForQuestions(t, d)

	resp := postJSON(t, srv.URL+"/v1/agents/sre/questions",
		map[string]any{"body": "Что выставить?", "context": "детали"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var q agentQuestionResponse
	if err := json.NewDecoder(resp.Body).Decode(&q); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := "Что выставить?" + store.ContextSeparator + "детали"
	if q.Body != want {
		t.Fatalf("body = %q, want %q", q.Body, want)
	}
	if q.Context != "" {
		t.Fatalf("context = %q, want empty", q.Context)
	}
	if q.Title != "Что выставить?" {
		t.Fatalf("title = %q, want it derived", q.Title)
	}
}

// TestThreadInbox_CarriesTitle: the inbox listing shows the title, so a client
// can render a thread line without parsing the body.
func TestThreadInbox_CarriesTitle(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "## Какой CIDR выставить\n\nдетали"})
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
	if len(body.Threads) != 1 {
		t.Fatalf("threads = %d, want 1", len(body.Threads))
	}
	if body.Threads[0].Title != "Какой CIDR выставить" {
		t.Fatalf("title = %q, want %q", body.Threads[0].Title, "Какой CIDR выставить")
	}
}
