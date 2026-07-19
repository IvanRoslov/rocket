package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestParseTmuxVersion(t *testing.T) {
	cases := []struct {
		in        string
		wantMajor int
		wantMinor int
		wantOK    bool
	}{
		{"tmux 3.6a\n", 3, 6, true},
		{"tmux 3.0\n", 3, 0, true},
		{"tmux 2.8\n", 2, 8, true},
		{"tmux next-3.4\n", 3, 4, true},
		{"garbage output\n", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		major, minor, ok := parseTmuxVersion(c.in)
		if ok != c.wantOK {
			t.Errorf("parseTmuxVersion(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if major != c.wantMajor || minor != c.wantMinor {
			t.Errorf("parseTmuxVersion(%q) = %d.%d, want %d.%d", c.in, major, minor, c.wantMajor, c.wantMinor)
		}
	}
}

func TestPrintDoctorResults_AnyFail(t *testing.T) {
	var buf bytes.Buffer
	results := []checkResult{
		{statusOK, "a", "ok"},
		{statusWarn, "b", "meh"},
	}
	if printDoctorResults(&buf, results) {
		t.Fatal("expected anyFail = false")
	}

	results = append(results, checkResult{statusFail, "c", "bad"})
	if !printDoctorResults(&buf, results) {
		t.Fatal("expected anyFail = true")
	}
}

// TestCheckGitHub tests the GitHub token check.
func TestCheckGitHub(t *testing.T) {
	cases := []struct {
		name           string
		tokenResponse  map[string]string
		expectedStatus checkStatus
		expectedDetail string
	}{
		{
			name:           "token set",
			tokenResponse:  map[string]string{"github_token": "ghp_abcd"},
			expectedStatus: statusOK,
			expectedDetail: "token set (PR tracking active)",
		},
		{
			name:           "no token",
			tokenResponse:  map[string]string{"github_token": ""},
			expectedStatus: statusWarn,
			expectedDetail: "no token — PR/CI tracking inactive (rocket github auth <token>)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/v1/settings", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tc.tokenResponse)
			})

			c := newUnixSocketTestServer(t, mux)

			result := checkGitHub(c)
			if result.Status != tc.expectedStatus {
				t.Errorf("status = %v, want %v", result.Status, tc.expectedStatus)
			}
			if result.Detail != tc.expectedDetail {
				t.Errorf("detail = %q, want %q", result.Detail, tc.expectedDetail)
			}
		})
	}
}
