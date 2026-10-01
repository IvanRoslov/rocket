package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

// askBrainstorm opens a brainstorm thread from orch-1 with options A/B/C,
// recommending B, and returns it.
func askBrainstorm(t *testing.T, srv *httptest.Server, taskID int64) questionResponse {
	t.Helper()
	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{
		"body": "Какую схему?", "type": "brainstorm",
		"options": []string{"A", "B", "C"}, "recommend": 2,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ask brainstorm = %d, want 201", resp.StatusCode)
	}
	return decodeQuestion(t, resp)
}

func intPtr(n int) *int { return &n }

func eqIntPtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestBrainstormAsk_RecordsRecommendation(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	q := askBrainstorm(t, srv, taskID)
	if q.Type != "brainstorm" || !eqIntPtr(q.RecommendedOption, intPtr(2)) {
		t.Fatalf("type/recommended = %q/%v, want brainstorm/2", q.Type, q.RecommendedOption)
	}
	if q.ChosenOption != nil || q.Outcome != "" || q.AnswerSource != "" {
		t.Fatalf("fresh thread carries answer fields: %+v", q)
	}
	// It waits on the human like any decision thread.
	if len(q.WaitingOn) != 1 || q.WaitingOn[0] != "human" {
		t.Fatalf("waiting_on = %v, want [human]", q.WaitingOn)
	}

	listed := getQuestions(t, srv, taskID, "")
	if len(listed) != 1 || !eqIntPtr(listed[0].RecommendedOption, intPtr(2)) {
		t.Fatalf("listing = %+v, want the recommendation", listed)
	}
}

func TestBrainstormAsk_RecommendValidation(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
	}{
		{"missing recommend", map[string]any{"body": "Q", "type": "brainstorm", "options": []string{"A", "B"}}},
		{"recommend zero", map[string]any{"body": "Q", "type": "brainstorm", "options": []string{"A", "B"}, "recommend": 0}},
		{"recommend too big", map[string]any{"body": "Q", "type": "brainstorm", "options": []string{"A", "B"}, "recommend": 3}},
		{"recommend negative", map[string]any{"body": "Q", "type": "brainstorm", "options": []string{"A", "B"}, "recommend": -1}},
		{"recommend without options", map[string]any{"body": "Q", "type": "brainstorm", "recommend": 1}},
		{"recommend on decision", map[string]any{"body": "Q", "options": []string{"A", "B"}, "recommend": 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := questionsTestDeps(t)
			srv := newTestServer(t, d)
			taskID := setupQuestionTask(t, d)
			resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", c.payload)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			if eb := decodeErr(t, resp); eb.Error.Code != "bad_request" {
				t.Errorf("code = %q, want bad_request", eb.Error.Code)
			}
		})
	}
}

// A brainstorm without options is legal: the human can only answer in their
// own words.
func TestBrainstormAsk_NoOptionsNoRecommend(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Что болит?", "type": "brainstorm"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	if q := decodeQuestion(t, resp); q.RecommendedOption != nil {
		t.Fatalf("recommended = %v, want null", *q.RecommendedOption)
	}
}

func lastMessage(t *testing.T, q questionResponse) questionMessageResponse {
	t.Helper()
	if len(q.Messages) == 0 {
		t.Fatalf("thread has no messages")
	}
	return q.Messages[len(q.Messages)-1]
}

