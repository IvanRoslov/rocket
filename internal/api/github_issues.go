package api

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"

	"github.com/IvanRoslov/rocket/internal/github"
	"github.com/IvanRoslov/rocket/internal/store"
)

// issueResponse is the JSON shape of a GitHub issue as returned by
// GET /v1/github/issues. Labels are flattened to their names.
type issueResponse struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	HTMLURL   string   `json:"html_url"`
	State     string   `json:"state"`
	UpdatedAt string   `json:"updated_at"`
	Labels    []string `json:"labels"`
}

func toIssueResponse(i github.Issue) issueResponse {
	labels := make([]string, 0, len(i.Labels))
	for _, l := range i.Labels {
		labels = append(labels, l.Name)
	}
	return issueResponse{
		Number:    i.Number,
		Title:     i.Title,
		Body:      i.Body,
		HTMLURL:   i.HTMLURL,
		State:     i.State,
		UpdatedAt: i.UpdatedAt,
		Labels:    labels,
	}
}

// registerGithubIssuesRoutes wires the /v1/github/issues route onto mux.
func registerGithubIssuesRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/github/issues", func(w http.ResponseWriter, r *http.Request) {
		handleGetGithubIssues(w, r, d)
	})
}

// handleGetGithubIssues serves GET /v1/github/issues, listing issues (pull
// requests excluded) for a repository identified either by
// ?repo=owner/name or by ?repo_id=<registered repo id> (resolved to
// owner/name via the repo's git remote origin). ?state defaults to "open".
//
// No caching is applied: issues change often enough (and this is a
// dashboard-driven, low-frequency endpoint) that a direct call each time is
// simpler and acceptable, unlike the 5-minute repo catalog cache.
func handleGetGithubIssues(w http.ResponseWriter, r *http.Request, d Deps) {
	if d.GH == nil {
		writeErr(w, http.StatusBadRequest, "no_token", "no GitHub token configured")
		return
	}

	owner, name, code, msg := resolveIssuesRepo(r, d)
	if code != "" {
		status := http.StatusBadRequest
		if code == "repo_not_found" {
			status = http.StatusNotFound
		}
		writeErr(w, status, code, msg)
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

	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		state = "open"
	}

	issues, err := client.ListIssues(r.Context(), owner, name, state)
	if err != nil {
		if errors.Is(err, github.ErrBackoff) {
			writeErr(w, http.StatusBadGateway, "github_unreachable", err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	out := make([]issueResponse, 0, len(issues))
	for _, issue := range issues {
		out = append(out, toIssueResponse(issue))
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": out})
}

// resolveIssuesRepo resolves the owner/name for the request, supporting
// both ?repo=owner/name and ?repo_id=<registered repo id>. On failure it
// returns a non-empty error code/message pair to write to the client.
func resolveIssuesRepo(r *http.Request, d Deps) (owner, name, code, msg string) {
	if repoParam := strings.TrimSpace(r.URL.Query().Get("repo")); repoParam != "" {
		owner, name, ok := parseGithubRepo(repoParam)
		if !ok {
			return "", "", "bad_request", "repo must be in owner/name format"
		}
		return owner, name, "", ""
	}

	repoID := strings.TrimSpace(r.URL.Query().Get("repo_id"))
	if repoID == "" {
		return "", "", "bad_request", "either repo or repo_id is required"
	}

	repo, err := d.Store.GetRepo(repoID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", "", "repo_not_found", "no repo registered with that id"
		}
		return "", "", "internal_error", err.Error()
	}

	remoteURL, err := gitRemoteOrigin(r.Context(), repo.Path)
	if err != nil {
		return "", "", "not_a_github_repo", "could not read git remote origin for repo"
	}

	owner, name, ok := github.ParseRemote(remoteURL)
	if !ok {
		return "", "", "not_a_github_repo", "repo's remote origin is not a GitHub URL"
	}
	return owner, name, "", ""
}

// gitRemoteOrigin executes `git -C dir remote get-url origin`, mirroring
// ghpoller's runGitRemoteOrigin (unexported there, so duplicated here
// rather than reaching across packages for one small exec.Command call).
func gitRemoteOrigin(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
