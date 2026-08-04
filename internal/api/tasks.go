package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/IvanRoslov/rocket/internal/session"
	"github.com/IvanRoslov/rocket/internal/store"
)

// validDocKinds and validLogKinds bound the "kind" field accepted by the
// task docs and task log write endpoints, respectively.
var (
	validDocKinds = map[string]bool{"spec": true, "plan": true, "report": true, "doc": true}
	validLogKinds = map[string]bool{"decision": true, "problem": true, "note": true, "status": true}

	// startMu serializes task start attempts within this daemon process.
	// This is a coarse-grained, process-level lock sufficient at this scale
	// to prevent two concurrent starts from spawning two orchestrators for
	// the same task. Each start caller must hold the lock for the entire
	// start operation, re-fetching the task within the lock to detect if
	// a concurrent caller already started it.
	startMu sync.Mutex
)

// taskResponse is the JSON shape of a task as returned by the API.
type taskResponse struct {
	ID          int64  `json:"id"`
	ParentID    int64  `json:"parent_id,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	ProjectID   string `json:"project_id"`
	RepoID      string `json:"repo_id,omitempty"`
	Status      string `json:"status"`
	FeatureSlug string `json:"feature_slug,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	CompletedAt int64  `json:"completed_at,omitempty"`
	// Open-question annotations (docs/superpowers/specs/2026-07-21-questions-
	// visibility-design.md §1): populated by list/board/detail handlers from
	// one taskQuestionCounts(d) aggregate, not per-task queries.
	OpenQuestions         int `json:"open_questions"`
	QuestionsAwaitingUser int `json:"questions_awaiting_user"`
	// WaitingTerminal reports that the task's session has been sitting on
	// interactive input (a pending quiz, or activity "waiting_input") longer
	// than the configured threshold — nothing moves until somebody types.
	// Derived on every read from the session's activity and the clock, never
	// persisted.
	WaitingTerminal bool `json:"waiting_terminal"`
}

func toTaskResponse(t store.Task) taskResponse {
	return taskResponse{
		ID:          t.ID,
		ParentID:    t.ParentID,
		Title:       t.Title,
		Description: t.Description,
		ProjectID:   t.ProjectID,
		RepoID:      t.RepoID,
		Status:      t.Status,
		FeatureSlug: t.FeatureSlug,
		SessionID:   t.SessionID,
		CreatedBy:   t.CreatedBy,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		CompletedAt: t.CompletedAt,
	}
}

// taskSessionResponse is the JSON shape of the session summary embedded in
// GET /v1/tasks/{id}.
type taskSessionResponse struct {
	ID       string   `json:"id"`
	TmuxName string   `json:"tmux_name"`
	Attach   []string `json:"attach"`
}

// subtaskResponse extends taskResponse with PR/CI fields from the session.
type subtaskResponse struct {
	taskResponse
	PRNumber int    `json:"pr_number,omitempty"`
	PRState  string `json:"pr_state,omitempty"`
	CIState  string `json:"ci_state,omitempty"`
}

