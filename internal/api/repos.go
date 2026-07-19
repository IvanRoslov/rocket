package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/store"
)

// cloneTimeout bounds how long a single `git clone` of a GitHub repo may
// run before it's killed. A package var so tests can shrink it.
var cloneTimeout = 10 * time.Minute

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
	ID     string `json:"id"`
	Path   string `json:"path"`
	Github string `json:"github"`
}

func handlePostRepo(w http.ResponseWriter, r *http.Request, d Deps) {
	var req postRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	if req.Github != "" && req.Path != "" {
		writeErr(w, http.StatusBadRequest, "bad_request", "specify either path or github, not both")
		return
	}

	if req.Github != "" {
		handlePostRepoGithub(w, r, d, req)
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

// githubRepoPattern matches the "owner/name" shorthand GitHub uses to
// identify a repository.
var githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// parseGithubRepo splits "owner/name" into its two parts, reporting ok=false
// if s doesn't match that shape.
func parseGithubRepo(s string) (owner, name string, ok bool) {
	if !githubRepoPattern.MatchString(s) {
		return "", "", false
	}
	parts := strings.SplitN(s, "/", 2)
	return parts[0], parts[1], true
}

// tokenInCloneURLRE matches the credential portion of a GitHub HTTPS clone
// URL (https://x-access-token:<token>@github.com/...), used to scrub a
// leaked token out of git's error output before it's logged or returned to
// a client.
var tokenInCloneURLRE = regexp.MustCompile(`://x-access-token:[^@]*@`)

// basicAuthInErrorRE matches an HTTP Basic auth header value that might
// appear in git's error output, used to scrub a leaked credential before
// it's logged or returned to a client.
var basicAuthInErrorRE = regexp.MustCompile(`Basic [A-Za-z0-9+/=]+`)

// sanitizeCloneError strips the GitHub token from git's (possibly
// multi-line) output: first a literal replace of the known token value (if
// any), then a regex scrub of the credential portion of any clone URL or
// Basic auth header that might still be present (e.g. if git echoed a
// differently-encoded form of the URL, or the extraheader value). The
// result is safe to log or return to a client.
func sanitizeCloneError(output, token string) string {
	msg := strings.TrimSpace(output)
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "***")
	}
	msg = tokenInCloneURLRE.ReplaceAllString(msg, "://x-access-token:***@")
	msg = basicAuthInErrorRE.ReplaceAllString(msg, "Basic ***")
	return msg
}

// buildCloneURL returns the URL `git clone` should fetch owner/name from.
// If cfg.GithubCloneBase is set (a test hook, e.g. "file:///tmp/bares/"),
// it's used verbatim as a prefix. Otherwise the standard, credential-free
// GitHub HTTPS clone URL is built; authentication (when needed) is carried
// separately via process environment, not embedded in the URL, so it never
// appears in argv (see buildCloneCmd).
func buildCloneURL(cloneBase, owner, name string) string {
	if cloneBase != "" {
		return cloneBase + owner + "/" + name + ".git"
	}
	return "https://github.com/" + owner + "/" + name + ".git"
}

// buildCloneCmd constructs the `git clone` command for cloning owner/name
// into targetDir. When cloneBase is set (test mode, e.g. file://), no
// authentication is used. Otherwise, if token is non-empty, credentials are
// passed via GIT_CONFIG_* environment variables rather than argv or the
// clone URL itself: a process's environment is not readable by other
// non-root users on macOS/Linux the way argv is (visible to anyone via
// `ps`), so this keeps the token out of that exposure surface.
func buildCloneCmd(ctx context.Context, cloneBase, owner, name, targetDir, token string) *exec.Cmd {
	cloneURL := buildCloneURL(cloneBase, owner, name)
	cmd := exec.CommandContext(ctx, "git", "clone", cloneURL, targetDir)
	if cloneBase == "" && token != "" {
		basicAuth := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraheader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+basicAuth,
		)
	}
	return cmd
}

