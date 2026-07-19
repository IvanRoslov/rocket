// Unit tests for the pure WS URL / control-message / close-handling helpers
// used by TermPanel, plus component-level tests against a mocked
// WebSocket exercising the reconnect state machine (Task 9 review fix:
// bounded reconnect loop that respects close codes).

import { Terminal } from '@xterm/xterm'
import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  MAX_HANDSHAKE_FAILURES,
  NORMAL_CLOSURE_CODE,
  TermPanel,
  buildTerminalOptions,
  configureUnicodeGraphemes,
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

describe('buildTerminalOptions', () => {
  // Regression tests for the second (and final) root cause of the
  // streaming corruption: convertEol. tmux optimizes its per-client
  // diffs with bare LF as "move down one row, KEEP the column" (captured
  // attach stream: `CSI 24;28 H "9" LF "10"` — line numbers updated down
  // a column with a 1-byte move). convertEol rewrites LF into CR+LF,
  // resetting the column to 0 and smearing every such diff across the
  // line start. Replaying the same captured byte stream into a headless
  // xterm with convertEol on vs off reproduced/eliminated the corruption
  // deterministically.
  it('leaves convertEol off so bare LF keeps the column, matching real terminal semantics', () => {
    expect(buildTerminalOptions().convertEol).toBeUndefined()
  })

  it('bare LF moves down but keeps the cursor column (tmux minimal-diff invariant)', async () => {
    const term = new Terminal(buildTerminalOptions())
    // No open() needed: the write pipeline works headless in jsdom.
    await new Promise<void>((resolve) => term.write('\x1b[1;28H9\n10', () => resolve()))
    // After "9" the cursor is at col 28 (0-based); LF must keep it there
    // (moving to row 2), so "10" starts at col 28 — with convertEol it
    // would have started at col 0.
    expect(term.buffer.active.cursorY).toBe(1)
    expect(term.buffer.active.cursorX).toBe(30)
    const row2 = term.buffer.active.getLine(1)!.translateToString(false)
    expect(row2.slice(28, 30)).toBe('10')
    expect(row2.slice(0, 2)).toBe('  ')
    term.dispose()
  })

  it('keeps the tuned rendering/API options', () => {
    const opts = buildTerminalOptions(16)
    expect(opts.fontSize).toBe(16)
    expect(opts.allowProposedApi).toBe(true)
    expect(opts.rescaleOverlappingGlyphs).toBe(true)
  })
})

describe('configureUnicodeGraphemes', () => {
  // Regression tests for the tmux/xterm cell-width desync that produced
  // stale 1-2 column fragments during streaming output.
  //
  // Byte-level root cause (round 6 debug session): tmux measures cell
  // widths with utf8proc, which gives VS16 emoji-presentation sequences —
  // base char + U+FE0F, e.g. Claude Code's spinner ✳️ (U+2733 U+FE0F) —
  // width 2. xterm's Unicode 6 AND Unicode 11 tables both give U+2733
  // width 1 and U+FE0F width 0 (per-codepoint APIs can't widen a
  // cluster). tmux keeps a per-client cell model and sends minimal
  // diffs: a captured attach stream shows tmux emitting `CUP row;3`
  // (skip columns 1-2, "unchanged" ✳️) before rewriting the rest of the
  // spinner line — but in xterm the spinner only covered column 1, so
  // column 2 keeps whatever was there in a previous frame. The
  // grapheme-cluster-aware provider ('15-graphemes') joins base+VS16 and
  // reports width 2, matching tmux's model exactly.
  function stubMatchMedia() {
    // xterm.js queries matchMedia for DPR change tracking; jsdom has none.
    const originalMatchMedia = window.matchMedia
    window.matchMedia = vi.fn().mockReturnValue({
      matches: false,
      media: '',
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as typeof window.matchMedia
    return () => {
      window.matchMedia = originalMatchMedia
    }
  }

  function openTerm(): { term: Terminal; cleanup: () => void } {
    const restore = stubMatchMedia()
    const term = new Terminal({ allowProposedApi: true })
    const div = document.createElement('div')
    document.body.appendChild(div)
    term.open(div)
    return {
      term,
      cleanup: () => {
        term.dispose()
        div.remove()
        restore()
      },
    }
  }

  it('makes xterm treat VS16 emoji sequences (✳️ U+2733 U+FE0F) as 2 columns, matching tmux/utf8proc', async () => {
    const { term, cleanup } = openTerm()

    // Before the fix: U+2733 is width 1 and U+FE0F width 0 under both the
    // default (Unicode 6) and Unicode 11 tables — total 1 column.
    await new Promise<void>((resolve) => term.write('X✳️|', () => resolve()))
    expect(term.buffer.active.cursorX).toBe(3) // 'X' + 1-col spinner + '|'

    term.reset()
    configureUnicodeGraphemes(term)
    await new Promise<void>((resolve) => term.write('X✳️|', () => resolve()))
    expect(term.buffer.active.cursorX).toBe(4) // 'X' + 2-col spinner + '|'

    cleanup()
  })

  it('keeps plain wide emoji (🚀 U+1F680) at 2 columns, matching tmux/utf8proc', async () => {
    const { term, cleanup } = openTerm()

    configureUnicodeGraphemes(term)
    await new Promise<void>((resolve) => term.write('X\u{1F680}|', () => resolve()))
    expect(term.buffer.active.cursorX).toBe(4) // 'X' + 2-col emoji + '|'

    cleanup()
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

  it('does not tear down/reopen the WebSocket when onResize is given a new identity on rerender', () => {
    const { rerender } = render(<TermPanel sessionId="sess-1" onResize={() => {}} />)

    expect(MockWebSocket.instances).toHaveLength(1)

    // Simulate a parent re-render that passes a brand-new onResize callback
    // (the TermOverlay bug: an inline arrow function recreated every render).
    // A stable connect-effect must not react to this at all.
    act(() => {
      rerender(<TermPanel sessionId="sess-1" onResize={() => {}} />)
    })
    expect(MockWebSocket.instances).toHaveLength(1)

    act(() => {
      rerender(<TermPanel sessionId="sess-1" onResize={() => {}} />)
    })
    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('still reconnects when sessionId actually changes', () => {
    const { rerender } = render(<TermPanel sessionId="sess-1" onResize={() => {}} />)
    expect(MockWebSocket.instances).toHaveLength(1)

    act(() => {
      rerender(<TermPanel sessionId="sess-2" onResize={() => {}} />)
    })
    expect(MockWebSocket.instances).toHaveLength(2)
  })
})
