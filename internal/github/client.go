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
	"net/url"
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

	// mu guards cache. cache is unbounded for the lifetime of the process:
	// it is keyed by distinct request URLs (which include query strings,
	// e.g. page number), so its size is bounded by the number of distinct
	// URLs this client is asked to poll, not by request volume over time.
	mu    sync.Mutex
	cache map[string]cacheEntry
}

// cacheEntry holds everything needed to answer a subsequent request without
// hitting the network: the ETag to send as If-None-Match, the cached body,
// and the Link header from the original 200 response. The Link header is
// cached because GitHub does not resend it on a 304 Not Modified response,
// so without caching it, pagination would silently truncate to one page
// whenever a request hit the ETag cache.
type cacheEntry struct {
	etag string
	body []byte
	link string
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
	// MergedAt is only used to derive Merged when the `merged` field itself
	// is absent (see merged()); GitHub's list-PRs endpoint never populates
	// `merged`, only the single-PR endpoint does.
	MergedAt string `json:"merged_at"`
	Head     struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// merged reports whether the PR is merged. The list-PRs endpoint (used by
// FindPRByBranch) never populates the `merged` field, so it is derived from
// merged_at being non-empty on a closed PR. The single-PR endpoint (used by
// GetPR) does populate `merged` directly, but we still OR in the merged_at
// derivation for consistency between the two code paths.
func (p prAPI) merged() bool {
	return p.Merged || (p.State == "closed" && p.MergedAt != "")
}

type reviewAPI struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	State       string `json:"state"`
	SubmittedAt string `json:"submitted_at"`
}

type checkRunAPI struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type checkRunsResp struct {
	TotalCount int           `json:"total_count"`
	CheckRuns  []checkRunAPI `json:"check_runs"`
}

// doGet performs an authenticated GET, transparently handling the ETag
// cache (sending If-None-Match and reusing the cached body on 304) and
// classifying rate-limit/server errors as ErrBackoff. It returns the Link
// response header (used for pagination) alongside the body: on a 304, the
// Link cached from the original 200 response is returned, since GitHub does
// not resend it on a 304.
func (c *Client) doGet(ctx context.Context, url string) (status int, body []byte, link string, err error) {
	if c.token == "" {
		return 0, nil, "", ErrNoToken
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, "", err
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
		return 0, nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return http.StatusOK, entry.body, entry.link, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, "", err
	}

	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return 0, nil, "", fmt.Errorf("%w: rate limited", ErrBackoff)
	}
	if resp.StatusCode >= 500 {
		return 0, nil, "", fmt.Errorf("%w: server error %d", ErrBackoff, resp.StatusCode)
	}

	respLink := resp.Header.Get("Link")
	if resp.StatusCode == http.StatusOK {
		if etag := resp.Header.Get("ETag"); etag != "" {
			c.mu.Lock()
			c.cache[url] = cacheEntry{etag: etag, body: respBody, link: respLink}
			c.mu.Unlock()
		}
	}

	return resp.StatusCode, respBody, respLink, nil
}

// getPaginated performs a series of authenticated GETs starting at url,
// invoking decodePage with the body of each page in turn and following the
// Link rel="next" header until pagination is exhausted or an error occurs.
func (c *Client) getPaginated(ctx context.Context, startURL string, decodePage func(body []byte) error) error {
	pageURL := startURL
	for pageURL != "" {
		status, body, link, err := c.doGet(ctx, pageURL)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("github: unexpected status %d for %s", status, pageURL)
		}
		if err := decodePage(body); err != nil {
			return err
		}
		pageURL = nextLink(link)
	}
	return nil
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
	err := c.getPaginated(ctx, c.baseURL+"/user/repos?per_page=100", func(body []byte) error {
		var page []Repo
		if err := json.Unmarshal(body, &page); err != nil {
			return err
		}
		repos = append(repos, page...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("github: ListRepos: %w", err)
	}
	return repos, nil
}

// FindPRByBranch finds the pull request whose head is owner:branch in
// owner/repo. Returns (nil, nil) if none exists.
func (c *Client) FindPRByBranch(ctx context.Context, owner, repo, branch string) (*PR, error) {
	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls?head=%s:%s&state=all&per_page=1",
		c.baseURL, owner, repo, url.QueryEscape(owner), url.QueryEscape(branch))
	status, body, _, err := c.doGet(ctx, reqURL)
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
	return &PR{Number: p.Number, State: p.State, Merged: p.merged(), HeadSHA: p.Head.SHA}, nil
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

	reviewsURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews?per_page=100", c.baseURL, owner, repo, number)
	var reviews []reviewAPI
	err = c.getPaginated(ctx, reviewsURL, func(body []byte) error {
		var page []reviewAPI
		if err := json.Unmarshal(body, &page); err != nil {
			return err
		}
		reviews = append(reviews, page...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("github: GetPR: reviews for PR #%d: %w", number, err)
	}

	return &PR{
		Number:         p.Number,
		State:          p.State,
		Merged:         p.merged(),
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
	// SubmittedAt is an ISO-8601 timestamp, so lexical string comparison
	// sorts correctly. On a tie (identical SubmittedAt for the same
	// reviewer, which GitHub does not produce in practice), ">=" picks the
	// review encountered later in the slice, which matches the order the
	// API returns reviews in (chronological, so effectively a no-op).
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
	checkRunsURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s/check-runs?per_page=100", c.baseURL, owner, repo, sha)
	var runs []checkRunAPI
	err := c.getPaginated(ctx, checkRunsURL, func(body []byte) error {
		var resp checkRunsResp
		if err := json.Unmarshal(body, &resp); err != nil {
			return err
		}
		runs = append(runs, resp.CheckRuns...)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("github: CheckRollup: %w", err)
	}
	if len(runs) == 0 {
		return "passing", nil
	}

	for _, run := range runs {
		if failingConclusions[run.Conclusion] {
			return "failing", nil
		}
	}
	for _, run := range runs {
		if run.Status != "completed" {
			return "pending", nil
		}
	}
	return "passing", nil
}
