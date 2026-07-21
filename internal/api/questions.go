package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/store"
)

// questionMessageResponse is the JSON shape of a single entry in a
// question's thread.
type questionMessageResponse struct {
	ID        int64  `json:"id"`
	Author    string `json:"author,omitempty"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at"`
}

func toQuestionMessageResponse(m store.QuestionMessage) questionMessageResponse {
	return questionMessageResponse{
		ID:        m.ID,
		Author:    m.Author,
		Kind:      m.Kind,
		Body:      m.Body,
		CreatedAt: m.CreatedAt,
	}
}

// questionResponse is the JSON shape of a question and its thread as
// returned by the API.
type questionResponse struct {
	ID         int64                     `json:"id"`
	TaskID     int64                     `json:"task_id"`
	Ordinal    int                       `json:"ordinal"`
	AskedBy    string                    `json:"asked_by"`
	Body       string                    `json:"body"`
	Context    string                    `json:"context,omitempty"`
	Status     string                    `json:"status"`
	Resolution string                    `json:"resolution,omitempty"`
	WhoseTurn  string                    `json:"whose_turn,omitempty"`
	AskedAt    int64                     `json:"asked_at"`
	ResolvedAt int64                     `json:"resolved_at,omitempty"`
	Messages   []questionMessageResponse `json:"messages"`
}

// whoseTurn derives whose turn it is to speak next in a question's thread
// from the author of its last entry. The question itself (asked_by the
// orchestrator) counts as the first entry when no messages exist yet. A
// resolved question has no pending turn.
func whoseTurn(q store.Question, msgs []store.QuestionMessage) string {
	if q.Status == "resolved" {
		return ""
	}
	if len(msgs) == 0 {
		if q.AskedBy == "" {
			// User-opened thread, no reply yet: the orchestrator owes a reply.
			return "orchestrator"
		}
		// Last entry is the question itself, from the orchestrator.
		return "user"
	}
	last := msgs[len(msgs)-1]
	if last.Author == "" {
		// Last entry from the human.
		return "orchestrator"
	}
	return "user"
}

// buildQuestionResponse loads a question's thread and ordinal and assembles
// the full API response shape.
func buildQuestionResponse(d Deps, q store.Question) (questionResponse, error) {
	msgs, err := d.Store.ListQuestionMessages(q.ID)
	if err != nil {
		return questionResponse{}, err
	}
	ordinal, err := d.Store.QuestionOrdinal(q)
	if err != nil {
		return questionResponse{}, err
	}

	msgOut := make([]questionMessageResponse, len(msgs))
	for i, m := range msgs {
		msgOut[i] = toQuestionMessageResponse(m)
	}

	return questionResponse{
		ID:         q.ID,
		TaskID:     q.TaskID,
		Ordinal:    ordinal,
		AskedBy:    q.AskedBy,
		Body:       q.Body,
		Context:    q.Context,
		Status:     q.Status,
		Resolution: q.Resolution,
		WhoseTurn:  whoseTurn(q, msgs),
		AskedAt:    q.AskedAt,
		ResolvedAt: q.ResolvedAt,
		Messages:   msgOut,
	}, nil
}

// registerQuestionRoutes wires the /v1/questions routes onto mux. The
// /v1/tasks/{id}/questions routes are wired by registerTaskRoutes in
// tasks.go, since they share that path prefix.
func registerQuestionRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/questions", func(w http.ResponseWriter, r *http.Request) {
		handleGetAllQuestions(w, r, d)
	})
	mux.HandleFunc("POST /v1/questions/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		handlePostQuestionReply(w, r, d)
	})
	mux.HandleFunc("POST /v1/questions/{id}/answer", func(w http.ResponseWriter, r *http.Request) {
		handlePostQuestionAnswer(w, r, d)
	})
}

// globalQuestionResponse is one entry of GET /v1/questions: a full question
// thread plus the task/project context the per-task endpoints get for free
// from their URL.
type globalQuestionResponse struct {
	questionResponse
	TaskTitle        string `json:"task_title"`
	ProjectID        string `json:"project_id"`
	ProjectName      string `json:"project_name"`
	OrchestratorName string `json:"orchestrator_name,omitempty"`
}

// handleGetAllQuestions serves GET /v1/questions: every open question across
// all tasks, enriched with task title, project and orchestrator name for the
// dashboard's global Questions page. Questions whose task has vanished are
// skipped rather than failing the whole listing.
func handleGetAllQuestions(w http.ResponseWriter, r *http.Request, d Deps) {
	qs, err := d.Store.ListAllOpenQuestions()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	tasks := map[int64]store.Task{}
	projectNames := map[string]string{}
	orchNames := map[string]string{}

	out := make([]globalQuestionResponse, 0, len(qs))
	for _, q := range qs {
		task, ok := tasks[q.TaskID]
		if !ok {
			task, err = d.Store.GetTask(q.TaskID)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			tasks[q.TaskID] = task
		}

		resp, err := buildQuestionResponse(d, q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		g := globalQuestionResponse{
			questionResponse: resp,
			TaskTitle:        task.Title,
			ProjectID:        task.ProjectID,
		}
		if name, ok := projectNames[task.ProjectID]; ok {
			g.ProjectName = name
		} else if p, err := d.Store.GetProject(task.ProjectID); err == nil {
			projectNames[task.ProjectID] = p.Name
			g.ProjectName = p.Name
		}
		if task.SessionID != "" {
			if name, ok := orchNames[task.SessionID]; ok {
				g.OrchestratorName = name
			} else if sess, err := d.Store.GetSession(task.SessionID); err == nil {
				orchNames[task.SessionID] = sess.TmuxName
				g.OrchestratorName = sess.TmuxName
			}
		}
		out = append(out, g)
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": out})
}

// parseQuestionID extracts and parses the {id} path value, writing a 404
// response and returning ok=false if it isn't a valid integer (mirroring
// how an unparseable id can never match a stored row anyway).
func parseQuestionID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusNotFound, "question_not_found", "question not found")
		return 0, false
	}
	return id, true
}

