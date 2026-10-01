package prompts

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CustomBrainstormSkill is rocket's own orchestrator brainstorm skill, shipped
// in the binary and laid into an orchestrator's worktree; StockBrainstormSkill
// is the Superpowers original it was copied from. The setting
// orchestrator_brainstorm_custom picks between them for a task's
// orchestrator; workers always use the stock skill.
const (
	CustomBrainstormSkill = "orchestrator-brainstorming"
	StockBrainstormSkill  = "superpowers:brainstorming"
)

// BrainstormSkill returns the skill name the orchestrator prompt names for
// the given value of the orchestrator_brainstorm_custom setting.
func BrainstormSkill(custom bool) string {
	if custom {
		return CustomBrainstormSkill
	}
	return StockBrainstormSkill
}

const orchestratorSkillRoot = "skills/" + CustomBrainstormSkill

//go:embed all:skills/orchestrator-brainstorming
var orchestratorSkillFS embed.FS

// WriteOrchestratorSkill lays the embedded orchestrator-brainstorming skill
// into skillsDir/orchestrator-brainstorming, replacing whatever was there so a
// skill from an older rocket never lingers. embed drops file modes, so shell
// scripts are written executable and everything else 0644.
func WriteOrchestratorSkill(skillsDir string) error {
	dest := filepath.Join(skillsDir, CustomBrainstormSkill)
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return fs.WalkDir(orchestratorSkillFS, orchestratorSkillRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(strings.TrimPrefix(p, orchestratorSkillRoot)))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := orchestratorSkillFS.ReadFile(p)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(p, ".sh") {
			mode = 0o755
		}
		return os.WriteFile(target, data, mode)
	})
}
