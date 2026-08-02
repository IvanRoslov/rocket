package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/IvanRoslov/rocket/internal/store"
)

// questionMessageResponse is the JSON shape of a single entry in a
// question's thread.
type questionMessageResponse struct {
	ID     int64  `json:"id"`
	Author string `json:"author,omitempty"`
	Kind   string `json:"kind"`
	Body   string `json:"body"`
	// AddressedTo narrows who is expected to respond. Empty means every
	// participant except the author.
	AddressedTo []string `json:"addressed_to,omitempty"`
	CreatedAt   int64    `json:"created_at"`
}

// wireAuthor renders a stored participant id for the current API contract.
// The store canonicalised the human to store.ParticipantHuman in migration
// 0009, but web and mobile still read an empty author as "you"; flipping the
// wire is deliberately left to subtask #731, which lands it together with the
// clients. Until then this keeps the contract byte-identical.
func wireAuthor(author string) string {
	if store.IsHuman(author) {
		return ""
	}
	return author
}

func toQuestionMessageResponse(m store.QuestionMessage) questionMessageResponse {
	return questionMessageResponse{
		ID:          m.ID,
		Author:      wireAuthor(m.Author),
		Kind:        m.Kind,
		Body:        m.Body,
		AddressedTo: m.AddressedTo,
		CreatedAt:   m.CreatedAt,
	}
}

// questionResponse is the JSON shape of a question and its thread as
// returned by the API.
type questionResponse struct {
	ID         int64  `json:"id"`
	TaskID     int64  `json:"task_id"`
	Ordinal    int    `json:"ordinal"`
	AskedBy    string `json:"asked_by"`
	Body       string `json:"body"`
	Context    string `json:"context,omitempty"`
	Status     string `json:"status"`
	Resolution string `json:"resolution,omitempty"`
	// Participants is everyone taking part in the thread; WaitingOn is the
	// subset expected to speak next; YourTurn says whether the caller is one
	// of them. WhoseTurn is the pre-participant field the clients still read,
	// derived from WaitingOn until subtask #736 retires it.
	Participants []string                  `json:"participants"`
	WaitingOn    []string                  `json:"waiting_on"`
	YourTurn     bool                      `json:"your_turn"`
	WhoseTurn    string                    `json:"whose_turn,omitempty"`
	AskedAt      int64                     `json:"asked_at"`
	ResolvedAt   int64                     `json:"resolved_at,omitempty"`
	Messages     []questionMessageResponse `json:"messages"`
}

// buildQuestionResponse loads a thread's messages, participants and ordinal
// and assembles the full API response. caller decides your_turn, the one
// caller-relative field in the shape.
func buildQuestionResponse(d Deps, caller *store.Session, q store.Question) (questionResponse, error) {
	msgs, err := d.Store.ListQuestionMessages(q.ID)
	if err != nil {
		return questionResponse{}, err
	}
	participants, err := d.Store.ListParticipants(q.ID)
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

	waiting := waitingOn(q, msgs, participants)
	return questionResponse{
		ID:           q.ID,
		TaskID:       q.TaskID,
		Ordinal:      ordinal,
		AskedBy:      q.AskedBy,
		Body:         q.Body,
		Context:      q.Context,
		Status:       q.Status,
		Resolution:   q.Resolution,
		Participants: participants,
		WaitingOn:    waiting,
		YourTurn:     contains(waiting, callerParticipant(caller)),
		WhoseTurn:    whoseTurnCompat(waiting, "orchestrator"),
		AskedAt:      q.AskedAt,
		ResolvedAt:   q.ResolvedAt,
		Messages:     msgOut,
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
	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}

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

		resp, err := buildQuestionResponse(d, caller, q)
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

type postQuestionRequest struct {
	Body    string `json:"body"`
	Context string `json:"context"`
	// To narrows who is expected to respond. Its ids join the thread as
	// participants and are stored as the message's addressed_to.
	To []string `json:"to"`
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
	subj := threadSubject{TaskID: task.ID, Counterpart: task.SessionID}
	if !canOpenThread(d, caller, subj) {
		writeErr(w, http.StatusForbidden, "forbidden",
			"only the human user, a persistent agent or the task's own orchestrator may ask questions")
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

	// Every thread starts with the human and the subject's own counterpart in
	// it, so a human-opened thread always has somebody to reach and the
	// orchestrator never has to be added by hand. An unattached task has no
	// counterpart to seed: seeding "" would be canonicalised to "human" and
	// invent a participant that is not there.
	author := callerParticipant(caller)
	seed := []string{store.ParticipantHuman, author}
	if task.SessionID != "" {
		seed = append(seed, task.SessionID)
	}
	seed = append(seed, req.To...)
	if err := d.Store.AddParticipants(qid, seed...); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	q, ok := getQuestionOr404(w, d, qid)
	if !ok {
		return
	}
	ordinal, err := d.Store.QuestionOrdinal(q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	participants, err := d.Store.ListParticipants(qid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	text := req.Body
	if req.Context != "" {
		text += "\n\n" + req.Context
	}
	if err := participantFanOut(d, subj, ordinal, "question", author, text, participants); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	d.Bus.Publish("task.question_asked", callerLabel(caller), map[string]any{
		"task_id": id, "question_id": qid,
	})

	// q was re-read above, after the participants were seeded, so the
	// response already carries them.
	resp, err := buildQuestionResponse(d, caller, q)
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

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
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
		resp, err := buildQuestionResponse(d, caller, q)
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
	// To narrows who is expected to respond and joins its ids to the thread.
	To []string `json:"to"`
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
	subj := threadSubject{TaskID: task.ID, Counterpart: task.SessionID}
	participants, err := d.Store.ListParticipants(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !canPostToThread(d, caller, subj, participants) {
		writeErr(w, http.StatusForbidden, "forbidden",
			"only a participant of this thread may reply")
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

	// Writing into a thread joins it, and so does being addressed.
	if err := d.Store.AddParticipants(id, append([]string{author}, req.To...)...); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if reopen {
		d.Bus.Publish("task.question_reopened", callerLabel(caller), map[string]any{
			"task_id": task.ID, "question_id": id,
		})
	}

	ordinal, err := d.Store.QuestionOrdinal(q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	// Re-read: req.To has just joined and must be notified too.
	recipients, err := d.Store.ListParticipants(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if err := participantFanOut(d, subj, ordinal, "reply", author, req.Body, recipients); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	d.Bus.Publish("task.question_replied", callerLabel(caller), map[string]any{
		"task_id": task.ID, "question_id": id,
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
	writeJSON(w, http.StatusCreated, resp)
}

type postQuestionAnswerRequest struct {
	Body    string `json:"body"`
	Dismiss bool   `json:"dismiss"`
	// To narrows who is expected to respond and joins its ids to the thread.
	// On an answer it only records intent: a resolved thread waits on nobody.
	To []string `json:"to"`
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
	if !canAnswerThread(d, caller) {
		writeErr(w, http.StatusForbidden, "forbidden",
			"only the human user or a persistent agent may answer; use reply")
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

		// The author used to be left empty, which was only correct while the
		// human was the only party allowed to answer.
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

		ordinal, err := d.Store.QuestionOrdinal(q)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		recipients, err := d.Store.ListParticipants(id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		subj := threadSubject{TaskID: task.ID, Counterpart: task.SessionID}
		if err := participantFanOut(d, subj, ordinal, "answer", author, req.Body, recipients); err != nil {
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
	resp, err := buildQuestionResponse(d, caller, updated)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
