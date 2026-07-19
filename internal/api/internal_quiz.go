package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// registerInternalQuizRoutes wires the /v1/internal/quiz route onto mux.
// This endpoint is the push side of the quiz channel: the AskUserQuestion
// PreToolUse/PostToolUse hooks rocket wires into a Claude Code worktree POST
// here so the daemon can see a pending TUI quiz in real time, without
// waiting for the transcript (which stays silent until the quiz is
// answered).
func registerInternalQuizRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/internal/quiz", func(w http.ResponseWriter, r *http.Request) {
		handlePostInternalQuiz(w, r, d)
	})
}

// postInternalQuizRequest is the body POSTed by quiz-hook.sh:
// {"session":"...","phase":"pending"|"resolved","payload":<raw hook stdin JSON>}.
// Payload is the complete, unmodified stdin JSON Claude Code passed to the
// hook (a PreToolUse or PostToolUse event for the AskUserQuestion tool).
type postInternalQuizRequest struct {
	Session string          `json:"session"`
	Phase   string          `json:"phase"`
	Payload json.RawMessage `json:"payload"`
}

// hookToolInputEnvelope extracts the tool_input field from a PreToolUse hook
// stdin payload. For AskUserQuestion, tool_input carries the "questions"
// array.
type hookToolInputEnvelope struct {
	ToolInput json.RawMessage `json:"tool_input"`
}

// quizToolInput is the shape of tool_input for the AskUserQuestion tool.
type quizToolInput struct {
	Questions json.RawMessage `json:"questions"`
}

// handlePostInternalQuiz handles POST /v1/internal/quiz. The session must
// already exist (404 session_not_found); phase must be "pending" or
// "resolved" (400 invalid_phase); a malformed request body or, for
// "pending", a payload missing tool_input/questions, is a 400 bad_payload
// (logged as a warning, since the hook always exits 0 regardless of what
// this endpoint returns).
//
// pending: stores {"questions":<tool_input.questions>,"asked_at":<unix
// now>} as the session's pending quiz (overwriting any previous one) and
// publishes session.quiz_asked.
//
// resolved: clears the pending quiz unconditionally (a PostToolUse fire is
// authoritative regardless of tool_response.is_error). If the session had
// no pending quiz, this is a no-op: 200 with no event published — that
// covers hook replay/duplicate delivery and matcher edge cases.
func handlePostInternalQuiz(w http.ResponseWriter, r *http.Request, d Deps) {
	var req postInternalQuizRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Default().Warn("internal quiz: malformed request body", "error", err)
		writeErr(w, http.StatusBadRequest, "bad_payload", "invalid JSON body")
		return
	}

	if req.Phase != "pending" && req.Phase != "resolved" {
		writeErr(w, http.StatusBadRequest, "invalid_phase", "phase must be \"pending\" or \"resolved\"")
		return
	}

	sess, err := d.Store.GetSession(req.Session)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "session_not_found", "session not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	switch req.Phase {
	case "pending":
		var envelope hookToolInputEnvelope
		if err := json.Unmarshal(req.Payload, &envelope); err != nil || len(envelope.ToolInput) == 0 {
			slog.Default().Warn("internal quiz: payload missing tool_input", "session", req.Session)
			writeErr(w, http.StatusBadRequest, "bad_payload", "payload missing tool_input")
			return
		}

		var toolInput quizToolInput
		if err := json.Unmarshal(envelope.ToolInput, &toolInput); err != nil || len(toolInput.Questions) == 0 {
			slog.Default().Warn("internal quiz: tool_input missing questions", "session", req.Session)
			writeErr(w, http.StatusBadRequest, "bad_payload", "tool_input missing questions")
			return
		}

		quiz := struct {
			Questions json.RawMessage `json:"questions"`
			AskedAt   int64           `json:"asked_at"`
		}{
			Questions: toolInput.Questions,
			AskedAt:   time.Now().Unix(),
		}
		quizJSON, err := json.Marshal(quiz)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		if err := d.Store.SetPendingQuiz(req.Session, string(quizJSON)); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusNotFound, "session_not_found", "session not found")
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		if d.Bus != nil {
			d.Bus.Publish("session.quiz_asked", req.Session, map[string]any{})
		}

	case "resolved":
		hadPending := sess.PendingQuiz != ""

		if err := d.Store.ClearPendingQuiz(req.Session); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusNotFound, "session_not_found", "session not found")
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		if hadPending && d.Bus != nil {
			d.Bus.Publish("session.quiz_resolved", req.Session, map[string]any{})
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
