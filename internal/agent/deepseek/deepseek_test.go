package deepseek

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/agent/claudecode"
)

var _ agent.Agent = (*DeepSeek)(nil)
var _ agent.UsageReader = (*DeepSeek)(nil)
var _ agent.BrainstormSkillShipper = (*DeepSeek)(nil)

func TestName(t *testing.T) {
	if got := New().Name(); got != "deepseek" {
		t.Fatalf("Name() = %q, want deepseek", got)
	}
	registered, err := agent.Get("deepseek")
	if err != nil {
		t.Fatalf("agent.Get(deepseek): %v", err)
	}
	if _, ok := registered.(*DeepSeek); !ok {
		t.Fatalf("registered agent has type %T, want *DeepSeek", registered)
	}
}

func TestAvailable(t *testing.T) {
	binDir := t.TempDir()
	claude := filepath.Join(binDir, "claude")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		withBinary bool
		key        string
		anthropic  string
		wantErr    bool
	}{
		{name: "missing binary", key: "test-deepseek-key", wantErr: true},
		{name: "missing key", withBinary: true, wantErr: true},
		{name: "anthropic token is not a fallback", withBinary: true, anthropic: "test-anthropic-token", wantErr: true},
		{name: "binary and key", withBinary: true, key: "test-deepseek-key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir()
			if tc.withBinary {
				path = binDir
			}
			t.Setenv("PATH", path)
			t.Setenv("DEEPSEEK_API_KEY", tc.key)
			t.Setenv("ANTHROPIC_AUTH_TOKEN", tc.anthropic)

			err := New().Available()
			if tc.wantErr && err == nil {
				t.Fatal("Available() error = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Available() error = %v, want nil", err)
			}
			if tc.withBinary && tc.key == "" && !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
				t.Fatalf("Available() error = %q, want DEEPSEEK_API_KEY requirement", err)
			}
		})
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "test-deepseek-key")
	spec := agent.LaunchSpec{
		SessionID:  "session-1",
		Kind:       "worker",
		ParentID:   "parent-1",
		ProjectID:  "project-1",
		RepoID:     "repo-1",
		Feature:    "feature-1",
		SocketPath: "/tmp/rocket.sock",
	}
	want := map[string]string{
		"CLAUDECODE":                      "",
		"ROCKET_SESSION_ID":               "session-1",
		"ROCKET_CLAUDE_LAUNCH":            "",
		"ROCKET_KIND":                     "worker",
		"ROCKET_PARENT_ID":                "parent-1",
		"ROCKET_PROJECT_ID":               "project-1",
		"ROCKET_REPO_ID":                  "repo-1",
		"ROCKET_FEATURE":                  "feature-1",
		"ROCKET_SOCKET":                   "/tmp/rocket.sock",
		"ANTHROPIC_BASE_URL":              "https://api.deepseek.com/anthropic",
		"ANTHROPIC_AUTH_TOKEN":            "test-deepseek-key",
		"ANTHROPIC_MODEL":                 "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":    "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_SONNET_MODEL":  "deepseek-flash[1m]",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":   "deepseek-flash",
		"CLAUDE_CODE_SUBAGENT_MODEL":      "deepseek-flash",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW": "786432",
		"CLAUDE_CODE_EFFORT_LEVEL":        "max",
	}

	if got := New().Env(spec); !reflect.DeepEqual(got, want) {
		t.Errorf("Env(defaults) = %#v\nwant %#v", got, want)
	}

	spec.Model = "deepseek-v4-pro"
	spec.Effort = "high"
	want["ANTHROPIC_MODEL"] = "deepseek-v4-pro"
	want["CLAUDE_CODE_EFFORT_LEVEL"] = "high"
	if got := New().Env(spec); !reflect.DeepEqual(got, want) {
		t.Errorf("Env(explicit model/effort) = %#v\nwant %#v", got, want)
	}
}

func TestEnvDoesNotUseAnthropicToken(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-anthropic-token")
	env := New().Env(agent.LaunchSpec{})
	if got := env["ANTHROPIC_AUTH_TOKEN"]; got != "" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %q, want empty without DEEPSEEK_API_KEY", got)
	}
}

func TestEnvClaudeCodeHasNoDeepSeekOverrides(t *testing.T) {
	env := claudecode.New().Env(agent.LaunchSpec{})
	for _, key := range []string{
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW",
		"CLAUDE_CODE_EFFORT_LEVEL",
	} {
		if value, ok := env[key]; ok {
			t.Errorf("ordinary Claude Code env contains %s=%q", key, value)
		}
	}
}

func TestInheritedClaudeLaunchAndEfforts(t *testing.T) {
	spec := agent.LaunchSpec{Model: "deepseek-v4-pro", Effort: "high", FirstMessage: "go"}
	if got, want := New().LaunchCommand(spec), (&claudecode.ClaudeCode{}).LaunchCommand(spec); !reflect.DeepEqual(got, want) {
		t.Errorf("LaunchCommand = %q, want inherited %q", got, want)
	}
	wantEfforts := []string{"low", "medium", "high", "xhigh", "max"}
	if got := New().Efforts(); !reflect.DeepEqual(got, wantEfforts) {
		t.Errorf("Efforts = %v, want %v", got, wantEfforts)
	}
	if !agent.ShipsBrainstormSkill(New()) {
		t.Error("DeepSeek must preserve Claude Code's brainstorm skill shipping")
	}
}

// Optional compatibility must be inherited from ClaudeCode, not inferred from
// adapter names, model profiles or generic wrapper behavior.
func TestDeepSeekClaudeSocketCapability(t *testing.T) {
	a := New()
	capability, ok := any(a).(interface{ ClaudeSocketCompatible() bool })
	if !ok || !capability.ClaudeSocketCompatible() {
		t.Fatal("DeepSeek does not inherit Claude socket compatibility")
	}
}

func TestClaudeSocketCapabilityIsExplicit(t *testing.T) {
	if !agent.SupportsClaudeSocket(New()) || !agent.SupportsClaudeSocket(claudecode.New()) {
		t.Fatal("Claude and DeepSeek must support socket")
	}
	wrapper := struct{ agent.Agent }{New()}
	if agent.SupportsClaudeSocket(wrapper) {
		t.Fatal("generic wrapper inherited an unexposed capability")
	}
}