// toTaskSessionResponse builds the session summary for a task's session_id.
// Returns (nil, nil) if the session no longer exists (e.g. cleaned up).
func toTaskSessionResponse(d Deps, sessionID string) (*taskSessionResponse, error) {
	sess, err := d.Store.GetSession(sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var attach []string
	if d.Manager != nil {
		attach, err = d.Manager.AttachCommand(sessionID)
		if err != nil {
			attach = nil
		}
	}
	return &taskSessionResponse{ID: sess.ID, TmuxName: sess.TmuxName, Attach: attach}, nil
}

// taskDetailResponse is the JSON shape of GET /v1/tasks/{id}.
type taskDetailResponse struct {
	taskResponse
	Subtasks []subtaskResponse    `json:"subtasks"`
	Session  *taskSessionResponse `json:"session,omitempty"`
}

// taskBoard is the JSON shape of the "board" grouping returned by GET
// /v1/tasks?board=true: tasks bucketed by status.
type taskBoard struct {
	Backlog    []taskResponse `json:"backlog"`
	InProgress []taskResponse `json:"in_progress"`
	Review     []taskResponse `json:"review"`
	Done       []taskResponse `json:"done"`
	Cancelled  []taskResponse `json:"cancelled"`
}

// annotateQuestionCounts fills the open-question fields on tr from counts.
func annotateQuestionCounts(tr *taskResponse, counts map[int64]store.QuestionCounts) {
	c := counts[tr.ID]
	tr.OpenQuestions = c.Open
	tr.QuestionsAwaitingUser = c.AwaitingUser
}

func toTaskBoard(tasks []store.Task, counts map[int64]store.QuestionCounts, waiting waitingTerminal) taskBoard {
	b := taskBoard{
		Backlog:    []taskResponse{},
		InProgress: []taskResponse{},
		Review:     []taskResponse{},
		Done:       []taskResponse{},
		Cancelled:  []taskResponse{},
	}
	for _, t := range tasks {
		tr := toTaskResponse(t)
		annotateQuestionCounts(&tr, counts)
		annotateWaitingTerminal(&tr, waiting)
		switch t.Status {
		case "backlog":
			b.Backlog = append(b.Backlog, tr)
		case "in_progress":
			b.InProgress = append(b.InProgress, tr)
		case "review":
			b.Review = append(b.Review, tr)
		case "done":
			b.Done = append(b.Done, tr)
		case "cancelled":
			b.Cancelled = append(b.Cancelled, tr)
		}
	}
	return b
}

// registerTaskRoutes wires the /v1/tasks routes onto mux.
func registerTaskRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		handleListTasks(w, r, d)
	})
	mux.HandleFunc("POST /v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		handlePostTask(w, r, d)
	})
	mux.HandleFunc("GET /v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleGetTask(w, r, d)
	})
	mux.HandleFunc("PATCH /v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		handlePatchTask(w, r, d)
	})
	mux.HandleFunc("POST /v1/tasks/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		handlePostTaskCancel(w, r, d)
	})
	mux.HandleFunc("POST /v1/tasks/{id}/start", func(w http.ResponseWriter, r *http.Request) {
		handlePostTaskStart(w, r, d)
	})
	mux.HandleFunc("GET /v1/tasks/{id}/docs", func(w http.ResponseWriter, r *http.Request) {
		handleGetTaskDocs(w, r, d)
	})
	mux.HandleFunc("PUT /v1/tasks/{id}/docs", func(w http.ResponseWriter, r *http.Request) {
		handlePutTaskDocs(w, r, d)
	})
	mux.HandleFunc("GET /v1/tasks/{id}/log", func(w http.ResponseWriter, r *http.Request) {
		handleGetTaskLog(w, r, d)
	})
	mux.HandleFunc("POST /v1/tasks/{id}/log", func(w http.ResponseWriter, r *http.Request) {
		handlePostTaskLog(w, r, d)
	})
	mux.HandleFunc("POST /v1/tasks/{id}/questions", func(w http.ResponseWriter, r *http.Request) {
		handlePostTaskQuestions(w, r, d)
	})
	mux.HandleFunc("GET /v1/tasks/{id}/questions", func(w http.ResponseWriter, r *http.Request) {
		handleGetTaskQuestions(w, r, d)
	})
}

// parseTaskID extracts and parses the {id} path value, writing a 400
// response and returning ok=false if it isn't a valid integer.
func parseTaskID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid task id")
		return 0, false
	}
	return id, true
}

// getTaskOr404 fetches the task, writing a 404 task_not_found (or 500)
// response and returning ok=false on failure.
func getTaskOr404(w http.ResponseWriter, d Deps, id int64) (store.Task, bool) {
	t, err := d.Store.GetTask(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "task_not_found", "task not found")
			return store.Task{}, false
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return store.Task{}, false
	}
	return t, true
}

