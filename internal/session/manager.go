// Package session implements rocket's session lifecycle: validating and
// executing a spawn request, killing a session, and restoring one that has
// died or errored out.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

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
// in phase 1). ParentID and SubtaskID are set by the orchestrator-spawn API
// path (internal/api's POST /v1/sessions): when ParentID is non-empty, Spawn
// renders the worker system prompt (internal/prompts, template "worker")
// instead of leaving it empty, and stores ParentID on the new session.
type SpawnReq struct {
	Project   string
	Repo      string
	Task      string
	Feature   string
	Prompt    string
	AgentName string
	Kind      string
	ParentID  string
	SubtaskID int64
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

	// getToken is an optional function that returns the GitHub token to be
	// injected into session environments. If nil or returns empty string, no
	// token is injected.
	getToken func() string

	// quizSleepFn is called between each keystroke AnswerQuiz sends (see
	// quiz.go's sendQuizKeys); defaults to time.Sleep, overridable via
	// SetQuizTiming so tests don't pay quizKeySettle in wall-clock time.
	quizSleepFn func(time.Duration)
	// quizUnconfirmedTimeout bounds how long waitQuizResolved waits for a
	// session.quiz_resolved event before publishing
	// session.quiz_answer_unconfirmed; defaults to 60s, overridable via
	// SetQuizTiming.
	quizUnconfirmedTimeout time.Duration

	// quizInFlight tracks session IDs with an in-progress quiz-answer
	// injection (see AnswerQuiz's tryStartQuizInFlight/clearQuizInFlight in
	// quiz.go), guarded by mu. A second POST /v1/sessions/{id}/quiz/answer
	// for a session already present in this set is rejected with
	// "quiz_answer_in_flight" rather than interleaving a second keystroke
	// sequence into the same tmux pane.
	quizInFlight map[string]bool
}

// NewManager builds a Manager wired to the given dependencies.
func NewManager(st *store.Store, b *bus.Bus, rt runtime.Runtime, ws workspace.Workspace, cfg *config.Config) *Manager {
	return &Manager{
		st: st, bus: b, rt: rt, ws: ws, cfg: cfg,
		quizSleepFn:            time.Sleep,
		quizUnconfirmedTimeout: 60 * time.Second,
	}
}

// SetTokenSource sets the function used to retrieve the GitHub token for
// session environments. If f is nil or returns an empty string, no GH_TOKEN
// will be injected into session environments.
func (m *Manager) SetTokenSource(f func() string) {
	m.getToken = f
}

// injectToken adds the GitHub token to the environment map if a token source
// is configured and returns a non-empty token. It does not override GH_TOKEN
// if it's already present in the environment (repo.Env takes precedence).
func (m *Manager) injectToken(env map[string]string) {
	// Don't inject if already set by repo config or if no token source
	if _, exists := env["GH_TOKEN"]; exists {
		return
	}
	if m.getToken == nil {
		return
	}

	token := m.getToken()
	if token != "" {
		env["GH_TOKEN"] = token
	}
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
			ParentID:    req.ParentID,
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

	if req.ParentID != "" {
		sysPrompt, err := prompts.Render(m.cfg.Home, "worker", prompts.Vars{
			"task_name":     req.Task,
			"subtask_id":    fmt.Sprint(req.SubtaskID),
			"feature_slug":  feature,
			"project_name":  proj.Name,
			"repo_id":       req.Repo,
			"branch":        branch,
			"session_id":    id,
			"worktree_path": wtRes.Path,
			"parent_id":     req.ParentID,
		})
		if err != nil {
			m.markErrored(id, err)
			return store.Session{}, err
		}
		spec.SystemPrompt = sysPrompt
	}

	if err := ag.SetupWorkspace(spec); err != nil {
		m.markErrored(id, err)
		return store.Session{}, err
	}

	env := mergeEnv(repo.Env, ag.Env(spec))
	m.injectToken(env)
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
	m.injectToken(env)
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
	return m.terminate(ctx, id, "killed", "session.killed", nil, cleanup)
}

// Complete transitions a live session to its final "done" state: destroys
// the runtime handle (best-effort) and the workspace, marks the session
// done, and publishes session.state_changed{to:"done", reason:"pr_merged"}
// followed by workspace.cleanup. It is the terminal path used by the
// auto-cleanup flow after a worker's PR merges (see ghpoller.Reactions),
// distinct from Kill in that the session ends up "done" rather than
// "killed" and cleanup is unconditional. Completing a session already in a
// terminal state is idempotent for the state/runtime-destroy step (mirrors
// Kill), but workspace cleanup still runs.
func (m *Manager) Complete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.terminate(ctx, id, "done", "session.state_changed",
		map[string]any{"to": "done", "reason": "pr_merged"}, true)
}

