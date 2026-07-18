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
}

// ---------------------------------------------------------------------------
// Projects — internal/api/projects.go
// ---------------------------------------------------------------------------

export interface TaskCounters {
  backlog: number
  in_progress: number
  review: number
  done: number
}

export interface Project {
  id: string
  name: string
  main: string
  linked: string[]
  live_sessions: number
  tasks: TaskCounters
  created_at: number
  /** Contract field (phase 3): open questions awaiting a user reply. Optional until phase 3 lands. */
  awaiting_questions?: number
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
// Contract types — phase 3/4, not yet implemented by the daemon.
// Shape follows docs/12-tasks.md «Поля» and docs/03-daemon-api.md
// «Задачи» / «Настройки и GitHub».
// ---------------------------------------------------------------------------

export type TaskStatus = 'backlog' | 'in_progress' | 'review' | 'done' | 'cancelled'

export type TaskCreatedBy = 'user' | 'orchestrator'

/** Contract type (phase 3): docs/12-tasks.md «Поля». */
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
 * Contract type (phase 3): `GET /v1/tasks/{id}` per docs/03-daemon-api.md
 * returns the task's fields plus its subtasks and the session it's bound to
 * (with `tmux_name` and the attach command). To be reconciled with the real
 * response shape at phase-3 integration.
 */
export interface TaskDetail extends Task {
  subtasks: Task[]
  session?: {
    id: string
    tmux_name: string
    attach: string[]
  }
}

export type TaskDocKind = 'spec' | 'plan' | 'report' | 'doc'

/** Contract type (phase 3): docs/12-tasks.md «Документы и журнал задачи». */
export interface TaskDoc {
  id: number
  task_id: number
  kind: TaskDocKind
  title: string
  body: string
  version: number
  created_at: number
}

export type TaskLogKind = 'decision' | 'problem' | 'note' | 'status'

/** Contract type (phase 3): docs/12-tasks.md «Документы и журнал задачи». */
export interface TaskLogEntry {
  id: number
  task_id: number
  kind: TaskLogKind
  body: string
  author: string
  created_at: number
}

export type QuestionStatus = 'open' | 'resolved'

/** Contract type (phase 3): docs/12-tasks.md «Вопросы и ответы через задачу». */
export interface Question {
  id: number
  task_id: number
  seq: number
  body: string
  context?: string
  status: QuestionStatus
  created_at: number
  resolved_at?: number
  messages: QuestionMessage[]
}

export type QuestionMessageAuthor = 'orchestrator' | 'user'
export type QuestionMessageKind = 'reply' | 'answer' | 'dismiss'

/** Contract type (phase 3): docs/12-tasks.md «Вопросы и ответы через задачу». */
export interface QuestionMessage {
  id: number
  question_id: number
  author: QuestionMessageAuthor
  kind: QuestionMessageKind
  body: string
  created_at: number
}

/** Contract type (phase 4): docs/03-daemon-api.md «Настройки и GitHub». */
export interface GithubRepo {
  full_name: string
  private: boolean
  default_branch: string
}

/** Contract type (phase 4): docs/03-daemon-api.md «Настройки и GitHub». */
export interface Settings {
  github_token?: string
  github_authorized_as?: string
}