// getQuestionOr404 fetches the question, writing a 404 question_not_found
// (or 500) response and returning ok=false on failure.
func getQuestionOr404(w http.ResponseWriter, d Deps, id int64) (store.Question, bool) {
	q, err := d.Store.GetQuestion(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "question_not_found", "question not found")
			return store.Question{}, false
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return store.Question{}, false
	}
	return q, true
}

// deliverToOrchestrator enqueues body to task's orchestrator session, mirroring
// the enqueue pattern used by POST /v1/messages: insert with status "queued",
// publish message.queued, and wake the recipient's delivery worker. If the
// task has no attached session, or that session is no longer active, delivery
// is skipped (logged) — the Q&A record itself still updates regardless.
func deliverToOrchestrator(d Deps, task store.Task, body string) error {
	if task.SessionID == "" {
		return nil
	}

	sess, err := d.Store.GetSession(task.SessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			slog.Warn("api: question delivery target session not found, skipping", "task_id", task.ID, "session_id", task.SessionID)
			return nil
		}
		return err
	}
	if isSessionTerminal(sess.State) {
		slog.Warn("api: question delivery target session is terminal, skipping", "task_id", task.ID, "session_id", task.SessionID, "state", sess.State)
		return nil
	}

	id, err := d.Store.AddMessage(store.Message{ToSession: task.SessionID, Body: body})
	if err != nil {
		return err
	}

	if d.Bus != nil {
		d.Bus.Publish("message.queued", task.SessionID, map[string]any{
			"id": id, "from": "", "to": task.SessionID,
		})
	}
	if d.Queue != nil {
		d.Queue.Wake(task.SessionID)
	} else {
		slog.Warn("api: question message queued with nil Queue, will not be delivered until daemon restart", "id", id, "to", task.SessionID)
	}
	return nil
}

type postQuestionRequest struct {
	Body    string `json:"body"`
	Context string `json:"context"`
}

