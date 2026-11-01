package api

import (
	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/agent/claudecode"
	"github.com/IvanRoslov/rocket/internal/monitor"
	"net/http"
	"strings"
	"testing"
)

func TestActivityMetadataBodyIsBounded(t *testing.T) {
	d := activityTestDeps(t)
	seedActivitySession(t, d.Store, "s")
	srv := newTestServer(t, d)
	resp := postJSON(t, srv.URL+"/v1/internal/activity", map[string]any{"session": "s", "state": "active", "ignored_prompt": strings.Repeat("x", 17<<10)})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversize accepted: %d", resp.StatusCode)
	}
}
func TestClaudeLegacyExitCannotEstablishRootAuthority(t *testing.T) {
	d := activityTestDeps(t)
	seedActivitySession(t, d.Store, "s")
	sess, _ := d.Store.GetSession("s")
	sess.Agent = "claude-code"
	sess.Activity = "active"
	d.Store.UpdateSession(sess)
	d.Monitor = monitor.New(d.Store, d.Bus, nil, d.Cfg, func(string) (agent.Agent, error) { return claudecode.New(), nil })
	srv := newTestServer(t, d)
	resp := postJSON(t, srv.URL+"/v1/internal/activity", map[string]any{"session": "s", "state": "exited"})
	resp.Body.Close()
	after, _ := d.Store.GetSession("s")
	if after.Activity != "active" {
		t.Fatal("legacy hook killed Claude root")
	}
}
