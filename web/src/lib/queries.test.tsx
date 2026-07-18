// Covers the msw-mocked board response shape (`{columns:{...}}`) and its
// single adapter in `useTasksBoard`, plus `useTask`/`TaskDetail` returning
// subtasks + the bound session.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { setupServer } from 'msw/node'
import type { ReactNode } from 'react'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers } from '../mocks/handlers'
import { useTask, useTasksBoard } from './queries'

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
  it('adapts the {columns:{...}} board response into per-status arrays', async () => {
    const { result } = renderHook(() => useTasksBoard('billing'), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const board = result.current.data!
    expect(board.backlog.map((t) => t.id)).toEqual([10])
    expect(board.in_progress.map((t) => t.id).sort()).toEqual([12, 13])
    expect(board.review.map((t) => t.id).sort()).toEqual([11, 14])
    expect(board.done.map((t) => t.id)).toEqual([9])
    // Not exposed on TaskBoard, but shouldn't leak in as an extra key either.
    expect(board).not.toHaveProperty('cancelled')
  })
})

describe('useTask', () => {
  it('resolves TaskDetail with subtasks and the bound orchestrator session', async () => {
    const { result } = renderHook(() => useTask(12), { wrapper })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    const task = result.current.data!
    expect(task.id).toBe(12)
    expect(task.subtasks.map((t) => t.id).sort()).toEqual([13, 14])
    expect(task.session).toEqual({
      id: 's-billing-v2-orch',
      tmux_name: 'billing-v2-orch',
      attach: ['rocket', 'attach', 's-billing-v2-orch'],
    })
  })
})
