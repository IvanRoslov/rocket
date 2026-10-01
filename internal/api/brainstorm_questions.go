// Brainstorm threads (task #4901, spec §2.2): a decision thread of an
// orchestrator's brainstorm that also records the asker's recommendation and
// how the human's answer related to it. This file holds what only brainstorm
// threads have — the wire fields, the recommendation check, the terminal
// record and the outcome override; everything else is the ordinary thread
// machinery in questions.go and threads.go.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/IvanRoslov/rocket/internal/store"
)

// brainstormWire is the part of a thread's JSON that reports a brainstorm
// answer. It is present on every thread so clients read one shape: on other
// thread types the options are null and the strings empty. AnsweredBy names
// who closed the thread with an answer ("human", an agent id, or "" while
// unanswered) on every thread type, so the brainstorm metric can count the
// human's answers only.
type brainstormWire struct {
	RecommendedOption *int   `json:"recommended_option"`
	ChosenOption      *int   `json:"chosen_option"`
	AnswerComment     string `json:"answer_comment"`
	AnswerSource      string `json:"answer_source"`
	Outcome           string `json:"outcome"`
	OutcomeOverridden bool   `json:"outcome_overridden"`
	AnsweredBy        string `json:"answered_by"`
}

// optionRef renders a 1-based option number for the wire; 0 is null.
func optionRef(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

// toBrainstormWire builds the wire fields of q. answerAuthor is the author of
// q's latest answer entry, "" when it has none; it only counts while the
// thread actually stands answered — a reopened thread is unanswered again.
func toBrainstormWire(q store.Question, answerAuthor string) brainstormWire {
	w := brainstormWire{
		RecommendedOption: optionRef(q.RecommendedOption),
		ChosenOption:      optionRef(q.ChosenOption),
		AnswerComment:     q.AnswerComment,
		AnswerSource:      q.AnswerSource,
		Outcome:           q.Outcome,
		OutcomeOverridden: q.OutcomeOverridden,
	}
	if q.Status == "resolved" && q.Resolution == "answered" && answerAuthor != "" {
		w.AnsweredBy = wireParticipant(answerAuthor)
	}
	return w
}

// latestAnswerAuthor returns the author of the last answer entry in msgs, or
// "" when there is none.
func latestAnswerAuthor(msgs []store.QuestionMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Kind == "answer" {
			return msgs[i].Author
		}
	}
	return ""
}

// validateRecommend checks a new thread's recommendation, writing a 400 and
// returning false when it is wrong. A brainstorm thread with options must
// recommend one of them; one without options has nothing to recommend; and no
// other thread type takes a recommendation at all — silently dropping it would
// hide a mistyped --brainstorm.
func validateRecommend(w http.ResponseWriter, threadType string, options []string, recommend int) bool {
	var msg string
	switch {
	case threadType != store.QuestionTypeBrainstorm && recommend != 0:
		msg = "recommend is only accepted on a brainstorm thread"
	case threadType == store.QuestionTypeBrainstorm && len(options) == 0 && recommend != 0:
		msg = "recommend needs options to point at"
	case threadType == store.QuestionTypeBrainstorm && len(options) > 0 && (recommend < 1 || recommend > len(options)):
		msg = fmt.Sprintf("a brainstorm thread with options must recommend one: recommend must be between 1 and %d", len(options))
	default:
		return true
	}
	writeErr(w, http.StatusBadRequest, "bad_request", msg)
	return false
}

// registerBrainstormQuestionRoutes wires the brainstorm-only thread routes.
func registerBrainstormQuestionRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/questions/{id}/brainstorm-record", func(w http.ResponseWriter, r *http.Request) {
		handlePostBrainstormRecord(w, r, d)
	})
	mux.HandleFunc("PATCH /v1/questions/{id}/outcome", func(w http.ResponseWriter, r *http.Request) {
		handlePatchQuestionOutcome(w, r, d)
	})
}

type postBrainstormRecordRequest struct {
	// Choose is the option the human picked (1-based, 0 = none); Body is the
	// human's words, verbatim — the comment to the choice, or the whole answer.
	Choose int    `json:"choose"`
	Body   string `json:"body"`
}

