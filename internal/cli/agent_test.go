package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ctor keeps the table below readable: every command constructor has the
// same shape.
func ctor(f func() *cobra.Command) func() *cobra.Command { return f }

func TestAgentCommandUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{"add without args", ctor(newAgentAddCmd), []string{"--dir", "/tmp"}},
		{"add with two ids", ctor(newAgentAddCmd), []string{"sre", "triage"}},
		{"edit without flags", ctor(newAgentEditCmd), []string{"sre"}},
		{"edit without id", ctor(newAgentEditCmd), []string{"--dir", "/tmp"}},
		{"ls with args", ctor(newAgentLsCmd), []string{"extra"}},
		{"show without id", ctor(newAgentShowCmd), []string{}},
		{"rm with two ids", ctor(newAgentRmCmd), []string{"a", "b"}},
		{"enable without id", ctor(newAgentEnableCmd), []string{}},
		{"disable without id", ctor(newAgentDisableCmd), []string{}},
		{"start without id", ctor(newAgentStartCmd), []string{}},
		{"stop with two ids", ctor(newAgentStopCmd), []string{"a", "b"}},
		{"attach without id", ctor(newAgentAttachCmd), []string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.cmd()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected an error for args %v", tc.args)
			}
			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("expected usageError, got %T: %v", err, err)
			}
		})
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := dashIfEmpty(""); got != "-" {
		t.Errorf("dashIfEmpty(\"\") = %q", got)
	}
	if got := dashIfEmpty("sre"); got != "sre" {
		t.Errorf("dashIfEmpty(sre) = %q", got)
	}
	if got := dashIfZero(float64(0)); got != "-" {
		t.Errorf("dashIfZero(0) = %q", got)
	}
	if got := dashIfZero(float64(45)); got != "45" {
		t.Errorf("dashIfZero(45) = %q", got)
	}
	if got := sessionCell(true); got != "live" {
		t.Errorf("sessionCell(true) = %q", got)
	}
	if got := sessionCell(false); got != "-" {
		t.Errorf("sessionCell(false) = %q", got)
	}
	if got := questionsCell(float64(2), float64(1)); got != "2 (1 ждут)" {
		t.Errorf("questionsCell(2, 1) = %q", got)
	}
	if got := questionsCell(float64(0), float64(0)); got != "-" {
		t.Errorf("questionsCell(0, 0) = %q", got)
	}
}

func TestAgentStartSendsProfile(t *testing.T) {
	for _, tc := range []struct {
		name        string
		args        []string
		wantProfile string
	}{
		{name: "explicit profile", args: []string{"cto", "--profile", "claude-opus"}, wantProfile: "claude-opus"},
		{name: "saved profile", args: []string{"cto"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen, h := fakeDaemon(t, map[string]any{
				"POST /v1/agents/cto/start": map[string]any{"id": "cto", "status": "running", "dir": "/tmp/cto"},
			})
			c := newUnixSocketTestServer(t, h)
			cmd := newAgentStartCmdWith(func() (agentClient, error) { return c, nil })
			cmd.SetArgs(tc.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(*seen) != 1 {
				t.Fatalf("requests = %+v, want 1", *seen)
			}
			req := (*seen)[0]
			if req.Method != "POST" || req.Path != "/v1/agents/cto/start" {
				t.Errorf("request = %+v", req)
			}
			profile, ok := req.Body["profile"]
			if tc.wantProfile == "" {
				if ok {
					t.Errorf("body = %+v, want no profile", req.Body)
				}
			} else if !ok || profile != tc.wantProfile {
				t.Errorf("body = %+v, want profile %q", req.Body, tc.wantProfile)
			}
		})
	}
}

func TestAgentShowPrintsLaunchProfile(t *testing.T) {
	_, h := fakeDaemon(t, map[string]any{
		"GET /v1/agents/cto": map[string]any{
			"id": "cto", "description": "chief", "enabled": true,
			"launch_profile": "claude-opus", "milestones": []any{},
		},
		"GET /v1/agents/cto/inbox": []any{},
	})
	c := newUnixSocketTestServer(t, h)
	cmd := newAgentShowCmdWith(func() (agentClient, error) { return c, nil })
	cmd.SetArgs([]string{"cto"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "profile:     claude-opus") {
		t.Errorf("output missing launch profile:\n%s", out.String())
	}
}
