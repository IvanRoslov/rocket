// Unit tests for the pure WS URL / control-message / close-handling helpers
// used by TermPanel, plus component-level tests against a mocked
// WebSocket exercising the reconnect state machine (Task 9 review fix:
// bounded reconnect loop that respects close codes).

import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  MAX_HANDSHAKE_FAILURES,
  NORMAL_CLOSURE_CODE,
  TermPanel,
  decideOnClose,
  encodeResize,
  nextReconnectDelay,
  termUrl,
} from './TermPanel'

describe('termUrl', () => {
  const originalLocation = window.location

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
    })
  })

  function setLocation(protocol: string, host: string) {
    Object.defineProperty(window, 'location', {
      value: { protocol, host },
      writable: true,
    })
  }

  it('uses ws:// for http pages', () => {
    setLocation('http:', 'localhost:5173')
    expect(termUrl('sess-1')).toBe('ws://localhost:5173/v1/sessions/sess-1/term')
  })

  it('uses wss:// for https pages', () => {
    setLocation('https:', 'dashboard.example.com')
    expect(termUrl('sess-1')).toBe('wss://dashboard.example.com/v1/sessions/sess-1/term')
  })

  it('appends readonly=true when requested', () => {
    setLocation('http:', 'localhost:5173')
    expect(termUrl('sess-1', true)).toBe('ws://localhost:5173/v1/sessions/sess-1/term?readonly=true')
  })

  it('omits the readonly param when false or omitted', () => {
    setLocation('http:', 'localhost:5173')
    expect(termUrl('sess-1', false)).toBe('ws://localhost:5173/v1/sessions/sess-1/term')
    expect(termUrl('sess-1')).toBe('ws://localhost:5173/v1/sessions/sess-1/term')
  })
})

describe('encodeResize', () => {
  it('encodes cols/rows as a resize control message', () => {
    expect(encodeResize(80, 24)).toBe(JSON.stringify({ type: 'resize', cols: 80, rows: 24 }))
  })

  it('round-trips through JSON.parse', () => {
    const parsed = JSON.parse(encodeResize(120, 40))
    expect(parsed).toEqual({ type: 'resize', cols: 120, rows: 40 })
  })
})

describe('nextReconnectDelay', () => {
  it('follows the 1s/2s/5s backoff schedule', () => {
    expect(nextReconnectDelay(0)).toBe(1_000)
    expect(nextReconnectDelay(1)).toBe(2_000)
    expect(nextReconnectDelay(2)).toBe(5_000)
  })

  it('caps at the last delay for further attempts', () => {
    expect(nextReconnectDelay(3)).toBe(5_000)
    expect(nextReconnectDelay(50)).toBe(5_000)
  })
})

describe('decideOnClose', () => {
  it('treats a normal closure (1000) as a terminal "ended" state regardless of history', () => {
    expect(decideOnClose(NORMAL_CLOSURE_CODE, true, 0)).toEqual({ kind: 'ended' })
    expect(decideOnClose(NORMAL_CLOSURE_CODE, false, 4)).toEqual({ kind: 'ended' })
  })

  it('resets the handshake-failure count when the close follows a successful open', () => {
    expect(decideOnClose(1006, true, 3)).toEqual({ kind: 'retry', handshakeFailures: 0 })
  })

  it('increments the handshake-failure count on a close with no prior open', () => {
    expect(decideOnClose(1006, false, 0)).toEqual({ kind: 'retry', handshakeFailures: 1 })
    expect(decideOnClose(1006, false, 3)).toEqual({ kind: 'retry', handshakeFailures: 4 })
  })

  it('gives up once handshake failures reach the max', () => {
    expect(decideOnClose(1006, false, MAX_HANDSHAKE_FAILURES - 1)).toEqual({ kind: 'lost' })
    expect(decideOnClose(1006, false, MAX_HANDSHAKE_FAILURES + 10)).toEqual({ kind: 'lost' })
  })
})

