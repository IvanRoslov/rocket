package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// agentQuestionResponse is the JSON shape of a role's Q&A thread. It mirrors
// questionResponse (task threads) with the role in place of the task, and
// reuses questionMessageResponse for thread entries so web/mobile can share
// one thread component.
type agentQuestionResponse struct {
	ID         int64                     `json:"id"`
	RoleID     string                    `json:"role_id"`
	Ordinal    int                       `json:"ordinal"`
	AskedBy    string                    `json:"asked_by"`
	Body       string                    `json:"body"`
	Context    string                    `json:"context,omitempty"`
	Status     string                    `json:"status"`
	Resolution string                    `json:"resolution,omitempty"`
	WhoseTurn  string                    `json:"whose_turn,omitempty"` // user|role
	AskedAt    int64                     `json:"asked_at"`
	ResolvedAt int64                     `json:"resolved_at,omitempty"`
	Messages   []questionMessageResponse `json:"messages"`
}

// whoseTurnAgent derives whose turn it is to speak next in a role's thread
// from the author of its last entry. The question itself counts as the first
// entry when no messages exist yet: a role-opened thread awaits the human, a
// human-opened one awaits the role. A resolved thread has no pending turn.
func whoseTurnAgent(q store.AgentQuestion, msgs []store.AgentQuestionMessage) string {
	if q.Status == "resolved" {
		return ""
	}
	if len(msgs) == 0 {
		if q.AskedBy == "" {
			return "role"
		}
		return "user"
	}
	if msgs[len(msgs)-1].Author == "" {
		// Last entry from the human.
		return "role"
	}
	return "user"
}

// buildAgentQuestionResponse loads a thread and its ordinal and assembles the
// full API response shape.
func buildAgentQuestionResponse(d Deps, q store.AgentQuestion) (agentQuestionResponse, error) {
	msgs, err := d.Store.ListAgentQuestionMessages(q.ID)
	if err != nil {
		return agentQuestionResponse{}, err
	}
	ordinal, err := d.Store.AgentQuestionOrdinal(q)
	if err != nil {
		return agentQuestionResponse{}, err
	}

	msgOut := make([]questionMessageResponse, len(msgs))
	for i, m := range msgs {
		msgOut[i] = questionMessageResponse{
			ID:        m.ID,
			Author:    m.Author,
			Kind:      m.Kind,
			Body:      m.Body,
			CreatedAt: m.CreatedAt,
		}
	}

	return agentQuestionResponse{
		ID:         q.ID,
		RoleID:     q.RoleID,
		Ordinal:    ordinal,
		AskedBy:    q.AskedBy,
		Body:       q.Body,
		Context:    q.Context,
		Status:     q.Status,
		Resolution: q.Resolution,
		WhoseTurn:  whoseTurnAgent(q, msgs),
		AskedAt:    q.AskedAt,
		ResolvedAt: q.ResolvedAt,
		Messages:   msgOut,
	}, nil
}

// registerAgentQuestionRoutes wires the role Q&A routes onto mux. Threads are
// listed and opened under the role; replies and answers are addressed by
// question id alone, mirroring /v1/questions/{id}/reply|answer for tasks.
func registerAgentQuestionRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/agents/{id}/questions", func(w http.ResponseWriter, r *http.Request) {
		handleGetAgentQuestions(w, r, d)
	})
	mux.HandleFunc("POST /v1/agents/{id}/questions", func(w http.ResponseWriter, r *http.Request) {
		handlePostAgentQuestions(w, r, d)
	})
	mux.HandleFunc("POST /v1/agent-questions/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		handlePostAgentQuestionReply(w, r, d)
	})
	mux.HandleFunc("POST /v1/agent-questions/{id}/answer", func(w http.ResponseWriter, r *http.Request) {
		handlePostAgentQuestionAnswer(w, r, d)
	})
}

// callerIsRoleInstance reports whether caller is the agent itself: its session
// is registered under the agent's own id (docs/10-agents.md).
func callerIsRoleInstance(caller *store.Session, roleID string) bool {
	return caller != nil && caller.Kind == session.AgentSessionKind && caller.ID == roleID
}

// writeAgentQuestionForbidden rejects a caller that is neither the human nor
// an instance of the role owning the thread.
func writeAgentQuestionForbidden(w http.ResponseWriter, roleID string) {
	writeErr(w, http.StatusForbidden, "forbidden",
		"only the human user or an instance of role "+roleID+" may use its question threads")
}

