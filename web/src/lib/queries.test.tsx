// Covers the msw-mocked board response shape (`{board:{...}}`) and its
// adapter in `useTasksBoard`, plus `useTask`/`TaskDetail` returning
// subtasks + the bound session.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { setupServer } from 'msw/node'
import type { ReactNode } from 'react'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { handlers } from '../mocks/handlers'
import {
  useAgentInbox,
  useAgentQuestions,
  useAgents,
  useAnswerAgentQuestion,
  useAnswerQuestion,
  useAnswerThread,
  useAskThread,
  useReplyThread,
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
  useBrainstormStats,
  useDecideGate,
  useSetOutcome,
  useTaskBrainstormStats,
  useTaskGates,
  useDeletePrice,
  usePrices,
  useSetPrice,
  useTaskUsage,
  useUsageStats,
  wireInvalidation,
  INVALIDATION_WINDOW_MS,
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
    expect(board.brainstorm.map((t) => t.id)).toEqual([17])
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

  it('useAnswerQuestion closes a thread with a 1-based `choose` and nothing else', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 })
      }),
    )
    const { result } = renderHook(() => useAnswerQuestion(), { wrapper })

    // Picking option #2 closes the thread; the daemon substitutes the option's
    // own text (internal/api/threads.go chooseOptionBody), so no body travels,
    // and nobody is left to respond, so no `to` either.
    result.current.mutate({ id: 3, choose: 2, taskId: 12 })
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ choose: 2 })
  })

  it('useAnswerAgentQuestion closes a role thread with the same `choose`', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/agent-questions/:id/answer', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 90 })
      }),
    )
    const { result } = renderHook(() => useAnswerAgentQuestion(), { wrapper })

    result.current.mutate({ id: 90, choose: 1, roleId: 'sre' })
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ choose: 1 })
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

// The v3 Questions screen drives task threads and role threads through one
// pair of controls, so the dispatch by `kind` — which URL a click reaches —
// is the thing worth pinning down.
describe('unified thread actions', () => {
  it('useAnswerThread reaches the task endpoint for a task thread', async () => {
    const seen: { url: string; body: Record<string, unknown> }[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        seen.push({ url: '/v1/questions/answer', body: (await request.json()) as never })
        return HttpResponse.json({ id: 3 })
      }),
    )
    const { result } = renderHook(() => useAnswerThread(), { wrapper })

    result.current.mutate({ ref: { id: 3, kind: 'task', taskId: 12 }, choose: 2 })
    await waitFor(() => expect(seen).toHaveLength(1))
    expect(seen[0]).toEqual({ url: '/v1/questions/answer', body: { choose: 2 } })
  })

  it('useAnswerThread reaches the agent endpoint for a role thread', async () => {
    const seen: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/agent-questions/:id/answer', async ({ request }) => {
        seen.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 90 })
      }),
    )
    const { result } = renderHook(() => useAnswerThread(), { wrapper })

    result.current.mutate({ ref: { id: 90, kind: 'role', roleId: 'sre' }, dismiss: true })
    await waitFor(() => expect(seen).toHaveLength(1))
    expect(seen[0]).toEqual({ dismiss: true })
  })

  it('useReplyThread keeps `to` optional on both kinds', async () => {
    const task: Record<string, unknown>[] = []
    const role: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/reply', async ({ request }) => {
        task.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 }, { status: 201 })
      }),
      http.post('/v1/agent-questions/:id/reply', async ({ request }) => {
        role.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 90 }, { status: 201 })
      }),
    )
    const { result } = renderHook(() => useReplyThread(), { wrapper })

    result.current.mutate({ ref: { id: 3, kind: 'task', taskId: 12 }, body: 'more?', to: ['cto'] })
    await waitFor(() => expect(task).toHaveLength(1))
    expect(task[0]).toEqual({ body: 'more?', to: ['cto'] })

    result.current.mutate({ ref: { id: 90, kind: 'role', roleId: 'sre' }, body: 'more?', to: [] })
    await waitFor(() => expect(role).toHaveLength(1))
    expect(role[0]).toEqual({ body: 'more?' })
  })

  it('useAskThread opens a task thread on an orchestrator and a role thread on an agent', async () => {
    const task: Record<string, unknown>[] = []
    const role: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/tasks/:id/questions', async ({ request }) => {
        task.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 7 }, { status: 201 })
      }),
      http.post('/v1/agents/:id/questions', async ({ request }) => {
        role.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 8 }, { status: 201 })
      }),
    )
    const { result } = renderHook(() => useAskThread(), { wrapper })

    result.current.mutate({ target: { kind: 'task', id: 12 }, body: 'decide this' })
    await waitFor(() => expect(task).toHaveLength(1))
    // A plain question carries no `type`: the daemon defaults to `decision`.
    expect(task[0]).toEqual({ body: 'decide this' })

    result.current.mutate({ target: { kind: 'role', id: 'sre' }, body: 'heads up', type: 'fyi' })
    await waitFor(() => expect(role).toHaveLength(1))
    expect(role[0]).toEqual({ body: 'heads up', type: 'fyi' })
  })
})

