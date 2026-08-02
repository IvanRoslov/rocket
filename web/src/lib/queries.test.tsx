// Covers the msw-mocked board response shape (`{board:{...}}`) and its
// adapter in `useTasksBoard`, plus `useTask`/`TaskDetail` returning
// subtasks + the bound session.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { setupServer } from 'msw/node'
import type { ReactNode } from 'react'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers } from '../mocks/handlers'
import {
  useAgentInbox,
  useAgentQuestions,
  useAgents,
  useSendAgentMessage,
  useStartAgent,
  useStopAgent,
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
