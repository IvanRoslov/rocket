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

// Every brainstorm question is a decision with a recommendation: without at
// least two options an open question could only ever score wrong_turn and
// would skew the metric.
func TestBrainstormAsk_NeedsTwoOptions(t *testing.T) {
	for name, payload := range map[string]map[string]any{
		"no options": {"body": "Что болит?", "type": "brainstorm"},
		"one option": {"body": "Так?", "type": "brainstorm", "options": []string{"A"}, "recommend": 1},
	} {
		t.Run(name, func(t *testing.T) {
			d := questionsTestDeps(t)
			srv := newTestServer(t, d)
			taskID := setupQuestionTask(t, d)
			resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", payload)
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

func recordURL(srv *httptest.Server, id int64) string {
	return srv.URL + "/v1/questions/" + itoa(id) + "/brainstorm-record"
}

// TestBrainstormRecord_OrchestratorRecordsHumanAnswer: the orchestrator writes
// down what the human said in its terminal. The answer is the human's, marked
// as coming from the terminal; it reaches every other participant but is not
// echoed back to the orchestrator that recorded it.
func TestBrainstormRecord_OrchestratorRecordsHumanAnswer(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	setupQuestionAgent(t, d)
	q := askBrainstorm(t, srv, taskID)
	if err := d.Store.AddParticipants(q.ID, "cto"); err != nil {
		t.Fatalf("AddParticipants: %v", err)
	}
	before, err := d.Store.ListMessages("orch-1", 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}

	resp := postJSONWithHeader(t, recordURL(srv, q.ID), "orch-1",
		map[string]any{"choose": 2, "body": "да, но без кэша"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("record = %d, want 200", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.Status != "resolved" || got.Resolution != "answered" {
		t.Fatalf("status/resolution = %q/%q", got.Status, got.Resolution)
	}
	if !eqIntPtr(got.ChosenOption, intPtr(2)) || got.AnswerComment != "да, но без кэша" ||
		got.AnswerSource != store.AnswerSourceTerminal || got.Outcome != store.OutcomeAccepted {
		t.Fatalf("answer fields = %+v", got.brainstormWire)
	}
	if got.AnsweredBy != "human" {
		t.Fatalf("answered_by = %q, want human", got.AnsweredBy)
	}
	if m := lastMessage(t, got); m.Kind != "answer" || m.Author != "human" || m.Body != "B\n\nда, но без кэша" {
		t.Fatalf("answer message = %+v", m)
	}
	if !strings.Contains(got.Echo, itoa(taskID)+"/Q1") {
		t.Errorf("echo = %q, want the thread ref", got.Echo)
	}
	if len(got.WaitingOn) != 0 {
		t.Errorf("waiting_on = %v, want empty", got.WaitingOn)
	}

	after, err := d.Store.ListMessages("orch-1", 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("orchestrator got its own record back: %d -> %d messages", len(before), len(after))
	}
	inbox, err := d.Store.ListInboxMessages("cto", store.InboxUnread, 0)
	if err != nil {
		t.Fatalf("ListInboxMessages: %v", err)
	}
	if len(inbox) == 0 || !strings.Contains(inbox[len(inbox)-1].Body, "да, но без кэша") {
		t.Errorf("cto inbox = %+v, want the recorded answer", inbox)
	}
}

func TestBrainstormRecord_OwnTextIsWrongTurn(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	q := askBrainstorm(t, srv, taskID)

	resp := postJSONWithHeader(t, recordURL(srv, q.ID), "orch-1", map[string]any{"body": "давай сначала про деньги"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("record = %d, want 200", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.ChosenOption != nil || got.Outcome != store.OutcomeWrongTurn {
		t.Fatalf("chosen/outcome = %v/%q, want null/wrong_turn", got.ChosenOption, got.Outcome)
	}
}

func TestBrainstormRecord_Forbidden(t *testing.T) {
	cases := []struct {
		name   string
		caller string
		setup  func(t *testing.T, d Deps, taskID int64)
	}{
		{"human", "", nil},
		{"worker of the same feature", "worker-1", func(t *testing.T, d Deps, taskID int64) {
			addTestSession(t, d, "worker-1", "worker", "proj1")
			if _, err := d.Store.AddTask(store.Task{Title: "sub", ProjectID: "proj1", ParentID: taskID, SessionID: "worker-1"}); err != nil {
				t.Fatalf("AddTask: %v", err)
			}
		}},
		{"orchestrator of another task", "orch-2", func(t *testing.T, d Deps, taskID int64) {
			addTestSession(t, d, "orch-2", "orchestrator", "proj1")
			if _, err := d.Store.AddTask(store.Task{Title: "other", ProjectID: "proj1", SessionID: "orch-2"}); err != nil {
				t.Fatalf("AddTask: %v", err)
			}
		}},
		{"persistent agent", "cto", func(t *testing.T, d Deps, taskID int64) {
			setupQuestionAgent(t, d)
			addLiveAgentSession(t, d, "cto")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := questionsTestDeps(t)
			srv := newTestServer(t, d)
			taskID := setupQuestionTask(t, d)
			if c.setup != nil {
				c.setup(t, d, taskID)
			}
			q := askBrainstorm(t, srv, taskID)

			var resp *http.Response
			if c.caller == "" {
				resp = postJSON(t, recordURL(srv, q.ID), map[string]any{"choose": 1})
			} else {
				resp = postJSONWithHeader(t, recordURL(srv, q.ID), c.caller, map[string]any{"choose": 1, "join": true})
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
			if stored, _ := d.Store.GetQuestion(q.ID); stored.Status != "open" {
				t.Fatalf("thread status = %q, want still open", stored.Status)
			}
		})
	}
}

func TestBrainstormRecord_NotBrainstorm(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	resp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1",
		map[string]any{"body": "Q", "options": []string{"A", "B"}})
	q := decodeQuestion(t, resp)
	resp.Body.Close()

	rec := postJSONWithHeader(t, recordURL(srv, q.ID), "orch-1", map[string]any{"choose": 1})
	defer rec.Body.Close()
	if rec.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.StatusCode)
	}
	if eb := decodeErr(t, rec); eb.Error.Code != "not_brainstorm" {
		t.Errorf("code = %q, want not_brainstorm", eb.Error.Code)
	}
}

func TestBrainstormRecord_AlreadyResolved(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	q := askBrainstorm(t, srv, taskID)

	first := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"choose": 2})
	first.Body.Close()

	rec := postJSONWithHeader(t, recordURL(srv, q.ID), "orch-1", map[string]any{"choose": 1})
	defer rec.Body.Close()
	if rec.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.StatusCode)
	}
	if stored, _ := d.Store.GetQuestion(q.ID); stored.ChosenOption != 2 || stored.AnswerSource != store.AnswerSourceUI {
		t.Fatalf("the earlier answer was overwritten: %+v", stored)
	}
}

func TestBrainstormRecord_BadInput(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
	}{
		{"empty", map[string]any{}},
		{"choose out of range", map[string]any{"choose": 4}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := questionsTestDeps(t)
			srv := newTestServer(t, d)
			taskID := setupQuestionTask(t, d)
			q := askBrainstorm(t, srv, taskID)
			rec := postJSONWithHeader(t, recordURL(srv, q.ID), "orch-1", c.payload)
			defer rec.Body.Close()
			if rec.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.StatusCode)
			}
		})
	}
}