// deliverHumanEntry delivers a human entry in a role's thread to the agent,
// through the very same live-or-inbox path an ordinary message takes: a live
// agent session gets it injected, a dead one finds it in its inbox. The
// "[role sre Q2 reply]" frame mirrors the one task threads use, so the agent
// can tell a thread entry from a plain message at a glance.
func deliverHumanEntry(d Deps, roleID string, questionID int64, ordinal int, entry, text string) error {
	body := fmt.Sprintf("[role %s Q%d %s] %s", roleID, ordinal, entry, text)
	_, _, err := deliverToAgent(d, roleID, "", body)
	return err
}

// parseAgentQuestionID extracts and parses the {id} path value, writing a 404
// response and returning ok=false if it isn't a valid integer.
func parseAgentQuestionID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusNotFound, "agent_question_not_found", "question not found")
		return 0, false
	}
	return id, true
}

// getAgentQuestionOr404 fetches the thread, writing a 404 (or 500) response
// and returning ok=false on failure.
func getAgentQuestionOr404(w http.ResponseWriter, d Deps, id int64) (store.AgentQuestion, bool) {
	q, err := d.Store.GetAgentQuestion(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "agent_question_not_found", "question not found")
			return store.AgentQuestion{}, false
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return store.AgentQuestion{}, false
	}
	return q, true
}

// writeAgentQuestion re-reads the thread and writes it with the given status.
func writeAgentQuestion(w http.ResponseWriter, d Deps, id int64, status int) {
	q, ok := getAgentQuestionOr404(w, d, id)
	if !ok {
		return
	}
	resp, err := buildAgentQuestionResponse(d, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, status, resp)
}

