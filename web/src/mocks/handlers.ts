// msw request handlers backed by the fixtures in ./fixtures.ts. Mirrors the
// daemon's response envelopes exactly: bare arrays for list endpoints,
// `{messages:[...]}` / `{events:[...]}` wrapper objects, and
// `{error:{code,message}}` on failure.

import { http, HttpResponse } from 'msw'
import {
  messages,
  projects,
  questions,
  repos,
  sessions,
  subtasks,
  systemInfo,
  taskDocs,
  taskLog,
  tasks,
} from './fixtures'

const allTasks = [...tasks, ...subtasks]

export const handlers = [
  http.get('/v1/projects', () => HttpResponse.json(projects)),

  http.get('/v1/sessions', ({ request }) => {
    const url = new URL(request.url)
    const project = url.searchParams.get('project')
    const result = project ? sessions.filter((s) => s.project_id === project) : sessions
    return HttpResponse.json(result)
  }),

  http.get('/v1/repos', () => HttpResponse.json(repos)),

  http.get('/v1/system', () => HttpResponse.json(systemInfo)),

  http.post('/v1/system/cleanup', () =>
    HttpResponse.json({
      killed_tmux: systemInfo.tmux.filter((t) => t.orphan).map((t) => t.name),
      removed_worktrees: systemInfo.worktrees.filter((w) => w.orphan).map((w) => w.path),
    }),
  ),

  http.post('/v1/sessions/:id/kill', ({ params }) => {
    const id = params.id as string
    const session = sessions.find((s) => s.id === id)
    if (!session) {
      return HttpResponse.json(
        { error: { code: 'not_found', message: `session ${id} not found` } },
        { status: 404 },
      )
    }
    return HttpResponse.json({ status: 'killed' })
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

  http.get('/v1/tasks', ({ request }) => {
    const url = new URL(request.url)
    const project = url.searchParams.get('project')
    const result = project ? allTasks.filter((t) => t.project_id === project) : allTasks

    if (url.searchParams.get('board') === 'true') {
      const columns = {
        backlog: [] as typeof result,
        in_progress: [] as typeof result,
        review: [] as typeof result,
        done: [] as typeof result,
        cancelled: [] as typeof result,
      }
      for (const task of result) {
        columns[task.status].push(task)
      }
      return HttpResponse.json({ columns })
    }

    return HttpResponse.json(result)
  }),

  http.get('/v1/tasks/:id', ({ params }) => {
    const id = Number(params.id)
    const task = allTasks.find((t) => t.id === id)
    if (!task) {
      return HttpResponse.json(
        { error: { code: 'not_found', message: `task ${id} not found` } },
        { status: 404 },
      )
    }
    const taskSubtasks = subtasks.filter((t) => t.parent_id === id)
    const session = task.session_id ? sessions.find((s) => s.id === task.session_id) : undefined
    return HttpResponse.json({
      ...task,
      subtasks: taskSubtasks,
      session: session
        ? { id: session.id, tmux_name: session.tmux_name, attach: ['rocket', 'attach', session.id] }
        : undefined,
    })
  }),

  http.get('/v1/tasks/:id/docs', ({ params }) => {
    const id = Number(params.id)
    return HttpResponse.json(taskDocs.filter((d) => d.task_id === id))
  }),

  http.get('/v1/tasks/:id/log', ({ params }) => {
    const id = Number(params.id)
    return HttpResponse.json(taskLog.filter((l) => l.task_id === id))
  }),

  http.get('/v1/tasks/:id/questions', ({ params }) => {
    const id = Number(params.id)
    return HttpResponse.json(questions.filter((q) => q.task_id === id))
  }),
]