func handleListTasks(w http.ResponseWriter, r *http.Request, d Deps) {
	q := r.URL.Query()

	filter := store.TaskFilter{
		Project: q.Get("project"),
		Status:  q.Get("status"),
	}

	switch p := q.Get("parent"); p {
	case "all":
		// ParentSet stays false: no filter on parent_id.
	case "":
		filter.ParentSet = true
		filter.Parent = 0
	default:
		pid, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid parent parameter")
			return
		}
		filter.ParentSet = true
		filter.Parent = pid
	}

	tasks, err := d.Store.ListTasks(filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	counts, err := taskQuestionCounts(d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	waiting, err := waitingTerminalSessions(d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if q.Get("board") == "true" {
		writeJSON(w, http.StatusOK, map[string]any{"board": toTaskBoard(tasks, counts, waiting)})
		return
	}

	out := make([]taskResponse, len(tasks))
	for i, t := range tasks {
		out[i] = toTaskResponse(t)
		annotateQuestionCounts(&out[i], counts)
		annotateWaitingTerminal(&out[i], waiting)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

type postTaskRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Project     string `json:"project"`
	ParentID    int64  `json:"parent_id"`
}

func handlePostTask(w http.ResponseWriter, r *http.Request, d Deps) {
	var req postTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	if req.Title == "" {
		writeErr(w, http.StatusBadRequest, "empty_title", "title is required")
		return
	}

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}

	var parent store.Task
	if req.ParentID != 0 {
		parent, err = d.Store.GetTask(req.ParentID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusNotFound, "task_not_found", "parent task not found")
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if parent.ParentID != 0 {
			writeErr(w, http.StatusBadRequest, "nested_subtask", "parent task must be a root task")
			return
		}
		if req.Project == "" {
			// Inherit project from the (already-validated) parent task, so
			// `rocket task add --parent N` works without --project even when
			// multiple projects exist.
			req.Project = parent.ProjectID
		}
	}

	if _, err := d.Store.GetProject(req.Project); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "project_not_found", "project not found: "+req.Project)
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	createdBy := "user"
	if caller != nil {
		switch caller.Kind {
		case "agent":
			// A registered persistent agent has the same task rights as the
			// human user; its session id is attribution, not authentication.
			createdBy = "agent"
		case "orchestrator":
			createdBy = "orchestrator"
			if req.ParentID == 0 {
				writeErr(w, http.StatusForbidden, "forbidden", "agents may only create subtasks")
				return
			}
			if parent.SessionID != caller.ID {
				writeErr(w, http.StatusForbidden, "forbidden", "parent task does not belong to caller")
				return
			}
		default:
			writeErr(w, http.StatusForbidden, "forbidden", "workers may not create tasks")
			return
		}
	}

	id, err := d.Store.AddTask(store.Task{
		ParentID:    req.ParentID,
		Title:       req.Title,
		Description: req.Description,
		ProjectID:   req.Project,
		CreatedBy:   createdBy,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	d.Bus.Publish("task.created", "", map[string]any{
		"task_id": id, "title": req.Title, "project": req.Project, "parent_id": req.ParentID,
	})

	created, ok := getTaskOr404(w, d, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, toTaskResponse(created))
}

func handleGetTask(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	t, ok := getTaskOr404(w, d, id)
	if !ok {
		return
	}

	subtasks, err := d.Store.ListTasks(store.TaskFilter{ParentSet: true, Parent: id})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	subOut := make([]subtaskResponse, len(subtasks))
	for i, s := range subtasks {
		subOut[i] = subtaskResponse{taskResponse: toTaskResponse(s)}
		// Fetch session info to get PR/CI state if session_id is set
		if s.SessionID != "" {
			sess, err := d.Store.GetSession(s.SessionID)
			if err == nil {
				subOut[i].PRNumber = sess.PRNumber
				subOut[i].PRState = sess.PRState
				subOut[i].CIState = sess.CIState
			}
		}
	}

	var sessResp *taskSessionResponse
	if t.SessionID != "" {
		sessResp, err = toTaskSessionResponse(d, t.SessionID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	counts, err := taskQuestionCounts(d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	waiting, err := waitingTerminalSessions(d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	for i := range subOut {
		annotateWaitingTerminal(&subOut[i].taskResponse, waiting)
	}

	tr := toTaskResponse(t)
	annotateQuestionCounts(&tr, counts)
	annotateWaitingTerminal(&tr, waiting)

	writeJSON(w, http.StatusOK, taskDetailResponse{
		taskResponse: tr,
		Subtasks:     subOut,
		Session:      sessResp,
	})
}

// callerLabel returns the label used in task_log bodies and status_changed
// events: the caller's session id, or "user" for a human caller.
func callerLabel(caller *store.Session) string {
	if caller == nil {
		return "user"
	}
	return caller.ID
}

// callerAuthor returns the author to store on task docs/log entries: the
// caller's session id, or "" for a human caller.
func callerAuthor(caller *store.Session) string {
	if caller == nil {
		return ""
	}
	return caller.ID
}

// applyTaskStatusChange updates task's status, publishes task.status_changed,
// and appends a status task_log entry.
func applyTaskStatusChange(d Deps, task store.Task, caller *store.Session, newStatus string) error {
	from := task.Status
	if err := d.Store.UpdateTaskStatus(task.ID, newStatus); err != nil {
		return err
	}

	by := callerLabel(caller)
	d.Bus.Publish("task.status_changed", task.SessionID, map[string]any{
		"task_id": task.ID, "from": from, "to": newStatus, "by": by,
	})

	_, err := d.Store.AddTaskLog(store.TaskLogEntry{
		TaskID: task.ID,
		Kind:   "status",
		Body:   fmt.Sprintf("status: %s → %s (by %s)", from, newStatus, by),
		Author: callerAuthor(caller),
	})
	return err
}

type patchTaskRequest struct {
	Status      *string `json:"status"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

// patchTaskResponse is the JSON shape of PATCH /v1/tasks/{id}. CleanedUp is
// only set (non-nil) when the request moved a root task to "done", to tell
// the caller whether the task's orchestrator (and its workers) were live
// and got killed as part of the transition.
type patchTaskResponse struct {
	taskResponse
	CleanedUp *bool `json:"cleaned_up,omitempty"`
}

// reviewBlockedResponse is the 409 body returned when PATCH ?status=review
// is refused because the root task still has open subtasks and/or live
// worker sessions.
type reviewBlockedResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	OpenSubtasks []int64  `json:"open_subtasks,omitempty"`
	LiveWorkers  []string `json:"live_workers,omitempty"`
}

func writeReviewBlocked(w http.ResponseWriter, openSubtasks []int64, liveWorkers []string) {
	var body reviewBlockedResponse
	body.Error.Code = "review_blocked"
	body.Error.Message = reviewBlockedMessage(openSubtasks, liveWorkers)
	body.OpenSubtasks = openSubtasks
	body.LiveWorkers = liveWorkers
	writeJSON(w, http.StatusConflict, body)
}

// reviewBlockedMessage renders a human-readable summary of why a review
// transition was blocked, ending with a hint to use --force to override.
func reviewBlockedMessage(openSubtasks []int64, liveWorkers []string) string {
	var parts []string
	if len(openSubtasks) > 0 {
		parts = append(parts, fmt.Sprintf("open subtasks: %v", openSubtasks))
	}
	if len(liveWorkers) > 0 {
		parts = append(parts, fmt.Sprintf("live workers: %v", liveWorkers))
	}
	return strings.Join(parts, "; ") + " — retry with --force to override"
}

// openSubtaskIDs returns the ids of taskID's subtasks that are not yet
// done or cancelled.
func openSubtaskIDs(d Deps, taskID int64) ([]int64, error) {
	subtasks, err := d.Store.ListTasks(store.TaskFilter{ParentSet: true, Parent: taskID})
	if err != nil {
		return nil, err
	}
	var open []int64
	for _, s := range subtasks {
		if s.Status != "done" && s.Status != "cancelled" {
			open = append(open, s.ID)
		}
	}
	return open, nil
}

// liveWorkerSessionIDs returns the ids of live (spawning/running) worker
// sessions whose ParentID is orchestratorSessionID.
func liveWorkerSessionIDs(d Deps, orchestratorSessionID string) ([]string, error) {
	if orchestratorSessionID == "" {
		return nil, nil
	}
	sessions, err := d.Store.ListSessions(store.SessionFilter{All: true})
	if err != nil {
		return nil, err
	}
	var live []string
	for _, s := range sessions {
		if s.Kind != "worker" || s.ParentID != orchestratorSessionID {
			continue
		}
		if s.State == "spawning" || s.State == "running" {
			live = append(live, s.ID)
		}
	}
	return live, nil
}

func handlePatchTask(w http.ResponseWriter, r *http.Request, d Deps) {
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
	if !canWriteTask(caller, task, d.Store) {
		writeErr(w, http.StatusForbidden, "forbidden", "caller may not modify this task")
		return
	}

	var req patchTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	if req.Status != nil && *req.Status == "cancelled" {
		writeErr(w, http.StatusBadRequest, "use_cancel", "use POST /v1/tasks/{id}/cancel (rocket task cancel) to cancel — it also stops sessions")
		return
	}

	if req.Status != nil && *req.Status == "review" && task.ParentID == 0 {
		force := false
		if v := r.URL.Query().Get("force"); v != "" {
			if b, err := strconv.ParseBool(v); err == nil {
				force = b
			}
		}
		if !force {
			openSubtasks, err := openSubtaskIDs(d, task.ID)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			liveWorkers, err := liveWorkerSessionIDs(d, task.SessionID)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			if len(openSubtasks) > 0 || len(liveWorkers) > 0 {
				writeReviewBlocked(w, openSubtasks, liveWorkers)
				return
			}
		}
	}

	var cleanedUp *bool
	if req.Status != nil {
		if err := applyTaskStatusChange(d, task, caller, *req.Status); err != nil {
			// UpdateTaskStatus's only failure mode here is a status value
			// outside the valid set (store.ErrNotFound can't happen: the
			// task was just fetched above).
			writeErr(w, http.StatusBadRequest, "invalid_status", err.Error())
			return
		}
		// Reflect the change for the title/description update below and
		// the final response.
		task.Status = *req.Status

		if *req.Status == "done" && task.ParentID == 0 {
			done := cascadeOrchestratorCleanup(r.Context(), d, task)
			cleanedUp = &done
		}
	}

	if req.Title != nil || req.Description != nil {
		if req.Title != nil {
			task.Title = *req.Title
		}
		if req.Description != nil {
			task.Description = *req.Description
		}
		if err := d.Store.UpdateTask(task); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	updated, ok := getTaskOr404(w, d, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, patchTaskResponse{taskResponse: toTaskResponse(updated), CleanedUp: cleanedUp})
}

// cascadeOrchestratorCleanup kills task's orchestrator session (and, via
// KillCascade, any workers still attached to it) when a root task is moved
// to "done" — automating the manual "the human kills the orchestrator"
// step. Returns whether a live orchestrator was actually cleaned up. Kill
// failures are logged but do not fail the request: the task's status change
// already succeeded.
func cascadeOrchestratorCleanup(ctx context.Context, d Deps, task store.Task) bool {
	if task.SessionID == "" || d.Manager == nil {
		return false
	}
	sess, err := d.Store.GetSession(task.SessionID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			log.Printf("task %d done: lookup orchestrator session %s: %v", task.ID, task.SessionID, err)
		}
		return false
	}
	if isSessionTerminal(sess.State) {
		return false
	}

	if err := d.Manager.KillCascade(ctx, task.SessionID, true); err != nil {
		log.Printf("task %d done: kill cascade for orchestrator session %s: %v", task.ID, task.SessionID, err)
		return false
	}

	d.Bus.Publish("task.orchestrator_cleaned_up", task.SessionID, map[string]any{
		"task_id": task.ID, "session_id": task.SessionID,
	})
	if _, err := d.Store.AddTaskLog(store.TaskLogEntry{
		TaskID: task.ID,
		Kind:   "status",
		Body:   "feature closed, orchestrator and workers cleaned up",
	}); err != nil {
		log.Printf("task %d done: add cleanup task log: %v", task.ID, err)
	}
	return true
}

func handlePostTaskCancel(w http.ResponseWriter, r *http.Request, d Deps) {
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
	if !canWriteTask(caller, task, d.Store) {
		writeErr(w, http.StatusForbidden, "forbidden", "caller may not cancel this task")
		return
	}

	killIDs := []int64{task.ID}
	if task.ParentID == 0 {
		subtasks, err := d.Store.ListTasks(store.TaskFilter{ParentSet: true, Parent: task.ID})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		for _, s := range subtasks {
			killIDs = append(killIDs, s.ID)
		}
	}

	if err := applyTaskStatusChange(d, task, caller, "cancelled"); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if err := killTaskSessions(r.Context(), d, killIDs); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	updated, ok := getTaskOr404(w, d, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toTaskResponse(updated))
}

// killTaskSessions kills (with cleanup) the sessions attached to each task
// id in taskIDs, if any. Missing sessions and sessions already in a
// terminal state are treated as no-ops (Manager.Kill is idempotent for the
// latter).
func killTaskSessions(ctx context.Context, d Deps, taskIDs []int64) error {
	for _, id := range taskIDs {
		t, err := d.Store.GetTask(id)
		if err != nil {
			return err
		}
		if t.SessionID == "" || d.Manager == nil {
			continue
		}
		if err := d.Manager.Kill(ctx, t.SessionID, true); err != nil {
			var verr *session.ValidationError
			if errors.As(err, &verr) && verr.Code == "session_not_found" {
				continue
			}
			return err
		}
	}
	return nil
}

// isSessionTerminal reports whether state is one of the terminal session
// states (killed/done/errored) — i.e. no live orchestrator is running for it.
func isSessionTerminal(state string) bool {
	return state == "killed" || state == "done" || state == "errored"
}

type postTaskStartRequest struct {
	Agent string `json:"agent"`
}

// handlePostTaskStart spawns an orchestrator for a root, backlog task,
// moving it to in_progress. Only the human user may start a task — agent
// callers get 403. Root-ness, backlog status, and the absence of an
// already-live orchestrator session on this task are all preconditions,
// checked before spawning.
func handlePostTaskStart(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}

	// Serialize all task starts with startMu to prevent two concurrent requests
	// from both spawning orchestrators for the same task. We lock for the entire
	// operation and re-fetch the task within the lock to detect any concurrent
	// changes.
	startMu.Lock()
	defer startMu.Unlock()

	task, ok := getTaskOr404(w, d, id)
	if !ok {
		return
	}

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if caller != nil && caller.Kind != "agent" {
		writeErr(w, http.StatusForbidden, "forbidden", "only the human user or a registered agent may start a task")
		return
	}

	if task.ParentID != 0 {
		writeErr(w, http.StatusBadRequest, "not_root_task", "only root tasks can be started")
		return
	}
	if task.Status != "backlog" {
		writeErr(w, http.StatusConflict, "task_not_startable", "task must be in backlog to start")
		return
	}

	if task.SessionID != "" {
		sess, err := d.Store.GetSession(task.SessionID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if err == nil && !isSessionTerminal(sess.State) {
			writeErr(w, http.StatusConflict, "already_started", "task already has a live orchestrator session")
			return
		}
	}

	project, err := d.Store.GetProject(task.ProjectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "project_not_found", "project not found: "+task.ProjectID)
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	var req postTaskStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	agentName := req.Agent
	if agentName == "" {
		agentName = d.Cfg.DefaultAgent
	}

	sess, err := d.Manager.SpawnOrchestrator(r.Context(), task, project, agentName)
	if err != nil {
		writeManagerErr(w, err)
		return
	}

	task.FeatureSlug = sess.FeatureSlug
	task.SessionID = sess.ID
	if err := d.Store.UpdateTask(task); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	from := task.Status
	if err := d.Store.UpdateTaskStatus(task.ID, "in_progress"); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	d.Bus.Publish("task.status_changed", sess.ID, map[string]any{
		"task_id": task.ID, "from": from, "to": "in_progress", "by": "system",
	})
	if _, err := d.Store.AddTaskLog(store.TaskLogEntry{
		TaskID: task.ID,
		Kind:   "status",
		Body:   fmt.Sprintf("status: %s → in_progress (by system)", from),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"task_id":      task.ID,
		"feature_slug": sess.FeatureSlug,
		"session_id":   sess.ID,
	})
}

// taskDocResponse is the JSON shape of a task doc as returned by the API.
type taskDocResponse struct {
	ID        int64  `json:"id"`
	TaskID    int64  `json:"task_id"`
	Version   int64  `json:"version"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Author    string `json:"author,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

func toTaskDocResponse(d store.TaskDoc) taskDocResponse {
	return taskDocResponse{
		ID:        d.ID,
		TaskID:    d.TaskID,
		Version:   d.Version,
		Kind:      d.Kind,
		Title:     d.Title,
		Body:      d.Body,
		Author:    d.Author,
		CreatedAt: d.CreatedAt,
	}
}

func handleGetTaskDocs(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	if _, ok := getTaskOr404(w, d, id); !ok {
		return
	}

	history := r.URL.Query().Get("history") == "true"
	docs, err := d.Store.ListTaskDocs(id, history)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := make([]taskDocResponse, len(docs))
	for i, doc := range docs {
		out[i] = toTaskDocResponse(doc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"docs": out})
}

type putTaskDocRequest struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func handlePutTaskDocs(w http.ResponseWriter, r *http.Request, d Deps) {
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
	if !canWriteTask(caller, task, d.Store) {
		writeErr(w, http.StatusForbidden, "forbidden", "caller may not write docs on this task")
		return
	}

	var req putTaskDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if !validDocKinds[req.Kind] {
		writeErr(w, http.StatusBadRequest, "invalid_kind", "invalid doc kind: "+req.Kind)
		return
	}

	doc, err := d.Store.PutTaskDoc(store.TaskDoc{
		TaskID: id,
		Kind:   req.Kind,
		Title:  req.Title,
		Body:   req.Body,
		Author: callerAuthor(caller),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toTaskDocResponse(doc))
}

// taskLogResponse is the JSON shape of a task log entry as returned by the API.
type taskLogResponse struct {
	ID        int64  `json:"id"`
	TaskID    int64  `json:"task_id"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	Author    string `json:"author,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

func toTaskLogResponse(e store.TaskLogEntry) taskLogResponse {
	return taskLogResponse{
		ID:        e.ID,
		TaskID:    e.TaskID,
		Kind:      e.Kind,
		Body:      e.Body,
		Author:    e.Author,
		CreatedAt: e.CreatedAt,
	}
}

func handleGetTaskLog(w http.ResponseWriter, r *http.Request, d Deps) {
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	if _, ok := getTaskOr404(w, d, id); !ok {
		return
	}

	kind := r.URL.Query().Get("kind")
	entries, err := d.Store.ListTaskLog(id, kind)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	out := make([]taskLogResponse, len(entries))
	for i, e := range entries {
		out[i] = toTaskLogResponse(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"log": out})
}

type postTaskLogRequest struct {
	Kind string `json:"kind"`
	Body string `json:"body"`
}

func handlePostTaskLog(w http.ResponseWriter, r *http.Request, d Deps) {
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
	if !canWriteTask(caller, task, d.Store) {
		writeErr(w, http.StatusForbidden, "forbidden", "caller may not write to this task's log")
		return
	}

	var req postTaskLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if !validLogKinds[req.Kind] {
		writeErr(w, http.StatusBadRequest, "invalid_kind", "invalid log kind: "+req.Kind)
		return
	}

	logID, err := d.Store.AddTaskLog(store.TaskLogEntry{
		TaskID: id,
		Kind:   req.Kind,
		Body:   req.Body,
		Author: callerAuthor(caller),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	entries, err := d.Store.ListTaskLog(id, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	for _, e := range entries {
		if e.ID == logID {
			writeJSON(w, http.StatusCreated, toTaskLogResponse(e))
			return
		}
	}
	writeErr(w, http.StatusInternalServerError, "internal_error", "log entry not found after insert")
}
