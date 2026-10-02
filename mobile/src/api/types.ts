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

/**
 * `brainstorm` (task #1077, spec v1) sits between `backlog` and `in_progress`:
 * the orchestrator is still brainstorming the task with the human and no
 * worker has been spawned yet. The daemon sets it on `task start` and moves it
 * to `in_progress` on the first spawn; a human can also move a task there by
 * hand, like into any other status.
 */
export type TaskStatus = 'backlog' | 'brainstorm' | 'in_progress' | 'review' | 'done' | 'cancelled'

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
  /**
   * Milestone (task #1023, spec v2): a root task outside every project
   * (`project_id` empty), held by a persistent agent. `assigned_role` names
   * that agent; `quiet` says the daemon has not seen it show work for longer
   * than `milestone_quiet_after` (absent = not quiet).
   */
  milestone?: boolean
  assigned_role?: string
  quiet?: boolean
  open_questions?: number
  questions_awaiting_user?: number
  /**
   * Brainstorm skill picked when the task started (task #4901):
   * `orchestrator-brainstorming@<version>` (task #5027) or
   * `superpowers:brainstorming`; `""` on
   * tasks started before it, absent on an older daemon.
   */
  brainstorm_skill?: string
}

/** A milestone as listed on its holder's agent card. */
export interface AgentMilestoneRef {
  id: number
  title: string
  status: TaskStatus
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

export type TaskDocKind = 'spec' | 'plan' | 'report' | 'doc' | 'problem'

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

/** How a brainstorm answer relates to the asker's recommendation (task #4901, spec §2.2). */
export type BrainstormOutcome = 'accepted' | 'corrected' | 'wrong_turn'

/**
 * The brainstorm part of a thread's JSON. The daemon sends it on every
 * thread: on non-brainstorm threads the options are null and the strings
 * empty; an older daemon omits it entirely — hence all optional.
 */
export interface BrainstormFields {
  /** 1-based option the asker recommends; null when none. */
  recommended_option?: number | null
  /** 1-based option the human picked; null for an answer in their own words. */
  chosen_option?: number | null
  /** The human's own text: the comment to a picked option, or the whole answer. */
  answer_comment?: string
  /** `ui` | `terminal`; `""` while unanswered. */
  answer_source?: '' | 'ui' | 'terminal'
  /** `""` while unanswered or dismissed. */
  outcome?: '' | BrainstormOutcome
  /** The human changed the computed outcome by hand. */
  outcome_overridden?: boolean
  /** Who closed the thread with an answer: "human", an agent id, or "". */
  answered_by?: string
}

export type QuestionType = 'decision' | 'fyi' | 'brainstorm'

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

export interface Question extends BrainstormFields {
  id: number
  task_id: number
  ordinal: number
  asked_by: string
  /** One-line heading; absent on a daemon older than task #1264. */
  title?: string
  /**
   * Plain-language markdown summary (problem → options → recommendation)
   * the agent writes alongside the body. `""` on old and human-opened
   * threads; absent on a daemon that predates it.
   */
  brief?: string
  body: string
  context?: string
  status: QuestionStatus
  resolution?: 'answered' | 'dismissed' | 'fyi'
  /** Everyone taking part in the thread; ids are "human", an agent or a session. */
  participants: string[]
  /** The subset expected to speak next. */
  waiting_on: string[]
  /** Whether the caller — us — is one of them. Drives the indicator. */
  your_turn: boolean
  /** Pre-participant compat field; retired by subtask #736. Do not read it. */
  whose_turn?: 'user' | 'orchestrator' | ''
  /** The one thread id a human sees — "1023/Q2" / "cto/Q1". See threadRefLabel(). */
  local_ref?: string
  /**
   * `decision` (the default), `fyi` — a status note, born closed — or
   * `brainstorm`: a decision thread of the orchestrator's storm that carries
   * a recommendation and an outcome (task #4901).
   */
  type?: QuestionType
  /** Answer choices; `choose` is a **1-based** index into this array. */
  options?: string[]
  /**
   * Open decision thread nobody has moved for longer than
   * `question_stale_after`. Daemon-derived: the threshold is configurable, so
   * never recompute it from `asked_at` here.
   */
  stale?: boolean
  /** The stored "whose turn" set; `waiting_on` is the same thing, older name. */
  attention?: string[]
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
  /** The milestones this agent holds (task #1023, spec v2). */
  milestones?: AgentMilestoneRef[]
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
  /** One-line heading; absent on a daemon older than task #1264. */
  title?: string
  /**
   * Plain-language markdown summary (problem → options → recommendation)
   * the agent writes alongside the body. `""` on old and human-opened
   * threads; absent on a daemon that predates it.
   */
  brief?: string
  body: string
  context?: string
  status: QuestionStatus
  resolution?: 'answered' | 'dismissed' | 'fyi'
  /** Everyone taking part in the thread; ids are "human", an agent or a session. */
  participants: string[]
  /** The subset expected to speak next. */
  waiting_on: string[]
  /** Whether the caller — us — is one of them. Drives the indicator. */
  your_turn: boolean
  /** Pre-participant compat field; retired by subtask #736. Do not read it. */
  whose_turn?: 'user' | 'role' | ''
  /** The one thread id a human sees — "1023/Q2" / "cto/Q1". See threadRefLabel(). */
  local_ref?: string
  /** `decision` (the default) or `fyi` — a status note, born closed. */
  type?: 'decision' | 'fyi'
  /** Answer choices; `choose` is a **1-based** index into this array. */
  options?: string[]
  /**
   * Open decision thread nobody has moved for longer than
   * `question_stale_after`. Daemon-derived: the threshold is configurable, so
   * never recompute it from `asked_at` here.
   */
  stale?: boolean
  /** The stored "whose turn" set; `waiting_on` is the same thing, older name. */
  attention?: string[]
  asked_at: number
  resolved_at?: number
  messages: QuestionMessage[]
}

/**
 * One row of `GET /v1/threads` — `threadInboxEntry` in
 * internal/api/thread_inbox.go. The unified inbox of task AND role threads:
 * what is open and on whom, in one listing, without walking every task.
 *
 * It deliberately carries the question body only, never the conversation: a
 * row that is expanded fetches the full thread from its per-subject endpoint.
 */
export interface ThreadInboxEntry extends BrainstormFields {
  /** "1023/Q2" or "cto/Q1" — the id a human types back. */
  local_ref: string
  kind: 'task' | 'role'
  /** Set for `kind: 'task'`. */
  task_id?: number
  /** Set for `kind: 'role'`. */
  role_id?: string
  /** Human-readable subject: `task #1023 "Ship it"` or `role cto`. Never parsed. */
  subject: string
  /** The global numeric id — what the per-thread write endpoints address. */
  id: number
  ordinal: number
  asked_by: string
  /** One-line heading; absent on a daemon older than task #1264. */
  title?: string
  /**
   * Plain-language markdown summary (problem → options → recommendation)
   * the agent writes alongside the body. `""` on old and human-opened
   * threads; absent on a daemon that predates it.
   */
  brief?: string
  body: string
  status: QuestionStatus
  resolution?: 'answered' | 'dismissed' | 'fyi'
  type: QuestionType
  options?: string[]
  participants: string[]
  /** The stored "whose turn" set; `waiting_on` is the same array, older name. */
  attention: string[]
  waiting_on: string[]
  /** Caller-relative: true when the app user is in `attention`. */
  your_turn: boolean
  asked_at: number
  /** Last movement — the last entry, or the question itself when nobody replied. */
  updated_at: number
  resolved_at?: number
  /** Open decision thread idle longer than `question_stale_after` — daemon-derived. */
  stale?: boolean
  /** Task threads only: the context a row needs to link and label itself. */
  project_id?: string
  task_title?: string
}

/** Storm exit gate (task #4901, spec §2.3). Times are unix seconds. */
export type GateStatus = 'pending' | 'go' | 'changes' | 'superseded'

export interface TaskGate {
  id: number
  task_id: number
  spec_version: number
  plan_version: number | null
  status: GateStatus
  /** The human's comment on "changes"; may be empty on "go". */
  comment: string
  decided_by: string
  requested_by: string
  requested_at: number
  decided_at: number | null
}

/** One storm's counters — `GET /v1/tasks/{id}/brainstorm/stats`. */
export interface BrainstormStorm {
  task_id: number
  title: string
  project_id: string
  /** Skill of the storm; `"unknown"` for tasks without one. */
  skill: string
  questions: number
  answered: number
  accepted: number
  accepted_with_comment: number
  corrected: number
  wrong_turn: number
  /** Participants who answered ("human" or an agent id), each once, in first-answer order; `[]` with none. */
  answered_by: string[]
  /** Per-participant counters in the same order; the totals above are their sum. */
  by_answerer: BrainstormAnswerer[]
  /** Gates decided as "changes" before the first Go (all of them without a Go). */
  spec_changes: number
  /** `go_at != null && spec_changes == 0`. */
  first_try_go: boolean
  /** The task had at least one gate, in any status. */
  has_gate: boolean
  /** Unix seconds of the Go; null while there is none. */
  go_at: number | null
}

/** One participant's answers within a storm. */
export interface BrainstormAnswerer {
  answered_by: string
  answered: number
  accepted: number
  accepted_with_comment: number
  corrected: number
  wrong_turn: number
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

export type ChatRole = 'user' | 'assistant' | 'tool' | 'quiz_answer' | 'permission'

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
  /** role="permission" only: a resolved permission dialog from the daemon's log. */
  permission?: PermissionEcho
}

/**
 * A Claude Code permission dialog the daemon saw resolve. `answered_via` is
 * "terminal" when the dialog went away without an answer through the API.
 */
export interface PermissionEcho {
  title: string
  context?: string
  answer_label?: string
  answered_via: 'chat' | 'terminal'
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
  /**
   * "permission" when the daemon read a TUI permission dialog off the pane;
   * absent for a regular AskUserQuestion quiz.
   */
  source?: string
  /** Permission dialogs only: the bottom pane lines, shown when options are empty. */
  raw?: string
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
