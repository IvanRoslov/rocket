package pi

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/prompts"
)

func TestLaunchCommand(t *testing.T) {
	a := New()
	full := agent.LaunchSpec{
		SessionID:    "s1",
		Model:        "deepseek/deepseek-flash",
		Effort:       "high",
		FirstMessage: "go",
	}
	want := []string{"pi", "--tui-mode", "regular", "--approve", "--session-id", "s1", "--model", "deepseek/deepseek-flash", "--thinking", "high", "--", "go"}
	if got := a.LaunchCommand(full); !reflect.DeepEqual(got, want) {
		t.Fatalf("LaunchCommand = %v, want %v", got, want)
	}

	minimal := a.LaunchCommand(agent.LaunchSpec{SessionID: "s1"})
	want = []string{"pi", "--tui-mode", "regular", "--approve", "--session-id", "s1"}
	if !reflect.DeepEqual(minimal, want) {
		t.Fatalf("minimal LaunchCommand = %v, want %v", minimal, want)
	}
	for _, flag := range []string{"--model", "--thinking", "--"} {
		for _, arg := range minimal {
			if arg == flag {
				t.Errorf("minimal LaunchCommand contains %q: %v", flag, minimal)
			}
		}
	}

	dash := a.LaunchCommand(agent.LaunchSpec{SessionID: "s1", FirstMessage: "-fix the bug"})
	if got := dash[len(dash)-2:]; !reflect.DeepEqual(got, []string{"--", "-fix the bug"}) {
		t.Fatalf("dash-prefixed message suffix = %v, want [-- -fix the bug]", got)
	}
}

var testProviderKeys = map[string]string{
	"anthropic":                  "ANTHROPIC_API_KEY",
	"ant-ling":                   "ANT_LING_API_KEY",
	"openai":                     "OPENAI_API_KEY",
	"deepseek":                   "DEEPSEEK_API_KEY",
	"nvidia":                     "NVIDIA_API_KEY",
	"google":                     "GEMINI_API_KEY",
	"github-copilot":             "COPILOT_GITHUB_TOKEN",
	"mistral":                    "MISTRAL_API_KEY",
	"groq":                       "GROQ_API_KEY",
	"cerebras":                   "CEREBRAS_API_KEY",
	"xai":                        "XAI_API_KEY",
	"openrouter":                 "OPENROUTER_API_KEY",
	"vercel-ai-gateway":          "AI_GATEWAY_API_KEY",
	"zai":                        "ZAI_API_KEY",
	"zai-coding-cn":              "ZAI_CODING_CN_API_KEY",
	"opencode":                   "OPENCODE_API_KEY",
	"opencode-go":                "OPENCODE_API_KEY",
	"radius":                     "RADIUS_API_KEY",
	"typesafe":                   "TYPESAFE_API_KEY",
	"huggingface":                "HF_TOKEN",
	"fireworks":                  "FIREWORKS_API_KEY",
	"together":                   "TOGETHER_API_KEY",
	"baseten":                    "BASETEN_API_KEY",
	"kimi-coding":                "KIMI_API_KEY",
	"meta":                       "META_API_KEY",
	"minimax":                    "MINIMAX_API_KEY",
	"minimax-cn":                 "MINIMAX_CN_API_KEY",
	"moonshotai":                 "MOONSHOT_API_KEY",
	"moonshotai-cn":              "MOONSHOT_API_KEY",
	"qwen-token-plan":            "QWEN_TOKEN_PLAN_API_KEY",
	"qwen-token-plan-individual": "QWEN_TOKEN_PLAN_API_KEY",
	"qwen-token-plan-cn":         "QWEN_TOKEN_PLAN_CN_API_KEY",
	"xiaomi":                     "XIAOMI_API_KEY",
	"xiaomi-token-plan-cn":       "XIAOMI_TOKEN_PLAN_CN_API_KEY",
	"xiaomi-token-plan-ams":      "XIAOMI_TOKEN_PLAN_AMS_API_KEY",
	"xiaomi-token-plan-sgp":      "XIAOMI_TOKEN_PLAN_SGP_API_KEY",
}

func TestEnv(t *testing.T) {
	spec := agent.LaunchSpec{
		SessionID: "s1", Kind: "worker", ParentID: "p1", ProjectID: "project",
		RepoID: "repo", Feature: "feature", SocketPath: "/tmp/rocket.sock",
	}
	for provider, selectedKey := range testProviderKeys {
		t.Run(provider, func(t *testing.T) {
			for _, key := range testProviderKeys {
				t.Setenv(key, "ambient-"+key)
			}
			t.Setenv(selectedKey, "selected-"+selectedKey)
			spec.Model = provider + "/model"
			env := New().Env(spec)

			wantRocket := map[string]string{
				"ROCKET_SESSION_ID": "s1", "ROCKET_KIND": "worker", "ROCKET_PARENT_ID": "p1",
				"ROCKET_PROJECT_ID": "project", "ROCKET_REPO_ID": "repo", "ROCKET_FEATURE": "feature",
				"ROCKET_SOCKET": "/tmp/rocket.sock", "PI_TELEMETRY": "0",
			}
			for key, want := range wantRocket {
				if env[key] != want {
					t.Errorf("Env[%s] = %q, want %q", key, env[key], want)
				}
			}
			if env[selectedKey] != "selected-"+selectedKey {
				t.Errorf("selected credential Env[%s] = %q", selectedKey, env[selectedKey])
			}
			for _, key := range testProviderKeys {
				if key != selectedKey {
					if _, ok := env[key]; ok {
						t.Errorf("unrelated credential %s was forwarded", key)
					}
				}
			}
			if _, ok := env["PI_CACHE_RETENTION"]; ok {
				t.Error("PI_CACHE_RETENTION must not be set")
			}
		})
	}

	t.Run("empty and unknown credentials", func(t *testing.T) {
		for _, key := range testProviderKeys {
			t.Setenv(key, "")
		}
		for _, model := range []string{"deepseek/model", "unknown/model", "model-without-provider", ""} {
			env := New().Env(agent.LaunchSpec{Model: model})
			for _, key := range testProviderKeys {
				if _, ok := env[key]; ok {
					t.Errorf("model %q forwarded empty/unrelated %s", model, key)
				}
			}
		}
	})

	t.Run("provider without model", func(t *testing.T) {
		for _, key := range testProviderKeys {
			t.Setenv(key, "ambient-"+key)
		}
		for _, model := range []string{"deepseek", "openai", "anthropic"} {
			env := New().Env(agent.LaunchSpec{Model: model})
			for _, key := range testProviderKeys {
				if _, ok := env[key]; ok {
					t.Errorf("provider-less model %q forwarded %s", model, key)
				}
			}
		}
	})
}