describe('brainstorm (task #4901)', () => {
  it('useAnswerQuestion sends a storm comment next to `choose`, and only when there is one', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 3 })
      }),
    )
    const { result } = renderHook(() => useAnswerQuestion(), { wrapper })

    result.current.mutate({ id: 3, choose: 2, body: 'но без кэша', taskId: 12 })
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ choose: 2, body: 'но без кэша' })

    result.current.mutate({ id: 3, choose: 1, body: '   ', taskId: 12 })
    await waitFor(() => expect(bodies).toHaveLength(2))
    expect(bodies[1]).toEqual({ choose: 1 })
  })

  it('useAnswerThread carries the same optional comment with `choose`', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/questions/:id/answer', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ id: 41 })
      }),
    )
    const { result } = renderHook(() => useAnswerThread(), { wrapper })
    result.current.mutate({ ref: { id: 41, kind: 'task', taskId: 40 }, choose: 1, body: 'ок' })
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ choose: 1, body: 'ок' })
  })

  it('useTaskGates unwraps {gates} newest first', async () => {
    const { result } = renderHook(() => useTaskGates(17), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.map((g) => g.status)).toEqual(['pending', 'changes'])
  })

  it('useDecideGate posts the decision and comment', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.post('/v1/gates/:id/decide', async ({ request, params }) => {
        bodies.push({ id: params.id, ...((await request.json()) as Record<string, unknown>) })
        return HttpResponse.json({ id: 2 })
      }),
    )
    const { result } = renderHook(() => useDecideGate(), { wrapper })
    result.current.mutate({ gateId: 2, taskId: 40, decision: 'changes', comment: 'добавь метрику' })
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ id: '2', decision: 'changes', comment: 'добавь метрику' })
  })

  it('useSetOutcome patches the outcome of a task thread', async () => {
    const bodies: Record<string, unknown>[] = []
    server.use(
      http.patch('/v1/questions/:id/outcome', async ({ request, params }) => {
        bodies.push({ id: params.id, ...((await request.json()) as Record<string, unknown>) })
        return HttpResponse.json({ id: 40 })
      }),
    )
    const { result } = renderHook(() => useSetOutcome(), { wrapper })
    result.current.mutate({ id: 40, taskId: 40, outcome: 'corrected' })
    await waitFor(() => expect(bodies).toHaveLength(1))
    expect(bodies[0]).toEqual({ id: '40', outcome: 'corrected' })
  })

  it('useTaskBrainstormStats and useBrainstormStats read the stats endpoints', async () => {
    const one = renderHook(() => useTaskBrainstormStats(17), { wrapper })
    await waitFor(() => expect(one.result.current.isSuccess).toBe(true))
    expect(one.result.current.data?.task_id).toBe(17)

    const all = renderHook(() => useBrainstormStats(), { wrapper })
    await waitFor(() => expect(all.result.current.isSuccess).toBe(true))
    expect(all.result.current.data?.storms.length).toBeGreaterThan(0)
  })

  it('task.* events refresh the brainstorm metric', () => {
    vi.useFakeTimers()
    try {
      const queryClient = new QueryClient()
      const spy = vi.spyOn(queryClient, 'invalidateQueries')
      wireInvalidation(queryClient)({ id: 1, ts: 1, type: 'task.question_outcome_set' })
      vi.advanceTimersByTime(INVALIDATION_WINDOW_MS)
      expect(spy).toHaveBeenCalledWith({ queryKey: ['stats'] }, { cancelRefetch: false })
    } finally {
      vi.useRealTimers()
    }
  })
})

