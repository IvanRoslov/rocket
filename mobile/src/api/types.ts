// API types for the rocket daemon HTTP+JSON API (`/v1`).
// Ported from web/src/lib/types.ts — keep in sync.

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
  pr_number?: number
  pr_state?: 'open' | 'closed' | 'merged'
  ci_state?: 'passing' | 'pending' | 'failing'
}

export interface Project {
  id: string
  name: string
  main: string
  linked: string[]
  live_sessions: number
  created_at: number
}

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
  state?: string
  orphan: boolean
}

export interface WorktreeEntry {
  path: string
  session_id?: string
  size_bytes: number
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

export type TaskStatus = 'backlog' | 'in_progress' | 'review' | 'done' | 'cancelled'

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
  created_by: 'user' | 'orchestrator'
  created_at: number
  updated_at: number
  completed_at?: number
}

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

export interface TaskLogEntry {
  id: number
  task_id: number
  kind: TaskLogKind
  body: string
  author?: string
  created_at: number
}

export type QuestionStatus = 'open' | 'resolved'

export interface QuestionMessage {
  id: number
  author?: string
  kind: 'reply' | 'answer'
  body: string
  created_at: number
}

export interface Question {
  id: number
  task_id: number
  ordinal: number
  asked_by: string
  body: string
  context?: string
  status: QuestionStatus
  resolution?: 'answered' | 'dismissed'
  whose_turn?: 'user' | 'orchestrator' | ''
  asked_at: number
  resolved_at?: number
  messages: QuestionMessage[]
}

export interface Settings {
  github_token: string
  login?: string
}

export interface Health {
  status: string
  version: string
  uptime: string
}