func TestAvailable(t *testing.T) {
	t.Run("missing binary", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if err := New().Available(); err == nil {
			t.Fatal("Available accepted a missing binary")
		}
		if _, err := resolveBinary(); err == nil {
			t.Fatal("resolveBinary accepted a missing binary")
		}
	})

	for _, tc := range []struct {
		version string
		wantErr bool
	}{
		{version: "not-a-version", wantErr: true},
		{version: "0.9.9", wantErr: true},
		{version: "0.74.0", wantErr: true},
		{version: "1.-1.0", wantErr: true},
		{version: "1.0.-1", wantErr: true},
		{version: "1.+0.0", wantErr: true},
		{version: "1.-0.0", wantErr: true},
		{version: "1.0.0"},
		{version: "1.4.0"},
		{version: "v2.0.0"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			binDir := t.TempDir()
			t.Setenv("PATH", binDir)
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s'\n", tc.version)
			if err := os.WriteFile(filepath.Join(binDir, "pi"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			err := New().Available()
			if tc.wantErr && err == nil {
				t.Fatalf("Available accepted version %q", tc.version)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Available rejected version %q: %v", tc.version, err)
			}
		})
	}
}

func TestSetupWorkspace(t *testing.T) {
	wt := t.TempDir()
	if out, err := exec.Command("git", "-C", wt, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	foreignAgents := "# Foreign instructions\nKeep this section.\n"
	if err := os.WriteFile(filepath.Join(wt, "AGENTS.md"), []byte(foreignAgents), 0o600); err != nil {
		t.Fatal(err)
	}
	foreignSkill := filepath.Join(wt, ".pi", "skills", "foreign", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(foreignSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreignSkill, []byte("foreign skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt := "Rocket prompt\n\n<!-- skills:start -->\nsuperpowers:writing-plans stays here\n<!-- skills:end -->"
	spec := agent.LaunchSpec{
		WorktreePath: wt, Kind: "orchestrator", SystemPrompt: prompt,
		BrainstormSkill: prompts.CustomBrainstormSkill,
	}
	if err := New().SetupWorkspace(spec); err != nil {
		t.Fatalf("SetupWorkspace: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(wt, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, prompt) || !strings.Contains(text, foreignAgents) {
		t.Fatalf("AGENTS.md did not preserve prompt skills and foreign content: %q", text)
	}
	if strings.Count(text, "<!-- rocket:start -->") != 1 {
		t.Fatalf("AGENTS.md has duplicate Rocket blocks: %q", text)
	}
	info, err := os.Stat(filepath.Join(wt, "AGENTS.md"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("AGENTS.md mode = %v, err = %v", info.Mode(), err)
	}

	skillDir := filepath.Join(wt, ".pi", "skills", prompts.CustomBrainstormSkill)
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatalf("custom orchestrator skill missing: %v", err)
	}
	if _, err := os.Stat(foreignSkill); err != nil {
		t.Fatalf("foreign skill was changed: %v", err)
	}
	exclude, err := os.ReadFile(filepath.Join(wt, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	var excludeLines []string
	for _, line := range strings.Split(string(exclude), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			excludeLines = append(excludeLines, line)
		}
	}
	wantExclude := []string{"AGENTS.md", ".pi/skills/orchestrator-brainstorming/"}
	if !reflect.DeepEqual(excludeLines, wantExclude) {
		t.Fatalf("git exclude = %v, want exactly %v", excludeLines, wantExclude)
	}

	for _, tc := range []struct {
		name  string
		kind  string
		skill string
	}{
		{name: "stock orchestrator", kind: "orchestrator", skill: prompts.StockBrainstormSkill},
		{name: "worker", kind: "worker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec.Kind = tc.kind
			spec.BrainstormSkill = tc.skill
			if err := New().SetupWorkspace(spec); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(skillDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Rocket skill was not removed: %v", err)
			}
			if _, err := os.Stat(foreignSkill); err != nil {
				t.Fatalf("foreign skill was removed: %v", err)
			}
		})
	}
}

func TestRegistrationAndCapabilities(t *testing.T) {
	a := New()
	registered, err := agent.Get("pi")
	if err != nil || registered.Name() != "pi" {
		t.Fatalf("agent.Get(pi) = %v, %v", registered, err)
	}
	if !agent.ShipsBrainstormSkill(a) {
		t.Fatal("Pi must ship the orchestrator brainstorm skill")
	}
	want := []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}
	if got := a.Efforts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Efforts = %v, want %v", got, want)
	}
}

func TestPiHome(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "/tmp/pi-override")
	if got := piHome(); got != "/tmp/pi-override" {
		t.Fatalf("piHome override = %q", got)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	if got := piHome(); got != filepath.Join(home, ".pi", "agent") {
		t.Fatalf("default piHome = %q", got)
	}
}
