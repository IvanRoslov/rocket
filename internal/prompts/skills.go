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

// CustomBrainstormSkillVersion is the version of the embedded
// orchestrator-brainstorming skill (see its README). A task stores the skill
// it started with as "<name>@<version>" so the brainstorm metric can tell
// versions apart; bump it with every change to the skill's meaning.
const CustomBrainstormSkillVersion = "1.2"

// BrainstormSkillRecord is the value a task stores in brainstorm_skill when
// it starts: the custom skill with its version, the stock skill as is.
func BrainstormSkillRecord(custom bool) string {
	if custom {
		return CustomBrainstormSkill + "@" + CustomBrainstormSkillVersion
	}
	return StockBrainstormSkill
}

// SkillName strips the "@version" suffix of a stored brainstorm_skill,
// leaving the skill name a prompt names and an agent lays out.
func SkillName(record string) string {
	name, _, _ := strings.Cut(record, "@")
	return name
}

// OrchestratorPlanningSkill is rocket's own decomposition skill for
// orchestrators (the orchestrator-level counterpart of
// superpowers:writing-plans, which stays the workers' skill). It is laid into
// every orchestrator worktree whose agent supports project skills, whatever
// brainstorm skill the task uses.
const OrchestratorPlanningSkill = "orchestrator-planning"

const orchestratorSkillRoot = "skills/" + CustomBrainstormSkill

//go:embed all:skills/orchestrator-brainstorming
var orchestratorSkillFS embed.FS

//go:embed all:skills/orchestrator-planning
var orchestratorPlanningSkillFS embed.FS

// WriteOrchestratorSkill lays the embedded orchestrator-brainstorming skill
// into skillsDir/orchestrator-brainstorming, replacing whatever was there so a
// skill from an older rocket never lingers.
func WriteOrchestratorSkill(skillsDir string) error {
	return writeEmbeddedSkill(orchestratorSkillFS, CustomBrainstormSkill, skillsDir)
}

// WriteOrchestratorPlanningSkill lays the embedded orchestrator-planning
// skill into skillsDir/orchestrator-planning, replacing whatever was there.
func WriteOrchestratorPlanningSkill(skillsDir string) error {
	return writeEmbeddedSkill(orchestratorPlanningSkillFS, OrchestratorPlanningSkill, skillsDir)
}

// writeEmbeddedSkill copies skills/<name> from fsys into skillsDir/<name>,
// removing the destination first. embed drops file modes, so shell scripts
// are written executable and everything else 0644.
func writeEmbeddedSkill(fsys embed.FS, name, skillsDir string) error {
	root := "skills/" + name
	dest := filepath.Join(skillsDir, name)
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(strings.TrimPrefix(p, root)))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fsys.ReadFile(p)
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

// OrchestratorSkillNames lists every skill rocket may lay into an
// orchestrator's skills directory, for agents to keep out of git.
func OrchestratorSkillNames() []string {
	return []string{CustomBrainstormSkill, OrchestratorPlanningSkill}
}

// SyncOrchestratorSkills makes skillsDir hold exactly the rocket skills a
// session should see. An orchestrator always gets orchestrator-planning, and
// orchestrator-brainstorming only when brainstormRecord names it: it carries
// the stock skill's description, so a stray copy could be picked instead of
// superpowers:brainstorming and blur the per-skill brainstorm metric. Any
// other session gets neither.
func SyncOrchestratorSkills(skillsDir, brainstormRecord string, orchestrator bool) error {
	if !orchestrator {
		for _, name := range OrchestratorSkillNames() {
			if err := os.RemoveAll(filepath.Join(skillsDir, name)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := WriteOrchestratorPlanningSkill(skillsDir); err != nil {
		return err
	}
	if SkillName(brainstormRecord) == CustomBrainstormSkill {
		return WriteOrchestratorSkill(skillsDir)
	}
	return os.RemoveAll(filepath.Join(skillsDir, CustomBrainstormSkill))
}

// SkillExcludes returns the git-exclude patterns for rocket's orchestrator
// skills laid under relDir (a worktree-relative skills directory).
func SkillExcludes(relDir string) []string {
	var out []string
	for _, n := range OrchestratorSkillNames() {
		out = append(out, relDir+"/"+n+"/")
	}
	return out
}
