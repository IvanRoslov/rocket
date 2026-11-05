package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/client"
)

func TestCreativeCloseCLI(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "orch")
	c := newUnixSocketTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/creative/runs/4/workers/scene-worker/close" || r.URL.RawQuery != "" || r.Header.Get("X-Rocket-Session") != "orch" {
			t.Errorf("request %s %s caller=%s", r.Method, r.URL, r.Header.Get("X-Rocket-Session"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"killed"}`))
	}))
	cmd := creativeAgentCommands(func() (*client.Client, error) { return c, nil })
	cmd.SetArgs([]string{"close", "4", "scene-worker"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got["status"] != "killed" {
		t.Fatalf("output %s: %v", out.String(), err)
	}
}

func TestCreativeCloseCLIRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{"close"}, {"close", "4"}, {"close", "0", "worker"}, {"close", "-1", "worker"}, {"close", "abc", "worker"}, {"close", "4", ""}, {"close", "4", "worker", "extra"}, {"close", "4", "worker", "--cleanup"}, {"close", "4", "worker", "--cascade"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			opened := false
			cmd := creativeAgentCommands(func() (*client.Client, error) { opened = true; return nil, nil })
			cmd.SetArgs(args)
			cmd.SetOut(new(bytes.Buffer))
			cmd.SetErr(new(bytes.Buffer))
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected usage error")
			}
			if opened {
				t.Fatal("invalid command connected to daemon")
			}
		})
	}
}

func TestCreativeCloseCLIPropagatesForbidden(t *testing.T) {
	c := newUnixSocketTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		w.Write([]byte(`{"error":{"code":"creative_forbidden","message":"only the live run orchestrator may close workers"}}`))
	}))
	cmd := creativeAgentCommands(func() (*client.Client, error) { return c, nil })
	cmd.SetArgs([]string{"close", "4", "worker"})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "creative_forbidden") {
		t.Fatalf("error %v", err)
	}
}
