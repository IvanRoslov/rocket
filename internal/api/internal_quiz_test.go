package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// quizTestDeps reuses activityTestDeps: a real store/bus/monitor is enough
// for exercising POST /v1/internal/quiz, which only touches d.Store and
// d.Bus.
func quizTestDeps(t *testing.T) Deps {
	t.Helper()
	return activityTestDeps(t)
}

const quizPendingPayload = `{
	"session_id": "ce22567b-69b4-4ea1-ba27-3665304ea0bf",
	"hook_event_name": "PreToolUse",
	"tool_name": "AskUserQuestion",
	"tool_input": {
		"questions": [
			{"question": "Which color?", "header": "Color", "options": [{"label": "Red", "description": "The color red"}], "multiSelect": false}
		]
	},
	"tool_use_id": "toolu_017AB88T3MrhfRZrc3MQgyTP"
}`

const quizResolvedPayload = `{
	"session_id": "ce22567b-69b4-4ea1-ba27-3665304ea0bf",
	"hook_event_name": "PostToolUse",
	"tool_name": "AskUserQuestion",
	"tool_input": {
		"questions": [{"question": "Which color?", "header": "Color", "options": [{"label": "Red", "description": "The color red"}], "multiSelect": false}],
		"answers": {"Which color?": "Red"}
	},
	"tool_response": {
		"questions": [{"question": "Which color?", "header": "Color", "options": [{"label": "Red", "description": "The color red"}], "multiSelect": false}],
		"answers": {"Which color?": "Red"}
	},
	"tool_use_id": "toolu_017AB88T3MrhfRZrc3MQgyTP",
	"duration_ms": 0
}`

