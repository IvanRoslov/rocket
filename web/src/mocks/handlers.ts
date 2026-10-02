// msw request handlers backed by the fixtures in ./fixtures.ts. Mirrors the
// daemon's response envelopes exactly: bare arrays for
// projects/sessions/repos, `{tasks:[]}`/`{docs:[]}`/`{log:[]}`/`{questions:[]}`
// wrappers for tasks/docs/log/questions, and `{error:{code,message}}` on
// failure. Verified against .superpowers/sdd/phase3-contract.md and
// internal/api/tasks.go / questions.go.

import { http, HttpResponse } from 'msw'
import type { Device, PairingCode } from '../lib/auth'
import { isHuman } from '../lib/participants'
import { slugify } from '../lib/slug'
import type {
  ModelProfile,
  Agent,
  AgentInboxMessage,
  BrainstormOutcome,
  BrainstormStorm,
  ChatEntry,
  PendingQuiz,
  Project,
  Question,
  QuestionMessage,
  Repo,
  Settings,
  Task,
  TaskDoc,
  TaskGate,
  TaskLogEntry,
  TaskStatus,
} from '../lib/types'
import {
  agentInbox,
  agentQuestions,
  agents,
  brainstormStats,
  chatEntries,
  githubIssues,
  githubRepos,
  messages,
  projects,
  questions,
  repos,
  sessions,
  modelCatalog,
  modelProfiles,
  settings,
  stormDocs,
  stormGates,
  stormQuestions,
  subtasks,
  systemInfo,
  taskDocs,
  milestones,
  taskLog,
  tasks,
} from './fixtures'

// Mutable copy of the settings fixture, written by `PUT /v1/settings`. Tests
// that mutate this (e.g. "connect GitHub") should call `resetSettings()` in
// `afterEach` to avoid leaking state into later tests in the same file.
let settingsState: Settings = { ...settings }

export function resetSettings(): void {
  settingsState = { ...settings }
}

// Mutable copy of the model-profile registry (task #5026), written by
// POST/PATCH/DELETE /v1/model-profiles. Mutating tests call
// `resetModelProfiles()` in `afterEach`.
let profilesState: ModelProfile[] = modelProfiles.map((p) => ({ ...p }))

export function resetModelProfiles(): void {
  profilesState = modelProfiles.map((p) => ({ ...p }))
}

// Mirrors Agent.Efforts() (internal/agent/claudecode, internal/agent/codex).
const AGENT_EFFORTS: Record<string, string[]> = {
  'claude-code': ['low', 'medium', 'high', 'xhigh', 'max'],
  codex: ['minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'],
}

function profileError(status: number, code: string, message: string) {
  return HttpResponse.json({ error: { code, message } }, { status })
}

// validateProfileAgent (internal/api/model_profiles.go).
function badProfileAgent(p: ModelProfile) {
  const efforts = AGENT_EFFORTS[p.agent]
  if (!efforts) return profileError(400, 'agent_unavailable', `unknown agent ${p.agent}`)
  if (p.effort && !efforts.includes(p.effort)) {
    return profileError(400, 'bad_effort', `effort ${p.effort} is not one of ${p.agent}'s: ${efforts.join(', ')}`)
  }
  return null
}

function settingsBody() {
  return {
    github_token: maskToken(settingsState.github_token),
    orchestrator_brainstorm_custom: settingsState.orchestrator_brainstorm_custom ?? false,
    default_orchestrator_profile: settingsState.default_orchestrator_profile ?? '',
    default_worker_profile: settingsState.default_worker_profile ?? '',
  }
}

// Mutable copies of the repos/projects fixtures, written by
// `PATCH`/`DELETE /v1/repos/{id}` and `/v1/projects/{id}` (Settings screen).
// Tests that mutate these should call `resetRepos()`/`resetProjects()` in
// `afterEach` to avoid leaking state into later tests in the same file.
let reposState: Repo[] = repos.map((r) => ({ ...r, env: { ...r.env }, symlinks: [...r.symlinks], post_create: [...r.post_create] }))
let projectsState: Project[] = projects.map((p) => ({ ...p, linked: [...p.linked] }))

export function resetRepos(): void {
  reposState = repos.map((r) => ({ ...r, env: { ...r.env }, symlinks: [...r.symlinks], post_create: [...r.post_create] }))
}

export function resetProjects(): void {
  projectsState = projects.map((p) => ({ ...p, linked: [...p.linked] }))
}

// Mutable copy of sessions, written by `POST /v1/sessions/{id}/restore`
// (errored -> running). Tests that mutate this should call `resetSessions()`
// in `afterEach`.
let sessionsState = sessions.map((s) => ({ ...s }))

export function resetSessions(): void {
  sessionsState = sessions.map((s) => ({ ...s }))
}

// Mutable copies of the agent fixtures, written by POST/PATCH/DELETE
// /v1/agents, enable/disable, start/stop and messages. Tests that mutate
// these should call `resetAgents()` in `afterEach`.
let agentsState: Agent[] = agents.map((a) => ({ ...a }))
let agentInboxState: AgentInboxMessage[] = agentInbox.map((m) => ({ ...m }))

/** Overrides a session's `pending_quiz` (undefined clears it) — reset via `resetSessions()`. */
export function setSessionPendingQuiz(id: string, quiz: PendingQuiz | undefined): void {
  sessionsState = sessionsState.map((s) => (s.id === id ? { ...s, pending_quiz: quiz } : s))
}

export function resetAgents(): void {
  agentsState = agents.map((a) => ({ ...a }))
  agentInboxState = agentInbox.map((m) => ({ ...m }))
}

/** The agent most recently added by `POST /v1/agents` — how tests assert on
 *  the registration payload (the project in particular) without intercepting
 *  the request themselves. */
export function lastCreatedAgent(): Agent | undefined {
  return agentsState[agentsState.length - 1]
}

/**
 * Spy for `POST /v1/sessions/:id/quiz/answer` bodies — tests assert against
 * this after triggering a submit rather than intercepting fetch directly.
 */
export let lastQuizAnswerBody: unknown = undefined

export function resetQuizAnswerSpy(): void {
  lastQuizAnswerBody = undefined
}

// Mutable copy of per-session chat transcripts (internal/api/chat.go). Tests
// simulating new agent/transcript activity should call `appendChatEntry` and
// reset via `resetChatEntries()` in `afterEach`.
let chatEntriesState: Record<string, ChatEntry[]> = Object.fromEntries(
  Object.entries(chatEntries).map(([id, entries]) => [id, entries.map((e) => ({ ...e }))]),
)

export function resetChatEntries(): void {
  chatEntriesState = Object.fromEntries(
    Object.entries(chatEntries).map(([id, entries]) => [id, entries.map((e) => ({ ...e }))]),
  )
}

export function appendChatEntry(sessionId: string, entry: ChatEntry): void {
  chatEntriesState[sessionId] = [...(chatEntriesState[sessionId] ?? []), entry]
}

// Mutable copy of tasks + subtasks, written by task create/status/cancel
// mutations. `nextTaskId` seeds past the highest fixture id.
let tasksState: Task[] = [...tasks, ...subtasks, ...milestones].map((t) => ({ ...t }))
let nextTaskId = Math.max(...tasksState.map((t) => t.id)) + 1

// Storm gates (task #4901). A Go moves the task, so `resetTasks()` resets them too.
let gatesState: TaskGate[] = stormGates.map((g) => ({ ...g }))

export function resetTasks(): void {
  tasksState = [...tasks, ...subtasks, ...milestones].map((t) => ({ ...t }))
  nextTaskId = Math.max(...tasksState.map((t) => t.id)) + 1
  gatesState = stormGates.map((g) => ({ ...g }))
}

const allQuestions = (): Question[] =>
  [...questions, ...stormQuestions].map((q) => ({ ...q, messages: q.messages.map((m) => ({ ...m })) }))
