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
  useAgentItems,
  useAgentMemory,
  useAgentQuestions,
  useAgents,
  useTask,
  useTasksBoard,
  useUpdateAgentMemory,
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

describe('agent role queries', () => {
  it('useAgents returns the bare array of roles, filtered by project', async () => {
    const { result } = renderHook(() => useAgents('billing'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.map((a) => a.id)).toEqual(['sre', 'triage'])
    expect(result.current.data![0].awaiting_user).toBe(1)
  })

  it('useAgentInbox filters by status', async () => {
    const { result } = renderHook(() => useAgentInbox('sre', 'queued'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.map((e) => e.kind)).toEqual(['message'])
  })

  it('useAgentItems filters the dossier by state', async () => {
    const { result } = renderHook(() => useAgentItems('sre', 'deferred'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.map((i) => i.ref)).toEqual(['acme/platform#43'])
  })

  it('useAgentQuestions unwraps the {questions:[]} envelope', async () => {
    const { result } = renderHook(() => useAgentQuestions('sre'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.map((q) => q.id)).toEqual([91, 90])
  })

  it('useAgentMemory reads the index and the fact files', async () => {
    const { result } = renderHook(() => useAgentMemory('sre'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.files.map((f) => f.name)).toEqual(['platform.md'])
  })

  it('useUpdateAgentMemory defaults to writing MEMORY.md', async () => {
    const { result } = renderHook(() => useUpdateAgentMemory(), { wrapper })

    result.current.mutate({ id: 'sre', body: '- [New](new.md)\n' })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data!.index).toBe('- [New](new.md)\n')
  })
})
