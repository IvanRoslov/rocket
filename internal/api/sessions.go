package api

import (
	"encoding/json"
	"errors"
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
	Project string `json:"project"`
	Repo    string `json:"repo"`
	Task    string `json:"task"`
	Feature string `json:"feature"`
	Prompt  string `json:"prompt"`
	Agent   string `json:"agent"`
}

func handlePostSession(w http.ResponseWriter, r *http.Request, d Deps) {
	var req postSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	sess, err := d.Manager.Spawn(r.Context(), session.SpawnReq{
		Project:   req.Project,
		Repo:      req.Repo,
		Task:      req.Task,
		Feature:   req.Feature,
		Prompt:    req.Prompt,
		AgentName: req.Agent,
	})
	if err != nil {
		writeManagerErr(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":            sess.ID,
		"feature_slug":  sess.FeatureSlug,
		"branch":        sess.Branch,
		"worktree_path": sess.WorktreePath,
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

func handleKillSession(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")
	cleanup := r.URL.Query().Get("cleanup") == "true"

	if err := d.Manager.Kill(r.Context(), id, cleanup); err != nil {
		writeManagerErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "killed"})
}

func handleRestoreSession(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

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
