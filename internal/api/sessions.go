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

// sessionResponse is the JSON shape of a session as returned by the API.
type sessionResponse struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	ProjectID    string `json:"project_id"`
	RepoID       string `json:"repo_id"`
	FeatureSlug  string `json:"feature_slug"`
	ParentID     string `json:"parent_id,omitempty"`
	Agent        string `json:"agent"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktree_path"`
	TmuxName     string `json:"tmux_name"`
	State        string `json:"state"`
	Activity     string `json:"activity,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	ActivityTS   int64  `json:"activity_ts,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

func toSessionResponse(s store.Session) sessionResponse {
	return sessionResponse{
		ID:           s.ID,
		Kind:         s.Kind,
		ProjectID:    s.ProjectID,
		RepoID:       s.RepoID,
		FeatureSlug:  s.FeatureSlug,
		ParentID:     s.ParentID,
		Agent:        s.Agent,
		Branch:       s.Branch,
		WorktreePath: s.WorktreePath,
		TmuxName:     s.TmuxName,
		State:        s.State,
		Activity:     s.Activity,
		Prompt:       s.Prompt,
		ActivityTS:   s.ActivityTS,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
	}
}

// registerSessionRoutes wires the /v1/sessions routes onto mux.
func registerSessionRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		handlePostSession(w, r, d)
	})
	mux.HandleFunc("GET /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		handleListSessions(w, r, d)
	})
	mux.HandleFunc("GET /v1/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleGetSession(w, r, d)
	})
	mux.HandleFunc("POST /v1/sessions/{id}/kill", func(w http.ResponseWriter, r *http.Request) {
		handleKillSession(w, r, d)
	})
	mux.HandleFunc("POST /v1/sessions/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		handleRestoreSession(w, r, d)
	})
	mux.HandleFunc("GET /v1/sessions/{id}/output", func(w http.ResponseWriter, r *http.Request) {
		handleSessionOutput(w, r, d)
	})
	mux.HandleFunc("GET /v1/sessions/{id}/attach", func(w http.ResponseWriter, r *http.Request) {
		handleSessionAttach(w, r, d)
	})
}

type postSessionRequest struct {
	Repo      string `json:"repo"`
	Task      string `json:"task"`
	Prompt    string `json:"prompt"`
	Agent     string `json:"agent"`
	SubtaskID int64  `json:"subtask_id"`
}

// isSpawningOrchestrator reports whether caller is a live orchestrator
// session (kind=orchestrator, state spawning or running) — the only kind of
// caller allowed to hit POST /v1/sessions.
func isSpawningOrchestrator(caller *store.Session) bool {
	if caller == nil || caller.Kind != "orchestrator" {
		return false
	}
	return caller.State == "spawning" || caller.State == "running"
}

// repoInProject reports whether repoID is project's main repo or one of its
// linked repos.
func repoInProject(project store.Project, repoID string) bool {
	for _, r := range project.Repos() {
		if r == repoID {
			return true
		}
	}
	return false
}

// findRootTaskForSession returns the root task (parent_id IS NULL) whose
// session_id == sessionID, if any.
func findRootTaskForSession(d Deps, sessionID string) (store.Task, bool, error) {
	tasks, err := d.Store.ListTasks(store.TaskFilter{ParentSet: true, Parent: 0})
	if err != nil {
		return store.Task{}, false, err
	}
	for _, t := range tasks {
		if t.SessionID == sessionID {
			return t, true, nil
		}
	}
	return store.Task{}, false, nil
}

