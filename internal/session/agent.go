package session

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// AgentSessionKind is the session kind under which an agent's tmux session is
// registered. Unlike orchestrators and workers, rocket does not own the
// process inside it: the session is either started by `rocket agent start` or
// created by hand, and the row merely records that it exists (docs/10-agents.md).
const AgentSessionKind = "agent"

// StartAgent launches an agent's tmux session in its own directory. A selected
// or previously saved model profile uses its agent adapter; without one, the
// legacy Command runs as before. Rocket sets no system prompt and manages no
// lifecycle — the agent's context is its own CLAUDE.md.
//
// An empty Command means an interactive login shell. An empty Dir is an error:
// there is nowhere to run, and guessing a directory would be worse than saying
// so.
func (m *Manager) StartAgent(ctx context.Context, a store.Agent, requestedProfile string) (store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !a.Enabled {
		return store.Session{}, validationErr("agent_disabled", "agent is disabled: "+a.ID)
	}
	if a.Dir == "" {
		return store.Session{}, validationErr("agent_no_dir",
			"agent "+a.ID+" has no dir: set one (rocket agent add/edit --dir <path>) "+
				"or create the tmux session yourself: tmux new -s "+a.ID)
	}
	if _, err := os.Stat(a.Dir); err != nil {
		return store.Session{}, validationErr("agent_dir_missing",
			"agent dir is not usable: "+err.Error())
	}
	if m.rt.Alive(ctx, runtime.Handle{Name: a.ID}) {
		return store.Session{}, validationErr("agent_live",
			"tmux session "+a.ID+" is already running (rocket agent attach "+a.ID+")")
	}

	env := map[string]string{
		"ROCKET_SESSION_ID": a.ID,
		"ROCKET_SOCKET":     m.cfg.SocketPath(),
		"ROCKET_HOME":       m.cfg.Home,
	}
	command := a.Command
	agentName := m.agentAdapter(a)
	var prof LaunchProfile
	spec := agent.LaunchSpec{
		SessionID: a.ID, Kind: AgentSessionKind, ProjectID: a.ProjectID,
		WorktreePath: a.Dir, SocketPath: m.cfg.SocketPath(),
	}
	// ag is the adapter rocket launches through a profile; nil for a
	// registered command, which runs verbatim.
	var ag agent.Agent

	profileName := requestedProfile
	if profileName == "" {
		profileName = a.LaunchProfile
	}
	if profileName != "" {
		p, err := m.st.GetModelProfile(profileName)
		if errors.Is(err, store.ErrNotFound) {
			return store.Session{}, validationErr("profile_not_found", "no profile "+profileName)
		}
		if err != nil {
			return store.Session{}, err
		}
		if !p.Enabled {
			return store.Session{}, validationErr("profile_disabled",
				"profile is disabled: "+profileName+"; choose another with --profile")
		}
		ag, err = agent.Get(p.Agent)
		if err != nil {
			return store.Session{}, validationErr("agent_unknown", "unknown agent: "+p.Agent)
		}
		if err := ag.Available(); err != nil {
			return store.Session{}, validationErr("agent_unavailable", "agent unavailable: "+err.Error())
		}
		spec.Model, spec.Effort = p.Model, p.Effort
		agentName = p.Agent
		prof = LaunchProfile{Name: p.Name, Model: p.Model, Effort: p.Effort}
		if requestedProfile != "" {
			a.LaunchProfile = requestedProfile
			if err := m.st.UpdateAgent(a); err != nil {
				return store.Session{}, err
			}
		}
	}

	// The row exists (spawning) before any launch preparation: the delivery
	// binding below references it, and a fresh row per start drops whatever
	// channel a previous launch — possibly on another harness — left behind.
	sess, err := m.upsertAgentSession(a, agentName, prof, "spawning")
	if err != nil {
		return store.Session{}, err
	}
	if err := m.prepareAgentDelivery(ag, agentName, &spec, &sess); err != nil {
		m.markErrored(a.ID, err)
		return store.Session{}, err
	}
	if ag != nil {
		argv, err := launchArgs(ag, spec)
		if err != nil {
			m.markErrored(a.ID, err)
			return store.Session{}, err
		}
		command = shellJoin(argv)
		env = mergeEnv(env, ag.Env(spec))
	} else if spec.ClaudeLaunchGeneration != "" {
		env["ROCKET_CLAUDE_LAUNCH"] = spec.ClaudeLaunchGeneration
	}
	m.injectToken(env)

	if _, err := m.rt.Create(ctx, runtime.CreateSpec{
		Name:    a.ID,
		Dir:     a.Dir,
		Command: command,
		Env:     env,
	}); err != nil {
		m.markErrored(a.ID, err)
		return store.Session{}, err
	}

	m.captureClaudeLaunch(ctx, spec)
	sess.State = "running"
	if err := m.st.UpdateSession(sess); err != nil {
		return store.Session{}, err
	}
	m.bus.Publish("session.state_changed", a.ID, map[string]any{"from": "spawning", "to": "running"})
	m.bus.Publish("agent.session_started", a.ID, map[string]any{"dir": a.Dir})
	return sess, nil
}