// resolveClonedDefaultBranch determines the default branch of a freshly
// cloned repo at path: it prefers `git symbolic-ref` against the clone's
// own origin/HEAD, falling back to the GitHub catalog's cached
// default_branch for owner/name (if the catalog happens to already know
// it), and finally to "main".
func resolveClonedDefaultBranch(path, owner, name string) string {
	branch := defaultBranch(path)
	if branch != "main" {
		return branch
	}

	full := owner + "/" + name
	catalogCache.mu.Lock()
	repos := catalogCache.repos
	catalogCache.mu.Unlock()
	for _, r := range repos {
		if r.FullName == full && r.DefaultBranch != "" {
			return r.DefaultBranch
		}
	}
	return branch
}

// handlePostRepoGithub handles POST /v1/repos {github: "owner/name", id?}:
// it clones the repo from GitHub (or, in tests, from d.Cfg.GithubCloneBase)
// into d.Cfg.ReposDir and registers it, publishing repo.clone_started,
// repo.clone_done/repo.clone_failed events along the way.
func handlePostRepoGithub(w http.ResponseWriter, r *http.Request, d Deps, req postRepoRequest) {
	owner, name, ok := parseGithubRepo(req.Github)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_github_repo", "github must be in owner/name format")
		return
	}
	full := owner + "/" + name

	id := req.ID
	if id == "" {
		id = normalizeID(name)
		if id == "" || !idPattern.MatchString(id) {
			writeErr(w, http.StatusBadRequest, "invalid_id", "derived id from repo name is empty; pass --id explicitly")
			return
		}
	} else if !idPattern.MatchString(id) {
		writeErr(w, http.StatusBadRequest, "invalid_id", "id must match ^[a-z0-9-]+$")
		return
	}

	if _, err := d.Store.GetRepo(id); err == nil {
		writeErr(w, http.StatusConflict, "repo_exists", "repo id already exists")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	token := ""
	if d.Cfg.GithubCloneBase == "" {
		if d.GH == nil {
			writeErr(w, http.StatusBadRequest, "no_token", "no GitHub token configured")
			return
		}
		client, err := d.GH()
		if err != nil {
			if errors.Is(err, github.ErrNoToken) {
				writeErr(w, http.StatusBadRequest, "no_token", "no GitHub token configured")
				return
			}
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		token = client.Token()
	}

	if err := os.MkdirAll(d.Cfg.ReposDir, 0700); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	targetDir := filepath.Join(d.Cfg.ReposDir, owner+"__"+name)

	slog.Default().Info("cloning github repo", "repo", full)
	if d.Bus != nil {
		d.Bus.Publish("repo.clone_started", "", map[string]any{"repo": full})
	}

	cloneCtx, cancel := context.WithTimeout(r.Context(), cloneTimeout)
	defer cancel()
	cmd := buildCloneCmd(cloneCtx, d.Cfg.GithubCloneBase, owner, name, targetDir, token)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if rmErr := os.RemoveAll(targetDir); rmErr != nil {
			slog.Default().Warn("failed to clean up partial clone", "repo", full, "dir", targetDir, "error", rmErr)
		}
		sanitized := sanitizeCloneError(string(out), token)
		slog.Default().Error("github clone failed", "repo", full, "error", sanitized)
		if d.Bus != nil {
			d.Bus.Publish("repo.clone_failed", "", map[string]any{"repo": full, "error": sanitized})
		}
		writeErr(w, http.StatusBadGateway, "clone_failed", sanitized)
		return
	}

	repo := store.Repo{
		ID:            id,
		Path:          targetDir,
		DefaultBranch: resolveClonedDefaultBranch(targetDir, owner, name),
	}
	if err := d.Store.AddRepo(repo); err != nil {
		if errors.Is(err, store.ErrExists) {
			writeErr(w, http.StatusConflict, "repo_exists", "repo id already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if d.Bus != nil {
		d.Bus.Publish("repo.clone_done", "", map[string]any{"repo": full})
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
