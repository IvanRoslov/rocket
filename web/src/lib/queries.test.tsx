// Covers the msw-mocked board response shape (`{board:{...}}`) and its
// adapter in `useTasksBoard`, plus `useTask`/`TaskDetail` returning
// subtasks + the bound session.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { setupServer } from 'msw/node'
import type { ReactNode } from 'react'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers } from '../mocks/handlers'
import {
  useAgentInbox,
  useAgentQuestions,
  useAgents,
  useAnswerQuestion,
  useReplyAgentQuestion,
  useReplyQuestion,
  useSendAgentMessage,
  useStartAgent,
  useStopAgent,
  useAssignMilestone,
  useCreateMilestone,
  useMilestonesBoard,
  useTask,
  useTasksBoard,
} from './queries'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}

describe('useTasksBoard', () => {
  it('adapts the {board:{...}} board response into per-status arrays', async () => {
    const { result } = renderHook(() => useTasksBoard('billing'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const board = result.current.data!
    // Board is root-only: subtasks #13/#14 are not included.
    expect(board.backlog.map((t) => t.id)).toEqual([10])
    expect(board.in_progress.map((t) => t.id)).toEqual([12])
    expect(board.review.map((t) => t.id)).toEqual([11])
    expect(board.done.map((t) => t.id)).toEqual([9])
    expect(board.cancelled).toEqual([])
  })
})

// Milestones (task #1023, spec v2): root tasks outside every project, held by
// a persistent agent. They must never leak into a project board, and the
// project boards must never leak into theirs.
describe('milestone queries', () => {
  it('useMilestonesBoard returns only milestones, grouped by status', async () => {
    const { result } = renderHook(() => useMilestonesBoard(), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const board = result.current.data!
    const every = Object.values(board).flat()
    expect(every.length).toBeGreaterThan(0)
    expect(every.every((t) => t.milestone === true)).toBe(true)
    expect(every.every((t) => t.project_id === '')).toBe(true)
    expect(board.in_progress.map((t) => t.assigned_role)).toContain('sre')
  })

  it('the project board carries no milestones', async () => {
    const { result } = renderHook(() => useTasksBoard('billing'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    const every = Object.values(result.current.data!).flat()
    expect(every.some((t) => t.milestone)).toBe(false)
  })

  it('useAssignMilestone hands a milestone over and takes it back', async () => {
    const { result } = renderHook(() => useAssignMilestone(), { wrapper })

    result.current.mutate({ id: 40, agentId: 'librarian' })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.assigned_role).toBe('librarian')

    result.current.mutate({ id: 40, agentId: null })
    await waitFor(() => expect(result.current.data!.assigned_role).toBeUndefined())
  })

  it('useCreateMilestone creates a task with no project', async () => {
    const { result } = renderHook(() => useCreateMilestone(), { wrapper })

    result.current.mutate({ title: 'Kill the flaky suite' })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.milestone).toBe(true)
    expect(result.current.data!.project_id).toBe('')
  })
})

describe('useTask', () => {
  it('resolves TaskDetail with subtasks and the bound orchestrator session', async () => {
    const { result } = renderHook(() => useTask(12), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const task = result.current.data!
    expect(task.id).toBe(12)
    expect(task.subtasks.map((t) => t.id).sort()).toEqual([13, 14, 15, 16])
    expect(task.open_questions).toBe(1)
    expect(task.session).toEqual({
      id: 's-billing-v2-orch',
      tmux_name: 'billing-v2-orch',
      attach: ['rocket', 'attach', 's-billing-v2-orch'],
    })
  })
})

describe('agent queries', () => {
  it('useAgents returns the bare array of agents, filtered by project', async () => {
    const { result } = renderHook(() => useAgents('billing'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.map((a) => a.id)).toEqual(['sre', 'triage'])
    expect(result.current.data![0].awaiting_user).toBe(1)
    expect(result.current.data![0].session_alive).toBe(true)
    expect(result.current.data![0].unread).toBe(2)
  })

  it('useAgentInbox filters by status', async () => {
    const { result } = renderHook(() => useAgentInbox('sre', 'unread'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.map((m) => m.from)).toEqual(['billing-v2-orch', 'ivan'])
    expect(result.current.data!.every((m) => m.status === 'unread')).toBe(true)
  })

  it('useAgentQuestions unwraps the {questions:[]} envelope', async () => {
    const { result } = renderHook(() => useAgentQuestions('sre'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.map((q) => q.id)).toEqual([91, 90])
  })

  it('useSendAgentMessage reports whether the message was queued or inboxed', async () => {
    const { result } = renderHook(() => useSendAgentMessage(), { wrapper })

    result.current.mutate({ id: 'sre', body: 'ping' })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    // The "sre" fixture has a live session, so delivery goes through the queue.
    expect(result.current.data).toMatchObject({ to: 'sre', status: 'queued', live: true })
  })

  it('useStartAgent brings the agent session up', async () => {
    const { result } = renderHook(() => useStartAgent(), { wrapper })

    result.current.mutate('triage')
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.status).toBe('running')
  })

  it('useStopAgent takes the agent session down', async () => {
    const { result } = renderHook(() => useStopAgent(), { wrapper })

    result.current.mutate('sre')
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.status).toBe('stopped')
  })
})

// `to` decides who must RESPOND, never who gets notified. An empty pick must
// not put the key on the wire at all: the API reads an absent `to` as
// "everyone except the author" (waitingOn in internal/api/threads.go).
describe('addressees on reply and answer', () => {
  it('useReplyQuestion sends `to` when picked and omits the key when not', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/reply', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 }, { status: 201 })
      }),
    )
    const { result } = renderHook(() => useReplyQuestion(), { wrapper })

    result.current.mutate({ id: 3, body: 'over to you', taskId: 12, to: ['cto'] })
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ body: 'over to you', to: ['cto'] })

    result.current.mutate({ id: 3, body: 'anyone', taskId: 12, to: [] })
    await waitFor(() => expect(bodies).toHaveLength(2))
    expect(bodies[1]).toEqual({ body: 'anyone' })
    expect(bodies[1]).not.toHaveProperty('to')
  })

  it('useAnswerQuestion sends `to` on an answer but never alongside a dismiss', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 })
      }),
    )
    const { result } = renderHook(() => useAnswerQuestion(), { wrapper })

    result.current.mutate({ id: 3, body: 'done', taskId: 12, to: ['cto', 'reply-answer-orch'] })
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ body: 'done', to: ['cto', 'reply-answer-orch'] })

    result.current.mutate({ id: 3, dismiss: true, taskId: 12, to: ['cto'] })
    await waitFor(() => expect(bodies).toHaveLength(2))
    expect(bodies[1]).toEqual({ dismiss: true })
  })

  it('useReplyAgentQuestion carries the same optional `to`', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/agent-questions/:id/reply', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 90 }, { status: 201 })
      }),
    )
    const { result } = renderHook(() => useReplyAgentQuestion(), { wrapper })

    result.current.mutate({ id: 90, body: 'ping', roleId: 'sre', to: ['sre'] })
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ body: 'ping', to: ['sre'] })

    result.current.mutate({ id: 90, body: 'ping', roleId: 'sre' })
    await waitFor(() => expect(bodies).toHaveLength(2))
    expect(bodies[1]).toEqual({ body: 'ping' })
  })
})