// ---------------------------------------------------------------------------
// Component-level tests against a mocked WebSocket.

class MockWebSocket {
  static OPEN = 1
  static instances: MockWebSocket[] = []

  readyState = 0
  onopen: ((ev: unknown) => void) | null = null
  onclose: ((ev: { code: number; reason?: string }) => void) | null = null
  onmessage: ((ev: unknown) => void) | null = null
  onerror: ((ev: unknown) => void) | null = null
  binaryType = ''
  sent: unknown[] = []

  url: string

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  send(data: unknown) {
    this.sent.push(data)
  }

  close() {
    this.readyState = 3
  }

  // Test helpers, not part of the real WebSocket API.
  triggerOpen() {
    this.readyState = 1
    this.onopen?.({})
  }

  triggerClose(code: number, reason = '') {
    this.readyState = 3
    this.onclose?.({ code, reason })
  }
}

class MockResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

describe('TermPanel', () => {
  const originalWebSocket = window.WebSocket
  const originalResizeObserver = window.ResizeObserver
  const originalMatchMedia = window.matchMedia

  beforeEach(() => {
    vi.useFakeTimers()
    MockWebSocket.instances = []
    window.WebSocket = MockWebSocket as unknown as typeof WebSocket
    window.ResizeObserver = MockResizeObserver as unknown as typeof ResizeObserver
    // xterm.js queries matchMedia for DPR change tracking; jsdom has none.
    window.matchMedia = vi.fn().mockReturnValue({
      matches: false,
      media: '',
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as typeof window.matchMedia
  })

  afterEach(() => {
    vi.runOnlyPendingTimers()
    vi.useRealTimers()
    window.WebSocket = originalWebSocket
    window.ResizeObserver = originalResizeObserver
    window.matchMedia = originalMatchMedia
  })

  it('shows "session ended" and stops retrying when the socket closes with code 1000', () => {
    render(<TermPanel sessionId="sess-1" />)

    expect(MockWebSocket.instances).toHaveLength(1)
    act(() => {
      MockWebSocket.instances[0].triggerClose(NORMAL_CLOSURE_CODE, 'session ended')
    })

    expect(screen.getByText('session ended')).toBeInTheDocument()

    // No reconnect should be scheduled.
    act(() => {
      vi.advanceTimersByTime(60_000)
    })
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('shows "connection lost" with a Retry button after 5 consecutive handshake failures, and Retry reconnects', () => {
    render(<TermPanel sessionId="sess-1" />)

    for (let i = 0; i < MAX_HANDSHAKE_FAILURES; i++) {
      expect(MockWebSocket.instances).toHaveLength(i + 1)
      act(() => {
        MockWebSocket.instances[i].triggerClose(1006, 'abnormal')
        // Let the reconnect timer (if any) fire so the next socket is created.
        vi.runOnlyPendingTimers()
      })
    }

    expect(screen.getByText('connection lost')).toBeInTheDocument()
    const retryButton = screen.getByRole('button', { name: /retry/i })
    expect(MockWebSocket.instances).toHaveLength(MAX_HANDSHAKE_FAILURES)

    act(() => {
      retryButton.click()
    })

    expect(MockWebSocket.instances).toHaveLength(MAX_HANDSHAKE_FAILURES + 1)
    expect(screen.queryByText('connection lost')).not.toBeInTheDocument()
  })

  it('shows "reconnecting…" and keeps retrying after an abnormal close following a successful open', () => {
    render(<TermPanel sessionId="sess-1" />)

    act(() => {
      MockWebSocket.instances[0].triggerOpen()
      MockWebSocket.instances[0].triggerClose(1006, 'daemon restart')
    })

    expect(screen.getByText('reconnecting…')).toBeInTheDocument()

    act(() => {
      vi.advanceTimersByTime(1_000)
    })
    expect(MockWebSocket.instances).toHaveLength(2)
  })
})
