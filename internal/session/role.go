package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/prompts"
	"github.com/IvanRoslov/rocket/internal/roles"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
)

// runInfix separates a role id from its run number in an instance session id
// ("sre-run-3"). The id is the only link between a run and its role — there is
// deliberately no column duplicating it (see docs/10-agents.md).
const runInfix = "-run-"

// RoleWorktreeName returns the name of a role's persistent worktree
// directory. Unlike ordinary sessions, every run of a role shares one
// worktree, so it is named after the role rather than after the session.
func RoleWorktreeName(roleID string) string { return roleID + "-agent" }

// RoleBranch returns the branch a role's persistent worktree is checked out
// on. Nothing is ever merged from it: it is a working directory for rocket
// and gh, not a source of PRs.
func RoleBranch(roleID string) string { return "agent/" + roleID }

// RoleInstanceID returns the session id of run n of a role.
func RoleInstanceID(roleID string, n int) string {
	return roleID + runInfix + strconv.Itoa(n)
}

// RoleFromInstanceID returns the role a run belongs to, and the run number.
// ok is false for any session id that is not a role instance.
func RoleFromInstanceID(sessionID string) (roleID string, run int, ok bool) {
	i := strings.LastIndex(sessionID, runInfix)
	if i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(sessionID[i+len(runInfix):])
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return sessionID[:i], n, true
}

// LiveRoleInstance returns the role's live instance (state spawning or
// running), if it has one. At most one instance of a role is alive at a time;
// SpawnRole enforces that.
func (m *Manager) LiveRoleInstance(roleID string) (store.Session, bool, error) {
	sessions, err := m.st.ListSessions(store.SessionFilter{Kind: "agent"})
	if err != nil {
		return store.Session{}, false, err
	}
	for _, s := range sessions {
		if id, _, ok := RoleFromInstanceID(s.ID); ok && id == roleID {
			return s, true, nil
		}
	}
	return store.Session{}, false, nil
}

