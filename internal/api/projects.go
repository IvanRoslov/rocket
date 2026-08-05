package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/IvanRoslov/rocket/internal/store"
)

// taskCounters is the phase-1 placeholder shape for a project's task board
// aggregates. Tasks land in phase 3; until then every counter is zero, but
// the JSON shape is fixed here so clients don't need to change later.
type taskCounters struct {
	Backlog    int `json:"backlog"`
	Brainstorm int `json:"brainstorm"`
	InProgress int `json:"in_progress"`
	Review     int `json:"review"`
	Done       int `json:"done"`
}

// projectResponse is the JSON shape of a project as returned by GET
// /v1/projects (list form): id/name/repos plus phase-1 aggregates.
type projectResponse struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	MainRepo     string       `json:"main"`
	LinkedRepos  []string     `json:"linked"`
	LiveSessions int          `json:"live_sessions"`
	Tasks        taskCounters `json:"tasks"`
	CreatedAt    int64        `json:"created_at"`
}

// projectDetailResponse is the JSON shape of GET /v1/projects/{id}: the
// project plus resolved repo detail for main and linked repos.
type projectDetailResponse struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Main         repoResponse   `json:"main"`
	Linked       []repoResponse `json:"linked"`
	LiveSessions int            `json:"live_sessions"`
	Tasks        taskCounters   `json:"tasks"`
	CreatedAt    int64          `json:"created_at"`
}

func liveSessionCount(d Deps, projectID string) (int, error) {
	sessions, err := d.Store.ListSessions(store.SessionFilter{Project: projectID, All: false})
	if err != nil {
		return 0, err
	}
	return len(sessions), nil
}

func toProjectResponse(d Deps, p store.Project) (projectResponse, error) {
	n, err := liveSessionCount(d, p.ID)
	if err != nil {
		return projectResponse{}, err
	}
	return projectResponse{
		ID:           p.ID,
		Name:         p.Name,
		MainRepo:     p.MainRepo,
		LinkedRepos:  emptyIfNil(p.LinkedRepos),
		LiveSessions: n,
		Tasks:        taskCounters{},
		CreatedAt:    p.CreatedAt,
	}, nil
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func toProjectDetailResponse(d Deps, p store.Project) (projectDetailResponse, error) {
	n, err := liveSessionCount(d, p.ID)
	if err != nil {
		return projectDetailResponse{}, err
	}

	mainRepo, err := d.Store.GetRepo(p.MainRepo)
	if err != nil {
		return projectDetailResponse{}, err
	}

	linked := make([]repoResponse, 0, len(p.LinkedRepos))
	for _, id := range p.LinkedRepos {
		r, err := d.Store.GetRepo(id)
		if err != nil {
			return projectDetailResponse{}, err
		}
		linked = append(linked, toRepoResponse(r))
	}

	return projectDetailResponse{
		ID:           p.ID,
		Name:         p.Name,
		Main:         toRepoResponse(mainRepo),
		Linked:       linked,
		LiveSessions: n,
		Tasks:        taskCounters{},
		CreatedAt:    p.CreatedAt,
	}, nil
}

// registerProjectRoutes wires the /v1/projects routes onto mux.
func registerProjectRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/projects", func(w http.ResponseWriter, r *http.Request) {
		projects, err := d.Store.ListProjects()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		out := make([]projectResponse, 0, len(projects))
		for _, p := range projects {
			pr, err := toProjectResponse(d, p)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			out = append(out, pr)
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /v1/projects", func(w http.ResponseWriter, r *http.Request) {
		handlePostProject(w, r, d)
	})

	mux.HandleFunc("GET /v1/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleGetProject(w, r, d)
	})

	mux.HandleFunc("PATCH /v1/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		handlePatchProject(w, r, d)
	})

	mux.HandleFunc("DELETE /v1/projects/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteProject(w, r, d)
	})
}

type postProjectRequest struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Main   string   `json:"main"`
	Linked []string `json:"linked"`
}

// dedupLinked returns linked with duplicates removed and any entry equal to
// main dropped, preserving first-seen order.
func dedupLinked(main string, linked []string) []string {
	seen := map[string]bool{main: true}
	out := make([]string, 0, len(linked))
	for _, id := range linked {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func handlePostProject(w http.ResponseWriter, r *http.Request, d Deps) {
	var req postProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	id := req.ID
	if id == "" {
		id = normalizeID(req.Name)
		if id == "" || !idPattern.MatchString(id) {
			writeErr(w, http.StatusBadRequest, "invalid_id", "derived id from name is empty; pass id explicitly")
			return
		}
	} else if !idPattern.MatchString(id) {
		writeErr(w, http.StatusBadRequest, "invalid_id", "id must match ^[a-z0-9-]+$")
		return
	}

	if _, err := d.Store.GetRepo(req.Main); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "repo_not_found", "main repo not found: "+req.Main)
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	linked := dedupLinked(req.Main, req.Linked)
	for _, lid := range linked {
		if _, err := d.Store.GetRepo(lid); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusBadRequest, "repo_not_found", "linked repo not found: "+lid)
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
	}

	name := req.Name
	if name == "" {
		name = id
	}

	p := store.Project{
		ID:          id,
		Name:        name,
		MainRepo:    req.Main,
		LinkedRepos: linked,
	}

	if err := d.Store.AddProject(p); err != nil {
		if errors.Is(err, store.ErrExists) {
			writeErr(w, http.StatusConflict, "project_exists", "project id already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	created, err := d.Store.GetProject(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	pr, err := toProjectResponse(d, created)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pr)
}

func handleGetProject(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	p, err := d.Store.GetProject(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "project_not_found", "project not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	detail, err := toProjectDetailResponse(d, p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func handlePatchProject(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	existing, err := d.Store.GetProject(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "project_not_found", "project not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	if v, ok := raw["name"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid name")
			return
		}
		existing.Name = s
	}
	if v, ok := raw["main"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid main")
			return
		}
		if _, err := d.Store.GetRepo(s); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusBadRequest, "repo_not_found", "main repo not found: "+s)
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		existing.MainRepo = s
	}
	if v, ok := raw["linked"]; ok {
		var s []string
		if err := json.Unmarshal(v, &s); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid linked")
			return
		}
		for _, lid := range s {
			if _, err := d.Store.GetRepo(lid); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					writeErr(w, http.StatusBadRequest, "repo_not_found", "linked repo not found: "+lid)
					return
				}
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
		}
		existing.LinkedRepos = s
	}

	existing.LinkedRepos = dedupLinked(existing.MainRepo, existing.LinkedRepos)

	if err := d.Store.UpdateProject(existing); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "project_not_found", "project not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	updated, err := d.Store.GetProject(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	pr, err := toProjectResponse(d, updated)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pr)
}

func handleDeleteProject(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	if _, err := d.Store.GetProject(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "project_not_found", "project not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	n, err := liveSessionCount(d, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if n > 0 {
		writeErr(w, http.StatusConflict, "project_busy", "project has live sessions")
		return
	}

	if err := d.Store.DeleteProject(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "project_not_found", "project not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
