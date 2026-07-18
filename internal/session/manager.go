// Package session implements rocket's session lifecycle: validating and
// executing a spawn request, killing a session, and restoring one that has
// died or errored out.
package session

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/prompts"
	"github.com/IvanRoslov/rocket/internal/runtime"
	"github.com/IvanRoslov/rocket/internal/store"
	"github.com/IvanRoslov/rocket/internal/workspace"
)

// idPattern validates task/feature slugs: lowercase alphanumerics and
// hyphens only.
var idPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// ValidationError is returned by Manager methods for request-shaped
// failures (bad input, unknown references, disallowed state transitions).
// internal/api maps Code to the appropriate HTTP status.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func validationErr(code, msg string) *ValidationError {
	return &ValidationError{Code: code, Message: msg}
}

// SpawnReq describes a request to spawn a new session. Feature defaults to
// Task when empty; Kind defaults to "worker" (the only kind spawn accepts
// in phase 1).
type SpawnReq struct {
	Project   string
	Repo      string
	Task      string
	Feature   string
	Prompt    string
	AgentName string
	Kind      string
}

// Manager owns session lifecycle: spawn, kill, restore.
type Manager struct {
	st  *store.Store
	bus *bus.Bus
	rt  runtime.Runtime
	ws  workspace.Workspace
	cfg *config.Config

	// mu serializes Spawn, Kill, Restore, and Reconcile end-to-end. Without
	// it, a Kill racing a concurrent Spawn between the session's
	// AddSession (state "spawning") and its final UpdateSession (state
	// "running") can lose: Kill marks the session "killed" first, then
	// Spawn's final write clobbers it back to "running" — a zombie session
	// the store believes is running but nothing is actually managing.
	//
	// A single manager-wide mutex (rather than per-session locking) is
	// intentionally coarse: at phase-1 scale (one operator, a handful of
	// concurrent sessions) simplicity and an easy-to-reason-about
	// invariant ("only one lifecycle operation touches store+runtime+
	// workspace state at a time") beat the complexity of per-id locks.
	// Revisit if contention becomes a real bottleneck.
	mu sync.Mutex
}

// NewManager builds a Manager wired to the given dependencies.
func NewManager(st *store.Store, b *bus.Bus, rt runtime.Runtime, ws workspace.Workspace, cfg *config.Config) *Manager {
	return &Manager{st: st, bus: b, rt: rt, ws: ws, cfg: cfg}
}

