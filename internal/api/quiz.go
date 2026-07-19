package api

import (
	"encoding/json"
	"net/http"

	"github.com/IvanRoslov/rocket/internal/session"
)

// registerQuizRoutes wires the public quiz-answer route onto mux. This is
// distinct from registerInternalQuizRoutes (POST /v1/internal/quiz), which
// is the push side hooks use to report a pending/resolved quiz; this route
// is the pull side a remote client (dashboard/mobile) uses to answer one.
func registerQuizRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/sessions/{id}/quiz/answer", func(w http.ResponseWriter, r *http.Request) {
		handlePostQuizAnswer(w, r, d)
	})
}

// quizOptionResponse and quizQuestionResponse/quizResponse are the
// public-API shapes of a pending quiz — snake_case (multi_select), as
// opposed to the camelCase (multiSelect) shape Session.PendingQuiz stores
// verbatim from the AskUserQuestion hook payload (see session.Quiz).
type quizOptionResponse struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type quizQuestionResponse struct {
	Question    string               `json:"question"`
	Header      string               `json:"header"`
	MultiSelect bool                 `json:"multi_select"`
	Options     []quizOptionResponse `json:"options"`
}

type quizResponse struct {
	Questions []quizQuestionResponse `json:"questions"`
	AskedAt   int64                  `json:"asked_at"`
}

// parseQuizResponse converts a session's raw PendingQuiz JSON (as stored by
// POST /v1/internal/quiz) into the public quizResponse shape. Returns nil
// if pendingQuizJSON is empty (no pending quiz) or fails to parse — the
// latter should never happen given internal_quiz.go always writes
// well-formed JSON, but a malformed stored value must not break session
// display, so it degrades to "no pending quiz shown" rather than a 500.
func parseQuizResponse(pendingQuizJSON string) *quizResponse {
	if pendingQuizJSON == "" {
		return nil
	}
	var quiz session.Quiz
	if err := json.Unmarshal([]byte(pendingQuizJSON), &quiz); err != nil {
		return nil
	}

	out := quizResponse{Questions: make([]quizQuestionResponse, len(quiz.Questions)), AskedAt: quiz.AskedAt}
	for i, q := range quiz.Questions {
		options := make([]quizOptionResponse, len(q.Options))
		for j, o := range q.Options {
			options[j] = quizOptionResponse{Label: o.Label, Description: o.Description}
		}
		out.Questions[i] = quizQuestionResponse{
			Question:    q.Question,
			Header:      q.Header,
			MultiSelect: q.MultiSelect,
			Options:     options,
		}
	}
	return &out
}

// postQuizAnswerRequest is the body of POST /v1/sessions/{id}/quiz/answer.
type postQuizAnswerRequest struct {
	Answers []quizAnswerRequest `json:"answers"`
}

type quizAnswerRequest struct {
	QuestionIndex int    `json:"question_index"`
	OptionIndices []int  `json:"option_indices,omitempty"`
	Text          string `json:"text,omitempty"`
}

// handlePostQuizAnswer handles POST /v1/sessions/{id}/quiz/answer: it
// validates answers against the session's pending quiz and, on success,
// hands off to session.Manager.AnswerQuiz to drive the TUI keystrokes
// asynchronously, responding 202 {"status":"answering"} immediately —
// actual completion is only ever confirmed out-of-band by the
// session.quiz_resolved event (see docs/superpowers/specs/
// 2026-07-19-remote-quiz-design.md §4).
//
// Errors: 404 session_not_found, 409 no_pending_quiz (nothing to answer —
// e.g. already answered in the terminal), 409 quiz_answer_in_flight (a
// previous answer for this session's quiz is still being typed by the
// injector — see session.Manager.AnswerQuiz's in-flight guard; retry once
// it resolves), 400 quiz_answer_invalid (bad shape: see
// session.validateQuizAnswers's doc comment for every case). All four
// codes are produced as a *session.ValidationError and mapped to their
// HTTP status by writeManagerErr (internal/api/sessions.go).
func handlePostQuizAnswer(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	var req postQuizAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	answers := make([]session.QuizAnswer, len(req.Answers))
	for i, a := range req.Answers {
		answers[i] = session.QuizAnswer{
			QuestionIndex: a.QuestionIndex,
			OptionIndices: a.OptionIndices,
			Text:          a.Text,
		}
	}

	if err := d.Manager.AnswerQuiz(r.Context(), id, answers); err != nil {
		writeManagerErr(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "answering"})
}
