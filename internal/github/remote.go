package github

import (
	"regexp"
	"strings"
)

// ParseRemote parses a Git remote URL and extracts the GitHub owner and
// repository name. It handles the following formats:
//
//   - git@github.com:owner/repo.git
//   - git@github.com:owner/repo
//   - https://github.com/owner/repo.git
//   - https://github.com/owner/repo
//   - ssh://git@github.com/owner/repo.git
//
// It returns (owner, repo, true) on success, or ("", "", false) if the URL
// is not a GitHub URL or is malformed.
func ParseRemote(url string) (owner, repo string, ok bool) {
	// SSH format: git@github.com:owner/repo[.git]
	if strings.HasPrefix(url, "git@github.com:") {
		rest := strings.TrimPrefix(url, "git@github.com:")
		return parseOwnerRepo(rest)
	}

	// SSH explicit format: ssh://git@github.com/owner/repo[.git]
	if strings.HasPrefix(url, "ssh://git@github.com/") {
		rest := strings.TrimPrefix(url, "ssh://git@github.com/")
		return parseOwnerRepo(rest)
	}

	// HTTPS format: https://github.com/owner/repo[.git]
	if strings.HasPrefix(url, "https://github.com/") {
		rest := strings.TrimPrefix(url, "https://github.com/")
		return parseOwnerRepo(rest)
	}

	return "", "", false
}

// parseOwnerRepo parses "owner/repo[.git]" format, returning (owner, repo, ok).
// It verifies that both owner and repo are non-empty after parsing.
func parseOwnerRepo(s string) (owner, repo string, ok bool) {
	// Remove trailing .git if present
	s = strings.TrimSuffix(s, ".git")

	// Split on "/" to get owner and repo
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return "", "", false
	}

	owner = strings.TrimSpace(parts[0])
	repo = strings.TrimSpace(parts[1])

	// Reject empty parts
	if owner == "" || repo == "" {
		return "", "", false
	}

	// Validate owner and repo names contain only valid characters
	// GitHub allows alphanumerics, hyphens, and underscores
	if !isValidGitHubName(owner) || !isValidGitHubName(repo) {
		return "", "", false
	}

	return owner, repo, true
}

// isValidGitHubName checks if a string is a valid GitHub owner or repo name.
// GitHub allows alphanumerics, hyphens, and underscores; we also accept dots.
func isValidGitHubName(s string) bool {
	if s == "" {
		return false
	}
	validNameRe := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	return validNameRe.MatchString(s)
}