// A burst of SSE events must not turn into a burst of requests: every event
// used to cancel the in-flight /v1/tasks and send a new one, and the daemon —
// which does not notice a client hanging up — kept running all of them.
describe('wireInvalidation coalescing', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('collapses a burst of events into one invalidation per key', () => {
    const queryClient = new QueryClient()
    const spy = vi.spyOn(queryClient, 'invalidateQueries')
    const invalidate = wireInvalidation(queryClient)
    for (let i = 0; i < 50; i++) invalidate({ id: i, ts: i, type: 'task.status_changed' })
    expect(spy).not.toHaveBeenCalled()

    vi.advanceTimersByTime(INVALIDATION_WINDOW_MS)
    const tasksCalls = spy.mock.calls.filter(([f]) => JSON.stringify(f?.queryKey) === '["tasks"]')
    expect(tasksCalls).toHaveLength(1)
  })

  it('never cancels a refetch that is already in flight', () => {
    const queryClient = new QueryClient()
    const spy = vi.spyOn(queryClient, 'invalidateQueries')
    wireInvalidation(queryClient)({ id: 1, ts: 1, type: 'pr.state_changed' })
    vi.advanceTimersByTime(INVALIDATION_WINDOW_MS)
    expect(spy.mock.calls.length).toBeGreaterThan(0)
    for (const [, opts] of spy.mock.calls) expect(opts).toEqual({ cancelRefetch: false })
  })

  it('a later burst gets its own invalidation', () => {
    const queryClient = new QueryClient()
    const spy = vi.spyOn(queryClient, 'invalidateQueries')
    const invalidate = wireInvalidation(queryClient)
    invalidate({ id: 1, ts: 1, type: 'message.queued' })
    vi.advanceTimersByTime(INVALIDATION_WINDOW_MS)
    invalidate({ id: 2, ts: 2, type: 'message.queued' })
    vi.advanceTimersByTime(INVALIDATION_WINDOW_MS)
    expect(spy).toHaveBeenCalledTimes(2)
  })

  it('ignores session.chat_updated — it fires on every tick of a talking agent', () => {
    const queryClient = new QueryClient()
    const spy = vi.spyOn(queryClient, 'invalidateQueries')
    wireInvalidation(queryClient)({ id: 1, ts: 1, type: 'session.chat_updated' })
    vi.advanceTimersByTime(INVALIDATION_WINDOW_MS)
    expect(spy).not.toHaveBeenCalled()
  })
})

