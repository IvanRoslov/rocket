package cli

import (
	"bytes"
	"encoding/json"
	"github.com/IvanRoslov/rocket/internal/client"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestCreativeCLIRequests(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "creative-orch")
	file := filepath.Join(t.TempDir(), "pipeline.json")
	if err := os.WriteFile(file, []byte(`{"name":"Video","stages":[{"name":"Plan","approval":"auto"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args         []string
		method, path string
		body         map[string]any
	}{
		{[]string{"pipeline", "ls"}, "GET", "/v1/creative/pipelines", nil},
		{[]string{"pipeline", "ls", "--project", "old-project"}, "GET", "/v1/creative/pipelines", nil},
		{[]string{"pipeline", "update", "3", "--file", file}, "PUT", "/v1/creative/pipelines/3", map[string]any{"name": "Video"}},
		{[]string{"pipeline", "create", "--file", file}, "POST", "/v1/creative/pipelines", map[string]any{"name": "Video"}},
		{[]string{"stage", "action", "3", "7", "approve", "--review-revision", "2"}, "POST", "/v1/creative/runs/3/stages/7/actions", map[string]any{"action": "approve", "review_revision": float64(2)}},
		{[]string{"item", "action", "3", "7", "9", "submit", "--artifact-version", "42"}, "POST", "/v1/creative/runs/3/stages/7/items/9/actions", map[string]any{"action": "submit", "artifact_version_ids": []any{float64(42)}}},
		{[]string{"stage", "action", "3", "7", "withdraw", "--review-revision", "2", "--reason", "Correct result"}, "POST", "/v1/creative/runs/3/stages/7/actions", map[string]any{"action": "withdraw", "review_revision": float64(2), "reason": "Correct result"}},
		{[]string{"item", "action", "3", "7", "9", "withdraw", "--review-revision", "2", "--reason", "Correct scene"}, "POST", "/v1/creative/runs/3/stages/7/items/9/actions", map[string]any{"action": "withdraw", "review_revision": float64(2), "reason": "Correct scene"}},
		{[]string{"run", "ls", "--project", "a b"}, "GET", "/v1/creative/runs?project_id=a+b", nil},
	} {
		t.Run(tc.path, func(t *testing.T) {
			called := false
			c := newUnixSocketTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Method != tc.method || r.URL.RequestURI() != tc.path {
					t.Errorf("got %s %s", r.Method, r.URL.RequestURI())
				}
				if r.Header.Get("X-Rocket-Session") != "creative-orch" {
					t.Error("missing agent identity")
				}
				if tc.body != nil {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					for k, v := range tc.body {
						a, _ := json.Marshal(body[k])
						b, _ := json.Marshal(v)
						if string(a) != string(b) {
							t.Errorf("%s=%s want %s", k, a, b)
						}
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":3}`))
			}))
			cmd := creativeCommands(func() (*client.Client, error) { return c, nil })
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if !called || !bytes.Contains(out.Bytes(), []byte(`"id": 3`)) {
				t.Fatalf("called=%v output=%s", called, out.String())
			}
		})
	}
}
func TestCreativeCLIRejectsInvalidInputBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{{"pipeline", "create"}, {"run", "show", "../shutdown"}, {"stage", "action", "1", "2", "approve"}, {"item", "progress", "1", "2", "3", "--progress", "101"}} {
		cmd := creativeCommands(func() (*client.Client, error) { t.Fatal("connected with invalid input"); return nil, nil })
		cmd.SetArgs(args)
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestCreativeCLIWithdrawRequiresReasonAndRevision(t *testing.T) {
	for _, flags := range [][]string{{}, {"--review-revision", "1"}, {"--reason", "Fix"}, {"--review-revision", "1", "--reason", "  "}} {
		cmd := creativeCommands(func() (*client.Client, error) { t.Fatal("connected with invalid withdrawal"); return nil, nil })
		cmd.SetArgs(append([]string{"stage", "action", "1", "2", "withdraw"}, flags...))
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetErr(new(bytes.Buffer))
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted %v", flags)
		}
	}
}

func TestCreativeCLIReturnRequiresFeedbackBeforeConnecting(t *testing.T) {
	for _, target := range [][]string{{"stage", "action", "1", "2", "reject"}, {"item", "action", "1", "2", "3", "reject"}} {
		for _, reason := range []string{"", " \n "} {
			cmd := creativeCommands(func() (*client.Client, error) { t.Fatal("connected without return feedback"); return nil, nil })
			cmd.SetArgs(append(target, "--review-revision", "1", "--reason", reason))
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))
			if err := cmd.Execute(); err == nil {
				t.Fatal("accepted blank return feedback")
			}
		}
	}
}
