package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/client"
)

func TestCreativeAgentCLIContextAndReplies(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		path  string
		key   string
		value any
	}{
		{[]string{"launch", "4", "--allow", "cheap,deep"}, "/v1/creative/runs/4/sessions", "allowed_profiles", []any{"cheap", "deep"}},
		{[]string{"launch", "4", "--allow", ""}, "/v1/creative/runs/4/sessions", "allowed_profiles", []any{}},
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

func TestCreativeSendWaitUsesMessageDelivery(t *testing.T) {
	restore := setPollIntervalForTest(t, time.Millisecond)
	defer restore()
	oldJSON := flags.JSON
	defer func() { flags.JSON = oldJSON }()
	for _, tc := range []struct {
		name     string
		json     bool
		terminal string
	}{
		{"confirmed", false, "delivered"}, {"failed", false, "failed"}, {"json confirmed", true, "delivered"}, {"json failed", true, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags.JSON = tc.json
			polls := 0
			c := newUnixSocketTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method + " " + r.URL.Path {
				case "POST /v1/creative/runs/4/messages":
					var in struct {
						To       string  `json:"to"`
						Body     string  `json:"body"`
						Stage    int64   `json:"stage_id"`
						Item     int64   `json:"item_id"`
						Versions []int64 `json:"artifact_version_ids"`
					}
					if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
						t.Error(err)
					}
					if in.To != "requested-worker" || in.Body != "hello" || in.Stage != 5 || in.Item != 6 || len(in.Versions) != 1 || in.Versions[0] != 7 {
						t.Errorf("send payload: %+v", in)
					}
					w.WriteHeader(201)
					w.Write([]byte(`{"id":42,"to":"actual-worker","status":"queued","creative_context":{"run_id":4,"stage_id":5,"item_id":6},"artifact_version_ids":[7]}`))
				case "GET /v1/messages/42":
					polls++
					status := "accepted"
					if polls == 1 {
						status = "queued"
					}
					if polls >= 4 {
						status = tc.terminal
					}
					json.NewEncoder(w).Encode(map[string]any{"id": 42, "to": "actual-worker", "status": status, "reason": "delivery_unconfirmed", "attempts": 1})
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.Error(w, "wrong endpoint", 404)
				}
			}))
			cmd := creativeAgentCommands(func() (*client.Client, error) { return c, nil })
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs([]string{"send", "4", "--to", "requested-worker", "--stage", "5", "--item", "6", "--version", "7", "--body", "hello", "--wait"})
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			err := cmd.Execute()
			if (err != nil) != (tc.terminal == "failed") {
				t.Fatalf("error: %v, output: %s %s", err, out.String(), errOut.String())
			}
			if polls != 4 {
				t.Fatalf("polls=%d; accepted must not terminate", polls)
			}
			if tc.json {
				var result struct {
					ID     int64  `json:"id"`
					To     string `json:"to"`
					Status string `json:"status"`
					Reason string `json:"reason"`
				}
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatalf("terminal JSON %q: %v", out.String(), err)
				}
				if result.ID != 42 || result.To != "actual-worker" || result.Status != tc.terminal {
					t.Fatalf("result: %+v", result)
				}
				if tc.terminal == "failed" && result.Reason != "delivery_unconfirmed" {
					t.Fatalf("reason: %+v", result)
				}
			} else {
				if !strings.Contains(out.String(), "message 42 to actual-worker queued") || strings.Count(out.String(), "accepted (") != 1 {
					t.Fatalf("progress: %s", out.String())
				}
				if tc.terminal == "failed" {
					if !strings.Contains(errOut.String(), "message 42 to actual-worker failed: delivery_unconfirmed") {
						t.Fatalf("failure: %s", errOut.String())
					}
				} else if !strings.Contains(out.String(), "message 42 to actual-worker delivered") {
					t.Fatalf("terminal: %s", out.String())
				}
			}
		})
	}
}

func TestCreativeSendWaitDoesNotPollRejectedSend(t *testing.T) {
	c := newUnixSocketTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/creative/runs/4/messages" {
			t.Errorf("polled rejected message: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"code":"self_send","message":"use --to worker"}}`))
	}))
	cmd := creativeAgentCommands(func() (*client.Client, error) { return c, nil })
	cmd.SetArgs([]string{"send", "4", "--body", "hello", "--wait"})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestCreativeSendWithoutWaitRetainsContextResponse(t *testing.T) {
	c := newUnixSocketTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Error("send without --wait polled")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":42,"to":"orchestrator","status":"queued","creative_context":{"run_id":4,"stage_id":5,"item_id":6},"artifact_version_ids":[7]}`))
	}))
	cmd := creativeAgentCommands(func() (*client.Client, error) { return c, nil })
	cmd.SetArgs([]string{"send", "4", "--body", "report"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		ID      int64  `json:"id"`
		To      string `json:"to"`
		Status  string `json:"status"`
		Context struct {
			Run   int64 `json:"run_id"`
			Stage int64 `json:"stage_id"`
			Item  int64 `json:"item_id"`
		} `json:"creative_context"`
		Versions []int64 `json:"artifact_version_ids"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != 42 || result.To != "orchestrator" || result.Status != "queued" || result.Context.Run != 4 || result.Context.Stage != 5 || result.Context.Item != 6 || len(result.Versions) != 1 || result.Versions[0] != 7 {
		t.Fatalf("lost response: %+v", result)
	}
}
