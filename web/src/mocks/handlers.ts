// msw request handlers backed by the fixtures in ./fixtures.ts. Mirrors the
// daemon's response envelopes exactly: bare arrays for
// projects/sessions/repos, `{tasks:[]}`/`{docs:[]}`/`{log:[]}`/`{questions:[]}`
// wrappers for tasks/docs/log/questions, and `{error:{code,message}}` on
// failure. Verified against .superpowers/sdd/phase3-contract.md and
// internal/api/tasks.go / questions.go.

import { http, HttpResponse } from 'msw'
import { isHuman } from '../lib/participants'
import type {
  Agent,
  AgentInboxMessage,
  ChatEntry,
  Project,
  Question,
  QuestionMessage,
  Repo,
  Settings,
  Task,
  TaskDoc,
  TaskLogEntry,
  TaskStatus,
} from '../lib/types'
import {
  agentInbox,
  agentQuestions,
  agents,
  chatEntries,
  githubIssues,
  githubRepos,
  messages,
  projects,
  questions,
  repos,
  sessions,
  settings,
  subtasks,
  systemInfo,
  taskDocs,
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
let tasksState: Task[] = [...tasks, ...subtasks].map((t) => ({ ...t }))
let nextTaskId = Math.max(...tasksState.map((t) => t.id)) + 1

export function resetTasks(): void {
  tasksState = [...tasks, ...subtasks].map((t) => ({ ...t }))
  nextTaskId = Math.max(...tasksState.map((t) => t.id)) + 1
}

let questionsState: Question[] = questions.map((q) => ({ ...q, messages: q.messages.map((m) => ({ ...m })) }))
let nextQuestionId = Math.max(...questionsState.map((q) => q.id)) + 1

export function resetQuestions(): void {
  questionsState = questions.map((q) => ({ ...q, messages: q.messages.map((m) => ({ ...m })) }))
  nextQuestionId = Math.max(...questionsState.map((q) => q.id)) + 1
}

let docsState: TaskDoc[] = taskDocs.map((d) => ({ ...d }))
let nextDocId = Math.max(...docsState.map((d) => d.id)) + 1

export function resetDocs(): void {
  docsState = taskDocs.map((d) => ({ ...d }))
  nextDocId = Math.max(...docsState.map((d) => d.id)) + 1
}

let logState: TaskLogEntry[] = taskLog.map((l) => ({ ...l }))
let nextLogId = Math.max(...logState.map((l) => l.id)) + 1

export function resetLog(): void {
  logState = taskLog.map((l) => ({ ...l }))
  nextLogId = Math.max(...logState.map((l) => l.id)) + 1
}

const TASK_STATUSES: TaskStatus[] = ['backlog', 'in_progress', 'review', 'done', 'cancelled']

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

export const handlers = [
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

    let result = project ? tasksState.filter((t) => t.project_id === project) : tasksState
    if (status) result = result.filter((t) => t.status === status)

    if (board) {
      // Board is root-only by default, same as the list endpoint.
      const rootOnly = result.filter((t) => t.parent_id === undefined)
      const b: Record<TaskStatus, Task[]> = {
        backlog: [],
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
    const body = (await request.json()) as { status?: TaskStatus; title?: string; description?: string }
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

  http.get('/v1/tasks/:id/docs', ({ params }) => {
    const id = Number(params.id)
    return HttpResponse.json({ docs: docsState.filter((d) => d.task_id === id) })
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
    const body = (await request.json()) as { body?: string; dismiss?: boolean; to?: string[] }
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
  // Settings & GitHub — internal/api/settings.go, internal/api/
  // github_catalog.go. Verified against .superpowers/sdd/phase4-contract.md.
  // --------------------------------------------------------------------

  // GET always 200s; `login` is never present here (only on PUT).
  http.get('/v1/settings', () => HttpResponse.json({ github_token: maskToken(settingsState.github_token) })),

  http.put('/v1/settings', async ({ request }) => {
    const body = (await request.json()) as { github_token?: string }
    const token = body.github_token ?? ''
    if (token === '') {
      settingsState = { github_token: '' }
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
    settingsState = { github_token: token }
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
      const id = body.id ?? name
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
        id: body.id ?? name,
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
    return HttpResponse.json(project ? agentsState.filter((a) => a.project === project) : agentsState)
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
    return HttpResponse.json(found)
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
]
