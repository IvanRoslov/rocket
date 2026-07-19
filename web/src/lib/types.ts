// API types for the rocket daemon HTTP+JSON API (`/v1`).
//
// Fields mirror the daemon's JSON responses verbatim (snake_case), per
// internal/api/*.go. Entities not yet implemented by the daemon (tasks,
// questions, settings, github repos — phase 3/4) are marked "Contract type"
// below: their shape follows docs/03-daemon-api.md and docs/12-tasks.md,
// not real Go code.

// ---------------------------------------------------------------------------
// Sessions — internal/api/sessions.go:14-31
// ---------------------------------------------------------------------------

export type SessionState = 'spawning' | 'running' | 'done' | 'killed' | 'errored'

export type SessionActivity = 'active' | 'ready' | 'idle' | 'waiting_input' | 'blocked' | 'exited'

export interface Session {
  id: string
  kind: string
  project_id: string
  repo_id: string
  feature_slug: string
  parent_id?: string
  agent: string
  branch: string
  worktree_path: string
  tmux_name: string
  state: SessionState
  activity?: SessionActivity
  prompt?: string
  activity_ts?: number
  created_at: number
  updated_at: number
  /**
   * PR fields (phase 4, internal/api/sessions.go:30-32): omitted until the
   * GitHub poller (every 2m, worker sessions only) finds a PR for the
   * session's branch.
   */
  pr_number?: number
  pr_state?: 'open' | 'closed' | 'merged'
  ci_state?: 'passing' | 'pending' | 'failing'
}

// ---------------------------------------------------------------------------
// Projects — internal/api/projects.go
// ---------------------------------------------------------------------------

export interface Project {
  id: string
  name: string
  main: string
  linked: string[]
  live_sessions: number
  created_at: number
}

// ---------------------------------------------------------------------------
// Repos — internal/api/repos.go
// ---------------------------------------------------------------------------

export interface Repo {
  id: string
  path: string
  default_branch: string
  auto_cleanup: boolean
  env: Record<string, string>
  symlinks: string[]
  post_create: string[]
  created_at: number
}

// ---------------------------------------------------------------------------
// Messages — internal/api/messages.go
// ---------------------------------------------------------------------------

export type MessageStatus = 'queued' | 'delivered' | 'failed'

export interface Message {
  id: number
  from?: string
  to: string
  body: string
  status: MessageStatus
  attempts: number
  created_at: number
  delivered_at?: number
  reason?: string
}

// ---------------------------------------------------------------------------
// Events — internal/api/events.go
// ---------------------------------------------------------------------------

export interface RocketEvent {
  id: number
  ts: number
  type: string
  session_id?: string
  data?: Record<string, unknown>
}

// ---------------------------------------------------------------------------
// System — internal/api/system.go
// ---------------------------------------------------------------------------

export interface DaemonInfo {
  version: string
  uptime_s: number
  port: number
  socket: string
  db_path: string
  config_path: string
}

export interface QueueCounts {
  queued: number
  failed: number
}

export interface TmuxEntry {
  name: string
  session_id?: string
  /** Owning session's state (e.g. "running", "killed"); omitted when orphan. */
  state?: string
  orphan: boolean
}

export interface WorktreeEntry {
  path: string
  session_id?: string
  size_bytes: number
  /** Owning session's state (e.g. "running", "killed"); omitted when orphan. */
  state?: string
  orphan: boolean
}

export interface SystemInfo {
  daemon: DaemonInfo
  queue: QueueCounts
  tmux: TmuxEntry[]
  worktrees: WorktreeEntry[]
  log_tail: string[]
}

export interface SystemCleanupResult {
  killed_tmux: string[]
  removed_worktrees: string[]
}

// ---------------------------------------------------------------------------
// Tasks / Questions — internal/api/tasks.go, internal/api/questions.go
// (phase 3, merged). Verified against .superpowers/sdd/phase3-contract.md.
// ---------------------------------------------------------------------------

export type TaskStatus = 'backlog' | 'in_progress' | 'review' | 'done' | 'cancelled'

export type TaskCreatedBy = 'user' | 'orchestrator'

/** `taskResponse` — internal/api/tasks.go. `parent_id` is omitted for root tasks. */
export interface Task {
  id: number
  parent_id?: number
  title: string
  description?: string
  project_id: string
  repo_id?: string
  status: TaskStatus
  feature_slug?: string
  session_id?: string
  created_by: TaskCreatedBy
  created_at: number
  updated_at: number
  completed_at?: number
}

/**
 * `taskDetailResponse` — `GET /v1/tasks/{id}`: `taskResponse` plus its
 * subtasks (always present, possibly empty) and the session it's bound to
 * (with `tmux_name` and the attach command as argv), and a count of open
 * questions.
 */
export interface TaskDetail extends Task {
  subtasks: Task[]
  session?: {
    id: string
    tmux_name: string
    attach: string[] | null
  }
  open_questions: number
}

export type TaskDocKind = 'spec' | 'plan' | 'report' | 'doc'

/** `GET /v1/tasks/{id}/docs` entry. `author` is "" (omitted) for the user. */
export interface TaskDoc {
  id: number
  task_id: number
  kind: TaskDocKind
  title: string
  body: string
  version: number
  author?: string
  created_at: number
}

export type TaskLogKind = 'decision' | 'problem' | 'note' | 'status'

/** `GET /v1/tasks/{id}/log` entry. `author` is "" (omitted) for the user. */
export interface TaskLogEntry {
  id: number
  task_id: number
  kind: TaskLogKind
  body: string
  author?: string
  created_at: number
}

export type QuestionStatus = 'open' | 'resolved'
export type QuestionResolution = 'answered' | 'dismissed'
export type QuestionWhoseTurn = 'user' | 'orchestrator' | ''

/** `questionResponse` — internal/api/questions.go. */
export interface Question {
  id: number
  task_id: number
  /** 1-based, displayed as "Q<ordinal>". */
  ordinal: number
  /** Session id of the orchestrator that asked the question. */
  asked_by: string
  body: string
  context?: string
  status: QuestionStatus
  resolution?: QuestionResolution
  whose_turn?: QuestionWhoseTurn
  asked_at: number
  resolved_at?: number
  messages: QuestionMessage[]
}

export type QuestionMessageKind = 'reply' | 'answer'

/**
 * A single entry in a question's thread. `author` is "" (omitted) for the
 * human user, otherwise the orchestrator session id that posted it — there
 * is no `dismiss` message kind; dismissing a question resolves it without
 * adding a thread message.
 */
export interface QuestionMessage {
  id: number
  author?: string
  kind: QuestionMessageKind
  body: string
  created_at: number
}

/** `catalogRepoResponse` — internal/api/github_catalog.go. GET /v1/github/repos wraps these as `{"repos":[...]}`. */
export interface GithubRepo {
  full_name: string
  private: boolean
  default_branch: string
}

/**
 * internal/api/settings.go. `GET /v1/settings` returns `{github_token}`
 * only — `github_token` is always present (masked, or "" when unset) and
 * `login` is never included. `PUT /v1/settings` returns the same shape plus
 * `login` (the authenticated GitHub login) when a non-empty token was
 * accepted — it's the *only* response that carries `login`, so callers that
 * want to display "Authorized as @login" must capture it from the PUT
 * response and hold it in local state.
 */
export interface Settings {
  github_token: string
  login?: string
}
