// Package github provides a minimal GitHub REST API client used by rocket
// to inspect repositories, pull requests, reviews and check-run status.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrNoToken is returned when a Client is used without an access token.
var ErrNoToken = errors.New("github: no token")

// ErrBackoff is returned when GitHub signals rate limiting (403 with
// X-RateLimit-Remaining: 0) or a server error (5xx), indicating the caller
// should back off and retry later.
var ErrBackoff = errors.New("github: backoff")

// Client is a minimal GitHub REST API client with an in-memory ETag cache.
type Client struct {
	baseURL string
	token   string
	http    *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	etag string
	body []byte
}

// New creates a Client for the given API base URL (trailing slash
// tolerated) and bearer token.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 15 * time.Second},
		cache:   make(map[string]cacheEntry),
	}
}

// Repo is a subset of the GitHub repository resource.
type Repo struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

// PR is a subset of the GitHub pull request resource, augmented with an
// aggregated review decision.
type PR struct {
	Number         int
	State          string
	Merged         bool
	HeadSHA        string
	ReviewDecision string
}

type prAPI struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Merged bool   `json:"merged"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

type reviewAPI struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	State       string `json:"state"`
	SubmittedAt string `json:"submitted_at"`
}

type checkRunsResp struct {
	TotalCount int `json:"total_count"`
	CheckRuns  []struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"check_runs"`
}

// doGet performs an authenticated GET, transparently handling the ETag
// cache (sending If-None-Match and reusing the cached body on 304) and
// classifying rate-limit/server errors as ErrBackoff.
func (c *Client) doGet(ctx context.Context, url string) (status int, body []byte, header http.Header, err error) {
	if c.token == "" {
		return 0, nil, nil, ErrNoToken
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	c.mu.Lock()
	entry, cached := c.cache[url]
	c.mu.Unlock()
	if cached && entry.etag != "" {
		req.Header.Set("If-None-Match", entry.etag)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return http.StatusOK, entry.body, resp.Header, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, nil, err
	}

	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return 0, nil, nil, fmt.Errorf("%w: rate limited", ErrBackoff)
	}
	if resp.StatusCode >= 500 {
		return 0, nil, nil, fmt.Errorf("%w: server error %d", ErrBackoff, resp.StatusCode)
	}

	if resp.StatusCode == http.StatusOK {
		if etag := resp.Header.Get("ETag"); etag != "" {
			c.mu.Lock()
			c.cache[url] = cacheEntry{etag: etag, body: respBody}
			c.mu.Unlock()
		}
	}

	return resp.StatusCode, respBody, resp.Header, nil
}

// GetUser returns the authenticated user's login.
func (c *Client) GetUser(ctx context.Context) (string, error) {
	status, body, _, err := c.doGet(ctx, c.baseURL+"/user")
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("github: GetUser: unexpected status %d", status)
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

var linkRE = regexp.MustCompile(`<([^>]+)>;\s*rel="([^"]+)"`)

func nextLink(link string) string {
	if link == "" {
		return ""
	}
	for _, part := range strings.Split(link, ",") {
		m := linkRE.FindStringSubmatch(strings.TrimSpace(part))
		if len(m) == 3 && m[2] == "next" {
			return m[1]
		}
	}
	return ""
}

// ListRepos returns all repositories visible to the authenticated user,
// following pagination via the Link response header.
func (c *Client) ListRepos(ctx context.Context) ([]Repo, error) {
	var repos []Repo
	url := c.baseURL + "/user/repos?per_page=100"
	for url != "" {
		status, body, header, err := c.doGet(ctx, url)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("github: ListRepos: unexpected status %d", status)
		}
		var page []Repo
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		repos = append(repos, page...)
		url = nextLink(header.Get("Link"))
	}
	return repos, nil
}

// FindPRByBranch finds the pull request whose head is owner:branch in
// owner/repo. Returns (nil, nil) if none exists.
func (c *Client) FindPRByBranch(ctx context.Context, owner, repo, branch string) (*PR, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls?head=%s:%s&state=all&per_page=1", c.baseURL, owner, repo, owner, branch)
	status, body, _, err := c.doGet(ctx, url)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("github: FindPRByBranch: unexpected status %d", status)
	}
	var prs []prAPI
	if err := json.Unmarshal(body, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	p := prs[0]
	return &PR{Number: p.Number, State: p.State, Merged: p.Merged, HeadSHA: p.Head.SHA}, nil
}

// GetPR fetches a pull request by number, along with its aggregated
// review decision.
func (c *Client) GetPR(ctx context.Context, owner, repo string, number int) (*PR, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.baseURL, owner, repo, number)
	status, body, _, err := c.doGet(ctx, url)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("github: GetPR: unexpected status %d for PR #%d", status, number)
	}
	var p prAPI
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}

	reviewsURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", c.baseURL, owner, repo, number)
	rstatus, rbody, _, err := c.doGet(ctx, reviewsURL)
	if err != nil {
		return nil, err
	}
	if rstatus != http.StatusOK {
		return nil, fmt.Errorf("github: GetPR: unexpected status %d for PR #%d reviews", rstatus, number)
	}
	var reviews []reviewAPI
	if err := json.Unmarshal(rbody, &reviews); err != nil {
		return nil, err
	}

	return &PR{
		Number:         p.Number,
		State:          p.State,
		Merged:         p.Merged,
		HeadSHA:        p.Head.SHA,
		ReviewDecision: reviewDecision(reviews),
	}, nil
}

// reviewDecision aggregates a PR's reviews: for each reviewer, only their
// latest review (by SubmittedAt) counts. If any reviewer's latest review
// is CHANGES_REQUESTED, the decision is "changes_requested". Otherwise, if
// there is at least one reviewer and all latest reviews are APPROVED, the
// decision is "approved". Otherwise "".
func reviewDecision(reviews []reviewAPI) string {
	latest := make(map[string]reviewAPI)
	for _, r := range reviews {
		existing, ok := latest[r.User.Login]
		if !ok || r.SubmittedAt >= existing.SubmittedAt {
			latest[r.User.Login] = r
		}
	}
	if len(latest) == 0 {
		return ""
	}

	hasChangesRequested := false
	allApproved := true
	for _, r := range latest {
		switch r.State {
		case "CHANGES_REQUESTED":
			hasChangesRequested = true
			allApproved = false
		case "APPROVED":
			// counts toward allApproved
		default:
			allApproved = false
		}
	}

	switch {
	case hasChangesRequested:
		return "changes_requested"
	case allApproved:
		return "approved"
	default:
		return ""
	}
}

var failingConclusions = map[string]bool{
	"failure":         true,
	"cancelled":       true,
	"timed_out":       true,
	"action_required": true,
}

// CheckRollup summarizes the check-run status for a commit SHA as one of
// "passing", "pending" or "failing".
func (c *Client) CheckRollup(ctx context.Context, owner, repo, sha string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s/check-runs", c.baseURL, owner, repo, sha)
	status, body, _, err := c.doGet(ctx, url)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("github: CheckRollup: unexpected status %d", status)
	}
	var resp checkRunsResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	if resp.TotalCount == 0 || len(resp.CheckRuns) == 0 {
		return "passing", nil
	}

	for _, run := range resp.CheckRuns {
		if failingConclusions[run.Conclusion] {
			return "failing", nil
		}
	}
	for _, run := range resp.CheckRuns {
		if run.Status != "completed" {
			return "pending", nil
		}
	}
	return "passing", nil
}