func outcomeURL(srv *httptest.Server, id int64) string {
	return srv.URL + "/v1/questions/" + itoa(id) + "/outcome"
}

// answeredBrainstorm opens a brainstorm thread and answers it with the
// recommended option, so its computed outcome is accepted.
func answeredBrainstorm(t *testing.T, srv *httptest.Server, taskID int64) questionResponse {
	t.Helper()
	q := askBrainstorm(t, srv, taskID)
	resp := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"choose": 2})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer = %d", resp.StatusCode)
	}
	return decodeQuestion(t, resp)
}

func TestBrainstormOutcome_HumanOverrides(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	q := answeredBrainstorm(t, srv, taskID)

	resp := patchJSON(t, outcomeURL(srv, q.ID), map[string]any{"outcome": "corrected"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("override = %d, want 200", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.Outcome != store.OutcomeCorrected || !got.OutcomeOverridden {
		t.Fatalf("outcome/overridden = %q/%v, want corrected/true", got.Outcome, got.OutcomeOverridden)
	}
	if !eqIntPtr(got.ChosenOption, intPtr(2)) {
		t.Fatalf("override touched the choice: %v", got.ChosenOption)
	}
}

func TestBrainstormOutcome_AgentsForbidden(t *testing.T) {
	for _, caller := range []string{"orch-1", "cto"} {
		t.Run(caller, func(t *testing.T) {
			d := questionsTestDeps(t)
			srv := newTestServer(t, d)
			taskID := setupQuestionTask(t, d)
			setupQuestionAgent(t, d)
			addLiveAgentSession(t, d, "cto")
			q := answeredBrainstorm(t, srv, taskID)

			resp := patchJSONWithHeader(t, outcomeURL(srv, q.ID), caller, map[string]any{"outcome": "corrected"})
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
			if stored, _ := d.Store.GetQuestion(q.ID); stored.OutcomeOverridden || stored.Outcome != store.OutcomeAccepted {
				t.Fatalf("outcome changed: %+v", stored)
			}
		})
	}
}

func TestBrainstormOutcome_Rejections(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)

	answered := answeredBrainstorm(t, srv, taskID)
	open := askBrainstorm(t, srv, taskID)

	dismissed := askBrainstorm(t, srv, taskID)
	dr := postJSON(t, srv.URL+"/v1/questions/"+itoa(dismissed.ID)+"/answer", map[string]any{"dismiss": true})
	dr.Body.Close()

	dresp := postJSONWithHeader(t, srv.URL+"/v1/tasks/"+itoa(taskID)+"/questions", "orch-1", map[string]any{"body": "Q"})
	decision := decodeQuestion(t, dresp)
	dresp.Body.Close()
	ar := postJSON(t, srv.URL+"/v1/questions/"+itoa(decision.ID)+"/answer", map[string]any{"body": "ok"})
	ar.Body.Close()

	cases := []struct {
		name    string
		id      int64
		outcome string
		want    int
	}{
		{"unknown outcome", answered.ID, "maybe", http.StatusBadRequest},
		{"decision thread", decision.ID, "accepted", http.StatusBadRequest},
		{"open thread", open.ID, "accepted", http.StatusConflict},
		{"dismissed thread", dismissed.ID, "accepted", http.StatusConflict},
		{"unknown thread", 99999, "accepted", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := patchJSON(t, outcomeURL(srv, c.id), map[string]any{"outcome": c.outcome})
			defer resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, c.want)
			}
		})
	}
}

