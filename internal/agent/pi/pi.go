package pi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/prompts"
)

type Pi struct{}

func New() agent.Agent { return &Pi{} }

func (p *Pi) Name() string { return "pi" }

func (p *Pi) ShipsBrainstormSkill() bool { return true }

func (p *Pi) Efforts() []string {
	return []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}
}

func piHome() string {
	if home := os.Getenv("PI_CODING_AGENT_DIR"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent")
}

func resolveBinary() (string, error) {
	return exec.LookPath("pi")
}

func (p *Pi) Available() error {
	bin, err := resolveBinary()
	if err != nil {
		return err
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return fmt.Errorf("check pi version: %w", err)
	}
	numbers, err := parsePiVersion(string(out))
	if err != nil {
		return err
	}
	if numbers[0] < 1 {
		return fmt.Errorf("pi 1.0.0 or newer is required (got %s)", strings.TrimSpace(string(out)))
	}
	return nil
}

func (p *Pi) LaunchCommand(spec agent.LaunchSpec) []string {
	cmd := []string{"pi", "--tui-mode", "regular", "--approve", "--session-id", spec.SessionID}
	if spec.Model != "" {
		cmd = append(cmd, "--model", spec.Model)
	}
	if spec.Effort != "" {
		cmd = append(cmd, "--thinking", spec.Effort)
	}
	cmd = withNativeArgs(cmd, spec)
	if spec.FirstMessage != "" {
		cmd = append(cmd, "--", spec.FirstMessage)
	}
	return cmd
}

var providerAPIKeys = map[string]string{
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

func selectedProviderAPIKey(model string) string {
	provider, _, ok := strings.Cut(model, "/")
	if !ok {
		return ""
	}
	return providerAPIKeys[provider]
}

func (p *Pi) Env(spec agent.LaunchSpec) map[string]string {
	env := map[string]string{
		"PI_TELEMETRY":      "0",
		"ROCKET_SESSION_ID": spec.SessionID,
		"ROCKET_KIND":       spec.Kind,
		"ROCKET_PARENT_ID":  spec.ParentID,
		"ROCKET_PROJECT_ID": spec.ProjectID,
		"ROCKET_REPO_ID":    spec.RepoID,
		"ROCKET_FEATURE":    spec.Feature,
		"ROCKET_SOCKET":     spec.SocketPath,
	}
	if key := selectedProviderAPIKey(spec.Model); key != "" {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
	for key, value := range nativeEnv(spec) {
		env[key] = value
	}
	return env
}

func (p *Pi) ScrubLaunchEnv(spec agent.LaunchSpec) []string {
	selected := selectedProviderAPIKey(spec.Model)
	seen := map[string]bool{}
	var keys []string
	for _, key := range providerAPIKeys {
		if key == "" || key == selected || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

func (p *Pi) SetupWorkspace(spec agent.LaunchSpec) error {
	if err := agent.UpsertAgentsMD(spec.WorktreePath, spec.SystemPrompt, false); err != nil {
		return fmt.Errorf("upsert AGENTS.md: %w", err)
	}
	skillDir := filepath.Join(spec.WorktreePath, ".pi", "skills", prompts.CustomBrainstormSkill)
	if spec.Kind == "orchestrator" && prompts.SkillName(spec.BrainstormSkill) == prompts.CustomBrainstormSkill {
		if err := prompts.WriteOrchestratorSkill(filepath.Dir(skillDir)); err != nil {
			return fmt.Errorf("lay orchestrator skill: %w", err)
		}
	} else if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("remove orchestrator skill: %w", err)
	}
	agent.ExcludeFromGit(spec.WorktreePath, []string{".pi/skills/" + prompts.CustomBrainstormSkill + "/"})
	return nil
}

func init() { agent.Register("pi", New) }