// Spawn validates req, reserves a session name/branch, persists the session,
// and (synchronously, in phase 1) creates its workspace and runtime. Any
// error after the session is persisted leaves the session in state
// "errored" (and its worktree, if created, in place for debugging) and is
// returned to the caller.
func (m *Manager) Spawn(ctx context.Context, req SpawnReq) (store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	proj, err := m.st.GetProject(req.Project)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Session{}, validationErr("project_not_found", "project not found: "+req.Project)
		}
		return store.Session{}, err
	}

	if !containsString(proj.Repos(), req.Repo) {
		return store.Session{}, validationErr("repo_not_in_project", "repo not linked to project: "+req.Repo)
	}

	repo, err := m.st.GetRepo(req.Repo)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Session{}, validationErr("repo_not_found", "repo not found: "+req.Repo)
		}
		return store.Session{}, err
	}

	if !idPattern.MatchString(req.Task) {
		return store.Session{}, validationErr("invalid_task", "task must match ^[a-z0-9-]+$")
	}

	feature := req.Feature
	if feature == "" {
		feature = req.Task
	}
	if !idPattern.MatchString(feature) {
		return store.Session{}, validationErr("invalid_feature", "feature must match ^[a-z0-9-]+$")
	}

	kind := req.Kind
	if kind == "" {
		kind = "worker"
	}
	if kind != "worker" {
		return store.Session{}, validationErr("invalid_kind", "only kind \"worker\" is supported by spawn in phase 1")
	}

	agentName := req.AgentName
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

	branch := "feature/" + feature + "/" + req.Task

	// Retry name reservation up to 3 times in case of concurrent spawn race.
	// The race: reserveName picks a name, but between that check and AddSession,
	// another spawn wins the race and inserts the same name. On ErrExists,
	// recompute the name (suffix search sees the winner) and retry.
	var id string
	var sess store.Session
	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		var err error
		id, err = m.reserveName(ctx, feature, req.Task)
		if err != nil {
			return store.Session{}, err
		}

		sess = store.Session{
			ID:          id,
			Kind:        kind,
			ProjectID:   req.Project,
			RepoID:      req.Repo,
			FeatureSlug: feature,
			Agent:       agentName,
			Branch:      branch,
			TmuxName:    id,
			State:       "spawning",
			Prompt:      req.Prompt,
		}

		if err := m.st.AddSession(sess); err != nil {
			if errors.Is(err, store.ErrExists) && attempt < maxRetries {
				continue // retry
			}
			if errors.Is(err, store.ErrExists) {
				return store.Session{}, fmt.Errorf("session name reservation raced 3 times for %q", feature+"-"+req.Task)
			}
			return store.Session{}, err
		}

		m.bus.Publish("session.spawned", id, map[string]any{
			"project": req.Project, "repo": req.Repo, "feature": feature, "task": req.Task,
		})
		break // success
	}

	wtRes, err := m.ws.Create(ctx, repo, id, branch)
	if err != nil {
		m.markErrored(id, err)
		return store.Session{}, err
	}
	if wtRes.BranchCollision {
		m.bus.Publish("workspace.branch_collision", id, map[string]any{"branch": branch})
	}
	sess.WorktreePath = wtRes.Path

	spec := agent.LaunchSpec{
		SessionID:    id,
		Kind:         kind,
		ProjectID:    req.Project,
		RepoID:       req.Repo,
		Feature:      feature,
		WorktreePath: wtRes.Path,
		FirstMessage: req.Prompt,
		SocketPath:   m.cfg.SocketPath(),
	}

	if err := ag.SetupWorkspace(spec); err != nil {
		m.markErrored(id, err)
		return store.Session{}, err
	}

	env := mergeEnv(repo.Env, ag.Env(spec))
	cmd := shellJoin(ag.LaunchCommand(spec))

	if _, err := m.rt.Create(ctx, runtime.CreateSpec{
		Name:    id,
		Dir:     wtRes.Path,
		Command: cmd,
		Env:     env,
	}); err != nil {
		m.markErrored(id, err)
		return store.Session{}, err
	}

	sess.State = "running"
	if err := m.st.UpdateSession(sess); err != nil {
		return store.Session{}, err
	}
	m.bus.Publish("session.state_changed", id, map[string]any{"from": "spawning", "to": "running"})

	return sess, nil
}

// slugWordRe matches runs of characters that are not lowercase ASCII
// alphanumerics; slugFromTitle collapses each run to a single "-".
var slugWordRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugFromTitle derives a feature slug from a task title: lowercased,
// non-alphanumeric runs collapsed to "-", trimmed of leading/trailing "-",
// capped at the first 4 "-"-separated words, then capped at 40 characters
// (trimming any trailing "-" left by the cut). Returns "" if the title
// yields no usable characters (e.g. empty, or entirely punctuation).
func slugFromTitle(title string) string {
	slug := slugWordRe.ReplaceAllString(strings.ToLower(title), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return ""
	}

	words := strings.Split(slug, "-")
	if len(words) > 4 {
		words = words[:4]
	}
	slug = strings.Join(words, "-")

	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}

	return slug
}

