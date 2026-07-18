// Unit tests for the pure WS URL / control-message helpers used by
// TermPanel. Real xterm.js + WebSocket wiring is exercised manually
// (docs/03-daemon-api.md term WS) and end-to-end in Task 15; jsdom has no
// usable WebSocket/canvas story for that, so we keep this file to the
// helpers, which are the only logic worth unit-testing here.

import { afterEach, describe, expect, it } from 'vitest'
import { encodeResize, termUrl } from './TermPanel'

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
