package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/IvanRoslov/rocket/internal/store"
)

// The existing question reply/answer URLs dispatch creative-bound general
// threads here; their durable participants, attention and reply history are shared.
func handleCreativeQuestionEntry(w http.ResponseWriter, r *http.Request, d Deps, q store.Question, answer bool) bool {
	b, err := d.Store.GetCreativeDiscussionContext("question", q.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		creativeError(w, err)
		return true
	}
	if err := d.Store.CheckCreativeRunMutable(b.RunID); err != nil {
		creativeError(w, err)
		return true
	}
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return true
	}
	c, ok := creativeAgentAccess(w, d, caller, b, true)
	if !ok {
		return true
	}
	if answer && caller != nil {
		writeErr(w, 403, "creative_forbidden", "only the human may answer a creative decision; agents use reply")
		return true
	}
	participants, err := d.Store.ListParticipants(q.ID)
	if err != nil {
		creativeError(w, err)
		return true
	}
	author := callerParticipant(caller)
	if caller != nil && caller.ID == c.OrchestratorSessionID && !contains(participants, author) {
		// Membership is persisted during successful fanout, not on a dry run.
		participants = append(participants, author)
	}
	if caller != nil && !contains(participants, author) {
		writeErr(w, 403, "creative_forbidden", "caller is not a question participant")
		return true
	}
	var body, kind string
	if answer {
		var in postQuestionAnswerRequest
		if !creativeBody(w, r, &in) {
			return true
		}
		var ok bool
		body, ok = chooseOptionBody(w, q, in.Choose, in.Body)
		if !ok {
			return true
		}
		if strings.TrimSpace(body) == "" && !in.Dismiss {
			writeErr(w, 400, "empty_body", "answer must not be empty")
			return true
		}
		if q.Status != "open" {
			writeErr(w, 409, "question_resolved", "question is resolved")
			return true
		}
		if in.DryRun {
			resp, err := creativeQuestionView(d, caller, q.ID)
			if err != nil {
				creativeError(w, err)
			} else {
				writeJSON(w, 200, resp)
			}
			return true
		}
		resolution := "answered"
		kind = "answer"
		if in.Dismiss {
			resolution, kind = "dismissed", "dismiss"
		}
		if err := d.Store.ResolveQuestion(q.ID, resolution); err != nil {
			if errors.Is(err, store.ErrQuestionResolved) {
				writeErr(w, 409, "question_resolved", err.Error())
			} else {
				creativeError(w, err)
			}
			return true
		}
	} else {
		var in postQuestionReplyRequest
		if !creativeBody(w, r, &in) {
			return true
		}
		body = in.Body
		kind = "reply"
		if strings.TrimSpace(body) == "" {
			writeErr(w, 400, "empty_body", "reply must not be empty")
			return true
		}
		reopen, conflict := replyReopens(caller, q.Status, q.Type, in.Dispute)
		if conflict {
			writeErr(w, 409, "question_resolved", "question is resolved")
			return true
		}
		if in.DryRun {
			resp, err := creativeQuestionView(d, caller, q.ID)
			if err != nil {
				creativeError(w, err)
			} else {
				writeJSON(w, 200, resp)
			}
			return true
		}
		if reopen {
			if err := d.Store.ReopenQuestion(q.ID); err != nil {
				creativeError(w, err)
				return true
			}
			q.Status = "open"
		}
	}
	if body != "" {
		if _, err := d.Store.AddQuestionMessage(store.QuestionMessage{QuestionID: q.ID, Author: author, Kind: kind, Body: body}); err != nil {
			creativeError(w, err)
			return true
		}
	}
	if answer {
		err = d.Store.ClearAttention(q.ID)
	} else if q.Status == "open" {
		err = d.Store.AttentionOnEntry(q.ID, author, nil, participants)
	}
	if err != nil {
		creativeError(w, err)
		return true
	}
	if body != "" {
		if err := creativeQuestionFanOut(d, q.ID, b, author, body); err != nil {
			creativeError(w, err)
			return true
		}
	}
	resp, err := creativeQuestionView(d, caller, q.ID)
	if err != nil {
		creativeError(w, err)
		return true
	}
	status := 201
	if answer {
		status = 200
	}
	writeJSON(w, status, resp)
	return true
}