// SpawnOrchestrator validates and spawns an orchestrator session for task in
// project, deriving its feature slug from the task title. It mirrors Spawn's
// shape (reserve name -> persist "spawning" session -> create workspace ->
// launch runtime -> mark "running"), but: the session id is "<slug>-orch",
// its branch is "orch/<slug>", its repo is the project's main repo, and its
// system/first-message prompts are rendered from the orchestrator/kickoff
// templates instead of taking a caller-supplied prompt.
func (m *Manager) SpawnOrchestrator(ctx context.Context, task store.Task, project store.Project, agentName string) (store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	repo, err := m.st.GetRepo(project.MainRepo)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Session{}, validationErr("repo_not_found", "main repo not found: "+project.MainRepo)
		}
		return store.Session{}, err
	}

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

	base := slugFromTitle(task.Title)
	if base == "" {
		base = fmt.Sprintf("task-%d", task.ID)
	}

	// Retry name reservation up to 3 times in case of concurrent spawn race
	// (mirrors Spawn's retry loop; see its comment for the rationale).
	var slug, id string
	var sess store.Session
	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		var err error
		slug, id, err = m.reserveOrchestratorSlug(ctx, base)
		if err != nil {
			return store.Session{}, err
		}

		sess = store.Session{
			ID:          id,
			Kind:        "orchestrator",
			ProjectID:   project.ID,
			RepoID:      project.MainRepo,
			FeatureSlug: slug,
			Agent:       agentName,
			Branch:      "orch/" + slug,
			TmuxName:    id,
			State:       "spawning",
		}

		if err := m.st.AddSession(sess); err != nil {
			if errors.Is(err, store.ErrExists) && attempt < maxRetries {
				continue // retry
			}
			if errors.Is(err, store.ErrExists) {
				return store.Session{}, fmt.Errorf("orchestrator name reservation raced 3 times for %q", base)
			}
			return store.Session{}, err
		}

		m.bus.Publish("session.spawned", id, map[string]any{
			"project": project.ID, "repo": project.MainRepo, "feature": slug, "task": task.ID,
		})
		break // success
	}

	wtRes, err := m.ws.Create(ctx, repo, id, sess.Branch)
	if err != nil {
		m.markErrored(id, err)
		return store.Session{}, err
	}
	if wtRes.BranchCollision {
		m.bus.Publish("workspace.branch_collision", id, map[string]any{"branch": sess.Branch})
	}
	sess.WorktreePath = wtRes.Path

	allowedRepos, err := allowedReposDesc(m.st, project)
	if err != nil {
		m.markErrored(id, err)
		return store.Session{}, err
	}

	sysPrompt, err := prompts.Render(m.cfg.Home, "orchestrator", prompts.Vars{
		"feature_slug":   slug,
		"task_id":        fmt.Sprint(task.ID),
		"project_name":   project.Name,
		"main_repo":      project.MainRepo,
		"main_repo_path": repo.Path,
		"allowed_repos":  allowedRepos,
		"session_id":     id,
		"worktree_path":  wtRes.Path,
		"project_rules":  "",
	})
	if err != nil {
		m.markErrored(id, err)
		return store.Session{}, err
	}

	kickoff, err := prompts.Render(m.cfg.Home, "kickoff", prompts.Vars{
		"task_id":          fmt.Sprint(task.ID),
		"task_title":       task.Title,
		"task_description": task.Description,
		"allowed_repos":    allowedRepos,
	})
	if err != nil {
		m.markErrored(id, err)
		return store.Session{}, err
	}

	spec := agent.LaunchSpec{
		SessionID:    id,
		Kind:         "orchestrator",
		ProjectID:    project.ID,
		RepoID:       project.MainRepo,
		Feature:      slug,
		WorktreePath: wtRes.Path,
		SystemPrompt: sysPrompt,
		FirstMessage: kickoff,
		SocketPath:   m.cfg.SocketPath(),
	}

	if err := ag.SetupWorkspace(spec); err != nil {
		m.markErrored(id, err)
		return store.Session{}, err
	}

	env := mergeEnv(repo.Env, ag.Env(spec))
	cmd := shellJoin(ag.LaunchCommand(spec))

	if _, err := m.rt.Create(ctx, runtime.CreateSpec{
		Name:    id,
		Dir:     wtRes.Path,
		Command: cmd,
		Env:     env,
	}); err != nil {
		m.markErrored(id, err)
		return store.Session{}, err
	}

	sess.State = "running"
	if err := m.st.UpdateSession(sess); err != nil {
		return store.Session{}, err
	}
	m.bus.Publish("session.state_changed", id, map[string]any{"from": "spawning", "to": "running"})

	return sess, nil
}

