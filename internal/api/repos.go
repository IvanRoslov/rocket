package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/IvanRoslov/rocket/internal/store"
)

// idPattern matches valid repo/project ids: lowercase alphanumerics and
// hyphens only.
var idPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// repoResponse is the JSON shape of a repo as returned by the API. store.Repo
// itself carries no json tags, so we translate here rather than adding API
// concerns to the store package.
type repoResponse struct {
	ID            string            `json:"id"`
	Path          string            `json:"path"`
	DefaultBranch string            `json:"default_branch"`
	AutoCleanup   bool              `json:"auto_cleanup"`
	Env           map[string]string `json:"env"`
	Symlinks      []string          `json:"symlinks"`
	PostCreate    []string          `json:"post_create"`
	CreatedAt     int64             `json:"created_at"`
}

func toRepoResponse(r store.Repo) repoResponse {
	return repoResponse{
		ID:            r.ID,
		Path:          r.Path,
		DefaultBranch: r.DefaultBranch,
		AutoCleanup:   r.AutoCleanup,
		Env:           r.Env,
		Symlinks:      r.Symlinks,
		PostCreate:    r.PostCreate,
		CreatedAt:     r.CreatedAt,
	}
}

func toRepoResponses(repos []store.Repo) []repoResponse {
	out := make([]repoResponse, len(repos))
	for i, r := range repos {
		out[i] = toRepoResponse(r)
	}
	return out
}

// registerRepoRoutes wires the /v1/repos routes onto mux.
func registerRepoRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/repos", func(w http.ResponseWriter, r *http.Request) {
		repos, err := d.Store.ListRepos()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, toRepoResponses(repos))
	})

	mux.HandleFunc("POST /v1/repos", func(w http.ResponseWriter, r *http.Request) {
		handlePostRepo(w, r, d)
	})

	mux.HandleFunc("PATCH /v1/repos/{id}", func(w http.ResponseWriter, r *http.Request) {
		handlePatchRepo(w, r, d)
	})

	mux.HandleFunc("DELETE /v1/repos/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleDeleteRepo(w, r, d)
	})
}

type postRepoRequest struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

func handlePostRepo(w http.ResponseWriter, r *http.Request, d Deps) {
	var req postRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	path, err := expandPath(req.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "repo_path_invalid", err.Error())
		return
	}

	if !repoPathValid(path) {
		writeErr(w, http.StatusBadRequest, "repo_path_invalid", "path must exist and contain a .git entry")
		return
	}

	id := req.ID
	if id == "" {
		id = normalizeID(filepath.Base(path))
		if id == "" || !idPattern.MatchString(id) {
			writeErr(w, http.StatusBadRequest, "invalid_id", "derived id from path is empty; pass --id explicitly")
			return
		}
	} else if !idPattern.MatchString(id) {
		writeErr(w, http.StatusBadRequest, "invalid_id", "id must match ^[a-z0-9-]+$")
		return
	}

	repo := store.Repo{
		ID:            id,
		Path:          path,
		DefaultBranch: defaultBranch(path),
	}

	if err := d.Store.AddRepo(repo); err != nil {
		if errors.Is(err, store.ErrExists) {
			writeErr(w, http.StatusConflict, "repo_exists", "repo id already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	created, err := d.Store.GetRepo(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toRepoResponse(created))
}

func handlePatchRepo(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	existing, err := d.Store.GetRepo(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "repo_not_found", "repo not found")
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

	if v, ok := raw["default_branch"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid default_branch")
			return
		}
		existing.DefaultBranch = s
	}
	if v, ok := raw["auto_cleanup"]; ok {
		var b bool
		if err := json.Unmarshal(v, &b); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid auto_cleanup")
			return
		}
		existing.AutoCleanup = b
	}
	if v, ok := raw["env"]; ok {
		var m map[string]string
		if err := json.Unmarshal(v, &m); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid env")
			return
		}
		existing.Env = m
	}
	if v, ok := raw["symlinks"]; ok {
		var s []string
		if err := json.Unmarshal(v, &s); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid symlinks")
			return
		}
		existing.Symlinks = s
	}
	if v, ok := raw["post_create"]; ok {
		var s []string
		if err := json.Unmarshal(v, &s); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", "invalid post_create")
			return
		}
		existing.PostCreate = s
	}

	if err := d.Store.UpdateRepo(existing); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "repo_not_found", "repo not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	updated, err := d.Store.GetRepo(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toRepoResponse(updated))
}

func handleDeleteRepo(w http.ResponseWriter, r *http.Request, d Deps) {
	id := r.PathValue("id")

	if err := d.Store.DeleteRepo(id); err != nil {
		if errors.Is(err, store.ErrRepoInUse) {
			writeErr(w, http.StatusConflict, "repo_in_use", "repo is in use by a project")
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "repo_not_found", "repo not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// expandPath resolves ~ to the user's home directory and requires the
// resulting path to be absolute.
func expandPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is required")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		u, err := user.Current()
		if err != nil {
			return "", err
		}
		p = filepath.Join(u.HomeDir, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("path must be absolute")
	}
	return p, nil
}

// repoPathValid reports whether path exists and contains a .git entry.
func repoPathValid(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		return false
	}
	return true
}

// normalizeID lowercases s, replaces any run of characters outside
// [a-z0-9-] with a single '-', and trims leading/trailing '-'.
func normalizeID(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// defaultBranch determines a repo's default branch via
// `git symbolic-ref --short refs/remotes/origin/HEAD`, falling back to
// "main" if that fails (e.g. no origin remote).
func defaultBranch(path string) string {
	cmd := exec.Command("git", "-C", path, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "main"
	}
	branch := strings.TrimSpace(string(out))
	branch = strings.TrimPrefix(branch, "origin/")
	if branch == "" {
		return "main"
	}
	return branch
}
