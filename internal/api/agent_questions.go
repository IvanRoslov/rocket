package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/IvanRoslov/rocket/internal/store"
)

// agentQuestionResponse is the JSON shape of a role's Q&A thread. It mirrors
// questionResponse (task threads) with the role in place of the task, and
// reuses questionMessageResponse for thread entries so web/mobile can share
// one thread component.
type agentQuestionResponse struct {
	ID         int64  `json:"id"`
	RoleID     string `json:"role_id"`
	Ordinal    int    `json:"ordinal"`
	AskedBy    string `json:"asked_by"`
	Body       string `json:"body"`
	Context    string `json:"context,omitempty"`
	Status     string `json:"status"`
	Resolution string `json:"resolution,omitempty"`
	// Participants, WaitingOn and YourTurn mirror questionResponse; WhoseTurn
	// keeps the role vocabulary (user|role) the clients already read.
	Participants []string `json:"participants"`
	// Attention is the stored "whose turn" set; WaitingOn is the same set
	// under its original name (see questionResponse).
	Attention  []string                  `json:"attention"`
	WaitingOn  []string                  `json:"waiting_on"`
	YourTurn   bool                      `json:"your_turn"`
	WhoseTurn  string                    `json:"whose_turn,omitempty"` // user|role
	Type       string                    `json:"type"`
	Options    []string                  `json:"options,omitempty"`
	LocalRef   string                    `json:"local_ref"`
	AskedAt    int64                     `json:"asked_at"`
	ResolvedAt int64                     `json:"resolved_at,omitempty"`
	Messages   []questionMessageResponse `json:"messages"`
}

// dryRunAgentQuestionResponse mirrors dryRunQuestionResponse for role threads.
type dryRunAgentQuestionResponse struct {
	agentQuestionResponse
	DryRun bool   `json:"dry_run"`
	Echo   string `json:"echo"`
}

// writeDryRunAgentQuestion answers a dry run on a role thread: the thread as
// it stands plus the target echo, with nothing written or delivered.
func writeDryRunAgentQuestion(w http.ResponseWriter, d Deps, caller *store.Session, q store.AgentQuestion, echo string) {
	resp, err := buildAgentQuestionResponse(d, caller, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dryRunAgentQuestionResponse{agentQuestionResponse: resp, DryRun: true, Echo: echo})
}