let questionsState: Question[] = allQuestions()
let nextQuestionId = Math.max(...questionsState.map((q) => q.id)) + 1

export function resetQuestions(): void {
  questionsState = allQuestions()
  nextQuestionId = Math.max(...questionsState.map((q) => q.id)) + 1
}

let docsState: TaskDoc[] = [...taskDocs, ...stormDocs].map((d) => ({ ...d }))
let nextDocId = Math.max(...docsState.map((d) => d.id)) + 1

export function resetDocs(): void {
  docsState = [...taskDocs, ...stormDocs].map((d) => ({ ...d }))
  nextDocId = Math.max(...docsState.map((d) => d.id)) + 1
}

/** Mirrors the outcome rule of store.ResolveBrainstormQuestion. */
function stormOutcome(q: Question, chosen: number | null): BrainstormOutcome {
  if (chosen === null) return 'wrong_turn'
  return chosen === q.recommended_option ? 'accepted' : 'corrected'
}

/** The fields every thread carries for a storm answer (brainstormWire). */
function brainstormWire(q: Question) {
  return {
    recommended_option: q.recommended_option ?? null,
    chosen_option: q.chosen_option ?? null,
    answer_comment: q.answer_comment ?? '',
    answer_source: q.answer_source ?? '',
    outcome: q.outcome ?? '',
    outcome_overridden: q.outcome_overridden ?? false,
    answered_by: q.answered_by ?? '',
  }
}

/** An empty storm row — what the daemon reports for a task with no storm yet. */
function emptyStorm(taskId: number): BrainstormStorm {
  const task = tasksState.find((t) => t.id === taskId)
  return {
    task_id: taskId,
    title: task?.title ?? '',
    project_id: task?.project_id ?? '',
    skill: task?.brainstorm_skill || 'unknown',
    questions: 0,
    answered: 0,
    accepted: 0,
    accepted_with_comment: 0,
    corrected: 0,
    wrong_turn: 0,
    answered_by: [],
    by_answerer: [],
    spec_changes: 0,
    first_try_go: false,
    has_gate: false,
    go_at: null,
  }
}

let logState: TaskLogEntry[] = taskLog.map((l) => ({ ...l }))
let nextLogId = Math.max(...logState.map((l) => l.id)) + 1

export function resetLog(): void {
  logState = taskLog.map((l) => ({ ...l }))
  nextLogId = Math.max(...logState.map((l) => l.id)) + 1
}

const TASK_STATUSES: TaskStatus[] = ['backlog', 'brainstorm', 'in_progress', 'review', 'done', 'cancelled']

function nowSeconds(): number {
  return Math.floor(Date.now() / 1000)
}

// Mirrors maskToken (internal/api/settings.go): >8 chars -> first4…last4;
// non-empty but <=8 chars -> "set"; empty -> "".
function maskToken(token: string): string {
  if (token === '') return ''
  if (token.length > 8) return `${token.slice(0, 4)}…${token.slice(-4)}`
  return 'set'
}

/**
 * Mirrors agentMilestones() in internal/api/milestones.go: an agent card
 * carries the milestones it holds, derived from the tasks — so an assign
 * made through the API shows up on the agent immediately.
 */
function withMilestones(agent: Agent): Agent {
  return {
    ...agent,
    milestones: tasksState
      .filter((t) => t.milestone && t.assigned_role === agent.id)
      .map((t) => ({ id: t.id, title: t.title, status: t.status })),
  }
}

function openQuestionsFor(taskId: number): number {
  return questionsState.filter((q) => q.task_id === taskId && q.status === 'open').length
}

function taskDetailFor(task: Task) {
  const taskSubtasks = tasksState.filter((t) => t.parent_id === task.id)
  const session = task.session_id ? sessionsState.find((s) => s.id === task.session_id) : undefined
  return {
    ...task,
    subtasks: taskSubtasks,
    session: session
      ? { id: session.id, tmux_name: session.tmux_name, attach: ['rocket', 'attach', session.id] }
      : undefined,
    open_questions: openQuestionsFor(task.id),
  }
}

// Mirrors waitingOn() in internal/api/threads.go for the fixture server: an
// explicit `to` decides who must respond, otherwise it is every participant
// except the author. `your_turn` is that set seen from the dashboard user.
function applyTurn(question: Question, author: string, to?: string[]) {
  const participants = question.participants ?? []
  const isAuthor = (p: string) => (isHuman(author) ? isHuman(p) : p === author)
  question.waiting_on = to ?? participants.filter((p) => !isAuthor(p))
  question.your_turn = (question.waiting_on ?? []).some((p) => isHuman(p))
  question.whose_turn = question.your_turn ? 'user' : 'orchestrator'
}

const devicesFixture = (): Device[] => [
  { id: 1, name: 'Chrome · macOS', kind: 'web', current: true, created_at: 1759000000, last_seen_at: null },
  { id: 2, name: 'iPhone', kind: 'mobile', current: false, created_at: 1759000100, last_seen_at: 1759000200 },
]
let devicesState: Device[] = devicesFixture()

export function resetDevices(): void {
  devicesState = devicesFixture()
}

