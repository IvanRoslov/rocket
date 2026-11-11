package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

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
	if err := m.st.CheckCreativeRunMutable(req.creative.RunID); err != nil {
		return err
	}
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
	spec.Creative = true
	spec.FirstMessage = fmt.Sprintf("Creative run #%d. Refresh the run, artifacts and all run/stage feeds, worker messages and legacy questions before continuing. Preserve existing results and agree rework.\n\n%s\n\nRun snapshot:\n%s", run.ID, brief, snapshot)
	memo := fmt.Sprintf("Conversation feed memo: the feed is the only channel to the human. Read rocket creative agent feed %d and each stage with --stage STAGE (optional --item ITEM filter). Reply: rocket creative agent feed post %d --stage STAGE --item ITEM --version VERSION --body TEXT. Paid card: rocket creative agent feed propose %d --stage STAGE --item ITEM --body PROPOSAL [--replaces CARD]. Decision: rocket creative agent feed card %d CARD. Omit optional scope/version flags outside that scope. Report results with every discussed published version attached to the same feed message (repeat --version). Identify each attachment and its item. Attach intermediate/service files only when the human should look at them. Do not open new questions. A typed Go is discussion: only the card button records consent. Never execute a replaced or undecided card; require status=decided and decision=go for that exact call set. Stage approval uses submit + review card only, never feed text. Read all feeds after restore to recover offline messages and decisions.", run.ID, run.ID, run.ID, run.ID)
	if sess.Kind != "orchestrator" {
		memo = fmt.Sprintf("Conversation feed memo for workers: never write to the human or publish cards; message the current run orchestrator with rocket creative agent send %d --stage %d --body TEXT (add --item ITEM and --version VERSION as needed). Do not open new questions. Human feed text never grants paid consent or stage approval.", run.ID, b.StageID)
	}
	spec.FirstMessage = memo + "\n\n" + spec.FirstMessage

	return nil
}

func (m *Manager) validateCreativeRestore(sess store.Session, b store.CreativeBinding) error {
	if err := m.st.CheckCreativeRunMutable(b.RunID); err != nil {
		return err
	}
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

// Called after runtime launch/restore, so offline human events follow the
// currently bound orchestrator and never depend on a terminal recipient.
func (m *Manager) deliverCreativeFeed(runID int64) {
	ids, err := m.st.QueueCreativeFeedDeliveries(runID)
	if err != nil {
		slog.Error("creative: drain feed deliveries", "run", runID, "error", err)
		return
	}
	for _, id := range ids {
		msg, err := m.st.GetMessage(id)
		if err != nil {
			continue
		}
		m.bus.Publish("message.queued", msg.ToSession, map[string]any{"id": id, "to": msg.ToSession})
		if m.wake != nil {
			m.wake(msg.ToSession)
		}
	}
}
