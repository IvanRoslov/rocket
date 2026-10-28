package kimicode

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/prompts"
)

func TestResolveBinaryUsesHomeWhenPathEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KIMI_CODE_HOME", "")
	t.Setenv("PATH", "")
	bin := filepath.Join(home, ".kimi-code", "bin", "kimi")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBinary()
	if err != nil || got != bin {
		t.Fatalf("resolveBinary = %q, %v; want %q", got, err, bin)
	}
	if err := New().Available(); err != nil {
		t.Fatalf("Available: %v", err)
	}
	if cmd := New().LaunchCommand(agent.LaunchSpec{FirstMessage: "do work"}); !reflect.DeepEqual(cmd, []string{bin, "--auto"}) {
		t.Fatalf("LaunchCommand = %v", cmd)
	}
}

func TestLaunchCommandUsesModelAndNeverPassesFirstMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	t.Setenv("PATH", "")
	bin := filepath.Join(home, "bin", "kimi")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := New().LaunchCommand(agent.LaunchSpec{Model: "kimi-code/k3.1-pro", FirstMessage: "secret brief", Effort: "max"})
	want := []string{bin, "--auto", "-m", "kimi-code/k3.1-pro"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LaunchCommand = %v, want %v", got, want)
	}
}

func TestResolveBinaryRejectsMissingOrNonExecutable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", home)
	t.Setenv("PATH", "")
	if _, err := resolveBinary(); err == nil {
		t.Fatal("missing binary resolved")
	}
	bin := filepath.Join(home, "bin", "kimi")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := New().Available(); err == nil {
		t.Fatal("non executable binary is available")
	}
}

func TestResolveBinaryFallsBackFromOverrideToDefaultHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	t.Setenv("PATH", "")
	bin := filepath.Join(home, ".kimi-code", "bin", "kimi")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBinary()
	if err != nil || got != bin {
		t.Fatalf("resolveBinary = %q, %v; want %q", got, err, bin)
	}
}

func TestEnvAndCapabilities(t *testing.T) {
	a := New()
	if a.Name() != "kimi-code" || !agent.ShipsBrainstormSkill(a) || !agent.TakesFirstMessageByInput(a) || !agent.WantsBracketedPaste(a) {
		t.Fatalf("adapter capabilities missing")
	}
	if got := a.Efforts(); !reflect.DeepEqual(got, []string{"low", "high", "max"}) {
		t.Fatalf("Efforts = %v", got)
	}
	env := a.Env(agent.LaunchSpec{SessionID: "s", Kind: "worker", SocketPath: "/tmp/rocket.sock"})
	for key, want := range map[string]string{
		"KIMI_CODE_TRUST_WORKSPACE": "1", "KIMI_CODE_NO_AUTO_UPDATE": "1", "KIMI_DISABLE_TELEMETRY": "1",
		"ROCKET_SESSION_ID": "s", "ROCKET_KIND": "worker", "ROCKET_SOCKET": "/tmp/rocket.sock",
	} {
		if env[key] != want {
			t.Errorf("Env[%s] = %q, want %q", key, env[key], want)
		}
	}
	if _, ok := env["KIMI_MODEL_THINKING_EFFORT"]; ok {
		t.Fatal("effort present for default")
	}
	if got := a.Env(agent.LaunchSpec{Effort: "high"})["KIMI_MODEL_THINKING_EFFORT"]; got != "high" {
		t.Fatalf("effort = %q", got)
	}
}

func TestSetupWorkspacePreservesSkillsAndForeignContent(t *testing.T) {
	wt := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	path := filepath.Join(wt, "AGENTS.md")
	if err := os.WriteFile(path, []byte("Foreign instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt := "First\n{{#if skills}}\ninvoke superpowers:writing-plans\n{{/if}}"
	spec := agent.LaunchSpec{WorktreePath: wt, SystemPrompt: prompt}
	for i := 0; i < 2; i++ {
		if err := New().SetupWorkspace(spec); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Count(s, "<!-- rocket:start -->") != 1 || !strings.Contains(s, prompt) || !strings.HasSuffix(s, "Foreign instructions\n") {
		t.Fatalf("AGENTS.md = %q", s)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err = %v", info.Mode(), err)
	}
}

func TestSetupWorkspaceExcludesUntrackedAgentsAndCustomSkill(t *testing.T) {
	wt := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	if out, err := exec.Command("git", "-C", wt, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	spec := agent.LaunchSpec{WorktreePath: wt, Kind: "orchestrator", SystemPrompt: "Rocket", BrainstormSkill: prompts.CustomBrainstormSkill}
	if err := New().SetupWorkspace(spec); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(wt, ".kimi-code", "skills", prompts.CustomBrainstormSkill)
	info, err := os.Stat(filepath.Join(skill, "scripts", "start-server.sh"))
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("skill script not executable: %v %v", info, err)
	}
	out, err := exec.Command("git", "-C", wt, "status", "--short").CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatalf("git status = %q, err %v", out, err)
	}
	spec.BrainstormSkill = prompts.StockBrainstormSkill
	if err := New().SetupWorkspace(spec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(skill); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stock-skill setup left custom skill: %v", err)
	}
}