func TestPostInternalQuiz_PendingStoresQuizAndPublishesEvent(t *testing.T) {
	d := quizTestDeps(t)
	srv := newTestServer(t, d)
	seedActivitySession(t, d.Store, "sess1")

	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	var rawPayload json.RawMessage = json.RawMessage(quizPendingPayload)
	resp := postJSON(t, srv.URL+"/v1/internal/quiz", map[string]any{
		"session": "sess1",
		"phase":   "pending",
		"payload": rawPayload,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	sess, err := d.Store.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.PendingQuiz == "" {
		t.Fatal("PendingQuiz is empty, want stored quiz JSON")
	}

	var stored struct {
		Questions json.RawMessage `json:"questions"`
		AskedAt   int64           `json:"asked_at"`
	}
	if err := json.Unmarshal([]byte(sess.PendingQuiz), &stored); err != nil {
		t.Fatalf("unmarshal stored PendingQuiz: %v", err)
	}
	if len(stored.Questions) == 0 {
		t.Error("stored quiz has no questions")
	}
	if stored.AskedAt == 0 {
		t.Error("stored quiz has no asked_at")
	}

	select {
	case ev := <-ch:
		if ev.Type != "session.quiz_asked" {
			t.Errorf("event type = %q, want session.quiz_asked", ev.Type)
		}
		if ev.SessionID != "sess1" {
			t.Errorf("event session = %q, want sess1", ev.SessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session.quiz_asked event")
	}
}

func TestPostInternalQuiz_PendingOverwritesPreviousQuiz(t *testing.T) {
	d := quizTestDeps(t)
	srv := newTestServer(t, d)
	seedActivitySession(t, d.Store, "sess1")

	if err := d.Store.SetPendingQuiz("sess1", `{"questions":"old","asked_at":1}`); err != nil {
		t.Fatalf("seed SetPendingQuiz: %v", err)
	}

	resp := postJSON(t, srv.URL+"/v1/internal/quiz", map[string]any{
		"session": "sess1",
		"phase":   "pending",
		"payload": json.RawMessage(quizPendingPayload),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	sess, err := d.Store.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.PendingQuiz == `{"questions":"old","asked_at":1}` {
		t.Error("PendingQuiz was not overwritten")
	}
}

func TestPostInternalQuiz_ResolvedClearsQuizAndPublishesEvent(t *testing.T) {
	d := quizTestDeps(t)
	srv := newTestServer(t, d)
	seedActivitySession(t, d.Store, "sess1")

	if err := d.Store.SetPendingQuiz("sess1", `{"questions":[],"asked_at":1}`); err != nil {
		t.Fatalf("seed SetPendingQuiz: %v", err)
	}

	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	resp := postJSON(t, srv.URL+"/v1/internal/quiz", map[string]any{
		"session": "sess1",
		"phase":   "resolved",
		"payload": json.RawMessage(quizResolvedPayload),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	sess, err := d.Store.GetSession("sess1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.PendingQuiz != "" {
		t.Errorf("PendingQuiz = %q, want empty after resolve", sess.PendingQuiz)
	}

	select {
	case ev := <-ch:
		if ev.Type != "session.quiz_resolved" {
			t.Errorf("event type = %q, want session.quiz_resolved", ev.Type)
		}
		if ev.SessionID != "sess1" {
			t.Errorf("event session = %q, want sess1", ev.SessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for session.quiz_resolved event")
	}
}

// TestPostInternalQuiz_ResolvedWithNoPendingIsNoOp asserts idempotency: a
// PostToolUse fire for a session with no pending quiz (e.g. a duplicate
// delivery, or a matcher edge case) succeeds with 200 but does not publish
// an event.
func TestPostInternalQuiz_ResolvedWithNoPendingIsNoOp(t *testing.T) {
	d := quizTestDeps(t)
	srv := newTestServer(t, d)
	seedActivitySession(t, d.Store, "sess1")

	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	resp := postJSON(t, srv.URL+"/v1/internal/quiz", map[string]any{
		"session": "sess1",
		"phase":   "resolved",
		"payload": json.RawMessage(quizResolvedPayload),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	select {
	case ev := <-ch:
		t.Fatalf("unexpected event published: %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// expected: no event
	}
}

func TestPostInternalQuiz_UnknownSessionIs404(t *testing.T) {
	d := quizTestDeps(t)
	srv := newTestServer(t, d)

	resp := postJSON(t, srv.URL+"/v1/internal/quiz", map[string]any{
		"session": "does-not-exist",
		"phase":   "pending",
		"payload": json.RawMessage(quizPendingPayload),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var body errBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "session_not_found" {
		t.Errorf("code = %q, want session_not_found", body.Error.Code)
	}
}

func TestPostInternalQuiz_MalformedBodyIs400(t *testing.T) {
	d := quizTestDeps(t)
	srv := newTestServer(t, d)
	seedActivitySession(t, d.Store, "sess1")

	resp, err := http.Post(srv.URL+"/v1/internal/quiz", "application/json", strings.NewReader(`{"session":"sess1","phase":`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPostInternalQuiz_PendingMissingToolInputIs400(t *testing.T) {
	d := quizTestDeps(t)
	srv := newTestServer(t, d)
	seedActivitySession(t, d.Store, "sess1")

	resp := postJSON(t, srv.URL+"/v1/internal/quiz", map[string]any{
		"session": "sess1",
		"phase":   "pending",
		"payload": json.RawMessage(`{"session_id":"x","hook_event_name":"PreToolUse"}`),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body errBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "bad_payload" {
		t.Errorf("code = %q, want bad_payload", body.Error.Code)
	}
}

func TestPostInternalQuiz_InvalidPhaseIs400(t *testing.T) {
	d := quizTestDeps(t)
	srv := newTestServer(t, d)
	seedActivitySession(t, d.Store, "sess1")

	resp := postJSON(t, srv.URL+"/v1/internal/quiz", map[string]any{
		"session": "sess1",
		"phase":   "bogus",
		"payload": json.RawMessage(quizPendingPayload),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body errBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "invalid_phase" {
		t.Errorf("code = %q, want invalid_phase", body.Error.Code)
	}
}

// TestPostInternalQuiz_ResolvedLeavesPermissionQuizAlone: the quiz hook's
// Stop/PostToolUse "resolved" report fires at the end of every turn, but a
// permission quiz (source "permission") is the monitor's — clearing it here
// would leave its journal row open and the dialog missing from the chat
// feed (seen live: answer in the terminal, the turn ends, Stop fires).
func TestPostInternalQuiz_ResolvedLeavesPermissionQuizAlone(t *testing.T) {
	d := quizTestDeps(t)
	srv := newTestServer(t, d)
	seedActivitySession(t, d.Store, "sess1")

	perm := `{"questions":[{"question":"Do you want to proceed?","header":"Разрешение","multiSelect":false,"options":[]}],"asked_at":1,"source":"permission","permission":{"prompt_id":1,"title":"Do you want to proceed?","context":""}}`
	if err := d.Store.SetPendingQuiz("sess1", perm); err != nil {
		t.Fatalf("seed SetPendingQuiz: %v", err)
	}

	ch, cancel := d.Bus.Subscribe()
	defer cancel()

	resp := postJSON(t, srv.URL+"/v1/internal/quiz", map[string]any{
		"session": "sess1",
		"phase":   "resolved",
		"payload": json.RawMessage(quizResolvedPayload),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if sess, _ := d.Store.GetSession("sess1"); sess.PendingQuiz != perm {
		t.Errorf("PendingQuiz = %q, want the permission quiz untouched", sess.PendingQuiz)
	}
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event published: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestPostInternalQuiz_PendingOverPermissionClosesItsRow: an AskUserQuestion
// quiz replacing a permission quiz means that dialog is gone; its journal
// row is closed so it shows up in the chat feed instead of staying open.
func TestPostInternalQuiz_PendingOverPermissionClosesItsRow(t *testing.T) {
	d := quizTestDeps(t)
	srv := newTestServer(t, d)
	seedActivitySession(t, d.Store, "sess1")

	id, _, err := d.Store.OpenPermissionPrompt("sess1", "Do you want to proceed?", "", "[]", 1)
	if err != nil {
		t.Fatal(err)
	}
	perm := `{"questions":[],"asked_at":1,"source":"permission","permission":{"prompt_id":` + strconv.FormatInt(id, 10) + `,"title":"Do you want to proceed?","context":""}}`
	_ = d.Store.SetPendingQuiz("sess1", perm)

	resp := postJSON(t, srv.URL+"/v1/internal/quiz", map[string]any{
		"session": "sess1",
		"phase":   "pending",
		"payload": json.RawMessage(quizPendingPayload),
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	rows, _ := d.Store.ListResolvedPermissionPrompts("sess1")
	if len(rows) != 1 || rows[0].ID != id || rows[0].AnsweredVia != "terminal" {
		t.Errorf("resolved rows = %+v, want row %d closed via terminal", rows, id)
	}
}