// handlePostSession spawns a worker session on behalf of an orchestrator
// caller (identified via X-Rocket-Session): only a live orchestrator may
// call this endpoint. project/feature/parent are all derived from the
// caller; the worker is attached to a subtask of the caller's root task,
// either an existing one named by subtask_id or one auto-created from the
// request.
func handlePostSession(w http.ResponseWriter, r *http.Request, d Deps) {
	var req postSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}
	if !isSpawningOrchestrator(caller) {
		writeErr(w, http.StatusForbidden, "orchestrator_only", "caller must be a live orchestrator session")
		return
	}

	project := caller.ProjectID
	proj, err := d.Store.GetProject(project)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "project_not_found", "project not found: "+project)
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !repoInProject(proj, req.Repo) {
		writeErr(w, http.StatusBadRequest, "repo_not_in_project", "repo not linked to project: "+req.Repo)
		return
	}

	feature := caller.FeatureSlug

	root, found, err := findRootTaskForSession(d, caller.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !found {
		writeErr(w, http.StatusConflict, "no_task", "orchestrator has no root task")
		return
	}

	var sub store.Task
	autoCreated := false
	if req.SubtaskID != 0 {
		sub, err = d.Store.GetTask(req.SubtaskID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusNotFound, "subtask_not_found", "subtask not found")
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if sub.ParentID != root.ID {
			writeErr(w, http.StatusBadRequest, "subtask_wrong_parent", "subtask does not belong to caller's task")
			return
		}
		if sub.SessionID != "" {
			existing, gerr := d.Store.GetSession(sub.SessionID)
			if gerr != nil && !errors.Is(gerr, store.ErrNotFound) {
				writeErr(w, http.StatusInternalServerError, "internal_error", gerr.Error())
				return
			}
			if gerr == nil && !isSessionTerminal(existing.State) {
				writeErr(w, http.StatusConflict, "subtask_taken", "subtask already has a live session")
				return
			}
		}
	} else {
		id, err := d.Store.AddTask(store.Task{
			Title:       req.Task,
			ParentID:    root.ID,
			ProjectID:   project,
			RepoID:      req.Repo,
			Status:      "in_progress",
			CreatedBy:   "orchestrator",
			FeatureSlug: feature,
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		sub, err = d.Store.GetTask(id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		autoCreated = true
	}

	sess, err := d.Manager.Spawn(r.Context(), session.SpawnReq{
		Project:   project,
		Repo:      req.Repo,
		Task:      req.Task,
		Feature:   feature,
		Prompt:    req.Prompt,
		AgentName: req.Agent,
		Kind:      "worker",
		ParentID:  caller.ID,
		SubtaskID: sub.ID,
	})
	if err != nil {
		if autoCreated {
			_ = d.Store.UpdateTaskStatus(sub.ID, "cancelled")
		}
		writeManagerErr(w, err)
		return
	}

	wasBacklog := sub.Status == "backlog"
	sub.SessionID = sess.ID
	sub.RepoID = req.Repo
	if err := d.Store.UpdateTask(sub); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if wasBacklog {
		if err := applyTaskStatusChange(d, sub, caller, "in_progress"); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	if _, err := d.Store.AddTaskLog(store.TaskLogEntry{
		TaskID: root.ID,
		Kind:   "status",
		Body:   fmt.Sprintf("spawned worker %s for subtask #%d", sess.ID, sub.ID),
		Author: caller.ID,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	d.Bus.Publish("task.worker_spawned", sess.ID, map[string]any{
		"task_id": root.ID, "subtask_id": sub.ID, "session_id": sess.ID,
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":            sess.ID,
		"feature_slug":  sess.FeatureSlug,
		"branch":        sess.Branch,
		"worktree_path": sess.WorktreePath,
		"subtask_id":    sub.ID,
	})
}

func handleListSessions(w http.ResponseWriter, r *http.Request, d Deps) {
	q := r.URL.Query()
	filter := store.SessionFilter{
		Kind:    q.Get("kind"),
		Project: q.Get("project"),
		Feature: q.Get("feature"),
		State:   q.Get("state"),
		All:     q.Get("all") == "true",
	}

	sessions, err := d.Store.ListSessions(filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	out := make([]sessionResponse, len(sessions))
	for i, s := range sessions {
		out[i] = toSessionResponse(s)
	}
	writeJSON(w, http.StatusOK, out)
}

func handleGetSession(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	s, err := d.Store.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "session_not_found", "session not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toSessionResponse(s))
}

// canKillOrRestoreSession reports whether caller may kill/restore the
// session identified by target. caller == nil means a human user, who is
// unrestricted. An agent caller may act on its own session (self), or —
// for non-cascading operations — on a session it is the parent of (an
// orchestrator managing its own worker). A cascading kill is restricted
// further: only the orchestrator acting on itself (or a human) may cascade,
// since cascade tears down the whole fleet under a root task.
func canKillOrRestoreSession(caller *store.Session, target store.Session, cascade bool) bool {
	if caller == nil {
		return true
	}
	if cascade {
		// Cascade tears down a whole fleet: only the orchestrator acting on
		// itself may trigger it (a worker has no fleet under it to cascade).
		return caller.ID == target.ID && caller.Kind == "orchestrator"
	}
	if caller.ID == target.ID {
		return true
	}
	return target.ParentID == caller.ID
}

func handleKillSession(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")
	cleanup := false
	if v := r.URL.Query().Get("cleanup"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cleanup = b
		}
	}
	cascade := false
	if v := r.URL.Query().Get("cascade"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cascade = b
		}
	}

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}

	target, err := d.Store.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "session_not_found", "session not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !canKillOrRestoreSession(caller, target, cascade) {
		writeErr(w, http.StatusForbidden, "forbidden", "caller may not kill this session")
		return
	}

	if cascade {
		err = d.Manager.KillCascade(r.Context(), id, cleanup)
	} else {
		err = d.Manager.Kill(r.Context(), id, cleanup)
	}
	if err != nil {
		writeManagerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "killed"})
}

func handleRestoreSession(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	caller, err := callerSession(r, d.Store)
	if writeCallerErr(w, err) {
		return
	}

	target, err := d.Store.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "session_not_found", "session not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if !canKillOrRestoreSession(caller, target, false) {
		writeErr(w, http.StatusForbidden, "forbidden", "caller may not restore this session")
		return
	}

	if err := d.Manager.Restore(r.Context(), id); err != nil {
		writeManagerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

func handleSessionOutput(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	lines := 50
	if v := r.URL.Query().Get("lines"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			lines = n
		}
	}

	out, err := d.Manager.Output(r.Context(), id, lines)
	if err != nil {
		writeManagerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": out})
}

func handleSessionAttach(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	cmd, err := d.Manager.AttachCommand(id)
	if err != nil {
		writeManagerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"command": cmd})
}

// writeManagerErr maps a session.ValidationError to its HTTP status/code,
// falling back to 500 internal_error for anything else.
func writeManagerErr(w http.ResponseWriter, err error) {
	var verr *session.ValidationError
	if errors.As(err, &verr) {
		status := http.StatusBadRequest
		switch verr.Code {
		case "session_not_found":
			status = http.StatusNotFound
		case "restore_not_allowed":
			status = http.StatusConflict
		}
		writeErr(w, status, verr.Code, verr.Message)
		return
	}
	writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
}
