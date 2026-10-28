package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/IvanRoslov/rocket/internal/prompts"
)

var rocketBlockRe = regexp.MustCompile(`(?s)<!-- rocket:start -->.*?<!-- rocket:end -->\n?`)

// UpsertAgentsMD writes the Rocket prompt block and preserves any other
// instructions. Codex strips skill sections; Kimi keeps them for its plugin.
func UpsertAgentsMD(worktreePath, systemPrompt string, stripSkills bool) error {
	if systemPrompt == "" {
		return nil
	}
	if stripSkills {
		systemPrompt = prompts.StripSkills(systemPrompt)
	}
	block := "<!-- rocket:start -->\n" + systemPrompt + "\n<!-- rocket:end -->\n"
	path := filepath.Join(worktreePath, "AGENTS.md")
	perm := os.FileMode(0o644)
	var existing []byte
	if data, err := os.ReadFile(path); err == nil {
		existing = data
		if info, statErr := os.Stat(path); statErr == nil {
			perm = info.Mode().Perm()
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	var out []byte
	switch {
	case len(existing) == 0:
		out = []byte(block)
	case rocketBlockRe.Match(existing):
		out = rocketBlockRe.ReplaceAll(existing, []byte(block))
	default:
		out = append([]byte(block+"\n"), existing...)
	}
	if err := atomicWriteAgentsMD(worktreePath, path, out, perm); err != nil {
		return err
	}
	ExcludeFromGit(worktreePath, []string{"AGENTS.md"})
	return nil
}

func atomicWriteAgentsMD(dir, path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(dir, ".rocket-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tempPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tempPath, perm); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