// reserveOrchestratorSlug returns an unused (slug, sessionID) pair for an
// orchestrator based on base: sessionID is "<slug>-orch", suffixed with
// "-2", "-3", ... (applied to base, not to "-orch") until both the session
// id and the feature slug are free. A slug is taken if any session (live or
// historical) already carries it as FeatureSlug, or if the derived session
// id exists in the store (any state) or names a currently live runtime
// session.
func (m *Manager) reserveOrchestratorSlug(ctx context.Context, base string) (slug, id string, err error) {
	liveNames, err := m.rt.List(ctx)
	if err != nil {
		return "", "", err
	}
	liveSet := make(map[string]bool, len(liveNames))
	for _, n := range liveNames {
		liveSet[n] = true
	}

	for i := 1; ; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}
		candidateID := candidate + "-orch"

		if liveSet[candidateID] {
			continue
		}
		if _, err := m.st.GetSession(candidateID); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return "", "", err
		}

		existing, err := m.st.ListSessions(store.SessionFilter{Feature: candidate, All: true})
		if err != nil {
			return "", "", err
		}
		if len(existing) > 0 {
			continue
		}

		return candidate, candidateID, nil
	}
}

// allowedReposDesc renders project's repos (main first, then linked) as a
// comma-separated "<id> (<path>)" list for the orchestrator prompt's
// allowed_repos variable.
func allowedReposDesc(st *store.Store, project store.Project) (string, error) {
	repoIDs := project.Repos()
	parts := make([]string, 0, len(repoIDs))
	for _, rid := range repoIDs {
		r, err := st.GetRepo(rid)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", r.ID, r.Path))
	}
	return strings.Join(parts, ", "), nil
}

// Kill destroys the session's runtime handle (best-effort) and marks it
// killed. Killing a session already in a terminal state (killed/done/
// errored) is idempotent: no state change and no session.killed event, but
// cleanup still runs if requested.
func (m *Manager) Kill(ctx context.Context, id string, cleanup bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, err := m.st.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return validationErr("session_not_found", "session not found: "+id)
		}
		return err
	}

	if !isTerminal(sess.State) {
		_ = m.rt.Destroy(ctx, runtime.Handle{Name: sess.TmuxName})
		if err := m.st.UpdateSessionState(id, "killed"); err != nil {
			return err
		}
		m.bus.Publish("session.killed", id, nil)
	}

	if cleanup {
		repo, err := m.st.GetRepo(sess.RepoID)
		if err != nil {
			return err
		}
		if err := m.ws.Destroy(ctx, repo, id); err != nil {
			return err
		}
		m.bus.Publish("workspace.cleanup", id, nil)
	}

	return nil
}