// handlePostBrainstormRecord serves POST /v1/questions/{id}/brainstorm-record
// {choose?, body?}: the orchestrator writes down the answer the human gave in
// its terminal. It is the one exception to "agents do not close threads" —
// the agent records the human's answer, not its own — so it is held tight:
// only the orchestrator of the thread's task (403 for anybody else, the human
// included: the human answers directly), only a brainstorm thread (400), only
// an open one (409).
//
// The answer is the human's: the entry is authored "human", the thread
// records answer_source=terminal, and the outcome follows the same rule as a
// click in a client. It is delivered to the other participants as any answer
// is, except the orchestrator that recorded it, which already knows.
func handlePostBrainstormRecord(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseQuestionID(w, r)
	if !ok {
		return
	}
	q, ok := getQuestionOr404(w, d, id)
	if !ok {
		return
	}
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	task, ok := getTaskOr404(w, d, q.TaskID)
	if !ok {
		return
	}
	subj := threadSubject{TaskID: task.ID, Counterpart: task.SessionID}
	if !callerIsCounterpart(caller, subj) {
		writeErr(w, http.StatusForbidden, "forbidden",
			"only the orchestrator of this task may record the human's terminal answer")
		return
	}
	if q.Type != store.QuestionTypeBrainstorm {
		writeErr(w, http.StatusBadRequest, "not_brainstorm",
			"only a brainstorm thread takes a recorded terminal answer; this one is "+q.Type)
		return
	}
	if q.Status != "open" {
		writeErr(w, http.StatusConflict, "question_resolved", "question is already resolved")
		return
	}

	var req postBrainstormRecordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	body, ok := chooseOptionBody(w, q, req.Choose, req.Body)
	if !ok {
		return
	}
	if body == "" {
		writeErr(w, http.StatusBadRequest, "empty_body", "choose an option or give the human's text")
		return
	}

	if _, err := d.Store.ResolveBrainstormQuestion(id, store.BrainstormAnswer{
		ChosenOption: req.Choose, Comment: req.Body, Source: store.AnswerSourceTerminal,
	}); err != nil {
		if errors.Is(err, store.ErrQuestionResolved) {
			writeErr(w, http.StatusConflict, "question_resolved", "question is already resolved")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if _, err := d.Store.AddQuestionMessage(store.QuestionMessage{
		QuestionID: id, Author: store.ParticipantHuman, Kind: "answer", Body: body,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ordinal, err := d.Store.QuestionOrdinal(q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	participants, err := d.Store.ListParticipants(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	recipients := make([]string, 0, len(participants))
	for _, p := range participants {
		if !sameParticipant(p, caller.ID) {
			recipients = append(recipients, p)
		}
	}
	if err := participantFanOut(d, subj, ordinal, "answer", store.ParticipantHuman, body, recipients); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if err := d.Store.ClearAttention(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	d.Bus.Publish("task.question_resolved", callerLabel(caller), map[string]any{
		"task_id": task.ID, "question_id": id, "resolution": "answered",
		"answer_source": store.AnswerSourceTerminal,
	})

	updated, ok := getQuestionOr404(w, d, id)
	if !ok {
		return
	}
	resp, err := buildQuestionResponse(d, caller, updated)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	resp.Echo = threadEcho(subj, ordinal, q.Body, task.Title)
	writeJSON(w, http.StatusOK, resp)
}

type patchQuestionOutcomeRequest struct {
	Outcome string `json:"outcome"`
}

// handlePatchQuestionOutcome serves PATCH /v1/questions/{id}/outcome
// {outcome}: the human corrects the outcome computed for an answered
// brainstorm thread — e.g. a choice of another option that was really a small
// tweak of the recommendation. Only the human (any agent, persistent ones
// included, gets 403): the outcome grades the agent's recommendations, and an
// agent must not grade itself. The thread must be a brainstorm (400) that
// stands answered (409 while open or after a dismissal: there is no outcome).
func handlePatchQuestionOutcome(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseQuestionID(w, r)
	if !ok {
		return
	}
	q, ok := getQuestionOr404(w, d, id)
	if !ok {
		return
	}
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil {
		writeErr(w, http.StatusForbidden, "forbidden", "only the human may override a brainstorm outcome")
		return
	}

	var req patchQuestionOutcomeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if !store.ValidOutcome(req.Outcome) {
		writeErr(w, http.StatusBadRequest, "bad_request",
			"outcome must be \"accepted\", \"corrected\" or \"wrong_turn\"")
		return
	}
	if q.Type != store.QuestionTypeBrainstorm {
		writeErr(w, http.StatusBadRequest, "not_brainstorm", "only a brainstorm thread has an outcome")
		return
	}
	if q.Status != "resolved" || q.Resolution != "answered" {
		writeErr(w, http.StatusConflict, "not_answered", "the thread has no answer to grade")
		return
	}

	if err := d.Store.SetQuestionOutcome(id, req.Outcome); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	d.Bus.Publish("task.question_outcome_set", "", map[string]any{
		"task_id": q.TaskID, "question_id": id, "outcome": req.Outcome,
	})

	updated, ok := getQuestionOr404(w, d, id)
	if !ok {
		return
	}
	resp, err := buildQuestionResponse(d, caller, updated)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