// terminate is the shared implementation behind Kill and Complete: it
// best-effort destroys the runtime handle, transitions the session to
// finalState (skipped if the session is already terminal), publishes
// event/eventData for that transition, and — if cleanup is true — destroys
// the workspace and publishes workspace.cleanup. Callers must hold m.mu.
func (m *Manager) terminate(ctx context.Context, id, finalState, event string, eventData map[string]any, cleanup bool) error {
	sess, err := m.st.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return validationErr("session_not_found", "session not found: "+id)
		}
		return err
	}

	if !isTerminal(sess.State) {
		_ = m.rt.Destroy(ctx, runtime.Handle{Name: sess.TmuxName})
		if err := m.st.UpdateSessionState(id, finalState); err != nil {
			return err
		}
		// Terminal sessions can't answer a pending quiz, so clear it along
		// with the state transition (best-effort: the transition already
		// happened, and a leftover pending_quiz is harmless clutter, not a
		// correctness issue worth failing the whole call over).
		_ = m.st.ClearPendingQuiz(id)
		m.bus.Publish(event, id, eventData)
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

// KillCascade kills id. When id names a live orchestrator session, every
// live session with ParentID == id (its workers) is killed first, then the
// orchestrator itself — so an orchestrator never outlives the workers it
// spawned. cleanup is applied to every session killed. Killing a
// non-orchestrator session behaves exactly like Kill.
func (m *Manager) KillCascade(ctx context.Context, id string, cleanup bool) error {
	sess, err := m.st.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return validationErr("session_not_found", "session not found: "+id)
		}
		return err
	}

	if sess.Kind == "orchestrator" {
		all, err := m.st.ListSessions(store.SessionFilter{All: true})
		if err != nil {
			return err
		}
		for _, w := range all {
			if w.ParentID != id || isTerminal(w.State) {
				continue
			}
			if err := m.Kill(ctx, w.ID, cleanup); err != nil {
				return err
			}
		}
	}

	return m.Kill(ctx, id, cleanup)
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

	m.fillRestorePrompt(&spec, sess, repo, path)

	if err := ag.SetupWorkspace(spec); err != nil {
		m.markErrored(id, err)
		return err
	}

	env := mergeEnv(repo.Env, ag.Env(spec))
	m.injectToken(env)
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