// buildAgentQuestionResponse loads a role thread's messages, participants and
// ordinal and assembles the full API response. Messages and participants come
// from the unified DAO: migration 0009 put role threads in the same tables, and
// only the unified QuestionMessage carries AddressedTo. caller decides
// your_turn.
func buildAgentQuestionResponse(d Deps, caller *store.Session, q store.AgentQuestion) (agentQuestionResponse, error) {
	msgs, err := d.Store.ListQuestionMessages(q.ID)
	if err != nil {
		return agentQuestionResponse{}, err
	}
	participants, err := d.Store.ListParticipants(q.ID)
	if err != nil {
		return agentQuestionResponse{}, err
	}
	ordinal, err := d.Store.AgentQuestionOrdinal(q)
	if err != nil {
		return agentQuestionResponse{}, err
	}

	msgOut := make([]questionMessageResponse, len(msgs))
	for i, m := range msgs {
		msgOut[i] = toQuestionMessageResponse(m)
	}

	attention, err := threadAttention(d, store.Question{ID: q.ID, Status: q.Status})
	if err != nil {
		return agentQuestionResponse{}, err
	}

	return agentQuestionResponse{
		ID:           q.ID,
		RoleID:       q.RoleID,
		Ordinal:      ordinal,
		AskedBy:      wireParticipant(q.AskedBy),
		Body:         q.Body,
		Context:      q.Context,
		Status:       q.Status,
		Resolution:   q.Resolution,
		Participants: participants,
		Attention:    attention,
		WaitingOn:    attention,
		YourTurn:     contains(attention, callerParticipant(caller)),
		WhoseTurn:    whoseTurnCompat(attention, "role"),
		Type:         q.Type,
		Options:      q.Options,
		LocalRef:     threadLocalRef(threadSubject{RoleID: q.RoleID}, ordinal),
		AskedAt:      q.AskedAt,
		ResolvedAt:   q.ResolvedAt,
		Messages:     msgOut,
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
func writeAgentQuestion(w http.ResponseWriter, d Deps, caller *store.Session, id int64, status int) {
	q, ok := getAgentQuestionOr404(w, d, id)
	if !ok {
		return
	}
	resp, err := buildAgentQuestionResponse(d, caller, q)
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

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}

	questions, err := d.Store.ListAgentQuestions(a.ID, r.URL.Query().Get("status") == "open")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	subj := threadSubject{RoleID: a.ID, Counterpart: a.ID}
	out := make([]agentQuestionResponse, 0, len(questions))
	for _, q := range questions {
		participants, err := d.Store.ListParticipants(q.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if !canReadThread(d, caller, subj, participants) {
			continue
		}
		resp, err := buildAgentQuestionResponse(d, caller, q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": out})
}

type postAgentQuestionRequest struct {
	Body    string   `json:"body"`
	Context string   `json:"context"`
	To      []string `json:"to"`
	Type    string   `json:"type"`
	Options []string `json:"options"`
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
	subj := threadSubject{RoleID: a.ID, Counterpart: a.ID}
	if !canOpenThread(d, caller, subj) {
		writeErr(w, http.StatusForbidden, "forbidden",
			"only the human user or a persistent agent may open a role question thread")
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

	threadType, ok := normalizeThreadType(w, req.Type)
	if !ok {
		return
	}
	newQ := store.AgentQuestion{
		RoleID:      a.ID,
		AskedBy:     callerAuthor(caller),
		Body:        req.Body,
		Context:     req.Context,
		AddressedTo: req.To,
		Type:        threadType,
		Options:     req.Options,
	}
	if threadType == store.QuestionTypeFYI {
		newQ.Status = "resolved"
		newQ.Resolution = store.QuestionResolutionFYI
		newQ.ResolvedAt = time.Now().Unix()
	}
	qid, err := d.Store.AddAgentQuestion(newQ)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	author := callerParticipant(caller)
	if err := d.Store.AddParticipants(qid,
		append([]string{store.ParticipantHuman, author, a.ID}, req.To...)...); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	q, ok := getAgentQuestionOr404(w, d, qid)
	if !ok {
		return
	}
	ordinal, err := d.Store.AgentQuestionOrdinal(q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	participants, err := d.Store.ListParticipants(qid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if threadType != store.QuestionTypeFYI {
		if err := d.Store.AttentionOnOpen(qid, author, req.To, participants); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}
	text := req.Body
	if req.Context != "" {
		text += "\n\n" + req.Context
	}
	if err := participantFanOut(d, subj, ordinal, "question", author, text, participants); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	d.Bus.Publish("agent.question_asked", callerLabel(caller), map[string]any{
		"role_id": a.ID, "question_id": qid,
	})

	writeAgentQuestion(w, d, caller, qid, http.StatusCreated)
}

type postAgentQuestionReplyRequest struct {
	Body   string   `json:"body"`
	To     []string `json:"to"`
	Join   bool     `json:"join"`
	DryRun bool     `json:"dry_run"`
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
	subj := threadSubject{RoleID: q.RoleID, Counterpart: q.RoleID}
	participants, err := d.Store.ListParticipants(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	ordinal, err := d.Store.AgentQuestionOrdinal(q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
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

	participants, ok = enforceThreadGuard(w, d, caller, subj,
		store.Question{ID: q.ID, RoleID: q.RoleID, Body: q.Body, Status: q.Status},
		ordinal, "", participants, req.Join)
	if !ok {
		return
	}

	reopen := false
	if q.Status != "open" {
		if caller == nil && q.Type != store.QuestionTypeFYI {
			writeErr(w, http.StatusConflict, "question_resolved", "question is already resolved")
			return
		}
		reopen = true
	}

	if req.DryRun {
		writeDryRunAgentQuestion(w, d, caller, q, threadEcho(subj, ordinal, q.Body, ""))
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

	// The unified DAO, not the facade: only store.QuestionMessage carries
	// AddressedTo, and since migration 0009 both live in the same table.
	author := callerParticipant(caller)
	if _, err := d.Store.AddQuestionMessage(store.QuestionMessage{
		QuestionID:  id,
		Author:      author,
		Kind:        "reply",
		Body:        req.Body,
		AddressedTo: req.To,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if err := d.Store.AddParticipants(id, append([]string{author}, req.To...)...); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if reopen {
		d.Bus.Publish("agent.question_reopened", callerLabel(caller), map[string]any{
			"role_id": q.RoleID, "question_id": id,
		})
	}

	recipients, err := d.Store.ListParticipants(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if err := d.Store.AttentionOnEntry(id, author, req.To, recipients); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if err := participantFanOut(d, subj, ordinal, "reply", author, req.Body, recipients); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	d.Bus.Publish("agent.question_replied", callerLabel(caller), map[string]any{
		"role_id": q.RoleID, "question_id": id,
	})

	writeAgentQuestion(w, d, caller, id, http.StatusCreated)
}

type postAgentQuestionAnswerRequest struct {
	Body    string   `json:"body"`
	Dismiss bool     `json:"dismiss"`
	To      []string `json:"to"`
	Choose  int      `json:"choose"`
	Join    bool     `json:"join"`
	DryRun  bool     `json:"dry_run"`
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
	if !canAnswerThread(d, caller) {
		writeErr(w, http.StatusForbidden, "forbidden",
			"only the human user or a persistent agent may answer; use reply")
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
	unified := store.Question{ID: q.ID, RoleID: q.RoleID, Body: q.Body, Status: q.Status, Options: q.Options}
	req.Body, ok = chooseOptionBody(w, unified, req.Choose, req.Body)
	if !ok {
		return
	}

	subj := threadSubject{RoleID: q.RoleID, Counterpart: q.RoleID}
	ordinal, err := d.Store.AgentQuestionOrdinal(q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	participants, err := d.Store.ListParticipants(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if _, ok := enforceThreadGuard(w, d, caller, subj, unified, ordinal, "", participants, req.Join); !ok {
		return
	}

	if req.DryRun {
		writeDryRunAgentQuestion(w, d, caller, q, threadEcho(subj, ordinal, q.Body, ""))
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

		author := callerParticipant(caller)
		if _, err := d.Store.AddQuestionMessage(store.QuestionMessage{
			QuestionID:  id,
			Author:      author,
			Kind:        "answer",
			Body:        req.Body,
			AddressedTo: req.To,
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		if err := d.Store.AddParticipants(id, append([]string{author}, req.To...)...); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		recipients, err := d.Store.ListParticipants(id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if err := participantFanOut(d, subj, ordinal, "answer", author, req.Body, recipients); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	if err := d.Store.ClearAttention(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	d.Bus.Publish("agent.question_resolved", "", map[string]any{
		"role_id": q.RoleID, "question_id": id, "resolution": resolution,
	})

	writeAgentQuestion(w, d, caller, id, http.StatusOK)
}