// handlePostTaskQuestions serves POST /v1/tasks/{id}/questions
// {body, context?}, in one of two directions, and only on a root task:
//   - The task's own orchestrator may open a question addressed to the
//     human (AskedBy = the orchestrator's session id). No injection — the
//     human reads it in the CLI/dashboard.
//   - The human user (no X-Rocket-Session header) may open a question
//     addressed to the task's orchestrator (AskedBy = ""). The question body
//     is injected into the orchestrator's message queue so it reaches the
//     agent.
//
// A worker caller, or an orchestrator not owning the task, gets 403.
func handlePostTaskQuestions(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	task, ok := getTaskOr404(w, d, id)
	if !ok {
		return
	}

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil && (caller.Kind != "orchestrator" || task.SessionID != caller.ID) {
		writeErr(w, http.StatusForbidden, "forbidden", "only the human user or the task's own orchestrator may ask questions")
		return
	}
	if task.ParentID != 0 {
		writeErr(w, http.StatusBadRequest, "not_root_task", "questions may only be asked on root tasks")
		return
	}

	var req postQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Body == "" {
		writeErr(w, http.StatusBadRequest, "empty_body", "body must not be empty")
		return
	}

	qid, err := d.Store.AddQuestion(store.Question{
		TaskID:  id,
		AskedBy: callerAuthor(caller),
		Body:    req.Body,
		Context: req.Context,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if caller == nil {
		q, ok := getQuestionOr404(w, d, qid)
		if !ok {
			return
		}
		ordinal, err := d.Store.QuestionOrdinal(q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		body := fmt.Sprintf("[task #%d Q%d question] %s", task.ID, ordinal, req.Body)
		if req.Context != "" {
			body += "\n\n" + req.Context
		}
		if err := deliverToOrchestrator(d, task, body); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	d.Bus.Publish("task.question_asked", callerLabel(caller), map[string]any{
		"task_id": id, "question_id": qid,
	})

	q, ok := getQuestionOr404(w, d, qid)
	if !ok {
		return
	}
	resp, err := buildQuestionResponse(d, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// handleGetTaskQuestions serves GET /v1/tasks/{id}/questions?status=open.
func handleGetTaskQuestions(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	if _, ok := getTaskOr404(w, d, id); !ok {
		return
	}

	openOnly := r.URL.Query().Get("status") == "open"
	questions, err := d.Store.ListQuestions(id, openOnly)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	out := make([]questionResponse, len(questions))
	for i, q := range questions {
		resp, err := buildQuestionResponse(d, q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		out[i] = resp
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": out})
}

type postQuestionReplyRequest struct {
	Body string `json:"body"`
}

// handlePostQuestionReply serves POST /v1/questions/{id}/reply {body}. The
// question must be open. Either the human user or the task's own
// orchestrator may reply (a worker caller gets 403). A human reply is
// delivered to the orchestrator via the message queue; an orchestrator
// reply is a thread-only entry the human sees in the CLI/dashboard.
func handlePostQuestionReply(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseQuestionID(w, r)
	if !ok {
		return
	}
	q, ok := getQuestionOr404(w, d, id)
	if !ok {
		return
	}

	task, ok := getTaskOr404(w, d, q.TaskID)
	if !ok {
		return
	}

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil && (caller.Kind != "orchestrator" || task.SessionID != caller.ID) {
		writeErr(w, http.StatusForbidden, "forbidden", "only the human user or the task's own orchestrator may reply")
		return
	}

	// A resolved question is final for the human (409), but the task's own
	// orchestrator may dispute the final answer: its reply REOPENS the
	// thread (status back to open, resolution cleared) so the disagreement
	// continues in the same thread with full context, instead of a
	// disconnected new question. See docs/12-tasks.md «Q&A».
	reopen := false
	if q.Status != "open" {
		if caller == nil {
			writeErr(w, http.StatusConflict, "question_resolved", "question is already resolved")
			return
		}
		reopen = true
	}

	var req postQuestionReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Body == "" {
		writeErr(w, http.StatusBadRequest, "empty_body", "body must not be empty")
		return
	}

	if reopen {
		if err := d.Store.ReopenQuestion(id); err != nil {
			// Raced with something else touching the question; surface as
			// conflict rather than corrupting the thread.
			writeErr(w, http.StatusConflict, "question_resolved", "question state changed concurrently: "+err.Error())
			return
		}
	}

	if _, err := d.Store.AddQuestionMessage(store.QuestionMessage{
		QuestionID: id,
		Author:     callerAuthor(caller),
		Kind:       "reply",
		Body:       req.Body,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if reopen {
		d.Bus.Publish("task.question_reopened", callerLabel(caller), map[string]any{
			"task_id": task.ID, "question_id": id,
		})
	}

	if caller == nil {
		ordinal, err := d.Store.QuestionOrdinal(q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		prefixed := fmt.Sprintf("[task #%d Q%d reply] %s", task.ID, ordinal, req.Body)
		if err := deliverToOrchestrator(d, task, prefixed); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	d.Bus.Publish("task.question_replied", callerLabel(caller), map[string]any{
		"task_id": task.ID, "question_id": id,
	})

	updated, ok := getQuestionOr404(w, d, id)
	if !ok {
		return
	}
	resp, err := buildQuestionResponse(d, updated)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

type postQuestionAnswerRequest struct {
	Body    string `json:"body"`
	Dismiss bool   `json:"dismiss"`
}

// handlePostQuestionAnswer serves POST /v1/questions/{id}/answer
// {body} | {dismiss:true}. Human callers only (any agent gets 403); the
// question must be open. Dismissing resolves the question without adding a
// thread message or delivering anything. Answering adds a thread message,
// resolves the question, and delivers the answer to the orchestrator.
func handlePostQuestionAnswer(w http.ResponseWriter, r *http.Request, d Deps) {
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
		writeErr(w, http.StatusForbidden, "forbidden", "only the human user may answer questions")
		return
	}

	if q.Status != "open" {
		writeErr(w, http.StatusConflict, "question_resolved", "question is already resolved")
		return
	}

	task, ok := getTaskOr404(w, d, q.TaskID)
	if !ok {
		return
	}

	var req postQuestionAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	var resolution string
	if req.Dismiss {
		resolution = "dismissed"
		if err := d.Store.ResolveQuestion(id, resolution); err != nil {
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

		// Resolve first — if it fails with already-resolved, return 409 immediately.
		// Only after successful resolve do we add the message, deliver, and publish event.
		if err := d.Store.ResolveQuestion(id, resolution); err != nil {
			if errors.Is(err, store.ErrQuestionResolved) {
				writeErr(w, http.StatusConflict, "question_resolved", "question is already resolved")
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		if _, err := d.Store.AddQuestionMessage(store.QuestionMessage{
			QuestionID: id,
			Kind:       "answer",
			Body:       req.Body,
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		ordinal, err := d.Store.QuestionOrdinal(q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		prefixed := fmt.Sprintf("[task #%d Q%d answer] %s", task.ID, ordinal, req.Body)
		if err := deliverToOrchestrator(d, task, prefixed); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	d.Bus.Publish("task.question_resolved", "", map[string]any{
		"task_id": task.ID, "question_id": id, "resolution": resolution,
	})

	updated, ok := getQuestionOr404(w, d, id)
	if !ok {
		return
	}
	resp, err := buildQuestionResponse(d, updated)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
