package session

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// AgentSessionKind is the session kind under which an agent's tmux session is
// registered. Unlike orchestrators and workers, rocket does not own the
// process inside it: the session is either started by `rocket agent start` or
// created by hand, and the row merely records that it exists (docs/10-agents.md).
const AgentSessionKind = "agent"

// StartAgent launches an agent's tmux session: a session named after the agent
// running its own command in its own directory, with just enough environment
// (ROCKET_SESSION_ID, ROCKET_SOCKET) for `rocket inbox|send|task` to work from
// inside it. Rocket sets no system prompt and manages no lifecycle — the
// agent's context is its own CLAUDE.md.
//
// An empty Command means an interactive login shell. An empty Dir is an error:
// there is nowhere to run, and guessing a directory would be worse than saying
// so.
func (m *Manager) StartAgent(ctx context.Context, a store.Agent) (store.Session, error) {
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
	m.injectToken(env)

	if _, err := m.rt.Create(ctx, runtime.CreateSpec{
		Name:    a.ID,
		Dir:     a.Dir,
		Command: a.Command,
		Env:     env,
	}); err != nil {
		return store.Session{}, err
	}

	sess, err := m.upsertAgentSession(a)
	if err != nil {
		return store.Session{}, err
	}
	m.bus.Publish("agent.session_started", a.ID, map[string]any{"dir": a.Dir})
	return sess, nil
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
		return existing, false, nil
	}
	adopted, err := m.upsertAgentSession(a)
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

// upsertAgentSession writes the session row describing an agent's live tmux
// session, replacing any dead row left over from a previous run (the id is
// fixed, so rows cannot accumulate per run the way worker sessions do).
//
// WorktreePath is the agent's own directory: it is not a rocket worktree, but
// it is where the queue writes large-message files the agent then reads.
func (m *Manager) upsertAgentSession(a store.Agent) (store.Session, error) {
	sess := store.Session{
		ID:           a.ID,
		Kind:         AgentSessionKind,
		ProjectID:    a.ProjectID,
		FeatureSlug:  a.ID,
		Agent:        m.cfg.DefaultAgent,
		WorktreePath: a.Dir,
		TmuxName:     a.ID,
		State:        "running",
	}

	err := m.st.AddSession(sess)
	if errors.Is(err, store.ErrExists) {
		if err := m.st.UpdateSession(sess); err != nil {
			return store.Session{}, err
		}
		m.bus.Publish("session.state_changed", a.ID, map[string]any{"to": "running"})
		return sess, nil
	}
	if err != nil {
		return store.Session{}, err
	}
	return sess, nil
}
