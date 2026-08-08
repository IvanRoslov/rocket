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
  /**
   * The session's currently pending AskUserQuestion quiz (internal/api's
   * internal_quiz.go / quiz.go, docs/13-chat.md «Квизы»), omitted when
   * there is none. Present on `GET /v1/sessions`, `GET /v1/sessions/{id}`
   * and the `session` ref of `GET /v1/sessions/{id}/chat` alike.
   */
  pending_quiz?: PendingQuiz
}

// ---------------------------------------------------------------------------
// Quizzes (AskUserQuestion) — internal/api/quiz.go, internal/api/
// internal_quiz.go, docs/13-chat.md «Квизы (AskUserQuestion)».
// ---------------------------------------------------------------------------

/** Public-API (snake_case) shape of a pending quiz option — `quizOptionResponse`. */
export interface PendingQuizOption {
  label: string
  description?: string
}

/** Public-API (snake_case) shape of a pending quiz question — `quizQuestionResponse`. */
export interface PendingQuizQuestion {
  question: string
  header: string
  multi_select: boolean
  options: PendingQuizOption[]
}

/**
 * `pending_quiz` — the session's live, unanswered AskUserQuestion quiz
 * (`quizResponse` in internal/api/quiz.go). Present only while the quiz is
 * showing in the agent's terminal; disappears (via `session.quiz_resolved`)
 * once it's answered, cancelled, or its 60s answer-injection times out.
 */
export interface PendingQuiz {
  questions: PendingQuizQuestion[]
  asked_at: number
}

/**
 * Raw shape of a chat entry's `quiz` field on the ASKING side: `role:"tool"`,
 * `tool_name:"AskUserQuestion"` entries carry this verbatim from the tool's
 * input (camelCase `multiSelect` — Claude Code's own shape, NOT the
 * snake_case `PendingQuiz` above). See internal/agent/claudecode/chat.go.
 */
export interface QuizToolInputOption {
  label: string
  description?: string
}

export interface QuizToolInputQuestion {
  question: string
  header: string
  multiSelect: boolean
  options: QuizToolInputOption[]
}

export interface QuizToolInput {
  questions: QuizToolInputQuestion[]
}

/**
 * Raw shape of a chat entry's `quiz` field on the ANSWERING side: `role:
 * "quiz_answer"` entries carry this echo of the closed round — `answers` is
 * keyed by question text, valued by the chosen label(s) (multi-select
 * values are a joined string with an unstable delimiter across CLI
 * versions — display as-is, never split). Absent (only `text: "квиз
 * отменён"`) when the round was cancelled instead of answered.
 */