// SpawnRole launches one ephemeral instance of role: a session kind=agent
// named "<role>-run-<n>" in the role's persistent worktree, with the rendered
// role system prompt and briefing as its first message.
//
// The worktree (branch "agent/<role>" in the project's main repo) is created
// on the first run and reused afterwards — Kill never destroys it, so the
// role keeps its scratch space, gh auth state and untracked notes across
// runs.
func (m *Manager) SpawnRole(ctx context.Context, role store.Agent, briefing string) (store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !role.Enabled {
		return store.Session{}, validationErr("agent_disabled", "role is disabled: "+role.ID)
	}

	project, err := m.st.GetProject(role.ProjectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Session{}, validationErr("project_not_found", "project not found: "+role.ProjectID)
		}
		return store.Session{}, err
	}

	repo, err := m.st.GetRepo(project.MainRepo)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Session{}, validationErr("repo_not_found", "main repo not found: "+project.MainRepo)
		}
		return store.Session{}, err
	}

	if live, ok, err := m.LiveRoleInstance(role.ID); err != nil {
		return store.Session{}, err
	} else if ok {
		return store.Session{}, validationErr("instance_live",
			"role "+role.ID+" already has a live instance: "+live.ID)
	}

	agentName := role.Agent
	if agentName == "" {
		agentName = m.cfg.DefaultAgent
	}
	ag, err := agent.Get(agentName)
	if err != nil {
		return store.Session{}, validationErr("agent_unknown", "unknown agent: "+agentName)
	}
	if err := ag.Available(); err != nil {
		return store.Session{}, validationErr("agent_unavailable", "agent unavailable: "+err.Error())
	}

	branch := RoleBranch(role.ID)

	// Reserve a run number. Mirrors Spawn's retry loop: the number is derived
	// from what is already stored, so a concurrent spawn can win the race
	// between deciding and inserting.
	var sess store.Session
	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		id, err := m.nextRoleInstanceID(role.ID)
		if err != nil {
			return store.Session{}, err
		}

		sess = store.Session{
			ID:          id,
			Kind:        "agent",
			ProjectID:   project.ID,
			RepoID:      project.MainRepo,
			FeatureSlug: role.ID,
			Agent:       agentName,
			Branch:      branch,
			TmuxName:    id,
			State:       "spawning",
			Prompt:      briefing,
		}

		if err := m.st.AddSession(sess); err != nil {
			if errors.Is(err, store.ErrExists) && attempt < maxRetries {
				continue
			}
			if errors.Is(err, store.ErrExists) {
				return store.Session{}, fmt.Errorf("role instance name reservation raced 3 times for %q", role.ID)
			}
			return store.Session{}, err
		}

		m.bus.Publish("agent.instance_spawned", sess.ID, map[string]any{
			"role": role.ID, "project": project.ID,
		})
		break
	}

	wtPath, err := m.ensureRoleWorktree(ctx, repo, role.ID, branch)
	if err != nil {
		m.markErrored(sess.ID, err)
		return store.Session{}, err
	}
	sess.WorktreePath = wtPath

	rolePrompt, err := roles.ReadPrompt(role.PromptPath)
	if err != nil {
		m.markErrored(sess.ID, err)
		return store.Session{}, err
	}

	sysPrompt, err := prompts.Render(m.cfg.Home, "agent", prompts.Vars{
		"role_id":        role.ID,
		"project_name":   project.Name,
		"main_repo":      project.MainRepo,
		"main_repo_path": repo.Path,
		"session_id":     sess.ID,
		"worktree_path":  wtPath,
		"memory_dir":     roles.MemoryDir(m.cfg.Home, role.ID),
		"role_prompt":    rolePrompt,
	})
	if err != nil {
		m.markErrored(sess.ID, err)
		return store.Session{}, err
	}

	spec := agent.LaunchSpec{
		SessionID:    sess.ID,
		Kind:         "agent",
		ProjectID:    project.ID,
		RepoID:       project.MainRepo,
		Feature:      role.ID,
		WorktreePath: wtPath,
		SystemPrompt: sysPrompt,
		FirstMessage: briefing,
		SocketPath:   m.cfg.SocketPath(),
	}

	if err := ag.SetupWorkspace(spec); err != nil {
		m.markErrored(sess.ID, err)
		return store.Session{}, err
	}

	env := mergeEnv(repo.Env, ag.Env(spec))
	m.injectToken(env)

	if _, err := m.rt.Create(ctx, runtime.CreateSpec{
		Name:    sess.ID,
		Dir:     wtPath,
		Command: shellJoin(ag.LaunchCommand(spec)),
		Env:     env,
	}); err != nil {
		m.markErrored(sess.ID, err)
		return store.Session{}, err
	}

	sess.State = "running"
	if err := m.st.UpdateSession(sess); err != nil {
		return store.Session{}, err
	}
	m.bus.Publish("session.state_changed", sess.ID, map[string]any{"from": "spawning", "to": "running"})

	return sess, nil
}

// ensureRoleWorktree returns the path of the role's persistent worktree,
// creating it on the first run and reusing it afterwards. The decision is
// made on the directory's existence rather than on a Restore error, so a
// genuine git failure is reported instead of being retried as a fresh
// create.
func (m *Manager) ensureRoleWorktree(ctx context.Context, repo store.Repo, roleID, branch string) (string, error) {
	name := RoleWorktreeName(roleID)
	path := filepath.Join(m.cfg.WorktreesDir, repo.ID, name)

	if _, err := os.Stat(path); err == nil {
		return m.ws.Restore(ctx, repo, name, branch)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat role worktree: %w", err)
	}

	res, err := m.ws.Create(ctx, repo, name, branch)
	if err != nil {
		return "", err
	}
	return res.Path, nil
}

// nextRoleInstanceID returns the id of the role's next run: one past the
// highest run number the store has ever seen for it (run numbers are never
// reused, so a role's history reads chronologically).
func (m *Manager) nextRoleInstanceID(roleID string) (string, error) {
	sessions, err := m.st.ListSessions(store.SessionFilter{Kind: "agent", All: true})
	if err != nil {
		return "", err
	}

	highest := 0
	for _, s := range sessions {
		id, n, ok := RoleFromInstanceID(s.ID)
		if ok && id == roleID && n > highest {
			highest = n
		}
	}
	return RoleInstanceID(roleID, highest+1), nil
}
