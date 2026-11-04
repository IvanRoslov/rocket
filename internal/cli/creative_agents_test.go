package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/IvanRoslov/rocket/internal/client"
)

func TestCreativeAgentCLIContextAndReplies(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		path  string
		key   string
		value any
	}{
		{[]string{"send", "4", "--stage", "5", "--item", "6", "--version", "7", "--body", "hello"}, "/v1/creative/runs/4/messages", "artifact_version_ids", []any{float64(7)}},
		{[]string{"spawn", "4", "--stage", "5", "--name", "scene", "--body", "make scene"}, "/v1/creative/runs/4/workers", "prompt", "make scene"},
		{[]string{"reply", "9", "--body", "noted"}, "/v1/questions/9/reply", "body", "noted"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			c := newUnixSocketTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.Method != "POST" {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				actual, _ := json.Marshal(body[tc.key])
				expected, _ := json.Marshal(tc.value)
				if !bytes.Equal(actual, expected) {
					t.Errorf("%s=%s", tc.key, actual)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":1}`))
			}))
			cmd := creativeAgentCommands(func() (*client.Client, error) { return c, nil })
			cmd.SetArgs(tc.args)
			cmd.SetOut(new(bytes.Buffer))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
