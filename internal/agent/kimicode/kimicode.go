// Package kimicode implements Rocket's Kimi Code interactive agent adapter.
package kimicode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/prompts"
)

type KimiCode struct{}

func New() agent.Agent                         { return &KimiCode{} }
func (k *KimiCode) Name() string               { return "kimi-code" }
func (k *KimiCode) FirstMessageByInput() bool  { return true }
func (k *KimiCode) BracketedPaste() bool       { return true }
func (k *KimiCode) ShipsBrainstormSkill() bool { return true }
func (k *KimiCode) Efforts() []string          { return []string{"low", "high", "max"} }

func kimiHome() string {
	if home := os.Getenv("KIMI_CODE_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kimi-code")
}

func resolveBinary() (string, error) {
	if bin, err := exec.LookPath("kimi"); err == nil {
		return filepath.Abs(bin)
	}
	var candidates []string
	if override := os.Getenv("KIMI_CODE_HOME"); override != "" {
		candidates = append(candidates, filepath.Join(override, "bin", "kimi"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".kimi-code", "bin", "kimi"))
	}
	for _, bin := range candidates {
		info, err := os.Stat(bin)
		if err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return filepath.Abs(bin)
		}
	}
	return "", fmt.Errorf("kimi binary not found or not executable in PATH or %v", candidates)
}

func (k *KimiCode) Available() error { _, err := resolveBinary(); return err }

func (k *KimiCode) LaunchCommand(spec agent.LaunchSpec) []string {
	bin, err := resolveBinary()
	if err != nil {
		return nil
	} // Available is checked before launch.
	cmd := []string{bin, "--auto"}
	if spec.Model != "" {
		cmd = append(cmd, "-m", spec.Model)
	}
	return cmd
}

func (k *KimiCode) Env(spec agent.LaunchSpec) map[string]string {
	env := map[string]string{
		"KIMI_CODE_TRUST_WORKSPACE": "1", "KIMI_CODE_NO_AUTO_UPDATE": "1", "KIMI_DISABLE_TELEMETRY": "1",
		"ROCKET_SESSION_ID": spec.SessionID, "ROCKET_KIND": spec.Kind, "ROCKET_PARENT_ID": spec.ParentID,
		"ROCKET_PROJECT_ID": spec.ProjectID, "ROCKET_REPO_ID": spec.RepoID, "ROCKET_FEATURE": spec.Feature,
		"ROCKET_SOCKET": spec.SocketPath,
	}
	if spec.Effort != "" {
		env["KIMI_MODEL_THINKING_EFFORT"] = spec.Effort
	}
	return env
}

func (k *KimiCode) SetupWorkspace(spec agent.LaunchSpec) error {
	if err := agent.UpsertAgentsMD(spec.WorktreePath, spec.SystemPrompt, false); err != nil {
		return fmt.Errorf("upsert AGENTS.md: %w", err)
	}
	if spec.Kind == "orchestrator" {
		skill := filepath.Join(spec.WorktreePath, ".kimi-code", "skills", prompts.CustomBrainstormSkill)
		if prompts.SkillName(spec.BrainstormSkill) == prompts.CustomBrainstormSkill {
			if err := prompts.WriteOrchestratorSkill(filepath.Dir(skill)); err != nil {
				return err
			}
		} else if err := os.RemoveAll(skill); err != nil {
			return err
		}
	}
	agent.ExcludeFromGit(spec.WorktreePath, []string{".kimi-code/skills/" + prompts.CustomBrainstormSkill + "/"})
	return nil
}

func init() { agent.Register("kimi-code", New) }
