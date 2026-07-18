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

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/bus"
	"github.com/IvanRoslov/rocket/internal/config"
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
