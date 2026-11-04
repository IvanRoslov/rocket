package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/prompts"
	"github.com/IvanRoslov/rocket/internal/store"
)

type CreativeSpawnReq struct {
	Context   store.CreativeBinding
	ParentID  string
	Repo      string
	Name      string
	Prompt    string
	AgentName string
	Profile   LaunchProfile
}

func (m *Manager) SpawnCreative(ctx context.Context, in CreativeSpawnReq) (store.Session, error) {
	c, err := m.st.ResolveCreativeContext(in.Context.RunID, in.Context.StageID, in.Context.ItemID)
	if err != nil {
		return store.Session{}, err
	}
	p, err := m.st.GetProject(c.ProjectID)
	if err != nil {
		return store.Session{}, err
	}
	repo := in.Repo
	if repo == "" {
		repo = p.MainRepo
	}
	kind, name := "worker", in.Name
	if in.ParentID == "" {
		kind, name = "orchestrator", "orch"
	}
	return m.Spawn(ctx, SpawnReq{creative: &in.Context, Project: c.ProjectID, Repo: repo, Task: name, Feature: fmt.Sprintf("creative-%d", c.RunID), Kind: kind, ParentID: in.ParentID, Prompt: in.Prompt, AgentName: in.AgentName, Profile: in.Profile})
}

// Called under the normal manager launch lock, preventing duplicate run launches.
func (m *Manager) validateCreativeSpawn(req SpawnReq) error {
	c, err := m.st.ResolveCreativeContext(req.creative.RunID, req.creative.StageID, req.creative.ItemID)
	if err != nil {
		return err
	}
	if req.TaskID != 0 || req.SubtaskID != 0 || c.ProjectID != req.Project {
		return validationErr("creative_context", "invalid creative launch context")
	}
	if req.Kind == "orchestrator" {
		if c.StageID != 0 || c.ItemID != 0 || req.ParentID != "" {
			return validationErr("creative_context", "orchestrator belongs to run scope")
		}
		if c.OrchestratorSessionID != "" {
			old, err := m.st.GetSession(c.OrchestratorSessionID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if err == nil && !isTerminal(old.State) {
				return validationErr("creative_already_running", "run already has an active orchestrator")
			}
		}
	} else {
		if c.StageID == 0 || req.ParentID == "" || req.ParentID != c.OrchestratorSessionID {
			return validationErr("creative_context", "worker requires stage and current run orchestrator")
		}
		parent, err := m.st.GetSession(req.ParentID)
		if err != nil {
			return err
		}
		if parent.Kind != "orchestrator" || isTerminal(parent.State) {
			return validationErr("creative_context", "parent must be a live orchestrator")
		}
	}
	return nil
}
func (m *Manager) fillCreativePrompt(spec *agent.LaunchSpec, sess store.Session, b store.CreativeBinding) error {
	run, err := m.st.GetCreativeRun(b.RunID)
	if err != nil {
		return err
	}
	if _, err := m.st.ResolveCreativeContext(b.RunID, b.StageID, b.ItemID); err != nil {
		return err
	}
	proj, err := m.st.GetProject(run.ProjectID)
	if err != nil {
		return err
	}
	repos, err := allowedReposDesc(m.st, proj)
	if err != nil {
		return err
	}
	snapshot, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	scope, _ := json.Marshal(b)
	role := "creative-worker"
	if sess.Kind == "orchestrator" {
		role = "creative-orchestrator"
	}
	brief := run.Brief
	if sess.Prompt != "" {
		brief += "\n\n" + sess.Prompt
	}
	spec.SystemPrompt, err = prompts.Render(m.cfg.Home, role, prompts.Vars{"session_id": sess.ID, "parent_id": sess.ParentID, "project_name": proj.Name, "worktree_path": spec.WorktreePath, "allowed_repos": repos, "creative_context": string(scope), "run_snapshot": string(snapshot), "tools_home": m.cfg.Home, "brief": brief})
	if err != nil {
		return err
	}
	spec.BrainstormSkill = ""
	spec.FirstMessage = fmt.Sprintf("Creative run #%d. Refresh the run, artifacts and contextual messages/questions before continuing. Preserve existing results and agree rework.\n\n%s\n\nRun snapshot:\n%s", run.ID, brief, snapshot)
	return nil
}

func (m *Manager) validateCreativeRestore(sess store.Session, b store.CreativeBinding) error {
	c, err := m.st.ResolveCreativeContext(b.RunID, b.StageID, b.ItemID)
	if err != nil {
		return err
	}
	if sess.Kind != "orchestrator" || c.OrchestratorSessionID == sess.ID {
		return nil
	}
	if c.OrchestratorSessionID != "" {
		current, err := m.st.GetSession(c.OrchestratorSessionID)
		if err != nil {
			return err
		}
		if !isTerminal(current.State) {
			return validationErr("creative_already_running", "another orchestrator is active for this run")
		}
	}
	return m.st.SetCreativeRunOrchestrator(b.RunID, sess.ID)
}