// fillRestorePrompt best-effort repopulates spec.SystemPrompt and
// spec.FirstMessage for a restored orchestrator or worker session. A bare
// relaunch (the old behavior) starts a brand new agent process with no
// system prompt and no first message: since claudecode.LaunchCommand
// never resumes prior chat history, that process has no idea what feature
// or subtask it owns, breaking the very recovery path the orchestrator is
// told to use for stalled workers ("rocket restore <id>").
//
// The task that owns sess (found via GetTaskBySessionID: the root task for
// an orchestrator, the subtask for a worker) carries everything needed to
// rebuild the same system prompt Start/Spawn used. If no such task is
// found (e.g. a session restored outside the task system, or in tests),
// spec is left untouched and Restore proceeds exactly as before.
func (m *Manager) fillRestorePrompt(spec *agent.LaunchSpec, sess store.Session, repo store.Repo, worktreePath string) {
	task, err := m.st.GetTaskBySessionID(sess.ID)
	if err != nil {
		return
	}

	proj, err := m.st.GetProject(sess.ProjectID)
	if err != nil {
		return
	}

	switch sess.Kind {
	case "orchestrator":
		allowedRepos, err := allowedReposDesc(m.st, proj)
		if err != nil {
			return
		}
		sysPrompt, err := prompts.Render(m.cfg.Home, "orchestrator", prompts.Vars{
			"feature_slug":   sess.FeatureSlug,
			"task_id":        fmt.Sprint(task.ID),
			"project_name":   proj.Name,
			"main_repo":      proj.MainRepo,
			"main_repo_path": repo.Path,
			"allowed_repos":  allowedRepos,
			"session_id":     sess.ID,
			"worktree_path":  worktreePath,
			"project_rules":  "",
		})
		if err != nil {
			return
		}
		spec.SystemPrompt = sysPrompt
		spec.FirstMessage = fmt.Sprintf(
			"[rocket restore] You were relaunched after a crash or interruption. "+
				"This is task #%d %q. Run `rocket task show %d` to see the spec, decisions, "+
				"subtasks and Q&A you already recorded, then continue from there.",
			task.ID, task.Title, task.ID,
		)
	case "worker":
		sysPrompt, err := prompts.Render(m.cfg.Home, "worker", prompts.Vars{
			"task_name":     task.Title,
			"subtask_id":    fmt.Sprint(task.ID),
			"feature_slug":  sess.FeatureSlug,
			"project_name":  proj.Name,
			"repo_id":       sess.RepoID,
			"branch":        sess.Branch,
			"session_id":    sess.ID,
			"worktree_path": worktreePath,
			"parent_id":     sess.ParentID,
		})
		if err != nil {
			return
		}
		spec.SystemPrompt = sysPrompt
		msg := fmt.Sprintf(
			"[rocket restore] You were relaunched after a crash or interruption. "+
				"Your branch is %q; check `git status`/`git log` for progress already made.",
			sess.Branch,
		)
		if sess.Prompt != "" {
			msg += "\n\nYour original brief:\n\n" + sess.Prompt
		}
		spec.FirstMessage = msg
	}
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

// PinWindowSize pins the session's tmux window to the drawable area of a
// clientCols×clientRows client (see runtime.Runtime.PinWindowSize). The
// web terminal calls this on every resize so the window always matches
// the web client exactly, per the client-size policy in
// docs/03-daemon-api.md.
func (m *Manager) PinWindowSize(ctx context.Context, id string, clientCols, clientRows int) error {
	sess, err := m.st.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return validationErr("session_not_found", "session not found: "+id)
		}
		return err
	}
	return m.rt.PinWindowSize(ctx, runtime.Handle{Name: sess.TmuxName}, clientCols, clientRows)
}

// UnpinWindowSize releases a PinWindowSize override so the window follows
// attached clients again. Called when the last web terminal for the
// session disconnects.
func (m *Manager) UnpinWindowSize(ctx context.Context, id string) error {
	sess, err := m.st.GetSession(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return validationErr("session_not_found", "session not found: "+id)
		}
		return err
	}
	return m.rt.UnpinWindowSize(ctx, runtime.Handle{Name: sess.TmuxName})
}

// TmuxInfo describes one live tmux session for /v1/system inspection.
type TmuxInfo struct {
	// Name is the tmux session name.
	Name string
	// SessionID is the store session that owns this tmux name, or "" if
	// Orphan is true.
	SessionID string
	// State is the owning session's state (e.g. "running", "killed",
	// "errored"), or "" if Orphan is true.
	State string
	// Orphan is true when no store record at all (in any state)
	// references this tmux name.
	Orphan bool
}

// WorktreeInfo describes one on-disk worktree directory for /v1/system
// inspection.
type WorktreeInfo struct {
	// Path is the absolute path to the worktree directory.
	Path string
	// SessionID is the session the worktree's directory name identifies
	// (regardless of whether that session still exists/is live).
	SessionID string
	// SizeBytes is the worktree's on-disk size.
	SizeBytes int64
	// State is the owning session's state (e.g. "running", "killed",
	// "errored"), or "" if Orphan is true.
	State string
	// Orphan is true when no store record at all (in any state)
	// references this worktree path.
	Orphan bool
}

// ListTmux returns every live tmux session whose name looks like a rocket
// session name, flagged with whether it's orphaned (see TmuxInfo.Orphan).
func (m *Manager) ListTmux(ctx context.Context) ([]TmuxInfo, error) {
	return m.tmuxInfos(ctx)
}

// ListWorktrees returns every worktree directory found on disk, flagged
// with whether it's orphaned (see WorktreeInfo.Orphan).
func (m *Manager) ListWorktrees() ([]WorktreeInfo, error) {
	return m.worktreeInfos()
}