// Restore recreates a dead/errored session's workspace and runtime, then
// marks it running. It is only allowed from state "errored"/"killed", or
// from "running" when the runtime handle is no longer alive.
func (m *Manager) Restore(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, err := m.st.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return validationErr("session_not_found", "session not found: "+id)
		}
		return err
	}

	allowed := sess.State == "errored" || sess.State == "killed"
	if !allowed && sess.State == "running" && !m.rt.Alive(ctx, runtime.Handle{Name: sess.TmuxName}) {
		allowed = true
	}
	if !allowed {
		return validationErr("restore_not_allowed", "session cannot be restored from state: "+sess.State)
	}

	repo, err := m.st.GetRepo(sess.RepoID)
	if err != nil {
		return err
	}

	path, err := m.ws.Restore(ctx, repo, id, sess.Branch)
	if err != nil {
		return err
	}

	ag, err := agent.Get(sess.Agent)
	if err != nil {
		return err
	}

	spec := agent.LaunchSpec{
		SessionID:    id,
		Kind:         sess.Kind,
		ProjectID:    sess.ProjectID,
		RepoID:       sess.RepoID,
		Feature:      sess.FeatureSlug,
		WorktreePath: path,
		FirstMessage: "",
		SocketPath:   m.cfg.SocketPath(),
	}

	env := mergeEnv(repo.Env, ag.Env(spec))
	cmd := shellJoin(ag.LaunchCommand(spec))

	if _, err := m.rt.Create(ctx, runtime.CreateSpec{
		Name:    id,
		Dir:     path,
		Command: cmd,
		Env:     env,
	}); err != nil {
		return err
	}

	sess.WorktreePath = path
	sess.State = "running"
	if err := m.st.UpdateSession(sess); err != nil {
		return err
	}
	m.bus.Publish("session.restored", id, nil)

	return nil
}

// Output returns the last `lines` lines of the session's runtime pane.
func (m *Manager) Output(ctx context.Context, id string, lines int) (string, error) {
	sess, err := m.st.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", validationErr("session_not_found", "session not found: "+id)
		}
		return "", err
	}
	return m.rt.Capture(ctx, runtime.Handle{Name: sess.TmuxName}, lines)
}

// AttachCommand returns the argv a user can run to attach to the session.
func (m *Manager) AttachCommand(id string) ([]string, error) {
	sess, err := m.st.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, validationErr("session_not_found", "session not found: "+id)
		}
		return nil, err
	}
	return m.rt.AttachCommand(runtime.Handle{Name: sess.TmuxName}), nil
}

// markErrored transitions a session to "errored" and publishes
// session.state_changed with the failure reason. Errors from the store
// update itself are swallowed (best-effort) since the caller already has a
// more relevant error to return.
func (m *Manager) markErrored(id string, cause error) {
	_ = m.st.UpdateSessionState(id, "errored")
	m.bus.Publish("session.state_changed", id, map[string]any{
		"from": "spawning", "to": "errored", "reason": cause.Error(),
	})
}

// reserveName returns an unused session id of the form "<feature>-<task>",
// suffixed with "-2", "-3", ... on collision. A name is taken if it exists
// in the store (any state) or names a currently live runtime session.
func (m *Manager) reserveName(ctx context.Context, feature, task string) (string, error) {
	base := feature + "-" + task

	liveNames, err := m.rt.List(ctx)
	if err != nil {
		return "", err
	}
	liveSet := make(map[string]bool, len(liveNames))
	for _, n := range liveNames {
		liveSet[n] = true
	}

	for i := 1; ; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d", base, i)
		}

		if liveSet[candidate] {
			continue
		}
		if _, err := m.st.GetSession(candidate); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return "", err
		}

		return candidate, nil
	}
}

func isTerminal(state string) bool {
	return state == "killed" || state == "done" || state == "errored"
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// mergeEnv combines repo-level and agent-level environment variables. On
// conflict the agent's value wins, since agent env carries protocol-level
// ROCKET_* variables the runtime depends on.
func mergeEnv(repoEnv, agentEnv map[string]string) map[string]string {
	out := make(map[string]string, len(repoEnv)+len(agentEnv))
	for k, v := range repoEnv {
		out[k] = v
	}
	for k, v := range agentEnv {
		out[k] = v
	}
	return out
}

// shellQuote wraps s in single quotes, escaping embedded single quotes, so
// it can be safely embedded in a command string passed to `sh -c`.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellJoin shell-quotes each element of parts and joins them with spaces,
// producing a single command string suitable for runtime.CreateSpec.Command
// (which the launch script hands to sh).
func shellJoin(parts []string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	return strings.Join(quoted, " ")
}
