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
    return HttpResponse.json(task)
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
