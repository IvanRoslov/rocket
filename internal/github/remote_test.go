package github

import (
	"testing"
)

func TestParseRemote(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantOk    bool
	}{
		// SSH format: git@github.com:owner/repo.git
		{
			name:      "ssh_with_git_suffix",
			url:       "git@github.com:octocat/Hello-World.git",
			wantOwner: "octocat",
			wantRepo:  "Hello-World",
			wantOk:    true,
		},
		// SSH format: git@github.com:owner/repo (no .git)
		{
			name:      "ssh_without_git_suffix",
			url:       "git@github.com:golang/go",
			wantOwner: "golang",
			wantRepo:  "go",
			wantOk:    true,
		},
		// HTTPS format: https://github.com/owner/repo.git
		{
			name:      "https_with_git_suffix",
			url:       "https://github.com:IvanRoslov/rocket.git",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false, // HTTPS should not have a colon before the domain
		},
		{
			name:      "https_with_git_suffix_correct",
			url:       "https://github.com/IvanRoslov/rocket.git",
			wantOwner: "IvanRoslov",
			wantRepo:  "rocket",
			wantOk:    true,
		},
		// HTTPS format: https://github.com/owner/repo (no .git)
		{
			name:      "https_without_git_suffix",
			url:       "https://github.com/microsoft/TypeScript",
			wantOwner: "microsoft",
			wantRepo:  "TypeScript",
			wantOk:    true,
		},
		// SSH explicit format: ssh://git@github.com/owner/repo.git
		{
			name:      "ssh_explicit_with_git_suffix",
			url:       "ssh://git@github.com/docker/cli.git",
			wantOwner: "docker",
			wantRepo:  "cli",
			wantOk:    true,
		},
		// SSH explicit format: ssh://git@github.com/owner/repo (no .git)
		{
			name:      "ssh_explicit_without_git_suffix",
			url:       "ssh://git@github.com/kubernetes/kubernetes",
			wantOwner: "kubernetes",
			wantRepo:  "kubernetes",
			wantOk:    true,
		},
		// Negative cases
		{
			name:      "gitlab_url",
			url:       "git@gitlab.com:owner/repo.git",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
		{
			name:      "bitbucket_url",
			url:       "git@bitbucket.org:owner/repo.git",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
		{
			name:      "empty_url",
			url:       "",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
		{
			name:      "malformed_ssh_missing_separator",
			url:       "git@github.com/owner/repo.git",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
		{
			name:      "malformed_https_missing_domain",
			url:       "https://owner/repo.git",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
		{
			name:      "malformed_missing_repo",
			url:       "git@github.com:owner",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
		{
			name:      "garbage",
			url:       "not-a-valid-url",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
		{
			name:      "empty_owner",
			url:       "git@github.com:/repo.git",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
		{
			name:      "empty_repo",
			url:       "git@github.com:owner/.git",
			wantOwner: "",
			wantRepo:  "",
			wantOk:    false,
		},
		// Valid special characters in names
		{
			name:      "names_with_hyphens_underscores_dots",
			url:       "git@github.com:my-org_name.io/my-repo_v1.0.git",
			wantOwner: "my-org_name.io",
			wantRepo:  "my-repo_v1.0",
			wantOk:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := ParseRemote(tt.url)
			if owner != tt.wantOwner || repo != tt.wantRepo || ok != tt.wantOk {
				t.Errorf("ParseRemote(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.url, owner, repo, ok, tt.wantOwner, tt.wantRepo, tt.wantOk)
			}
		})
	}
}
