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
  pending_quiz?: PendingQuiz
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
  created_by: 'user' | 'orchestrator' | 'agent'
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
  /**
   * Who owes a response to this message. Empty or absent means "everyone in
   * the thread except its author" — the daemon's default fan-out.
   */
  addressed_to?: string[]
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
  /** Everyone taking part in the thread; ids are "human", an agent or a session. */
  participants: string[]
  /** The subset expected to speak next. */
  waiting_on: string[]
  /** Whether the caller — us — is one of them. Drives the indicator. */
  your_turn: boolean
  /** Pre-participant compat field; retired by subtask #736. Do not read it. */
  whose_turn?: 'user' | 'orchestrator' | ''
  asked_at: number
  resolved_at?: number
  messages: QuestionMessage[]
}

// Agents — docs/10-agents.md. An agent is a registration plus a tmux session
// named after it: rocket delivers messages to it and knows whether it is alive,
// nothing more. Everything past the stored columns is derived by the daemon.

export interface Agent {
  id: string
  description: string
  project: string
  /** Launcher pair, both optional: cwd and command for `POST /start`. */
  dir: string
  command: string
  enabled: boolean
  /** True while a tmux session named `id` is registered as live. */
  session_alive: boolean
  /** Unread inbox messages — what the agent has yet to pull. */
  unread: number
  open_questions: number
  awaiting_user: number
  created_at: number
  updated_at: number
}

export type AgentInboxStatus = 'unread' | 'read'

/** One inbox message — the only kind of row the inbox holds. */
export interface AgentInboxMessage {
  id: number
  from: string
  body: string
  status: AgentInboxStatus
  created_at: number
  /** Omitted while the message is unread. */
  read_at?: number
}

/** Agent Q&A thread — mirrors `Question` with the agent in place of the task. */
export interface AgentQuestion {
  id: number
  role_id: string
  ordinal: number
  asked_by: string
  body: string
  context?: string
  status: QuestionStatus
  resolution?: 'answered' | 'dismissed'
  /** Everyone taking part in the thread; ids are "human", an agent or a session. */
  participants: string[]
  /** The subset expected to speak next. */
  waiting_on: string[]
  /** Whether the caller — us — is one of them. Drives the indicator. */
  your_turn: boolean
  /** Pre-participant compat field; retired by subtask #736. Do not read it. */
  whose_turn?: 'user' | 'role' | ''
  asked_at: number
  resolved_at?: number
  messages: QuestionMessage[]
}

export interface GithubRepo {
  full_name: string
  private: boolean
  default_branch: string
}

export interface Settings {
  github_token: string
  login?: string
}

// Chat — docs/13-chat.md. A read-only mirror of the agent's native
// transcript; sending goes through the regular message queue.

export type ChatRole = 'user' | 'assistant' | 'tool' | 'quiz_answer'

export interface ChatEntry {
  role: ChatRole
  text: string
  /** Present only for role="tool" (e.g. "Bash", "Edit"). */
  tool_name?: string
  /** Unix seconds; 0 when the transcript record has no timestamp. */
  ts: number
  /**
   * AskUserQuestion rounds only: raw quiz JSON. On the tool entry — the
   * tool input (camelCase multiSelect); on the quiz_answer entry — echo of
   * `{questions, answers}` where answers maps question text → chosen label.
   */
  quiz?: ClosedQuizEcho | { questions?: RawQuizQuestion[] }
}

export interface RawQuizQuestion {
  question: string
  header?: string
  multiSelect?: boolean
  options?: { label: string; description?: string }[]
}

export interface ClosedQuizEcho {
  questions?: RawQuizQuestion[]
  answers?: Record<string, string>
}

// Live quiz state — docs/13-chat.md "Квизы (AskUserQuestion)".

export interface QuizOption {
  label: string
  description?: string
}

export interface PendingQuizQuestion {
  question: string
  header?: string
  multi_select: boolean
  options: QuizOption[]
}

export interface PendingQuiz {
  questions: PendingQuizQuestion[]
  asked_at: number
}

/** One answer per pending-quiz question: either option indices or free text. */
export interface QuizAnswer {
  question_index: number
  option_indices?: number[]
  text?: string
}

export interface ChatSessionInfo {
  id: string
  kind: string
  state: SessionState
  activity?: SessionActivity
  pending_quiz?: PendingQuiz
}

export interface ChatResponse {
  entries: ChatEntry[]
  next_cursor: string
  session?: ChatSessionInfo
}

export interface Health {
  status: string
  version: string
  uptime: string
}
