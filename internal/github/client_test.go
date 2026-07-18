package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNew_TrimsTrailingSlash(t *testing.T) {
	c := New("https://api.example.com/", "tok")
	if c.baseURL != "https://api.example.com" {
		t.Fatalf("expected trimmed base URL, got %q", c.baseURL)
	}
}

func TestEmptyToken_ReturnsErrNoToken(t *testing.T) {
	c := New("https://api.example.com", "")
	if _, err := c.GetUser(context.Background()); !errors.Is(err, ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
	if _, err := c.ListRepos(context.Background()); !errors.Is(err, ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}

func TestGetUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Fatalf("unexpected auth header %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Fatalf("unexpected accept header %q", got)
		}
		w.Write([]byte(`{"login":"octocat"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	login, err := c.GetUser(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if login != "octocat" {
		t.Fatalf("expected octocat, got %q", login)
	}
}

func TestListRepos_Pagination(t *testing.T) {
	var baseURL string
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/repos" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		pages++
		if r.URL.Query().Get("page") == "2" || pages == 2 {
			w.Write([]byte(`[{"full_name":"o/repo2","private":false,"default_branch":"main"}]`))
			return
		}
		w.Header().Set("Link", `<`+baseURL+`/user/repos?per_page=100&page=2>; rel="next"`)
		w.Write([]byte(`[{"full_name":"o/repo1","private":true,"default_branch":"main"}]`))
	}))
	defer srv.Close()
	baseURL = srv.URL

	c := New(srv.URL, "tok")
	repos, err := c.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d: %+v", len(repos), repos)
	}
	if repos[0].FullName != "o/repo1" || !repos[0].Private || repos[0].DefaultBranch != "main" {
		t.Fatalf("unexpected repo1: %+v", repos[0])
	}
	if repos[1].FullName != "o/repo2" || repos[1].Private {
		t.Fatalf("unexpected repo2: %+v", repos[1])
	}
	if pages != 2 {
		t.Fatalf("expected 2 requests, got %d", pages)
	}
}

func TestETagCache_304Reuse(t *testing.T) {
	requests := 0
	sawIfNoneMatch := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if inm := r.Header.Get("If-None-Match"); inm != "" {
			sawIfNoneMatch = inm
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"abc123"`)
		w.Write([]byte(`{"login":"octocat"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	first, err := c.GetUser(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := c.GetUser(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first != second {
		t.Fatalf("expected identical results, got %q vs %q", first, second)
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
	if sawIfNoneMatch != `"abc123"` {
		t.Fatalf("expected If-None-Match sent, got %q", sawIfNoneMatch)
	}
}

func TestFindPRByBranch_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("head"); got != "o:branch" {
			t.Fatalf("unexpected head param %q", got)
		}
		if got := r.URL.Query().Get("state"); got != "all" {
			t.Fatalf("unexpected state param %q", got)
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	pr, err := c.FindPRByBranch(context.Background(), "o", "r", "branch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr != nil {
		t.Fatalf("expected nil PR, got %+v", pr)
	}
}

func TestFindPRByBranch_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"number":42,"state":"open","merged":false,"head":{"sha":"deadbeef"}}]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	pr, err := c.FindPRByBranch(context.Background(), "o", "r", "branch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pr == nil || pr.Number != 42 || pr.HeadSHA != "deadbeef" {
		t.Fatalf("unexpected PR: %+v", pr)
	}
}

func Test403RateLimit_ReturnsErrBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	if _, err := c.GetUser(context.Background()); !errors.Is(err, ErrBackoff) {
		t.Fatalf("expected ErrBackoff, got %v", err)
	}
}

func Test5xx_ReturnsErrBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	if _, err := c.GetUser(context.Background()); !errors.Is(err, ErrBackoff) {
		t.Fatalf("expected ErrBackoff, got %v", err)
	}
}

func TestGetPR_404IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	if _, err := c.GetPR(context.Background(), "o", "r", 1); err == nil {
		t.Fatalf("expected error for 404 GetPR")
	}
}

func TestGetPR_ReviewDecision(t *testing.T) {
	cases := []struct {
		name     string
		reviews  string
		expected string
	}{
		{
			name:     "no_reviews",
			reviews:  `[]`,
			expected: "",
		},
		{
			name: "all_approved",
			reviews: `[
				{"user":{"login":"alice"},"state":"APPROVED","submitted_at":"2026-01-01T00:00:00Z"},
				{"user":{"login":"bob"},"state":"APPROVED","submitted_at":"2026-01-01T00:00:00Z"}
			]`,
			expected: "approved",
		},
		{
			name: "changes_requested_wins_over_different_user_approval",
			reviews: `[
				{"user":{"login":"alice"},"state":"CHANGES_REQUESTED","submitted_at":"2026-01-01T00:00:00Z"},
				{"user":{"login":"bob"},"state":"APPROVED","submitted_at":"2026-01-02T00:00:00Z"}
			]`,
			expected: "changes_requested",
		},
		{
			name: "same_user_later_approval_clears_changes_requested",
			reviews: `[
				{"user":{"login":"alice"},"state":"CHANGES_REQUESTED","submitted_at":"2026-01-01T00:00:00Z"},
				{"user":{"login":"alice"},"state":"APPROVED","submitted_at":"2026-01-02T00:00:00Z"}
			]`,
			expected: "approved",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/repos/o/r/pulls/1":
					w.Write([]byte(`{"number":1,"state":"open","merged":false,"head":{"sha":"sha1"}}`))
				case r.URL.Path == "/repos/o/r/pulls/1/reviews":
					w.Write([]byte(tc.reviews))
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
			}))
			defer srv.Close()

			c := New(srv.URL, "tok")
			pr, err := c.GetPR(context.Background(), "o", "r", 1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pr.ReviewDecision != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, pr.ReviewDecision)
			}
		})
	}
}

func TestCheckRollup(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "empty",
			body:     `{"total_count":0,"check_runs":[]}`,
			expected: "passing",
		},
		{
			name:     "all_success",
			body:     `{"total_count":2,"check_runs":[{"status":"completed","conclusion":"success"},{"status":"completed","conclusion":"success"}]}`,
			expected: "passing",
		},
		{
			name:     "mixed_neutral_skipped",
			body:     `{"total_count":2,"check_runs":[{"status":"completed","conclusion":"neutral"},{"status":"completed","conclusion":"skipped"}]}`,
			expected: "passing",
		},
		{
			name:     "failure",
			body:     `{"total_count":2,"check_runs":[{"status":"completed","conclusion":"success"},{"status":"completed","conclusion":"failure"}]}`,
			expected: "failing",
		},
		{
			name:     "in_progress",
			body:     `{"total_count":1,"check_runs":[{"status":"in_progress","conclusion":""}]}`,
			expected: "pending",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/o/r/commits/sha1/check-runs" {
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := New(srv.URL, "tok")
			rollup, err := c.CheckRollup(context.Background(), "o", "r", "sha1")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rollup != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, rollup)
			}
		})
	}
}