func TestBrainstormAnswer_Outcomes(t *testing.T) {
	cases := []struct {
		name        string
		payload     map[string]any
		wantChosen  *int
		wantComment string
		wantOutcome string
		wantBody    string
	}{
		{"recommended with comment", map[string]any{"choose": 2, "body": "но аккуратно"},
			intPtr(2), "но аккуратно", store.OutcomeAccepted, "B\n\nно аккуратно"},
		{"recommended bare", map[string]any{"choose": 2},
			intPtr(2), "", store.OutcomeAccepted, "B"},
		{"other option", map[string]any{"choose": 3, "body": "C надёжнее"},
			intPtr(3), "C надёжнее", store.OutcomeCorrected, "C\n\nC надёжнее"},
		{"own text", map[string]any{"body": "всё не то"},
			nil, "всё не то", store.OutcomeWrongTurn, "всё не то"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := questionsTestDeps(t)
			srv := newTestServer(t, d)
			taskID := setupQuestionTask(t, d)
			q := askBrainstorm(t, srv, taskID)

			resp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", c.payload)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("answer = %d, want 200", resp.StatusCode)
			}
			got := decodeQuestion(t, resp)
			if got.Status != "resolved" || got.Resolution != "answered" {
				t.Fatalf("status/resolution = %q/%q", got.Status, got.Resolution)
			}
			if !eqIntPtr(got.ChosenOption, c.wantChosen) {
				t.Errorf("chosen = %v, want %v", got.ChosenOption, c.wantChosen)
			}
			if got.AnswerComment != c.wantComment || got.Outcome != c.wantOutcome ||
				got.AnswerSource != store.AnswerSourceUI || got.OutcomeOverridden {
				t.Errorf("comment/outcome/source/overridden = %q/%q/%q/%v",
					got.AnswerComment, got.Outcome, got.AnswerSource, got.OutcomeOverridden)
			}
			if got.AnsweredBy != "human" {
				t.Errorf("answered_by = %q, want human", got.AnsweredBy)
			}
			if m := lastMessage(t, got); m.Kind != "answer" || m.Body != c.wantBody || m.Author != "human" {
				t.Errorf("answer message = %+v, want body %q by human", m, c.wantBody)
			}

			// The orchestrator is told the same text.
			msgs, err := d.Store.ListMessages("orch-1", 10)
			if err != nil {
				t.Fatalf("ListMessages: %v", err)
			}
			if len(msgs) == 0 || !strings.HasSuffix(msgs[len(msgs)-1].Body, c.wantBody) {
				t.Errorf("delivered = %+v, want suffix %q", msgs, c.wantBody)
			}
		})
	}
}

func TestBrainstormAnswer_EmptyRejected(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	q := askBrainstorm(t, srv, taskID)

	resp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"body": ""})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// A persistent agent may close a brainstorm thread like any other; its answer
// is attributed to it through answered_by, so the metric can leave it out.
func TestBrainstormAnswer_PersistentAgentIsAttributed(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)
	addLiveAgentSession(t, d, "cto")
	q := askBrainstorm(t, srv, taskID)

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", "cto",
		map[string]any{"choose": 2, "join": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer = %d, want 200", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.AnsweredBy != "cto" || got.Outcome != store.OutcomeAccepted || got.AnswerSource != store.AnswerSourceUI {
		t.Fatalf("answered_by/outcome/source = %q/%q/%q", got.AnsweredBy, got.Outcome, got.AnswerSource)
	}
}

// Decision threads keep their old answer behaviour and carry no brainstorm
// fields — except that a comment given with a choice is no longer dropped.
func TestDecisionAnswer_NoBrainstormFields(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Q", "options": []string{"A", "B"}})
	q := decodeQuestion(t, resp)
	resp.Body.Close()

	ans := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"choose": 1, "body": "почему"})
	defer ans.Body.Close()
	if ans.StatusCode != http.StatusOK {
		t.Fatalf("answer = %d, want 200", ans.StatusCode)
	}
	got := decodeQuestion(t, ans)
	if got.Type != "decision" || got.RecommendedOption != nil || got.ChosenOption != nil ||
		got.Outcome != "" || got.AnswerSource != "" || got.AnswerComment != "" {
		t.Fatalf("decision thread carries brainstorm fields: %+v", got)
	}
	if m := lastMessage(t, got); m.Body != "A\n\nпочему" {
		t.Fatalf("answer body = %q, want option + comment", m.Body)
	}
	if got.AnsweredBy != "human" {
		t.Fatalf("answered_by = %q, want human", got.AnsweredBy)
	}
}

func TestAgentAsk_BrainstormRejected(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	setupQuestionAgent(t, d)

	resp := postJSON(t, srv.URL+"/v1/agents/cto/questions",
		map[string]any{"body": "Q", "type": "brainstorm", "options": []string{"A"}, "recommend": 1})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGetThreads_CarriesBrainstormFields(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	q := askBrainstorm(t, srv, taskID)

	resp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"choose": 1})
	resp.Body.Close()

	entries := getThreads(t, srv, "?all=true", "")
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if !eqIntPtr(e.RecommendedOption, intPtr(2)) || !eqIntPtr(e.ChosenOption, intPtr(1)) ||
		e.Outcome != store.OutcomeCorrected || e.AnswerSource != store.AnswerSourceUI || e.AnsweredBy != "human" {
		t.Fatalf("inbox entry = %+v", e.brainstormWire)
	}
}
