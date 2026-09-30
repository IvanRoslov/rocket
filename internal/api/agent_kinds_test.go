package api

import (
	"encoding/json"
	"net/http"
	"testing"

	_ "github.com/IvanRoslov/rocket/internal/agent/claudecode" // registers "claude-code"
	_ "github.com/IvanRoslov/rocket/internal/agent/codex"      // registers "codex"
)

// The agent-kinds endpoint feeds the "which agent runs this orchestrator"
// picker in the UI: every registered agent, with whether its executable is
// actually available on this machine.
func TestGetAgentKinds(t *testing.T) {
	d, _ := systemTestDeps(t, &systemFakeRuntime{})
	d.Cfg.DefaultAgent = "claude-code"
	srv := newTestServer(t, d)

	resp, err := http.Get(srv.URL + "/v1/agent-kinds")
	if err != nil {
		t.Fatalf("GET /v1/agent-kinds: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body agentKindsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body.Default != "claude-code" {
		t.Errorf("default = %q, want %q", body.Default, "claude-code")
	}

	byName := map[string]agentKindResponse{}
	for _, k := range body.Kinds {
		byName[k.Name] = k
	}
	for _, want := range []string{"claude-code", "codex"} {
		k, ok := byName[want]
		if !ok {
			t.Fatalf("kinds missing %q, got %+v", want, body.Kinds)
		}
		if !k.Available && k.Error == "" {
			t.Errorf("kind %q is unavailable but carries no error", want)
		}
		if k.Available && k.Error != "" {
			t.Errorf("kind %q is available but carries error %q", want, k.Error)
		}
	}
}
