import { renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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
})
