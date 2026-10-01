import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook } from '@testing-library/react-native'
import {
  emitDaemonEvent,
  keyMatches,
  parseEventType,
  subscribeDaemonEvents,
  useEventStream,
  useSessionEvent,
} from './events'
import type { SseHandlers } from './sse'

let mockSseHandlers: SseHandlers | undefined
jest.mock('./sse', () => ({
  connectSse: (_url: string, handlers: SseHandlers) => {
    mockSseHandlers = handlers
    return { close: () => {} }
  },
}))
jest.mock('../servers/ServerContext', () => ({
  useServers: () => ({ baseUrl: 'http://daemon', authLost: false }),
}))

describe('parseEventType', () => {
  it('session events touch sessions, system, task and projects', () => {
    expect(parseEventType('session.state_changed')).toEqual(['sessions', 'system', 'task', 'projects'])
    expect(parseEventType('session.spawned')).toContain('sessions')
  })
  it('task events touch tasks and task detail', () => {
    expect(parseEventType('task.question_asked')).toEqual(['tasks', 'task', 'projects', 'threads'])
  })
  it('message events touch messages and system', () => {
    expect(parseEventType('message.delivered')).toEqual(['messages', 'system'])
  })
  it('pr events touch tasks and sessions', () => {
    expect(parseEventType('pr.merged')).toEqual(['tasks', 'task', 'sessions'])
  })
  it('chat pings do not invalidate anything', () => {
    expect(parseEventType('session.chat_updated')).toEqual([])
  })
  it('agent events touch the agents list and agent detail', () => {
    expect(parseEventType('agent.question_asked')).toEqual(['agents', 'agent', 'threads'])
    expect(parseEventType('agent.session_started')).toEqual(['agents', 'agent', 'threads'])
  })
  it('question events refresh the threads inbox', () => {
    expect(parseEventType('task.question_asked')).toContain('threads')
    expect(parseEventType('agent.question_resolved')).toContain('threads')
  })
  it('unknown events map to nothing', () => {
    expect(parseEventType('weird.thing')).toEqual([])
    expect(parseEventType('')).toEqual([])
  })
})

describe('keyMatches', () => {
  const BASE = 'http://10.0.0.5:4477'
  it('matches by second key segment', () => {
    expect(keyMatches([BASE, 'sessions', 'all'], ['sessions'])).toBe(true)
    expect(keyMatches([BASE, 'task', 12], ['tasks', 'task'])).toBe(true)
  })
  it('rejects other segments', () => {
    expect(keyMatches([BASE, 'settings'], ['sessions'])).toBe(false)
  })
  it('rejects malformed keys', () => {
    expect(keyMatches([BASE], ['sessions'])).toBe(false)
    expect(keyMatches([BASE, 42], ['sessions'])).toBe(false)
  })
})

describe('daemon event fan-out', () => {
  const frame = (type: string, sessionId: string) =>
    JSON.stringify({ id: 1, ts: 1, type, session_id: sessionId, data: {} })

  it('delivers emitted events to subscribers until they unsubscribe', () => {
    const got: string[] = []
    const off = subscribeDaemonEvents((type) => got.push(type))
    emitDaemonEvent('session.quiz_resolved', '{}')
    off()
    emitDaemonEvent('session.quiz_asked', '{}')
    expect(got).toEqual(['session.quiz_resolved'])
  })

  it('useSessionEvent fires only for its own session and event type', async () => {
    const handler = jest.fn()
    const { unmount } = await renderHook(() => useSessionEvent('w1', 'session.quiz_answer_unconfirmed', handler))
    emitDaemonEvent('session.quiz_answer_unconfirmed', frame('session.quiz_answer_unconfirmed', 'other'))
    emitDaemonEvent('session.quiz_resolved', frame('session.quiz_resolved', 'w1'))
    emitDaemonEvent('session.quiz_answer_unconfirmed', 'not json')
    expect(handler).not.toHaveBeenCalled()
    emitDaemonEvent('session.quiz_answer_unconfirmed', frame('session.quiz_answer_unconfirmed', 'w1'))
    expect(handler).toHaveBeenCalledTimes(1)
    await unmount()
    emitDaemonEvent('session.quiz_answer_unconfirmed', frame('session.quiz_answer_unconfirmed', 'w1'))
    expect(handler).toHaveBeenCalledTimes(1)
  })

  it('useEventStream forwards every stream event to subscribers', async () => {
    const got: [string, string][] = []
    const off = subscribeDaemonEvents((type, data) => got.push([type, data]))
    await renderHook(() => useEventStream(), {
      wrapper: ({ children }) => (
        <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
      ),
    })
    mockSseHandlers!.onEvent('session.quiz_answer_unconfirmed', '{"session_id":"w1"}')
    off()
    expect(got).toEqual([['session.quiz_answer_unconfirmed', '{"session_id":"w1"}']])
  })
})
