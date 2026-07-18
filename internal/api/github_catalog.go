package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/github"
)

// catalogTTL bounds how long a fetched GitHub repo catalog is served from
// the in-memory cache before ListRepos is called again. Overridable in
// tests.
var catalogTTL = 5 * time.Minute

// catalogCache is a process-wide cache of the authenticated user's GitHub
// repos, shared by every GET /v1/github/repos request regardless of the
// "q" filter (filtering happens after the cache lookup). rocketd is a
// single daemon process, so a package-level cache (rather than one threaded
// through Deps) is sufficient and keeps the handler simple.
var catalogCache struct {
	mu      sync.Mutex
	fetched time.Time
	repos   []github.Repo
}

// resetCatalogCache clears the cache. Used by tests to avoid cross-test
// pollution of the package-level cache.
func resetCatalogCache() {
	catalogCache.mu.Lock()
	defer catalogCache.mu.Unlock()
	catalogCache.fetched = time.Time{}
	catalogCache.repos = nil
}

// catalogRepoResponse is the JSON shape of a GitHub repo as returned by
// GET /v1/github/repos.
type catalogRepoResponse struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

// registerGithubCatalogRoutes wires the /v1/github/repos route onto mux.
func registerGithubCatalogRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/github/repos", func(w http.ResponseWriter, r *http.Request) {
		handleGetGithubRepos(w, r, d)
	})
}

// handleGetGithubRepos serves GET /v1/github/repos?q=<substring>: the
// authenticated user's GitHub repos (via a 5-minute in-memory cache),
// filtered case-insensitively by substring match on full_name.
func handleGetGithubRepos(w http.ResponseWriter, r *http.Request, d Deps) {
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

	repos, err := cachedListRepos(r.Context(), client)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "github_unreachable", err.Error())
		return
	}

	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	out := make([]catalogRepoResponse, 0, len(repos))
	for _, repo := range repos {
		if q != "" && !strings.Contains(strings.ToLower(repo.FullName), q) {
			continue
		}
		out = append(out, catalogRepoResponse{
			FullName:      repo.FullName,
			Private:       repo.Private,
			DefaultBranch: repo.DefaultBranch,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": out})
}

// cachedListRepos returns the cached repo catalog if it was fetched within
// catalogTTL, otherwise calls client.ListRepos and refreshes the cache.
func cachedListRepos(ctx context.Context, client *github.Client) ([]github.Repo, error) {
	catalogCache.mu.Lock()
	if !catalogCache.fetched.IsZero() && time.Since(catalogCache.fetched) < catalogTTL {
		repos := catalogCache.repos
		catalogCache.mu.Unlock()
		return repos, nil
	}
	catalogCache.mu.Unlock()

	repos, err := client.ListRepos(ctx)
	if err != nil {
		return nil, err
	}

	catalogCache.mu.Lock()
	catalogCache.repos = repos
	catalogCache.fetched = time.Now()
	catalogCache.mu.Unlock()

	return repos, nil
}