// tmuxInfos is the unlocked implementation shared by ListTmux and Cleanup.
//
// A tmux name is an orphan only if no store record at all (in any state:
// spawning/running/killed/errored/done) references it. A resource left
// behind by a killed or errored session still has a store record, so it is
// reported (with its owning session's id and state) but never touched by
// Cleanup — only kill --cleanup or restore should remove it.
func (m *Manager) tmuxInfos(ctx context.Context) ([]TmuxInfo, error) {
	names, err := m.rt.List(ctx)
	if err != nil {
		return nil, err
	}

	all, err := m.st.ListSessions(store.SessionFilter{All: true})
	if err != nil {
		return nil, err
	}
	byTmux := make(map[string]store.Session, len(all))
	for _, s := range all {
		byTmux[s.TmuxName] = s
	}

	out := make([]TmuxInfo, 0, len(names))
	for _, name := range names {
		if !idPattern.MatchString(name) {
			// Not a rocket-shaped session name; not ours to report or
			// touch.
			continue
		}
		s, ok := byTmux[name]
		if !ok {
			out = append(out, TmuxInfo{Name: name, Orphan: true})
			continue
		}
		out = append(out, TmuxInfo{Name: name, SessionID: s.ID, State: s.State, Orphan: false})
	}
	return out, nil
}

// worktreeInfos is the unlocked implementation shared by ListWorktrees and
// Cleanup.
//
// A worktree path is an orphan only if no store record at all (in any
// state) references it; see tmuxInfos for the rationale.
func (m *Manager) worktreeInfos() ([]WorktreeInfo, error) {
	entries, err := m.ws.List()
	if err != nil {
		return nil, err
	}

	all, err := m.st.ListSessions(store.SessionFilter{All: true})
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]store.Session, len(all))
	for _, s := range all {
		if s.WorktreePath != "" {
			byPath[s.WorktreePath] = s
		}
	}

	out := make([]WorktreeInfo, 0, len(entries))
	for _, e := range entries {
		s, ok := byPath[e.Path]
		if !ok {
			out = append(out, WorktreeInfo{Path: e.Path, SessionID: e.SessionID, SizeBytes: e.SizeBytes, Orphan: true})
			continue
		}
		out = append(out, WorktreeInfo{Path: e.Path, SessionID: e.SessionID, SizeBytes: e.SizeBytes, State: s.State, Orphan: false})
	}
	return out, nil
}

// Cleanup destroys every orphaned tmux session and removes every orphaned
// worktree directory (see TmuxInfo.Orphan / WorktreeInfo.Orphan), never
// touching a resource that any store record references — including
// resources left behind by killed or errored sessions, which are removed
// via kill --cleanup or restore instead. It returns the names/paths
// actually cleaned up; errors from individual operations are collected and
// joined, but cleanup continues for the rest.
func (m *Manager) Cleanup(ctx context.Context) (killedTmux, removedWorktrees []string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	tmux, terr := m.tmuxInfos(ctx)
	if terr != nil {
		errs = append(errs, terr)
	}
	for _, t := range tmux {
		if !t.Orphan {
			continue
		}
		if derr := m.rt.Destroy(ctx, runtime.Handle{Name: t.Name}); derr != nil {
			errs = append(errs, derr)
			continue
		}
		killedTmux = append(killedTmux, t.Name)
	}

	wt, werr := m.worktreeInfos()
	if werr != nil {
		errs = append(errs, werr)
	}
	for _, e := range wt {
		if !e.Orphan {
			continue
		}
		// e.SessionID's parent directory name doubles as the repo ID
		// (worktreesDir/<repo-id>/<session-id>/), so we can look up the
		// repo even for a worktree whose store session record is long
		// gone.
		repoID := filepath.Base(filepath.Dir(e.Path))
		if repo, gerr := m.st.GetRepo(repoID); gerr == nil {
			if derr := m.ws.Destroy(ctx, repo, e.SessionID); derr != nil {
				errs = append(errs, derr)
				continue
			}
		} else {
			// No known repo to run `git worktree remove` against
			// (e.g. the repo itself was deleted): fall back to a
			// plain directory removal. This never touches a branch
			// ref, consistent with the package's iron rule.
			if derr := os.RemoveAll(e.Path); derr != nil {
				errs = append(errs, derr)
				continue
			}
		}
		removedWorktrees = append(removedWorktrees, e.Path)
	}

	if len(errs) > 0 {
		err = errors.Join(errs...)
	}
	return killedTmux, removedWorktrees, err
}

// markErrored transitions a session to "errored" and publishes
// session.state_changed with the failure reason. Errors from the store
// update itself are swallowed (best-effort) since the caller already has a
// more relevant error to return.
func (m *Manager) markErrored(id string, cause error) {
	_ = m.st.UpdateSessionState(id, "errored")
	_ = m.st.ClearPendingQuiz(id)
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
