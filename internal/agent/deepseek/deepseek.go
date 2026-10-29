// Package deepseek adapts Claude Code to DeepSeek's Anthropic-compatible API.
package deepseek

import (
	"errors"
	"os"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/agent/claudecode"
)

type DeepSeek struct {
	*claudecode.ClaudeCode
}

func New() agent.Agent {
	return &DeepSeek{ClaudeCode: &claudecode.ClaudeCode{}}
}

func (d *DeepSeek) Name() string {
	return "deepseek"
}

func (d *DeepSeek) Available() error {
	if err := d.ClaudeCode.Available(); err != nil {
		return err
	}
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		return errors.New("DEEPSEEK_API_KEY is required")
	}
	return nil
}

func (d *DeepSeek) Env(spec agent.LaunchSpec) map[string]string {
	env := d.ClaudeCode.Env(spec)
	model := spec.Model
	if model == "" {
		model = "deepseek-flash[1m]"
	}
	effort := spec.Effort
	if effort == "" {
		effort = "max"
	}
	env["ANTHROPIC_BASE_URL"] = "https://api.deepseek.com/anthropic"
	env["ANTHROPIC_AUTH_TOKEN"] = os.Getenv("DEEPSEEK_API_KEY")
	env["ANTHROPIC_MODEL"] = model
	env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = "deepseek-flash[1m]"
	env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = "deepseek-flash[1m]"
	env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = "deepseek-flash"
	env["CLAUDE_CODE_SUBAGENT_MODEL"] = "deepseek-flash"
	env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = "786432"
	env["CLAUDE_CODE_EFFORT_LEVEL"] = effort
	return env
}

func init() {
	agent.Register("deepseek", New)
}