describe('usage (task #5138)', () => {
  it('useUsageStats sends the period and project to /v1/stats/usage', async () => {
    const urls: string[] = []
    server.use(
      http.get('/v1/stats/usage', ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({
          from: '2026-09-01', to: '2026-09-30', pending: 0, models: [], tasks: [],
          totals: { sessions: 0, cost_usd: 0, cost_partial: false,
            tokens: { input: 0, cache_write: 0, cache_read: 0, output: 0, reasoning: 0, billable: 0 } },
        })
      }),
    )
    const { result } = renderHook(
      () => useUsageStats({ from: '2026-09-01', to: '2026-09-30', project: 'billing' }),
      { wrapper },
    )
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    const url = new URL(urls[0])
    expect(url.searchParams.get('from')).toBe('2026-09-01')
    expect(url.searchParams.get('to')).toBe('2026-09-30')
    expect(url.searchParams.get('project')).toBe('billing')
  })

  it('useUsageStats omits an empty project', async () => {
    const urls: string[] = []
    server.use(
      http.get('/v1/stats/usage', ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({ from: 'a', to: 'b', pending: 0, models: [], tasks: [], totals: {} })
      }),
    )
    const { result } = renderHook(() => useUsageStats({ from: '2026-09-01', to: '2026-09-30' }), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(new URL(urls[0]).searchParams.has('project')).toBe(false)
  })

  it('useTaskUsage reads the feature usage', async () => {
    const { result } = renderHook(() => useTaskUsage(12), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.task_id).toBe(12)
    expect(result.current.data?.sessions.length).toBeGreaterThan(0)
  })

  it('usePrices unwraps {prices}, useSetPrice PUTs and useDeletePrice DELETEs by model', async () => {
    const calls: Array<{ method: string; model: string; body?: unknown }> = []
    server.use(
      http.put('/v1/stats/prices/:model', async ({ request, params }) => {
        calls.push({ method: 'PUT', model: String(params.model), body: await request.json() })
        return HttpResponse.json({ model: params.model })
      }),
      http.delete('/v1/stats/prices/:model', ({ params }) => {
        calls.push({ method: 'DELETE', model: String(params.model) })
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const prices = renderHook(() => usePrices(), { wrapper })
    await waitFor(() => expect(prices.result.current.isSuccess).toBe(true))
    expect(prices.result.current.data?.length).toBeGreaterThan(0)

    const set = renderHook(() => useSetPrice(), { wrapper })
    set.result.current.mutate({ model: 'gpt-6-sol', input: 1.25, cache_write: null, cache_read: 0.125, output: 10 })
    const del = renderHook(() => useDeletePrice(), { wrapper })
    del.result.current.mutate('claude-opus-5-5')
    await waitFor(() => expect(calls).toHaveLength(2))
    expect(calls).toContainEqual({
      method: 'PUT', model: 'gpt-6-sol', body: { input: 1.25, cache_write: null, cache_read: 0.125, output: 10 },
    })
    expect(calls).toContainEqual({ method: 'DELETE', model: 'claude-opus-5-5' })
  })

  it('usage.collected refreshes the usage screen and every task usage tab', () => {
    vi.useFakeTimers()
    try {
      const queryClient = new QueryClient()
      const spy = vi.spyOn(queryClient, 'invalidateQueries')
      wireInvalidation(queryClient)({ id: 1, ts: 1, type: 'usage.collected', session_id: 's1', data: { task_id: 12 } })
      vi.advanceTimersByTime(INVALIDATION_WINDOW_MS)
      expect(spy).toHaveBeenCalledWith({ queryKey: ['usage'] }, { cancelRefetch: false })
      expect(spy).toHaveBeenCalledWith({ queryKey: ['taskUsage'] }, { cancelRefetch: false })
    } finally {
      vi.useRealTimers()
    }
  })

  it('session, PR and task events refresh the usage views that show them', () => {
    vi.useFakeTimers()
    try {
      const keysFor = (type: string) => {
        const queryClient = new QueryClient()
        const spy = vi.spyOn(queryClient, 'invalidateQueries')
        wireInvalidation(queryClient)({ id: 1, ts: 1, type })
        vi.advanceTimersByTime(INVALIDATION_WINDOW_MS)
        return spy.mock.calls.map(([f]) => JSON.stringify(f?.queryKey))
      }
      // A new worker's "running" row and its PR state live in the task Usage tab.
      expect(keysFor('session.spawned')).toContain('["taskUsage"]')
      expect(keysFor('pr.merged')).toContain('["taskUsage"]')
      // Task status shows in the By task table.
      expect(keysFor('task.status_changed')).toContain('["usage"]')
      expect(keysFor('session.chat_updated')).toEqual([])
    } finally {
      vi.useRealTimers()
    }
  })
})