// handleGetAgentQuestions serves GET /v1/agents/{id}/questions?status=open.
func handleGetAgentQuestions(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	questions, err := d.Store.ListAgentQuestions(a.ID, r.URL.Query().Get("status") == "open")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	out := make([]agentQuestionResponse, len(questions))
	for i, q := range questions {
		resp, err := buildAgentQuestionResponse(d, q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		out[i] = resp
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": out})
}

type postAgentQuestionRequest struct {
	Body    string `json:"body"`
	Context string `json:"context"`
}

// handlePostAgentQuestions serves POST /v1/agents/{id}/questions
// {body, context?} in one of two directions, decided by the caller:
//   - the human user (no X-Rocket-Session header) opens a thread addressed to
//     the role (AskedBy = ""); a `question` inbox event wakes it;
//   - an instance of that role opens a thread addressed to the human
//     (AskedBy = the instance's session id) — an escalation, no inbox event.
//
// Any other session gets 403: agent-to-agent traffic goes through rocket send.
func handlePostAgentQuestions(w http.ResponseWriter, r *http.Request, d Deps) {
	a, ok := lookupAgent(w, r, d)
	if !ok {
		return
	}

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil && !callerIsRoleInstance(caller, a.ID) {
		writeAgentQuestionForbidden(w, a.ID)
		return
	}

	var req postAgentQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Body == "" {
		writeErr(w, http.StatusBadRequest, "empty_body", "body must not be empty")
		return
	}

	qid, err := d.Store.AddAgentQuestion(store.AgentQuestion{
		RoleID:  a.ID,
		AskedBy: callerAuthor(caller),
		Body:    req.Body,
		Context: req.Context,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if caller == nil {
		q, ok := getAgentQuestionOr404(w, d, qid)
		if !ok {
			return
		}
		ordinal, err := d.Store.AgentQuestionOrdinal(q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		text := req.Body
		if req.Context != "" {
			text += "\n\n" + req.Context
		}
		if err := deliverHumanEntry(d, a.ID, qid, ordinal, "question", text); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	d.Bus.Publish("agent.question_asked", callerLabel(caller), map[string]any{
		"role_id": a.ID, "question_id": qid,
	})

	writeAgentQuestion(w, d, qid, http.StatusCreated)
}

type postAgentQuestionReplyRequest struct {
	Body string `json:"body"`
}

// handlePostAgentQuestionReply serves POST /v1/agent-questions/{id}/reply
// {body}. Either the human or an instance of the thread's role may reply. A
// human reply reaches the role as an inbox event (plus a queued message when
// an instance is live); a role reply is a thread entry the human reads via
// API/CLI.
//
// A resolved thread is final for the human (409), but the role may dispute the
// answer: its reply REOPENS the thread, so the disagreement continues with
// full context instead of in a disconnected new question — same rule as task
// questions (docs/12-tasks.md «Q&A»).
func handlePostAgentQuestionReply(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseAgentQuestionID(w, r)
	if !ok {
		return
	}
	q, ok := getAgentQuestionOr404(w, d, id)
	if !ok {
		return
	}

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil && !callerIsRoleInstance(caller, q.RoleID) {
		writeAgentQuestionForbidden(w, q.RoleID)
		return
	}

	reopen := false
	if q.Status != "open" {
		if caller == nil {
			writeErr(w, http.StatusConflict, "question_resolved", "question is already resolved")
			return
		}
		reopen = true
	}

	var req postAgentQuestionReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Body == "" {
		writeErr(w, http.StatusBadRequest, "empty_body", "body must not be empty")
		return
	}

	if reopen {
		if err := d.Store.ReopenAgentQuestion(id); err != nil {
			// Raced with something else touching the thread; surface as a
			// conflict rather than corrupting it.
			writeErr(w, http.StatusConflict, "question_resolved", "question state changed concurrently: "+err.Error())
			return
		}
	}

	if _, err := d.Store.AddAgentQuestionMessage(store.AgentQuestionMessage{
		QuestionID: id,
		Author:     callerAuthor(caller),
		Kind:       "reply",
		Body:       req.Body,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if reopen {
		d.Bus.Publish("agent.question_reopened", callerLabel(caller), map[string]any{
			"role_id": q.RoleID, "question_id": id,
		})
	}

	if caller == nil {
		ordinal, err := d.Store.AgentQuestionOrdinal(q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if err := deliverHumanEntry(d, q.RoleID, id, ordinal, "reply", req.Body); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	d.Bus.Publish("agent.question_replied", callerLabel(caller), map[string]any{
		"role_id": q.RoleID, "question_id": id,
	})

	writeAgentQuestion(w, d, id, http.StatusCreated)
}

type postAgentQuestionAnswerRequest struct {
	Body    string `json:"body"`
	Dismiss bool   `json:"dismiss"`
}

// handlePostAgentQuestionAnswer serves POST /v1/agent-questions/{id}/answer
// {body} | {dismiss:true}. Human callers only (any agent gets 403); the thread
// must be open. Dismissing resolves it without a thread message or delivery.
// Answering adds the message, resolves the thread and delivers the answer to
// the role.
func handlePostAgentQuestionAnswer(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseAgentQuestionID(w, r)
	if !ok {
		return
	}
	q, ok := getAgentQuestionOr404(w, d, id)
	if !ok {
		return
	}

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil {
		writeErr(w, http.StatusForbidden, "forbidden", "only the human user may answer questions")
		return
	}

	if q.Status != "open" {
		writeErr(w, http.StatusConflict, "question_resolved", "question is already resolved")
		return
	}

	var req postAgentQuestionAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	var resolution string
	if req.Dismiss {
		resolution = "dismissed"
		if err := d.Store.ResolveAgentQuestion(id, resolution); err != nil {
			if errors.Is(err, store.ErrQuestionResolved) {
				writeErr(w, http.StatusConflict, "question_resolved", "question is already resolved")
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	} else {
		if req.Body == "" {
			writeErr(w, http.StatusBadRequest, "empty_body", "body must not be empty")
			return
		}
		resolution = "answered"

		// Resolve first: an already-resolved thread must 409 before anything
		// is written or delivered.
		if err := d.Store.ResolveAgentQuestion(id, resolution); err != nil {
			if errors.Is(err, store.ErrQuestionResolved) {
				writeErr(w, http.StatusConflict, "question_resolved", "question is already resolved")
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		if _, err := d.Store.AddAgentQuestionMessage(store.AgentQuestionMessage{
			QuestionID: id,
			Kind:       "answer",
			Body:       req.Body,
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		ordinal, err := d.Store.AgentQuestionOrdinal(q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if err := deliverHumanEntry(d, q.RoleID, id, ordinal, "answer", req.Body); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	d.Bus.Publish("agent.question_resolved", "", map[string]any{
		"role_id": q.RoleID, "question_id": id, "resolution": resolution,
	})

	writeAgentQuestion(w, d, id, http.StatusOK)
}
