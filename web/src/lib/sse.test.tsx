// Verifies the module-level EventSource singleton: any number of mounted
// useEventStream subscribers must share ONE connection (browsers cap
// HTTP/1.1 at ~6 connections per host across all tabs — per-component
// streams exhausted the pool and page loads hung).
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useEventStream } from './sse'

class FakeEventSource {
  static instances: FakeEventSource[] = []
  static listenersByType = new Map<string, Set<(ev: MessageEvent) => void>>()
  url: string
  closed = false
  onerror: (() => void) | null = null

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  addEventListener(type: string, fn: (ev: MessageEvent) => void) {
    let set = FakeEventSource.listenersByType.get(type)
    if (!set) {
      set = new Set()
      FakeEventSource.listenersByType.set(type, set)
    }
    set.add(fn)
  }

  close() {
    this.closed = true
  }
}

describe('useEventStream singleton', () => {
  beforeEach(() => {
    FakeEventSource.instances = []
    FakeEventSource.listenersByType = new Map()
    vi.stubGlobal('EventSource', FakeEventSource)
  })

  it('shares one EventSource across subscribers and closes on last unmount', () => {
    const events1: unknown[] = []
    const events2: unknown[] = []
    const h1 = renderHook(() => useEventStream((e) => events1.push(e)))
    const h2 = renderHook(() => useEventStream((e) => events2.push(e)))

    expect(FakeEventSource.instances.length).toBe(1)

    // A named event reaches BOTH subscribers through the shared source.
    const listeners = FakeEventSource.listenersByType.get('session.chat_updated')
    expect(listeners?.size).toBe(1)
    listeners?.forEach((fn) =>
      fn({ data: '{"id":1,"type":"session.chat_updated","session_id":"s1"}' } as MessageEvent),
    )
    expect(events1.length).toBe(1)
    expect(events2.length).toBe(1)

    // First unmount keeps the connection; last unmount closes it.
    h1.unmount()
    expect(FakeEventSource.instances[0].closed).toBe(false)
    h2.unmount()
    expect(FakeEventSource.instances[0].closed).toBe(true)

    // A new subscriber after full teardown reconnects (fresh instance).
    const h3 = renderHook(() => useEventStream(() => {}))
    expect(FakeEventSource.instances.length).toBe(2)
    h3.unmount()
  })
})