export const handlers = [
  http.get('/v1/auth/status', () =>
    HttpResponse.json({ authenticated: true, device: devicesState.find((d) => d.current) ?? devicesFixture()[0] }),
  ),
  http.get('/v1/auth/devices', () => HttpResponse.json(devicesState)),
  http.delete('/v1/auth/devices/:id', ({ params }) => {
    devicesState = devicesState.filter((d) => d.id !== Number(params.id))
    return new HttpResponse(null, { status: 204 })
  }),
  http.post('/v1/auth/pairing-codes', () => {
    const code: PairingCode = {
      code: 'AB12-CD34',
      expires_at: Math.floor(Date.now() / 1000) + 600,
      url: 'https://mac.tail1.ts.net',
    }
    return HttpResponse.json(code)
  }),
  http.post('/v1/auth/logout', () => new HttpResponse(null, { status: 204 })),

  http.get('/v1/projects', () => HttpResponse.json(projectsState)),

  // Mirrors internal/api/sessions.go handleListSessions + store.ListSessions:
  // without `all=true`, only live sessions (state spawning/running) are
  // returned — a `done` worker (e.g. after its PR merges and the daemon
  // auto-cleans it) is only visible with `all=true`.
  http.get('/v1/sessions', ({ request }) => {
    const url = new URL(request.url)
    const project = url.searchParams.get('project')
    const kind = url.searchParams.get('kind')
    const all = url.searchParams.get('all') === 'true'
    let result = [...sessionsState]
    if (project) result = result.filter((s) => s.project_id === project)
    if (kind) result = result.filter((s) => s.kind === kind)
    if (!all) result = result.filter((s) => s.state === 'spawning' || s.state === 'running')
    return HttpResponse.json(result)
  }),

  http.get('/v1/sessions/:id', ({ params }) => {
    const id = params.id as string
    const session = sessionsState.find((s) => s.id === id)
    if (!session) {
      return HttpResponse.json(
        { error: { code: 'not_found', message: `session ${id} not found` } },
        { status: 404 },
      )
    }
    return HttpResponse.json(session)
  }),

  // GET /v1/sessions/:id/chat — internal/api/chat.go, docs/13-chat.md.
  // Fixture cursor semantics: an opaque decimal offset into the session's
  // fixture transcript array. cursor="" is tail semantics (last `limit`
  // entries); cursor="<N>" returns everything at/after index N, uncapped —
  // matching the real handler's "incremental reads aren't sliced" contract.
  http.get('/v1/sessions/:id/chat', ({ params, request }) => {
    const id = params.id as string
    const session = sessionsState.find((s) => s.id === id)
    if (!session) {
      return HttpResponse.json(
        { error: { code: 'session_not_found', message: `session ${id} not found` } },
        { status: 404 },
      )
    }
    const url = new URL(request.url)
    const cursorParam = url.searchParams.get('cursor') ?? ''
    const limitParam = url.searchParams.get('limit')
    const limit = limitParam ? Number(limitParam) : 200
    const all = chatEntriesState[id] ?? []

    let start: number
    let entries: ChatEntry[]
    if (cursorParam) {
      start = Number(cursorParam) || 0
      entries = all.slice(start)
    } else {
      start = Math.max(0, all.length - limit)
      entries = all.slice(start)
    }

    return HttpResponse.json({
      entries,
      next_cursor: all.length > 0 ? String(start + entries.length) : '',
      session: {
        id: session.id,
        kind: session.kind,
        state: session.state,
        activity: session.activity,
        pending_quiz: session.pending_quiz,
      },
    })
  }),

  // POST /v1/sessions/:id/quiz/answer — internal/api/quiz.go, docs/13-chat.md
  // «Квизы (AskUserQuestion)»: records the posted body (see
  // `lastQuizAnswerBody`/`resetQuizAnswerSpy`) and, against the session's
  // `pending_quiz` fixture, returns 404/409 no_pending_quiz/400
  // quiz_answer_invalid/202 exactly like the real handler's contract.
  // `quiz_answer_in_flight` has no fixture-driven trigger — tests needing
  // it should `server.use()` an override.
  http.post('/v1/sessions/:id/quiz/answer', async ({ params, request }) => {
    const id = params.id as string
    const session = sessionsState.find((s) => s.id === id)
    if (!session) {
      return HttpResponse.json(
        { error: { code: 'session_not_found', message: `session ${id} not found` } },
        { status: 404 },
      )
    }
    const body = (await request.json()) as {
      answers?: { question_index: number; option_indices?: number[]; text?: string }[]
    }
    lastQuizAnswerBody = body

    if (!session.pending_quiz) {
      return HttpResponse.json(
        { error: { code: 'no_pending_quiz', message: 'session has no pending quiz' } },
        { status: 409 },
      )
    }
    const quiz = session.pending_quiz
    const answers = body.answers ?? []
    if (quiz.source === 'permission') {
      // Permission prompt (task #4881): exactly one option index (or -1 =
      // Esc) for question 0; free text has no meaning for a TUI dialog.
      const a = answers[0]
      const idx = a?.option_indices
      const optionCount = quiz.questions[0]?.options.length ?? 0
      if (
        answers.length !== 1 ||
        a.question_index !== 0 ||
        a.text !== undefined ||
        idx?.length !== 1 ||
        idx[0] < -1 ||
        idx[0] >= optionCount
      ) {
        return HttpResponse.json(
          { error: { code: 'invalid_answer', message: 'permission answer needs exactly one option_index' } },
          { status: 400 },
        )
      }
      return HttpResponse.json({ status: 'answering' }, { status: 202 })
    }
    if (answers.length !== quiz.questions.length) {
      return HttpResponse.json(
        { error: { code: 'quiz_answer_invalid', message: 'all questions must be answered' } },
        { status: 400 },
      )
    }
    for (const a of answers) {
      const q = quiz.questions[a.question_index]
      if (!q) {
        return HttpResponse.json(
          { error: { code: 'quiz_answer_invalid', message: `question_index ${a.question_index} out of range` } },
          { status: 400 },
        )
      }
      const hasOptions = a.option_indices !== undefined
      const hasText = a.text !== undefined && a.text !== ''
      if (hasOptions === hasText) {
        return HttpResponse.json(
          { error: { code: 'quiz_answer_invalid', message: 'exactly one of option_indices/text required' } },
          { status: 400 },
        )
      }
      if (hasOptions && !q.multi_select && a.option_indices?.length !== 1) {
        return HttpResponse.json(
          { error: { code: 'quiz_answer_invalid', message: 'single-select requires exactly one option_index' } },
          { status: 400 },
        )
      }
    }
    return HttpResponse.json({ status: 'answering' }, { status: 202 })
  }),

  http.get('/v1/repos', () => HttpResponse.json(reposState)),

  http.get('/v1/system', () => HttpResponse.json(systemInfo)),

  http.post('/v1/system/cleanup', () =>
    HttpResponse.json({
      killed_tmux: systemInfo.tmux.filter((t) => t.orphan).map((t) => t.name),
      removed_worktrees: systemInfo.worktrees.filter((w) => w.orphan).map((w) => w.path),
    }),
  ),

  http.post('/v1/sessions/:id/kill', ({ params }) => {
    const id = params.id as string
    const session = sessionsState.find((s) => s.id === id)
    if (!session) {
      return HttpResponse.json(
        { error: { code: 'not_found', message: `session ${id} not found` } },
        { status: 404 },
      )
    }
    return HttpResponse.json({ status: 'killed' })
  }),

  http.post('/v1/sessions/:id/restore', ({ params }) => {
    const id = params.id as string
    const session = sessionsState.find((s) => s.id === id)
    if (!session) {
      return HttpResponse.json(
        { error: { code: 'not_found', message: `session ${id} not found` } },
        { status: 404 },
      )
    }
    session.state = 'running'
    session.activity = 'ready'
    return HttpResponse.json(session)
  }),

  http.get('/v1/messages', ({ request }) => {
    const url = new URL(request.url)
    const session = url.searchParams.get('session')
    if (!session) {
      return HttpResponse.json(
        { error: { code: 'bad_request', message: 'session parameter required' } },
        { status: 400 },
      )
    }
    const result = messages.filter((m) => m.to === session || m.from === session)
    return HttpResponse.json({ messages: result })
  }),

  // --------------------------------------------------------------------
  // Tasks — internal/api/tasks.go.
  // --------------------------------------------------------------------

  http.get('/v1/tasks', ({ request }) => {
    const url = new URL(request.url)
    const project = url.searchParams.get('project')
    const status = url.searchParams.get('status')
    const parent = url.searchParams.get('parent')
    const board = url.searchParams.get('board') === 'true'
    // `milestones=true` narrows to milestones; without it the daemon applies
    // no milestone condition at all (internal/store/tasks.go ListTasks).
    const milestonesOnly = url.searchParams.get('milestones') === 'true'

    let result = project ? tasksState.filter((t) => t.project_id === project) : tasksState
    if (milestonesOnly) result = result.filter((t) => t.milestone === true)
    if (status) result = result.filter((t) => t.status === status)

    if (board) {
      // Board is root-only by default, same as the list endpoint.
      const rootOnly = result.filter((t) => t.parent_id === undefined)
      const b: Record<TaskStatus, Task[]> = {
        backlog: [],
        brainstorm: [],
        in_progress: [],
        review: [],
        done: [],
        cancelled: [],
      }
      for (const task of rootOnly) {
        b[task.status].push(task)
      }
      return HttpResponse.json({ board: b })
    }

    if (parent === 'all') {
      // no-op: keep every task regardless of parent_id
    } else if (parent !== null) {
      const parentId = Number(parent)
      result = result.filter((t) => t.parent_id === parentId)
    } else {
      result = result.filter((t) => t.parent_id === undefined)
    }

    return HttpResponse.json({ tasks: result })
  }),

  http.post('/v1/tasks', async ({ request }) => {
    const body = (await request.json()) as {
      title?: string
      description?: string
      project?: string
      parent_id?: number
      milestone?: boolean
    }
    if (!body.title) {
      return HttpResponse.json({ error: { code: 'empty_title', message: 'title must not be empty' } }, { status: 400 })
    }
    if (body.project && !projectsState.some((p) => p.id === body.project)) {
      return HttpResponse.json(
        { error: { code: 'project_not_found', message: `project ${body.project} not found` } },
        { status: 400 },
      )
    }
    if (body.parent_id !== undefined) {
      const parent = tasksState.find((t) => t.id === body.parent_id)
      if (!parent) {
        return HttpResponse.json({ error: { code: 'task_not_found', message: `task ${body.parent_id} not found` } }, { status: 404 })
      }
      if (parent.parent_id !== undefined) {
        return HttpResponse.json(
          { error: { code: 'nested_subtask', message: 'subtasks cannot themselves have subtasks' } },
          { status: 400 },
        )
      }
    }
    const now = nowSeconds()
    const task: Task = {
      id: nextTaskId++,
      parent_id: body.parent_id,
      title: body.title,
      description: body.description,
      project_id: body.project ?? '',
      status: 'backlog',
      ...(body.milestone ? { milestone: true } : {}),
      created_by: 'user',
      created_at: now,
      updated_at: now,
      open_questions: 0,
      questions_awaiting_user: 0,
    }
    tasksState.push(task)
    return HttpResponse.json(task, { status: 201 })
  }),

  http.get('/v1/tasks/:id', ({ params }) => {
    const id = Number(params.id)
    const task = tasksState.find((t) => t.id === id)
    if (!task) {
      return HttpResponse.json(
        { error: { code: 'not_found', message: `task ${id} not found` } },
        { status: 404 },
      )
    }
    return HttpResponse.json(taskDetailFor(task))
  }),

  http.patch('/v1/tasks/:id', async ({ params, request }) => {
    const id = Number(params.id)
    const task = tasksState.find((t) => t.id === id)
    if (!task) {
      return HttpResponse.json({ error: { code: 'not_found', message: `task ${id} not found` } }, { status: 404 })
    }
    const body = (await request.json()) as {
      status?: TaskStatus
      title?: string
      description?: string
      allowed_profiles?: string[]
    }
    if (body.allowed_profiles) {
      const unknown = body.allowed_profiles.filter((n) => !profilesState.some((p) => p.name === n))
      if (unknown.length > 0) {
        return HttpResponse.json(
          { error: { code: 'profile_not_found', message: `unknown profiles: ${unknown.join(', ')}` } },
          { status: 400 },
        )
      }
      task.allowed_profiles = body.allowed_profiles
    }
    if (body.status === 'cancelled') {
      return HttpResponse.json(
        { error: { code: 'use_cancel', message: 'use POST /v1/tasks/{id}/cancel to cancel a task' } },
        { status: 400 },
      )
    }
    if (body.status && !TASK_STATUSES.includes(body.status)) {
      return HttpResponse.json({ error: { code: 'invalid_status', message: `invalid status ${body.status}` } }, { status: 400 })
    }
    if (body.status) task.status = body.status
    if (body.title !== undefined) task.title = body.title
    if (body.description !== undefined) task.description = body.description
    task.updated_at = nowSeconds()
    return HttpResponse.json(task)
  }),

  http.post('/v1/tasks/:id/start', ({ params }) => {
    const id = Number(params.id)
    const task = tasksState.find((t) => t.id === id)
    if (!task) {
      return HttpResponse.json({ error: { code: 'not_found', message: `task ${id} not found` } }, { status: 404 })
    }
    if (task.parent_id !== undefined) {
      return HttpResponse.json({ error: { code: 'not_root_task', message: 'only root tasks can be started' } }, { status: 400 })
    }
    if (task.session_id && task.status === 'in_progress') {
      return HttpResponse.json(
        { error: { code: 'already_started', message: 'task already has a running session' } },
        { status: 409 },
      )
    }
    if (task.status !== 'backlog') {
      return HttpResponse.json(
        { error: { code: 'task_not_startable', message: `task is ${task.status}, not backlog` } },
        { status: 409 },
      )
    }
    const featureSlug = task.feature_slug ?? `task-${task.id}`
    const sessionId = `s-${featureSlug}-orch`
    task.status = 'in_progress'
    task.feature_slug = featureSlug
    task.session_id = sessionId
    task.updated_at = nowSeconds()
    return HttpResponse.json({ task_id: task.id, feature_slug: featureSlug, session_id: sessionId }, { status: 201 })
  }),

  http.post('/v1/tasks/:id/cancel', ({ params }) => {
    const id = Number(params.id)
    const task = tasksState.find((t) => t.id === id)
    if (!task) {
      return HttpResponse.json({ error: { code: 'not_found', message: `task ${id} not found` } }, { status: 404 })
    }
    task.status = 'cancelled'
    task.updated_at = nowSeconds()
    return HttpResponse.json(task)
  }),

  // internal/api/milestones.go: the human's half of milestone ownership.
  // `{none:true}` releases it, `{agent_id}` hands it to a registered agent.
  http.post('/v1/tasks/:id/assign', async ({ params, request }) => {
    const id = Number(params.id)
    const task = tasksState.find((t) => t.id === id)
    if (!task) {
      return HttpResponse.json({ error: { code: 'not_found', message: `task ${id} not found` } }, { status: 404 })
    }
    if (!task.milestone) {
      return HttpResponse.json(
        {
          error: {
            code: 'not_a_milestone',
            message: 'only milestones are taken by agents: use rocket task start for a project task',
          },
        },
        { status: 403 },
      )
    }
    const body = (await request.json()) as { agent_id?: string; none?: boolean }
    if (!!body.none === !!body.agent_id) {
      return HttpResponse.json(
        { error: { code: 'bad_request', message: 'pass exactly one of agent_id or none' } },
        { status: 400 },
      )
    }
    if (body.agent_id && !agentsState.some((a) => a.id === body.agent_id)) {
      return HttpResponse.json(
        { error: { code: 'agent_not_found', message: `agent not found: ${body.agent_id}` } },
        { status: 400 },
      )
    }
    task.assigned_role = body.none ? undefined : body.agent_id
    task.updated_at = nowSeconds()
    return HttpResponse.json(task)
  }),

  // Mirrors store.ListTaskDocs: only the newest version of each (kind, title)
  // unless ?history=true asks for every version.
  http.get('/v1/tasks/:id/docs', ({ params, request }) => {
    const id = Number(params.id)
    const history = new URL(request.url).searchParams.get('history') === 'true'
    const own = docsState.filter((d) => d.task_id === id)
    const docs = history
      ? own
      : own.filter(
          (d) => !own.some((o) => o.kind === d.kind && o.title === d.title && o.version > d.version),
        )
    return HttpResponse.json({ docs })
  }),

  http.put('/v1/tasks/:id/docs', async ({ params, request }) => {
    const id = Number(params.id)
    const body = (await request.json()) as { kind: TaskDoc['kind']; title: string; body: string }
    const priorVersions = docsState.filter((d) => d.task_id === id && d.kind === body.kind)
    const version = priorVersions.length > 0 ? Math.max(...priorVersions.map((d) => d.version)) + 1 : 1
    const doc: TaskDoc = {
      id: nextDocId++,
      task_id: id,
      kind: body.kind,
      title: body.title,
      body: body.body,
      version,
      created_at: nowSeconds(),
    }
    docsState.push(doc)
    return HttpResponse.json(doc)
  }),

  http.get('/v1/tasks/:id/log', ({ params, request }) => {
    const id = Number(params.id)
    const url = new URL(request.url)
    const kind = url.searchParams.get('kind')
    let result = logState.filter((l) => l.task_id === id)
    if (kind) result = result.filter((l) => l.kind === kind)
    return HttpResponse.json({ log: result })
  }),

  http.post('/v1/tasks/:id/log', async ({ params, request }) => {
    const id = Number(params.id)
    const body = (await request.json()) as { kind: TaskLogEntry['kind']; body: string }
    const entry: TaskLogEntry = {
      id: nextLogId++,
      task_id: id,
      kind: body.kind,
      body: body.body,
      created_at: nowSeconds(),
    }
    logState.push(entry)
    return HttpResponse.json(entry, { status: 201 })
  }),

  // Global open-questions list (internal/api/questions.go
  // handleGetAllQuestions): open only, enriched with task/project context.
  http.get('/v1/questions', () => {
    const open = questionsState.filter((q) => q.status === 'open')
    return HttpResponse.json({
      questions: open.map((q) => {
        const task = tasksState.find((t) => t.id === q.task_id)
        return {
          ...q,
          task_title: task?.title ?? '',
          project_id: task?.project_id ?? 'demo',
          project_name: 'Demo',
          orchestrator_name: 'demo-orch',
        }
      }),
    })
  }),

  // GET /v1/threads — internal/api/thread_inbox.go. The unified inbox of task
  // AND role threads. Derived from the same state the per-subject endpoints
  // serve, so a row and the thread it opens can never disagree.
  http.get('/v1/threads', ({ request }) => {
    const url = new URL(request.url)
    const all = url.searchParams.get('all') === 'true'
    const waitingOn = url.searchParams.get('waiting_on')

    const taskThreads = questionsState.map((q) => {
      const task = tasksState.find((t) => t.id === q.task_id)
      const attention = q.status === 'open' ? (q.waiting_on ?? []) : []
      return {
        local_ref: q.local_ref ?? `${q.task_id}/Q${q.ordinal}`,
        kind: 'task' as const,
        task_id: q.task_id,
        subject: `task #${q.task_id} "${task?.title ?? ''}"`,
        id: q.id,
        ordinal: q.ordinal,
        asked_by: q.asked_by,
        title: q.title ?? '',
        body: q.body,
        brief: q.brief ?? '',
        status: q.status,
        resolution: q.resolution,
        type: q.type ?? ('decision' as const),
        options: q.options,
        participants: q.participants ?? [],
        attention,
        waiting_on: attention,
        your_turn: attention.some((p) => isHuman(p)),
        asked_at: q.asked_at,
        updated_at: q.messages.at(-1)?.created_at ?? q.asked_at,
        resolved_at: q.resolved_at,
        stale: q.stale,
        project_id: task?.project_id,
        task_title: task?.title,
        ...brainstormWire(q),
      }
    })

    const roleThreads = agentQuestions.map((q) => {
      const attention = q.status === 'open' ? (q.waiting_on ?? []) : []
      return {
        local_ref: q.local_ref ?? `${q.role_id}/Q${q.ordinal}`,
        kind: 'role' as const,
        role_id: q.role_id,
        subject: `role ${q.role_id}`,
        id: q.id,
        ordinal: q.ordinal,
        asked_by: q.asked_by,
        title: q.title ?? '',
        body: q.body,
        brief: q.brief ?? '',
        status: q.status,
        resolution: q.resolution,
        type: q.type ?? ('decision' as const),
        options: q.options,
        participants: q.participants ?? [],
        attention,
        waiting_on: attention,
        your_turn: attention.some((p) => isHuman(p)),
        asked_at: q.asked_at,
        updated_at: q.messages.at(-1)?.created_at ?? q.asked_at,
        resolved_at: q.resolved_at,
        stale: q.stale,
      }
    })

    let threads = [...taskThreads, ...roleThreads]
    if (!all) threads = threads.filter((t) => t.status === 'open')
    if (waitingOn) threads = threads.filter((t) => t.attention.some((p) => p === waitingOn))
    return HttpResponse.json({ threads })
  }),

  http.get('/v1/tasks/:id/questions', ({ params, request }) => {
    const id = Number(params.id)
    const url = new URL(request.url)
    const status = url.searchParams.get('status')
    let result = questionsState.filter((q) => q.task_id === id)
    if (status) result = result.filter((q) => q.status === status)
    return HttpResponse.json({ questions: result })
  }),

  // Opens a user->orchestrator question thread (dashboard sends no
  // X-Rocket-Session, so asked_by is "" and whose_turn starts "orchestrator").
  http.post('/v1/tasks/:id/questions', async ({ params, request }) => {
    const taskId = Number(params.id)
    const body = (await request.json()) as { body: string; context?: string; to?: string[] }
    const ordinal = questionsState.filter((q) => q.task_id === taskId).length + 1
    const question: Question = {
      id: nextQuestionId++,
      task_id: taskId,
      ordinal,
      asked_by: '',
      body: body.body,
      // The human opened it: nobody wrote a brief, so it is empty on the wire.
      brief: '',
      context: body.context,
      status: 'open',
      participants: ['human', 's-billing-v2-orch'],
      waiting_on: [],
      your_turn: false,
      whose_turn: 'orchestrator',
      asked_at: nowSeconds(),
      messages: [],
    }
    applyTurn(question, '', body.to)
    questionsState.push(question)
    return HttpResponse.json(question, { status: 201 })
  }),

  // --------------------------------------------------------------------
  // Questions — internal/api/questions.go.
  // --------------------------------------------------------------------

  http.post('/v1/questions/:id/reply', async ({ params, request }) => {
    const id = Number(params.id)
    const question = questionsState.find((q) => q.id === id)
    if (!question) {
      return HttpResponse.json({ error: { code: 'not_found', message: `question ${id} not found` } }, { status: 404 })
    }
    if (question.status !== 'open') {
      return HttpResponse.json({ error: { code: 'question_resolved', message: 'question is already resolved' } }, { status: 409 })
    }
    const body = (await request.json()) as { body: string; to?: string[] }
    // Dashboard sends no X-Rocket-Session, so it acts as the user: author "".
    const message: QuestionMessage = { id: Date.now(), author: undefined, kind: 'reply', body: body.body, addressed_to: body.to, created_at: nowSeconds() }
    question.messages.push(message)
    applyTurn(question, '', body.to)
    return HttpResponse.json(question, { status: 201 })
  }),

  http.post('/v1/questions/:id/answer', async ({ params, request }) => {
    const id = Number(params.id)
    const question = questionsState.find((q) => q.id === id)
    if (!question) {
      return HttpResponse.json({ error: { code: 'not_found', message: `question ${id} not found` } }, { status: 404 })
    }
    if (question.status !== 'open') {
      return HttpResponse.json({ error: { code: 'question_resolved', message: 'question is already resolved' } }, { status: 409 })
    }
    const body = (await request.json()) as {
      body?: string
      dismiss?: boolean
      to?: string[]
      choose?: number
    }
    // `choose` is a 1-based index into `options`; the daemon substitutes the
    // option's own text as the answer (internal/api/threads.go
    // chooseOptionBody), so the mock does the same rather than echoing a body.
    // On a storm thread the human's own words are the comment; the answer
    // message is the option text followed by them (task #4901).
    const comment = body.body ?? ''
    if (body.choose) {
      const picked = (question.options ?? [])[body.choose - 1]
      if (picked === undefined) {
        return HttpResponse.json(
          { error: { code: 'bad_request', message: `choose must be between 1 and ${(question.options ?? []).length}` } },
          { status: 400 },
        )
      }
      body.body = question.type === 'brainstorm' && comment.trim() ? `${picked}\n\n${comment}` : picked
      delete body.to
    }
    if (question.type === 'brainstorm' && !body.dismiss) {
      const chosen = body.choose ? body.choose : null
      question.chosen_option = chosen
      question.answer_comment = comment
      question.answer_source = 'ui'
      question.outcome = stormOutcome(question, chosen)
      question.outcome_overridden = false
      question.answered_by = 'human'
    }
    if (body.dismiss) {
      // Dismissing resolves the question without adding a thread message.
      question.status = 'resolved'
      question.resolution = 'dismissed'
      question.waiting_on = []
      question.your_turn = false
      question.whose_turn = ''
      question.resolved_at = nowSeconds()
      return HttpResponse.json(question)
    }
    question.messages.push({ id: Date.now(), author: undefined, kind: 'answer', body: body.body ?? '', addressed_to: body.to, created_at: nowSeconds() })
    question.status = 'resolved'
    question.resolution = 'answered'
    // A resolved thread waits on nobody, whatever `to` said.
    question.waiting_on = []
    question.your_turn = false
    question.whose_turn = ''
    question.resolved_at = nowSeconds()
    return HttpResponse.json(question)
  }),

  // --------------------------------------------------------------------
  // Storm — internal/api/brainstorm_questions.go, gates.go, stats (task #4901).
  // --------------------------------------------------------------------

  http.patch('/v1/questions/:id/outcome', async ({ params, request }) => {
    const question = questionsState.find((q) => q.id === Number(params.id))
    if (!question) {
      return HttpResponse.json({ error: { code: 'not_found', message: 'question not found' } }, { status: 404 })
    }
    if (question.type !== 'brainstorm') {
      return HttpResponse.json({ error: { code: 'not_brainstorm', message: 'only a brainstorm thread has an outcome' } }, { status: 400 })
    }
    if (question.status !== 'resolved' || question.resolution !== 'answered') {
      return HttpResponse.json({ error: { code: 'not_answered', message: 'the thread has no answer to grade' } }, { status: 409 })
    }
    const body = (await request.json()) as { outcome: BrainstormOutcome }
    question.outcome = body.outcome
    question.outcome_overridden = true
    return HttpResponse.json(question)
  }),

  http.get('/v1/tasks/:id/gates', ({ params }) => {
    const id = Number(params.id)
    return HttpResponse.json({ gates: gatesState.filter((g) => g.task_id === id).sort((a, b) => b.id - a.id) })
  }),

  http.post('/v1/gates/:id/decide', async ({ params, request }) => {
    const gate = gatesState.find((g) => g.id === Number(params.id))
    if (!gate) {
      return HttpResponse.json({ error: { code: 'not_found', message: 'gate not found' } }, { status: 404 })
    }
    const body = (await request.json()) as { decision: 'go' | 'changes'; comment?: string }
    const comment = (body.comment ?? '').trim()
    if (body.decision === 'changes' && !comment) {
      return HttpResponse.json({ error: { code: 'comment_required', message: 'needs-changes requires a comment' } }, { status: 400 })
    }
    if (gate.status !== 'pending') {
      return HttpResponse.json(
        { error: { code: 'gate_not_pending', message: `gate is ${gate.status}, not pending: request a new gate` } },
        { status: 409 },
      )
    }
    gate.status = body.decision
    gate.comment = comment
    gate.decided_by = 'human'
    gate.decided_at = nowSeconds()
    const task = tasksState.find((t) => t.id === gate.task_id)
    if (body.decision === 'go' && task?.status === 'brainstorm') task.status = 'in_progress'
    return HttpResponse.json(gate)
  }),

  http.get('/v1/tasks/:id/brainstorm/stats', ({ params }) => {
    const id = Number(params.id)
    return HttpResponse.json(brainstormStats.storms.find((s) => s.task_id === id) ?? emptyStorm(id))
  }),

  http.get('/v1/stats/brainstorm', () => HttpResponse.json(brainstormStats)),

  // --------------------------------------------------------------------
  // Settings & GitHub — internal/api/settings.go, internal/api/
  // github_catalog.go. Verified against .superpowers/sdd/phase4-contract.md.
  // --------------------------------------------------------------------

  // GET always 200s; `login` is never present here (only on PUT).
  http.get('/v1/settings', () => HttpResponse.json(settingsBody())),

  http.put('/v1/settings', async ({ request }) => {
    const body = (await request.json()) as {
      github_token?: string
      orchestrator_brainstorm_custom?: boolean
      default_orchestrator_profile?: string
      default_worker_profile?: string
    }
    // Only the fields present are applied (handlePutSettings): the storm
    // toggle alone never touches the token.
    if (body.github_token === undefined) {
      const defaults = [body.default_orchestrator_profile, body.default_worker_profile]
      if (body.orchestrator_brainstorm_custom === undefined && defaults.every((d) => d === undefined)) {
        return HttpResponse.json({ error: { code: 'bad_request', message: 'nothing to update' } }, { status: 400 })
      }
      for (const d of defaults) {
        if (d && !profilesState.some((p) => p.name === d)) {
          return profileError(400, 'profile_not_found', `no profile ${d}`)
        }
      }
      settingsState = {
        ...settingsState,
        ...(body.orchestrator_brainstorm_custom !== undefined && {
          orchestrator_brainstorm_custom: body.orchestrator_brainstorm_custom,
        }),
        ...(body.default_orchestrator_profile !== undefined && {
          default_orchestrator_profile: body.default_orchestrator_profile,
        }),
        ...(body.default_worker_profile !== undefined && { default_worker_profile: body.default_worker_profile }),
      }
      return HttpResponse.json(settingsBody())
    }
    const token = body.github_token ?? ''
    if (token === '') {
      settingsState = { ...settingsState, github_token: '' }
      return HttpResponse.json({ github_token: '' })
    }
    // Fixture stand-ins for GitHub's /user validation: a token starting
    // with "invalid" is rejected (400 invalid_token); "unreachable" fakes a
    // network failure (502 github_unreachable); anything else is accepted.
    if (token.toLowerCase().startsWith('invalid')) {
      return HttpResponse.json(
        { error: { code: 'invalid_token', message: 'GitHub rejected the token' } },
        { status: 400 },
      )
    }
    if (token === 'unreachable') {
      return HttpResponse.json(
        { error: { code: 'github_unreachable', message: 'could not reach GitHub to validate token' } },
        { status: 502 },
      )
    }
    settingsState = { ...settingsState, github_token: token }
    return HttpResponse.json({ github_token: maskToken(token), login: 'acme-bot' })
  }),

  http.get('/v1/github/repos', ({ request }) => {
    if (!settingsState.github_token) {
      return HttpResponse.json({ error: { code: 'no_token', message: 'no GitHub token configured' } }, { status: 400 })
    }
    const url = new URL(request.url)
    const q = (url.searchParams.get('q') ?? '').toLowerCase()
    const result = q ? githubRepos.filter((r) => r.full_name.toLowerCase().includes(q)) : githubRepos
    return HttpResponse.json({ repos: result })
  }),

  // GET /v1/github/issues — internal/api/github_issues.go. `repo_id` is a
  // registered repo id, resolved server-side to owner/name via that repo's
  // git remote origin; the fixture keys `githubIssues` by that same repo id
  // rather than modeling owner/name resolution. `infra` (no fixture entry)
  // stands in for "repo has no GitHub origin" -> not_a_github_repo, matching
  // its use as a project-linked repo in tests.
  http.get('/v1/github/issues', ({ request }) => {
    if (!settingsState.github_token) {
      return HttpResponse.json({ error: { code: 'no_token', message: 'no GitHub token configured' } }, { status: 400 })
    }
    const url = new URL(request.url)
    const repoId = url.searchParams.get('repo_id')
    const repoParam = url.searchParams.get('repo')
    const state = url.searchParams.get('state') || 'open'
    if (!repoId && !repoParam) {
      return HttpResponse.json(
        { error: { code: 'bad_request', message: 'either repo or repo_id is required' } },
        { status: 400 },
      )
    }
    if (repoId) {
      if (!reposState.some((r) => r.id === repoId)) {
        return HttpResponse.json(
          { error: { code: 'repo_not_found', message: `no repo registered with id ${repoId}` } },
          { status: 404 },
        )
      }
      if (repoId === 'infra') {
        return HttpResponse.json(
          { error: { code: 'not_a_github_repo', message: "repo's remote origin is not a GitHub URL" } },
          { status: 400 },
        )
      }
    }
    const issues = repoId ? (githubIssues[repoId] ?? []) : []
    const result = state === 'all' ? issues : issues.filter((i) => i.state === state)
    return HttpResponse.json({ issues: result })
  }),

  // --------------------------------------------------------------------
  // Repo/project creation (New Project wizard).
  // --------------------------------------------------------------------

  http.post('/v1/repos', async ({ request }) => {
    const body = (await request.json()) as { id?: string; path?: string; github?: string }
    if (body.github) {
      const gh = githubRepos.find((r) => r.full_name === body.github)
      const name = body.github.split('/').pop() ?? body.github
      // The real handler derives the id via normalizeID (internal/api/repos.go),
      // so `acme/status.page` registers as `status-page`, not `status.page`.
      const id = body.id ?? slugify(name)
      if (reposState.some((r) => r.id === id)) {
        return HttpResponse.json(
          { error: { code: 'repo_exists', message: `repo id ${id} already exists` } },
          { status: 409 },
        )
      }
      const created: Repo = {
        id,
        path: `/home/dev/.rocket/repos/${name}`,
        default_branch: gh?.default_branch ?? 'main',
        auto_cleanup: true,
        env: {},
        symlinks: [],
        post_create: [],
        created_at: Math.floor(Date.now() / 1000),
      }
      reposState.push(created)
      return HttpResponse.json(created, { status: 201 })
    }
    const path = body.path ?? ''
    const name = path.split('/').filter(Boolean).pop() ?? 'repo'
    return HttpResponse.json(
      {
        id: body.id ?? slugify(name),
        path,
        default_branch: 'main',
        auto_cleanup: true,
        env: {},
        symlinks: [],
        post_create: [],
        created_at: Math.floor(Date.now() / 1000),
      },
      { status: 201 },
    )
  }),

  http.post('/v1/projects', async ({ request }) => {
    const body = (await request.json()) as { id?: string; name: string; main: string; linked?: string[] }
    return HttpResponse.json({
      id: body.id ?? body.name.toLowerCase().replace(/\s+/g, '-'),
      name: body.name,
      main: body.main,
      linked: body.linked ?? [],
      live_sessions: 0,
      created_at: Math.floor(Date.now() / 1000),
    })
  }),

  // --------------------------------------------------------------------
  // Repo/project editing & deletion (Settings screen).
  // --------------------------------------------------------------------

  http.patch('/v1/repos/:id', async ({ params, request }) => {
    const id = params.id as string
    const repo = reposState.find((r) => r.id === id)
    if (!repo) {
      return HttpResponse.json({ error: { code: 'not_found', message: `repo ${id} not found` } }, { status: 404 })
    }
    const body = (await request.json()) as Partial<Pick<Repo, 'env' | 'symlinks' | 'post_create'>>
    Object.assign(repo, body)
    return HttpResponse.json(repo)
  }),

  http.delete('/v1/repos/:id', ({ params }) => {
    const id = params.id as string
    const repo = reposState.find((r) => r.id === id)
    if (!repo) {
      return HttpResponse.json({ error: { code: 'not_found', message: `repo ${id} not found` } }, { status: 404 })
    }
    const usedBy = projectsState.filter((p) => p.main === id || p.linked.includes(id))
    if (usedBy.length > 0) {
      return HttpResponse.json(
        {
          error: {
            code: 'repo_in_use',
            message: `repo ${id} is used by project(s): ${usedBy.map((p) => p.id).join(', ')}`,
          },
        },
        { status: 409 },
      )
    }
    reposState = reposState.filter((r) => r.id !== id)
    return new HttpResponse(null, { status: 204 })
  }),

  http.patch('/v1/projects/:id', async ({ params, request }) => {
    const id = params.id as string
    const project = projectsState.find((p) => p.id === id)
    if (!project) {
      return HttpResponse.json({ error: { code: 'not_found', message: `project ${id} not found` } }, { status: 404 })
    }
    const body = (await request.json()) as Partial<Pick<Project, 'name' | 'main' | 'linked'>>
    Object.assign(project, body)
    return HttpResponse.json(project)
  }),

  // The real daemon (internal/api/projects.go) blocks DELETE only when
  // live_sessions>0 -> 409 project_busy. It does NOT check tasks.
  // Attachment upload (internal/api/attachments.go): raw image body -> id+url.
  http.post('/v1/attachments', () =>
    HttpResponse.json({ id: 1, url: '/v1/attachments/1' }, { status: 201 }),
  ),

  http.delete('/v1/projects/:id', ({ params }) => {
    const id = params.id as string
    const project = projectsState.find((p) => p.id === id)
    if (!project) {
      return HttpResponse.json({ error: { code: 'not_found', message: `project ${id} not found` } }, { status: 404 })
    }
    if (project.live_sessions > 0) {
      return HttpResponse.json(
        {
          error: {
            code: 'project_busy',
            message: 'project has live sessions',
          },
        },
        { status: 409 },
      )
    }
    projectsState = projectsState.filter((p) => p.id !== id)
    return new HttpResponse(null, { status: 204 })
  }),

  // --- Agents (internal/api/agents.go, agent_questions.go) ----------------

  http.get('/v1/agents', ({ request }) => {
    const project = new URL(request.url).searchParams.get('project')
    const list = project ? agentsState.filter((a) => a.project === project) : agentsState
    return HttpResponse.json(list.map(withMilestones))
  }),

  http.post('/v1/agents', async ({ request }) => {
    const body = (await request.json()) as {
      id: string
      project: string
      description?: string
      dir?: string
      command?: string
    }
    if (agentsState.some((a) => a.id === body.id)) {
      return HttpResponse.json(
        { error: { code: 'agent_exists', message: 'agent id already exists' } },
        { status: 409 },
      )
    }
    const created: Agent = {
      id: body.id,
      description: body.description ?? '',
      project: body.project,
      dir: body.dir ?? '',
      command: body.command ?? '',
      enabled: true,
      session_alive: false,
      unread: 0,
      open_questions: 0,
      awaiting_user: 0,
      created_at: 1_800_000_000,
      updated_at: 1_800_000_000,
    }
    agentsState = [...agentsState, created]
    return HttpResponse.json(created, { status: 201 })
  }),

  http.get('/v1/agents/:id', ({ params }) => {
    const found = agentsState.find((a) => a.id === params.id)
    if (!found) {
      return HttpResponse.json(
        { error: { code: 'agent_not_found', message: 'agent not found' } },
        { status: 404 },
      )
    }
    return HttpResponse.json(withMilestones(found))
  }),

  http.patch('/v1/agents/:id', async ({ params, request }) => {
    const patch = (await request.json()) as Partial<Agent>
    agentsState = agentsState.map((a) => (a.id === params.id ? { ...a, ...patch } : a))
    const updated = agentsState.find((a) => a.id === params.id)
    if (!updated) {
      return HttpResponse.json(
        { error: { code: 'agent_not_found', message: 'agent not found' } },
        { status: 404 },
      )
    }
    return HttpResponse.json(updated)
  }),

  http.delete('/v1/agents/:id', ({ params }) => {
    agentsState = agentsState.filter((a) => a.id !== params.id)
    return HttpResponse.json({ status: 'deleted' })
  }),

  http.post('/v1/agents/:id/enable', ({ params }) => {
    agentsState = agentsState.map((a) => (a.id === params.id ? { ...a, enabled: true } : a))
    return HttpResponse.json(agentsState.find((a) => a.id === params.id))
  }),

  http.post('/v1/agents/:id/disable', ({ params }) => {
    agentsState = agentsState.map((a) => (a.id === params.id ? { ...a, enabled: false } : a))
    return HttpResponse.json(agentsState.find((a) => a.id === params.id))
  }),

  // Live-or-inbox delivery, the daemon's single path: a running session takes
  // the message through the queue, a dead one grows its inbox by one.
  http.post('/v1/agents/:id/messages', async ({ params, request }) => {
    const body = (await request.json()) as { body: string }
    const id = params.id as string
    const agent = agentsState.find((a) => a.id === id)
    const live = agent?.session_alive === true
    if (!live) {
      agentInboxState = [
        ...agentInboxState,
        { id: agentInboxState.length + 1, from: '', body: body.body, status: 'unread', created_at: 1_800_000_000 },
      ]
      agentsState = agentsState.map((a) => (a.id === id ? { ...a, unread: a.unread + 1 } : a))
    }
    return HttpResponse.json(
      { id: 7, to: id, status: live ? 'queued' : 'inbox', live },
      { status: 202 },
    )
  }),

  http.get('/v1/agents/:id/inbox', ({ request }) => {
    const status = new URL(request.url).searchParams.get('status')
    return HttpResponse.json(
      status ? agentInboxState.filter((m) => m.status === status) : agentInboxState,
    )
  }),

  http.post('/v1/agents/:id/start', ({ params }) => {
    const id = params.id as string
    const agent = agentsState.find((a) => a.id === id)
    if (!agent) {
      return HttpResponse.json(
        { error: { code: 'agent_not_found', message: 'agent not found' } },
        { status: 404 },
      )
    }
    agentsState = agentsState.map((a) => (a.id === id ? { ...a, session_alive: true } : a))
    return HttpResponse.json({ id, status: 'running', dir: agent.dir })
  }),

  http.post('/v1/agents/:id/stop', ({ params }) => {
    const id = params.id as string
    agentsState = agentsState.map((a) => (a.id === id ? { ...a, session_alive: false } : a))
    return HttpResponse.json({ id, status: 'stopped' })
  }),

  http.get('/v1/agents/:id/questions', ({ params, request }) => {
    const openOnly = new URL(request.url).searchParams.get('status') === 'open'
    const all = agentQuestions.filter((q) => q.role_id === params.id)
    return HttpResponse.json({ questions: openOnly ? all.filter((q) => q.status === 'open') : all })
  }),

  http.post('/v1/agents/:id/questions', async ({ params, request }) => {
    const body = (await request.json()) as { body: string; context?: string; to?: string[] }
    const waiting = body.to ?? [params.id as string]
    return HttpResponse.json(
      {
        id: 92,
        role_id: params.id as string,
        ordinal: 3,
        asked_by: '',
        body: body.body,
        brief: '',
        context: body.context,
        status: 'open',
        participants: ['human', params.id as string],
        waiting_on: waiting,
        your_turn: waiting.some((p) => isHuman(p)),
        whose_turn: 'role',
        asked_at: 1_800_000_000,
        messages: [],
      },
      { status: 201 },
    )
  }),

  http.post('/v1/agent-questions/:id/reply', async ({ request }) => {
    const body = (await request.json()) as { body: string; to?: string[] }
    const waiting = body.to ?? ['sre']
    return HttpResponse.json(
      {
        ...agentQuestions[0],
        waiting_on: waiting,
        your_turn: waiting.some((p) => isHuman(p)),
        whose_turn: 'role',
        messages: [
          ...agentQuestions[0].messages,
          {
            id: 99,
            author: '',
            kind: 'reply',
            body: body.body,
            addressed_to: body.to,
            created_at: 1_800_000_000,
          },
        ],
      },
      { status: 201 },
    )
  }),

  http.post('/v1/agent-questions/:id/answer', () =>
    HttpResponse.json({
      ...agentQuestions[0],
      status: 'resolved',
      waiting_on: [],
      your_turn: false,
      whose_turn: undefined,
    }),
  ),

  // Agent kinds — the orchestrator picker in StartModal. codex is registered
  // but has no binary on the fixture machine, so it shows up disabled.
  http.get('/v1/agent-kinds', () =>
    HttpResponse.json({
      default: 'claude-code',
      kinds: [
        { name: 'claude-code', available: true, efforts: AGENT_EFFORTS['claude-code'] },
        { name: 'codex', available: false, error: 'codex not found in PATH', efforts: AGENT_EFFORTS.codex },
      ],
    }),
  ),

  // Model profiles — internal/api/model_profiles.go (task #5026).
  http.get('/v1/model-profiles', () => HttpResponse.json({ profiles: profilesState })),

  http.post('/v1/model-profiles', async ({ request }) => {
    const body = (await request.json()) as Partial<ModelProfile>
    if (!body.name || !/^[a-z0-9][a-z0-9-]{0,39}$/.test(body.name)) {
      return profileError(400, 'bad_request', 'name must match ^[a-z0-9][a-z0-9-]{0,39}$')
    }
    if (profilesState.some((p) => p.name === body.name)) {
      return profileError(409, 'profile_exists', `profile ${body.name} already exists`)
    }
    const p: ModelProfile = {
      name: body.name,
      agent: body.agent ?? '',
      model: body.model ?? '',
      effort: body.effort ?? '',
      description: body.description ?? '',
      enabled: body.enabled ?? true,
      position: body.position ?? Math.max(-1, ...profilesState.map((e) => e.position)) + 1,
    }
    const bad = badProfileAgent(p)
    if (bad) return bad
    profilesState = [...profilesState, p]
    return HttpResponse.json(p, { status: 201 })
  }),

  http.patch('/v1/model-profiles/:name', async ({ params, request }) => {
    const current = profilesState.find((p) => p.name === params.name)
    if (!current) return profileError(404, 'profile_not_found', `no profile ${params.name}`)
    const body = (await request.json()) as Partial<ModelProfile>
    if (body.name !== undefined && body.name !== current.name) {
      return profileError(400, 'bad_request', 'a profile cannot be renamed')
    }
    const next = { ...current, ...body }
    const bad = badProfileAgent(next)
    if (bad) return bad
    profilesState = profilesState.map((p) => (p.name === current.name ? next : p))
    return HttpResponse.json(next)
  }),

  // Model catalog — internal/api/model_catalog.go (choose-model v2).
  http.get('/v1/model-catalog', () => HttpResponse.json({ agents: modelCatalog })),

  // importCatalog: a disabled profile per catalog model no profile of the
  // same agent uses yet; names as the daemon derives them.
  http.post('/v1/model-profiles/import-catalog', async ({ request }) => {
    const body = (await request.json().catch(() => ({}))) as { include_legacy?: boolean }
    const created: string[] = []
    const skipped: { model: string; reason: string }[] = []
    for (const cat of modelCatalog) {
      for (const m of cat.models) {
        if (!m.main && !body.include_legacy) continue
        const owner = profilesState.find((p) => p.agent === cat.agent && p.model === m.id)
        if (owner) {
          skipped.push({ model: m.id, reason: `profile ${owner.name} already uses it` })
          continue
        }
        const base = (cat.agent === 'codex' ? `codex-${m.id}` : m.id).replace(/[^a-z0-9-]/g, '-')
        let name = base
        for (let i = 2; profilesState.some((p) => p.name === name); i++) name = `${base}-${i}`
        profilesState = [
          ...profilesState,
          {
            name,
            agent: cat.agent,
            model: m.id,
            effort: '',
            description: m.description || m.name,
            enabled: false,
            position: Math.max(-1, ...profilesState.map((e) => e.position)) + 1,
          },
        ]
        created.push(name)
      }
    }
    return HttpResponse.json({ created, skipped })
  }),

  http.delete('/v1/model-profiles/:name', ({ params }) => {
    const name = String(params.name)
    for (const key of ['default_orchestrator_profile', 'default_worker_profile'] as const) {
      if (settingsState[key] === name) {
        return profileError(409, 'profile_in_use', `profile ${name} is the ${key}; pick another default first`)
      }
    }
    if (!profilesState.some((p) => p.name === name)) return profileError(404, 'profile_not_found', `no profile ${name}`)
    profilesState = profilesState.filter((p) => p.name !== name)
    return new HttpResponse(null, { status: 204 })
  }),
]
