// msw request handlers backed by the fixtures in ./fixtures.ts. Mirrors the
// daemon's response envelopes exactly: bare arrays for list endpoints,
// `{messages:[...]}` / `{events:[...]}` wrapper objects, and
// `{error:{code,message}}` on failure.

import { http, HttpResponse } from 'msw'
import type { Settings } from '../lib/types'
import {
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

const allTasks = [...tasks, ...subtasks]

// Mutable copy of the settings fixture, written by `PUT /v1/settings`. Tests
// that mutate this (e.g. "connect GitHub") should call `resetSettings()` in
// `afterEach` to avoid leaking state into later tests in the same file.
let settingsState: Settings = { ...settings }

export function resetSettings(): void {
  settingsState = { ...settings }
}

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

  // ------------------------------------------------------------------------
  // Settings & GitHub — contract types (phase 4), docs/03-daemon-api.md
  // «Настройки и GitHub».
  // ------------------------------------------------------------------------

  http.get('/v1/settings', () => HttpResponse.json(settingsState)),

  http.put('/v1/settings', async ({ request }) => {
    const body = (await request.json()) as { github_token?: string }
    settingsState = {
      github_token: body.github_token,
      github_authorized_as: body.github_token ? 'acme-bot' : undefined,
    }
    return HttpResponse.json(settingsState)
  }),

  http.get('/v1/github/repos', ({ request }) => {
    if (!settingsState.github_token) {
      return HttpResponse.json(
        { error: { code: 'github_token_missing', message: 'no GitHub token configured' } },
        { status: 404 },
      )
    }
    const url = new URL(request.url)
    const q = (url.searchParams.get('q') ?? '').toLowerCase()
    const result = q ? githubRepos.filter((r) => r.full_name.toLowerCase().includes(q)) : githubRepos
    return HttpResponse.json(result)
  }),

  // ------------------------------------------------------------------------
  // Repo/project creation (New Project wizard).
  // ------------------------------------------------------------------------

  http.post('/v1/repos', async ({ request }) => {
    const body = (await request.json()) as { id?: string; path?: string; github?: string }
    if (body.github) {
      const gh = githubRepos.find((r) => r.full_name === body.github)
      const name = body.github.split('/').pop() ?? body.github
      return HttpResponse.json({
        id: body.id ?? name,
        path: `/home/dev/.rocket/repos/${name}`,
        default_branch: gh?.default_branch ?? 'main',
        auto_cleanup: true,
        env: {},
        symlinks: [],
        post_create: [],
        created_at: Math.floor(Date.now() / 1000),
      })
    }
    const path = body.path ?? ''
    const name = path.split('/').filter(Boolean).pop() ?? 'repo'
    return HttpResponse.json({
      id: body.id ?? name,
      path,
      default_branch: 'main',
      auto_cleanup: true,
      env: {},
      symlinks: [],
      post_create: [],
      created_at: Math.floor(Date.now() / 1000),
    })
  }),

  http.post('/v1/projects', async ({ request }) => {
    const body = (await request.json()) as { id?: string; name: string; main: string; linked?: string[] }
    return HttpResponse.json({
      id: body.id ?? body.name.toLowerCase().replace(/\s+/g, '-'),
      name: body.name,
      main: body.main,
      linked: body.linked ?? [],
      live_sessions: 0,
      tasks: { backlog: 0, in_progress: 0, review: 0, done: 0 },
      created_at: Math.floor(Date.now() / 1000),
    })
  }),
]
