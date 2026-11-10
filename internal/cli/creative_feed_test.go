package cli

import (
	"bytes"
	"encoding/json"
	"github.com/IvanRoslov/rocket/internal/client"
	"net/http"
	"strings"
	"testing"
)

func TestCreativeFeedCLI(t *testing.T) {
	cases := []struct {
		args                      []string
		method, path, query, body string
	}{
		{[]string{"feed", "7", "--stage", "9", "--item", "12"}, "GET", "/v1/creative/runs/7/feed", "item_id=12&stage_id=9", ""},
		{[]string{"feed", "post", "7", "--stage", "9", "--item", "12", "--version", "101", "--version", "102", "--file", "-"}, "POST", "/v1/creative/runs/7/feed", "", `{"stage_id":9,"item_id":12,"artifact_version_ids":[101,102],"body":"reply from stdin"}`},
		{[]string{"feed", "propose", "7", "--body", "2 calls $0.20", "--replaces", "42"}, "POST", "/v1/creative/runs/7/feed/cards", "", `{"stage_id":0,"item_id":0,"artifact_version_ids":[],"body":"2 calls $0.20","replaces_entry_id":42}`},
		{[]string{"feed", "card", "7", "42"}, "GET", "/v1/creative/runs/7/feed/cards/42", "", ""},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			c := newUnixSocketTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.path || r.URL.RawQuery != tc.query {
					t.Errorf("request %s %s", r.Method, r.URL)
				}
				if tc.body != "" {
					var got, want any
					if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
						t.Error(err)
					}
					json.Unmarshal([]byte(tc.body), &want)
					a, _ := json.Marshal(got)
					b, _ := json.Marshal(want)
					if !bytes.Equal(a, b) {
						t.Errorf("body %s want %s", a, b)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":42}`))
			}))
			cmd := creativeAgentCommands(func() (*client.Client, error) { return c, nil })
			cmd.SetArgs(tc.args)
			cmd.SetIn(strings.NewReader("reply from stdin"))
			cmd.SetOut(new(bytes.Buffer))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestCreativeFeedCLIRefusesAskAndInvalidScope(t *testing.T) {
	for _, args := range [][]string{{"ask", "7", "--body", "old question"}, {"feed", "post", "7", "--item", "2", "--body", "bad"}, {"feed", "propose", "7", "--body", "bad", "--replaces", "-1"}} {
		cmd := creativeAgentCommands(func() (*client.Client, error) { t.Fatal("invalid command reached server"); return nil, nil })
		cmd.SetArgs(args)
		cmd.SetOut(new(bytes.Buffer))
		err := cmd.Execute()
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
		if args[0] == "ask" && !strings.Contains(err.Error(), "rocket creative agent feed post 7") {
			t.Fatalf("missing hint: %v", err)
		}
	}
}