export interface QuizAnswerEcho {
  questions: { question: string }[]
  answers: Record<string, string>
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
// Chat — internal/api/chat.go, docs/13-chat.md
// ---------------------------------------------------------------------------

export type ChatRole = 'user' | 'assistant' | 'tool' | 'quiz_answer'

/**
 * One entry in a session's chat feed (`GET /v1/sessions/{id}/chat`) — a
 * mirror of the agent's native transcript, not a stored entity. `tool_name`
 * is present only for `role: "tool"`; `text` for tool entries is a
 * caller-truncated (<=120 rune) single-line digest of the tool's *input*,
 * not its output. `ts` is unix-seconds, `0` when the transcript line had no
 * timestamp — clients should hide the time in that case rather than render
 * "1970".
 *
 * `quiz` (omitempty) is present only on the two entry shapes that make up a
 * closed AskUserQuestion round (docs/13-chat.md «Квиз-раунды в ленте»): the
 * asking `role:"tool"`/`tool_name:"AskUserQuestion"` entry (raw tool input,
 * `QuizToolInput`) and the closing `role:"quiz_answer"` entry (raw answers
 * echo, `QuizAnswerEcho` — absent for a cancelled round, where `text` is
 * the literal "квиз отменён").
 */
export interface ChatEntry {
  role: ChatRole
  text: string
  tool_name?: string
  ts: number
  quiz?: QuizToolInput | QuizAnswerEcho
}

/**
 * Minimal session card embedded in the chat response (`sessionRef` in
 * internal/api/chat.go) so the chat screen doesn't need a separate
 * `GET /v1/sessions/{id}` just for its header state/activity dot.
 */
export interface ChatSessionRef {
  id: string
  kind: string
  state: SessionState
  activity?: SessionActivity
  /** Mirrors `Session.pending_quiz` — see its doc comment. */
  pending_quiz?: PendingQuiz
}

export interface ChatResponse {
  entries: ChatEntry[]
  /** Opaque — store and echo back verbatim as `cursor` on the next incremental request. */
  next_cursor: string
  session: ChatSessionRef
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

export type TaskStatus = 'backlog' | 'brainstorm' | 'in_progress' | 'review' | 'done' | 'cancelled'

export type TaskCreatedBy = 'user' | 'orchestrator' | 'agent'

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
  /** Open-question annotations (list/board/detail all carry them). */
  open_questions: number
  questions_awaiting_user: number
  /**
   * Derived, never persisted: the task's session has been sitting on
   * interactive input (a pending quiz, or activity "waiting_input") longer
   * than the daemon's threshold — nothing moves until somebody types.
   */
  waiting_terminal?: boolean
  /**
   * Milestone (task #1023, spec v2): a root task outside every project
   * (`project_id` is empty), taken by a persistent agent instead of started
   * as a feature. `assigned_role` is that agent's id, absent until somebody
   * takes it.
   */
  milestone?: boolean
  assigned_role?: string
  /**
   * The holding agent has shown no work — no journal entry, doc or thread
   * message — for longer than the daemon's `milestone_quiet_after`. Written
   * by the daemon (subtask #1032); treat its absence as "not quiet".
   */
  quiet?: boolean
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
/**
 * `fyi` is not a way a thread was answered but the kind of thread it always
 * was: a status note, created resolved, waiting on nobody (task #1023 spec v1
 * §«Тип треда»).
 */
export type QuestionResolution = 'answered' | 'dismissed' | 'fyi'
/** `questions.type` — internal/api/threads.go `normalizeThreadType`. */
export type ThreadType = 'decision' | 'fyi'
export type QuestionWhoseTurn = 'user' | 'orchestrator' | ''

/** `questionResponse` — internal/api/questions.go. */
export interface Question {
  id: number
  task_id: number
  /** 1-based, displayed as "Q<ordinal>". */
  ordinal: number
  /** Session id of the orchestrator that asked the question. */
  asked_by: string
  /**
   * One-line heading, derived from the body when the author sent none.
   * Optional on the wire only for a daemon older than task #1264 — read it
   * through `questionTitle()`, which falls back to the body.
   */
  title?: string
  body: string
  /**
   * @deprecated Task #1264 folded the context into the body. The API still
   * sends the field for older clients, but it is always empty.
   */
  context?: string
  status: QuestionStatus
  resolution?: QuestionResolution
  /** Everyone taking part in the thread: "human", agent ids or session ids. */
  participants?: string[]
  /** The subset of `participants` expected to speak next. */
  waiting_on?: string[]
  /** Caller-relative: true when the dashboard user is in `waiting_on`. */
  your_turn?: boolean
  /** Legacy two-party turn field, derived from `waiting_on` by the API. */
  whose_turn?: QuestionWhoseTurn
  /**
   * The one thread id a human sees: "1023/Q2" for a task thread, "cto/Q1" for
   * a role thread. Absent on a daemon older than task #1023 — fall back to
   * `Q${ordinal}`, never to the global numeric id.
   */
  local_ref?: string
  /** `decision` (the default) or `fyi`; absent on a pre-#1023 daemon. */
  type?: ThreadType
  /**
   * Answer choices. Picking one closes the thread with that text — the API
   * takes `choose` as a **1-based** index into this array.
   */
  options?: string[]
  /**
   * An open decision thread nobody has moved for longer than
   * `question_stale_after`. Derived by the daemon because the threshold is
   * configurable — never recompute it from `asked_at` on the client.
   */
  stale?: boolean
  /** The stored "whose turn" set; `waiting_on` is the same thing, older name. */
  attention?: string[]
  asked_at: number
  resolved_at?: number
  messages: QuestionMessage[]
}

/**
 * `globalQuestionResponse` — GET /v1/questions entry: a question plus the
 * task/project context the global Questions page needs to label and link it.
 */
export interface GlobalQuestion extends Question {
  task_title: string
  project_id: string
  project_name: string
  orchestrator_name?: string
}

/**
 * One row of `GET /v1/threads` — `threadInboxEntry` in
 * internal/api/thread_inbox.go. The unified inbox of task AND role threads:
 * what is open and on whom, in one listing, without walking every task.
 *
 * It deliberately carries the question body only, never the conversation: a
 * row that is expanded fetches the full thread from its per-subject endpoint.
 */
export interface ThreadInboxEntry {
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
  body: string
  status: QuestionStatus
  resolution?: QuestionResolution
  type: ThreadType
  options?: string[]
  participants: string[]
  /** The stored "whose turn" set; `waiting_on` is the same array, older name. */
  attention: string[]
  waiting_on: string[]
  /** Caller-relative: true when the dashboard user is in `attention`. */
  your_turn: boolean
  asked_at: number
  /** Last movement — the last entry, or the question itself when nobody replied. */
  updated_at: number
  resolved_at?: number
  /** Open decision thread idle longer than `question_stale_after` — daemon-derived. */
  stale?: boolean
  /** Task threads only: the context a dashboard row needs to link and label itself. */
  project_id?: string
  task_title?: string
}

export type QuestionMessageKind = 'reply' | 'answer'

/**
 * A single entry in a question's thread. `author` is the participant id that
 * posted it: "" (omitted) today for the human and "human" once subtask #736
 * flips the wire — use `isHuman()` from lib/participants.ts, never a literal
 * comparison. There is no `dismiss` message kind; dismissing a question
 * resolves it without adding a thread message.
 */
export interface QuestionMessage {
  id: number
  author?: string
  kind: QuestionMessageKind
  /**
   * Who must RESPOND to this entry — never who gets notified; every
   * participant but the author is notified regardless. Absent or empty means
   * "everyone except the author".
   */
  addressed_to?: string[]
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
 * `issueResponse` — internal/api/github_issues.go. `GET /v1/github/issues`
 * wraps these as `{"issues":[...]}`. `labels` is flattened to label names
 * (the daemon does the same on the Go side).
 */
export interface GithubIssue {
  number: number
  title: string
  body: string
  html_url: string
  state: string
  updated_at: string
  labels: string[]
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

// ---------------------------------------------------------------------------
// Agents — internal/api/agents.go, internal/api/agent_questions.go
// (docs/10-agents.md «Постоянные агенты»)
// ---------------------------------------------------------------------------

/** A registered agent. Everything past the stored columns is derived by the
 * daemon: `session_alive` (is the tmux session named `<id>` up), `unread` and
 * the Q&A counters. `dir`/`command` are the optional launcher pair — without a
 * `dir` the agent can only be started by hand (`tmux new -s <id>`). */
export interface Agent {
  id: string
  description: string
  project: string
  dir: string
  command: string
  enabled: boolean
  session_alive: boolean
  unread: number
  open_questions: number
  awaiting_user: number
  /** The milestones this agent holds — the one place a human sees what it took on. */
  milestones?: AgentMilestoneRef[]
  created_at: number
  updated_at: number
}

/** A milestone as listed on its holder's agent card (`GET /v1/agents/{id}`). */
export interface AgentMilestoneRef {
  id: number
  title: string
  status: TaskStatus
}

/** One inbox message. The inbox is what an agent gets when its session is
 * down; it reads them back one by one (`rocket inbox next`), which is what
 * flips `status` from `unread` to `read`. */
export interface AgentInboxMessage {
  id: number
  from: string
  body: string
  status: 'unread' | 'read'
  created_at: number
  read_at?: number
}

/** `POST /v1/agents/{id}/messages` -> 202. `live` says which path the message
 * took: `queued` into the running session, or `inbox` for later. */
export interface AgentDelivery {
  id: number
  to: string
  status: 'queued' | 'inbox'
  live: boolean
}

/** A role Q&A thread. Mirrors `Question` with `role_id` in place of the task
 * and `whose_turn: 'user' | 'role'`; thread entries share `QuestionMessage`. */
export interface AgentQuestion {
  id: number
  role_id: string
  ordinal: number
  asked_by: string
  /** One-line heading; absent on a daemon older than task #1264. */
  title?: string
  body: string
  /**
   * @deprecated Task #1264 folded the context into the body; always empty.
   */
  context?: string
  status: 'open' | 'resolved'
  resolution?: string
  /** Everyone taking part in the thread: "human", agent ids or session ids. */
  participants?: string[]
  /** The subset of `participants` expected to speak next. */
  waiting_on?: string[]
  /** Caller-relative: true when the dashboard user is in `waiting_on`. */
  your_turn?: boolean
  /** Legacy two-party turn field, derived from `waiting_on` by the API. */
  whose_turn?: 'user' | 'role'
  /** The one thread id a human sees — "cto/Q1" for a role thread. See `Question.local_ref`. */
  local_ref?: string
  /** `decision` (the default) or `fyi`; absent on a pre-#1023 daemon. */
  type?: ThreadType
  /** Answer choices; `choose` is a **1-based** index into this array. */
  options?: string[]
  /** Open decision thread idle longer than `question_stale_after` — daemon-derived. */
  stale?: boolean
  /** The stored "whose turn" set; `waiting_on` is the same thing, older name. */
  attention?: string[]
  asked_at: number
  resolved_at?: number
  messages: QuestionMessage[]
}