// Reopening a brainstorm thread wipes its answer: it waits on the human
// again, and the next answer computes a fresh outcome.
func TestBrainstormReopen_ResetsAnswer(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	q := answeredBrainstorm(t, srv, taskID)
	ov := patchJSON(t, outcomeURL(srv, q.ID), map[string]any{"outcome": "wrong_turn"})
	ov.Body.Close()

	resp := postJSONWithHeader(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/reply", "orch-1",
		map[string]any{"body": "Оспариваю: B ломает миграцию", "dispute": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("dispute = %d, want 201", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if got.Status != "open" || got.Type != "brainstorm" {
		t.Fatalf("status/type = %q/%q, want open/brainstorm", got.Status, got.Type)
	}
	if got.ChosenOption != nil || got.AnswerComment != "" || got.AnswerSource != "" ||
		got.Outcome != "" || got.OutcomeOverridden || got.AnsweredBy != "" {
		t.Fatalf("reopened thread kept the answer: %+v", got.brainstormWire)
	}
	if !eqIntPtr(got.RecommendedOption, intPtr(2)) {
		t.Fatalf("recommendation lost: %v", got.RecommendedOption)
	}

	again := postJSON(t, srv.URL+"/v1/questions/"+itoa(q.ID)+"/answer", map[string]any{"choose": 1})
	defer again.Body.Close()
	if fresh := decodeQuestion(t, again); fresh.Outcome != store.OutcomeCorrected || fresh.OutcomeOverridden {
		t.Fatalf("new answer outcome = %q/%v, want corrected/false", fresh.Outcome, fresh.OutcomeOverridden)
	}
}

// TestBrainstormRecord_HumanJoinsThread: the recorded answer is the human's,
// so the human is a participant afterwards — as on the ordinary answer path —
// even on a thread whose participants were never seeded with them.
func TestBrainstormRecord_HumanJoinsThread(t *testing.T) {
	d := questionsTestDeps(t)
	srv := newTestServer(t, d)
	taskID := setupQuestionTask(t, d)
	qid, err := d.Store.AddQuestion(store.Question{
		TaskID: taskID, AskedBy: "orch-1", Body: "Где кэш?",
		Type: store.QuestionTypeBrainstorm, Options: []string{"A", "B"}, RecommendedOption: 1,
	})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	if err := d.Store.AddParticipants(qid, "orch-1"); err != nil {
		t.Fatalf("AddParticipants: %v", err)
	}

	resp := postJSONWithHeader(t, recordURL(srv, qid), "orch-1", map[string]any{"choose": 1})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("record = %d, want 200", resp.StatusCode)
	}
	got := decodeQuestion(t, resp)
	if !contains(got.Participants, "human") {
		t.Fatalf("participants = %v, want the human among them", got.Participants)
	}
}
