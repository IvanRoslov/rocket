// Covers docs/13-chat.md's "Жизненный цикл экрана" client contract: initial
// tail load, SSE `session.chat_updated` ping -> cursor increment, periodic
// fallback poll, and the cursor-rollback redraw policy.

import { renderHook, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { appendChatEntry, handlers, resetChatEntries } from '../mocks/handlers'
import { chatEntries } from '../mocks/fixtures'
import { useSessionChat } from './useSessionChat'

type Listener = (ev: MessageEvent) => void

class MockEventSource {
  static instances: MockEventSource[] = []
  url: string
  listeners: Record<string, Listener[]> = {}
  closed = false
  onerror: ((ev: unknown) => void) | null = null

  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
  }

  addEventListener(type: string, cb: Listener) {
    this.listeners[type] ??= []
    this.listeners[type].push(cb)
  }

  close() {
    this.closed = true
  }

  emit(type: string, data: unknown) {
    for (const cb of this.listeners[type] ?? []) {
      cb({ data: JSON.stringify(data) } as MessageEvent)
    }
  }
}

const server = setupServer(...handlers)
const SESSION_ID = 's-billing-v2-orch'

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
})
afterEach(() => {
  server.resetHandlers()
  resetChatEntries()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})
afterAll(() => server.close())

describe('useSessionChat', () => {
  it('loads the tail on mount and exposes the session card', async () => {
    const { result } = renderHook(() => useSessionChat(SESSION_ID))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.entries).toHaveLength(chatEntries[SESSION_ID].length)
    expect(result.current.entries.map((e) => e.role)).toEqual(['user', 'assistant', 'tool', 'assistant'])
    expect(result.current.session).toEqual({
      id: SESSION_ID,
      kind: 'orchestrator',
      state: 'running',
      activity: 'active',
    })
  })

  it('appends new entries on a session.chat_updated ping for this session', async () => {
    const { result } = renderHook(() => useSessionChat(SESSION_ID))
    await waitFor(() => expect(result.current.loading).toBe(false))
    const before = result.current.entries.length

    appendChatEntry(SESSION_ID, { role: 'assistant', text: 'фикс отправлен', ts: 1_800_000_100 })

    const es = MockEventSource.instances[0]
    es.emit('session.chat_updated', { id: 9, ts: 1, type: 'session.chat_updated', session_id: SESSION_ID })

    await waitFor(() => expect(result.current.entries.length).toBe(before + 1))
    expect(result.current.entries.at(-1)?.text).toBe('фикс отправлен')
  })

  it('ignores a chat_updated ping for a different session', async () => {
    const { result } = renderHook(() => useSessionChat(SESSION_ID))
    await waitFor(() => expect(result.current.loading).toBe(false))
    const before = result.current.entries.length

    appendChatEntry(SESSION_ID, { role: 'assistant', text: 'should not show up yet', ts: 1_800_000_200 })
    const es = MockEventSource.instances[0]
    es.emit('session.chat_updated', { id: 9, ts: 1, type: 'session.chat_updated', session_id: 's-billing-v2-w1' })

    // Give any accidental fetch a moment to (not) land.
    await new Promise((r) => setTimeout(r, 20))
    expect(result.current.entries.length).toBe(before)
  })

  it('picks up new entries via the fallback poll even without an SSE ping', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { result } = renderHook(() => useSessionChat(SESSION_ID))
    await vi.waitFor(() => expect(result.current.loading).toBe(false))
    const before = result.current.entries.length

    appendChatEntry(SESSION_ID, { role: 'assistant', text: 'picked up by fallback poll', ts: 1_800_000_300 })

    await vi.advanceTimersByTimeAsync(7000)
    await vi.waitFor(() => expect(result.current.entries.length).toBe(before + 1))
  })

  it('redraws the feed instead of duplicating when the cursor response re-delivers an already-shown entry', async () => {
    const { result } = renderHook(() => useSessionChat(SESSION_ID))
    await waitFor(() => expect(result.current.loading).toBe(false))
    const originalLast = result.current.entries.at(-1)

    // Simulate an invalid-cursor fallback: the daemon re-sends the fixture
    // transcript from scratch instead of only what's after our cursor.
    server.use(
      http.get('/v1/sessions/:id/chat', () =>
        HttpResponse.json({
          entries: chatEntries[SESSION_ID],
          next_cursor: '999',
          session: { id: SESSION_ID, kind: 'orchestrator', state: 'running', activity: 'active' },
        }),
      ),
    )

    const es = MockEventSource.instances[0]
    es.emit('session.chat_updated', { id: 10, ts: 2, type: 'session.chat_updated', session_id: SESSION_ID })

    await waitFor(() => expect(result.current.entries).toHaveLength(chatEntries[SESSION_ID].length))
    expect(result.current.entries.at(-1)).toEqual(originalLast)
  })
})

describe('useSessionChat quiz events', () => {
  const QUIZ_SESSION_ID = 's-quiz-demo-orch'

  it('refetches (surfacing pending_quiz) on a session.quiz_asked ping', async () => {
    const { result } = renderHook(() => useSessionChat(QUIZ_SESSION_ID))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.session?.pending_quiz).toBeDefined()

    // A quiz_asked ping for a DIFFERENT session must not touch this hook.
    const es = MockEventSource.instances[0]
    es.emit('session.quiz_asked', { id: 1, ts: 1, type: 'session.quiz_asked', session_id: 's-billing-v2-orch' })
    await new Promise((r) => setTimeout(r, 20))
    expect(result.current.session?.pending_quiz).toBeDefined()
  })

  it('clears pending_quiz once the fixture session no longer carries one, on a quiz_resolved ping', async () => {
    server.use(
      http.get('/v1/sessions/:id/chat', ({ params }) =>
        HttpResponse.json({
          entries: chatEntries[QUIZ_SESSION_ID] ?? [],
          next_cursor: '999',
          session: { id: params.id as string, kind: 'orchestrator', state: 'running', activity: 'blocked' },
        }),
      ),
    )
    const { result } = renderHook(() => useSessionChat(QUIZ_SESSION_ID))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.session?.pending_quiz).toBeUndefined()

    const es = MockEventSource.instances[0]
    es.emit('session.quiz_resolved', { id: 2, ts: 2, type: 'session.quiz_resolved', session_id: QUIZ_SESSION_ID })
    await waitFor(() => expect(result.current.session?.activity).toBe('blocked'))
    expect(result.current.session?.pending_quiz).toBeUndefined()
  })

  it('sets quizUnconfirmed on session.quiz_answer_unconfirmed and clears it once pending_quiz changes', async () => {
    const { result } = renderHook(() => useSessionChat(QUIZ_SESSION_ID))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.quizUnconfirmed).toBe(false)

    const es = MockEventSource.instances[0]
    es.emit('session.quiz_answer_unconfirmed', {
      id: 3,
      ts: 3,
      type: 'session.quiz_answer_unconfirmed',
      session_id: QUIZ_SESSION_ID,
    })
    await waitFor(() => expect(result.current.quizUnconfirmed).toBe(true))

    // pending_quiz clearing (quiz eventually resolved) clears the flag too.
    server.use(
      http.get('/v1/sessions/:id/chat', ({ params }) =>
        HttpResponse.json({
          entries: [],
          next_cursor: '999',
          session: { id: params.id as string, kind: 'orchestrator', state: 'running' },
        }),
      ),
    )
    es.emit('session.quiz_resolved', { id: 4, ts: 4, type: 'session.quiz_resolved', session_id: QUIZ_SESSION_ID })
    await waitFor(() => expect(result.current.quizUnconfirmed).toBe(false))
  })
})
