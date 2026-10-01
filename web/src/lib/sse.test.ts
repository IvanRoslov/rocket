import { renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { resetUnauthorizedLatch } from './auth'
import { EVENT_TYPES, useEventStream } from './sse'
import type { RocketEvent } from './types'

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

describe('useEventStream', () => {
  beforeEach(() => {
    MockEventSource.instances = []
    vi.stubGlobal('EventSource', MockEventSource)
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('opens an EventSource to /v1/events/stream and listens for every known type', () => {
    const onEvent = vi.fn()
    renderHook(() => useEventStream(onEvent))

    expect(MockEventSource.instances).toHaveLength(1)
    const es = MockEventSource.instances[0]
    expect(es.url).toBe('/v1/events/stream')
    for (const type of EVENT_TYPES) {
      expect(es.listeners[type]?.length).toBe(1)
    }
  })

  it('parses event data and forwards it to the callback', () => {
    const onEvent = vi.fn()
    renderHook(() => useEventStream(onEvent))

    const es = MockEventSource.instances[0]
    const payload: RocketEvent = {
      id: 1,
      ts: 123,
      type: 'session.state_changed',
      session_id: 's-1',
      data: { state: 'running' },
    }
    es.emit('session.state_changed', payload)

    expect(onEvent).toHaveBeenCalledWith(payload)
  })

  it('reconnects 2s after an error', () => {
    const onEvent = vi.fn()
    renderHook(() => useEventStream(onEvent))

    expect(MockEventSource.instances).toHaveLength(1)
    const first = MockEventSource.instances[0]
    first.onerror?.({})
    expect(first.closed).toBe(true)

    expect(MockEventSource.instances).toHaveLength(1)
    vi.advanceTimersByTime(2000)
    expect(MockEventSource.instances).toHaveLength(2)
  })

  it('closes the stream and cancels reconnects on unmount', () => {
    const onEvent = vi.fn()
    const { unmount } = renderHook(() => useEventStream(onEvent))

    const es = MockEventSource.instances[0]
    unmount()

    expect(es.closed).toBe(true)
    vi.advanceTimersByTime(5000)
    expect(MockEventSource.instances).toHaveLength(1)
  })

  describe('error probe', () => {
    const original = window.location
    const assign = vi.fn()
    beforeEach(() => {
      assign.mockClear()
      resetUnauthorizedLatch()
      Object.defineProperty(window, 'location', { configurable: true, value: { ...original, pathname: '/', search: '', assign } })
    })
    afterEach(() => {
      Object.defineProperty(window, 'location', { configurable: true, value: original })
      resetUnauthorizedLatch()
    })
    const flush = async () => {
      for (let i = 0; i < 10; i++) await Promise.resolve()
    }

    it('redirects to /login when the status probe says 401 after a stream error', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{}', { status: 401 })))
      renderHook(() => useEventStream(vi.fn()))
      MockEventSource.instances[0].onerror?.({})
      await flush()
      expect(assign).toHaveBeenCalledWith('/login?next=%2F')
    })

    it('does not redirect when the probe gets a 503', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 503 })))
      renderHook(() => useEventStream(vi.fn()))
      MockEventSource.instances[0].onerror?.({})
      await flush()
      expect(assign).not.toHaveBeenCalled()
    })
  })
})

describe('storm events (task #4901)', () => {
  it('listens to the doc, gate and outcome events the Brainstorm tab refreshes on', () => {
    for (const type of [
      'task.doc_put',
      'task.gate_requested',
      'task.gate_decided',
      'task.gate_superseded',
      'task.question_outcome_set',
    ]) {
      expect(EVENT_TYPES).toContain(type)
    }
  })
})