// prepareAgentDelivery gives an agent launch the same delivery preparation a
// spawned session gets, whichever harness runs it: a native channel (Pi) for
// a profile launch, and the exact Claude launch binding for any Claude Code
// launch — profile or registered command. Without the binding the monitor
// cannot attribute the pane's Claude process, its activity stays unknown and
// every queued message waits out queue_timeout.
//
// A registered command runs verbatim, so only the env-carried Claude binding
// applies to it; a native channel needs launch arguments rocket does not own.
func (m *Manager) prepareAgentDelivery(ag agent.Agent, agentName string, spec *agent.LaunchSpec, sess *store.Session) error {
	if ag != nil {
		if err := m.prepareNativeLaunch(ag, spec, sess); err != nil {
			return err
		}
		return m.prepareClaudeLaunch(ag, spec, sess)
	}
	detected, err := agent.Get(agentName)
	if err != nil {
		return nil // unknown CLI: nothing rocket can bind
	}
	return m.prepareClaudeLaunch(detected, spec, sess)
}

// StopAgent kills an agent's tmux session and marks its session row done. The
// registration itself survives: stopping an agent is not deleting it.
func (m *Manager) StopAgent(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.rt.Destroy(ctx, runtime.Handle{Name: id}); err != nil {
		return err
	}
	if err := m.RetireAgentSession(id); err != nil {
		return err
	}
	m.bus.Publish("agent.session_stopped", id, map[string]any{})
	return nil
}

// AdoptAgentSession records that an agent's tmux session is alive, whether
// rocket started it or a human created it by hand. It is idempotent: an
// already-live row is left alone.
//
// fresh reports that this call brought the session up (the row was created or
// revived from a dead state) rather than finding it already live. Callers use
// it to tell one continuous session from a new one — the unread notifier owes
// a fresh session its notification even when no new mail has arrived.
//
// It refuses to touch a session row of any other kind: an orchestrator or
// worker whose id happens to equal an agent id is not this agent's session,
// and overwriting it would corrupt a real session's bookkeeping.
func (m *Manager) AdoptAgentSession(a store.Agent) (sess store.Session, fresh bool, err error) {
	existing, err := m.st.GetSession(a.ID)
	if err == nil && existing.Kind != AgentSessionKind {
		return store.Session{}, false, fmt.Errorf(
			"session %s is a %s session, refusing to adopt it as an agent session",
			a.ID, existing.Kind)
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Session{}, false, err
	}
	if err == nil && (existing.State == "spawning" || existing.State == "running") {
		// The row is live and its lifecycle fields stay untouched, but the
		// adapter may have drifted: a session restarted on a different CLI
		// keeps the row while the pane runs something else. Delivery, the
		// monitor and the dashboard all key off sessions.agent — a Kimi pane
		// tracked as claude-code gets the wrong paste dialect, no journal
		// confirmer, and redelivery loops (incident: cto on Kimi). Reconcile
		// the column from the registered launcher command. A non-empty profile
		// snapshot belongs to a rocket launch: its adapter is already exact.
		if existing.Profile == "" {
			if want := m.agentAdapter(a); existing.Agent != want {
				if err := m.st.UpdateSessionAgent(a.ID, want); err != nil {
					return existing, false, fmt.Errorf("reconcile agent session adapter: %w", err)
				}
				existing.Agent = want
			}
		}
		return existing, false, nil
	}
	adopted, err := m.upsertAgentSession(a, m.agentAdapter(a), LaunchProfile{}, "running")
	return adopted, err == nil, err
}

// RetireAgentSession marks an agent's session row dead once its tmux session
// is gone, so liveness and the unread notifier stay truthful. A missing row,
// an already-dead row, or a row of another kind is left alone.
func (m *Manager) RetireAgentSession(id string) error {
	sess, err := m.st.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if sess.Kind != AgentSessionKind {
		return nil
	}
	if sess.State != "spawning" && sess.State != "running" {
		return nil
	}

	sess.State = "done"
	if err := m.st.UpdateSession(sess); err != nil {
		return err
	}
	m.bus.Publish("session.state_changed", id, map[string]any{"from": "running", "to": "done"})
	return nil
}

// agentAdapter resolves the adapter an agent's session row is tracked under:
// detected from the registered launcher command when the leading binary is a
// known CLI, else the configured default (see AdoptAgentSession for why the
// row must name the adapter the pane actually runs).
func (m *Manager) agentAdapter(a store.Agent) string {
	if name := agent.DetectFromCommand(a.Command); name != "" {
		return name
	}
	return m.cfg.DefaultAgent
}

// upsertAgentSession writes the session row describing an agent's live tmux
// session, replacing any dead row left over from a previous run (the id is
// fixed, so rows cannot accumulate per run the way worker sessions do).
//
// WorktreePath is the agent's own directory: it is not a rocket worktree, but
// it is where the queue writes large-message files the agent then reads.
func (m *Manager) upsertAgentSession(a store.Agent, agentName string, prof LaunchProfile, state string) (store.Session, error) {
	sess := store.Session{
		ID:           a.ID,
		Kind:         AgentSessionKind,
		ProjectID:    a.ProjectID,
		FeatureSlug:  a.ID,
		Agent:        agentName,
		WorktreePath: a.Dir,
		TmuxName:     a.ID,
		State:        state,
		Profile:      prof.Name,
		Model:        prof.Model,
		Effort:       prof.Effort,
	}

	err := m.st.AddSession(sess)
	if errors.Is(err, store.ErrExists) {
		if err := m.st.UpdateSession(sess); err != nil {
			return store.Session{}, err
		}
		m.bus.Publish("session.state_changed", a.ID, map[string]any{"to": state})
		return sess, nil
	}
	if err != nil {
		return store.Session{}, err
	}
	return sess, nil
}
