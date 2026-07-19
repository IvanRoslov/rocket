// Embedded terminal panel: attaches an xterm.js instance to the daemon's
// per-session terminal WebSocket (Task 8, internal/api/term.go).
//
// Server->client binary frames are raw PTY output written straight into
// the terminal; client->server binary frames carry keystrokes/input.
// Resize is a JSON text control frame; ping/pong keeps the connection
// (and any intermediate proxy) alive.
//
// Close handling (Task 9 review fix):
//   - code 1000 (StatusNormalClosure, "session ended"): the daemon closes
//     this way once the tmux attach ends for good. Terminal state, no
//     retry — banner reads "session ended".
//   - the socket never finished opening (pre-upgrade 404/409 etc. surface
//     as onerror -> onclose without an open): counted as a handshake
//     failure. After MAX_HANDSHAKE_FAILURES consecutive failures we stop
//     retrying automatically and show "connection lost" with a manual
//     Retry button.
//   - abnormal close after a successful open (e.g. daemon restart): keeps
//     retrying with capped exponential backoff ("reconnecting…"), and the
//     backoff/failure counters reset on the next successful open.

import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { useEffect, useRef, useState } from 'react'
import '@xterm/xterm/css/xterm.css'
import './TermPanel.css'

export interface TermPanelProps {
  sessionId: string
  readonly?: boolean
  /** Called whenever the fitted terminal geometry changes (for the overlay header). */
  onResize?: (cols: number, rows: number) => void
}

const PING_INTERVAL_MS = 30_000
const RECONNECT_DELAYS_MS = [1_000, 2_000, 5_000]

/** Consecutive handshake failures (close before ever opening) before we
 * stop auto-retrying and ask the user to hit Retry. */
export const MAX_HANDSHAKE_FAILURES = 5

/** WS close code the daemon uses for a clean, final session end. */
export const NORMAL_CLOSURE_CODE = 1000

export type CloseOutcome =
  | { kind: 'ended' }
  | { kind: 'lost' }
  | { kind: 'retry'; handshakeFailures: number }

/**
 * Pure decision for how to react to the term socket closing.
 *
 * - `code === NORMAL_CLOSURE_CODE` -> terminal "ended" state, never retry.
 * - the socket closed without ever opening (handshake failure) -> bump the
 *   failure count; once it hits MAX_HANDSHAKE_FAILURES, give up ("lost").
 * - the socket closed after having opened at least once -> just retry, and
 *   reset the handshake-failure count (a fresh handshake succeeded).
 */
export function decideOnClose(
  code: number,
  openedThisAttempt: boolean,
  handshakeFailures: number
): CloseOutcome {
  if (code === NORMAL_CLOSURE_CODE) return { kind: 'ended' }
  if (openedThisAttempt) return { kind: 'retry', handshakeFailures: 0 }
  const failures = handshakeFailures + 1
  if (failures >= MAX_HANDSHAKE_FAILURES) return { kind: 'lost' }
  return { kind: 'retry', handshakeFailures: failures }
}

/** Capped exponential backoff delay (ms) for the given reconnect attempt. */
export function nextReconnectDelay(attempt: number): number {
  return RECONNECT_DELAYS_MS[Math.min(attempt, RECONNECT_DELAYS_MS.length - 1)]
}

type Banner = 'reconnecting' | 'session-ended' | 'connection-lost' | null

const BANNER_TEXT: Record<Exclude<Banner, null>, string> = {
  reconnecting: 'reconnecting…',
  'session-ended': 'session ended',
  'connection-lost': 'connection lost',
}

/** Builds the ws(s):// URL for a session's terminal WebSocket. */
export function termUrl(sessionId: string, readonly?: boolean): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const base = `${proto}//${window.location.host}/v1/sessions/${sessionId}/term`
  return readonly ? `${base}?readonly=true` : base
}

/** Encodes a resize control message per the daemon's term WS protocol. */
export function encodeResize(cols: number, rows: number): string {
  return JSON.stringify({ type: 'resize', cols, rows })
}

export function TermPanel({ sessionId, readonly, onResize }: TermPanelProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [banner, setBanner] = useState<Banner>(null)
  // Set by the effect below; the Retry button calls through this ref so a
  // retry reuses the existing terminal/socket machinery instead of
  // re-running the whole effect (which would recreate the xterm instance).
  const retryRef = useRef<() => void>(() => {})

  // `onResize` is frequently an inline callback recreated on every parent
  // render (e.g. TermOverlay). Keeping only the *latest* value in a ref lets
  // the connect-effect below omit it from its dependency array — otherwise
  // every parent re-render would tear down and reopen the WebSocket,
  // producing an endless "closed before connection established" loop and a
  // terminal that never stays open long enough to render anything.
  const onResizeRef = useRef(onResize)
  useEffect(() => {
    onResizeRef.current = onResize
  }, [onResize])

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const term = new Terminal({
      convertEol: true,
      fontFamily: 'var(--font-mono, ui-monospace, monospace)',
      fontSize: 13.5,
      theme: {
        background: '#161618',
        foreground: '#d4d4d8',
        cursor: '#16a34a',
        cursorAccent: '#161618',
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(container)
    fitAddon.fit()
    onResizeRef.current?.(term.cols, term.rows)

    let ws: WebSocket | null = null
    let pingTimer: ReturnType<typeof setInterval> | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    let attempt = 0
    let handshakeFailures = 0
    let openedThisAttempt = false
    let cancelled = false

    function clearTimers() {
      if (pingTimer !== null) {
        clearInterval(pingTimer)
        pingTimer = null
      }
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer)
        reconnectTimer = null
      }
    }

    function scheduleReconnect() {
      if (cancelled) return
      setBanner('reconnecting')
      const delay = nextReconnectDelay(attempt)
      attempt += 1
      reconnectTimer = setTimeout(connect, delay)
    }

    function connect() {
      openedThisAttempt = false
      const socket = new WebSocket(termUrl(sessionId, readonly))
      socket.binaryType = 'arraybuffer'
      ws = socket

      socket.onopen = () => {
        if (cancelled) {
          socket.close()
          return
        }
        openedThisAttempt = true
        attempt = 0
        handshakeFailures = 0
        setBanner(null)
        fitAddon.fit()
        socket.send(encodeResize(term.cols, term.rows))
        pingTimer = setInterval(() => {
          if (socket.readyState === WebSocket.OPEN) {
            socket.send(JSON.stringify({ type: 'ping' }))
          }
        }, PING_INTERVAL_MS)
      }

      socket.onmessage = (ev) => {
        if (ev.data instanceof ArrayBuffer) {
          term.write(new Uint8Array(ev.data))
        }
        // Text frames are control replies (e.g. {"type":"pong"}); nothing
        // to render for those.
      }

      socket.onclose = (ev) => {
        if (pingTimer !== null) {
          clearInterval(pingTimer)
          pingTimer = null
        }
        if (cancelled) return

        const outcome = decideOnClose(ev.code, openedThisAttempt, handshakeFailures)
        if (outcome.kind === 'ended') {
          setBanner('session-ended')
          return
        }
        if (outcome.kind === 'lost') {
          setBanner('connection-lost')
          return
        }
        handshakeFailures = outcome.handshakeFailures
        scheduleReconnect()
      }

      socket.onerror = () => {
        socket.close()
      }
    }

    retryRef.current = () => {
      if (cancelled) return
      clearTimers()
      attempt = 0
      handshakeFailures = 0
      setBanner(null)
      connect()
    }

    if (!readonly) {
      term.onData((data) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(new TextEncoder().encode(data))
        }
      })
    }

    term.onResize(({ cols, rows }) => {
      onResizeRef.current?.(cols, rows)
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(encodeResize(cols, rows))
      }
    })

    const resizeObserver = new ResizeObserver(() => {
      fitAddon.fit()
    })
    resizeObserver.observe(container)

    connect()

    return () => {
      cancelled = true
      clearTimers()
      resizeObserver.disconnect()
      ws?.close()
      term.dispose()
      retryRef.current = () => {}
    }
    // Intentionally omit `onResize` — it's read via `onResizeRef` above so
    // that a new callback identity from the parent never tears down and
    // reopens this WebSocket. Only a real identity/mode change (a different
    // session, or flipping readonly) should reconnect.
  }, [sessionId, readonly])

  return (
    <div className="term-panel">
      <div className="term-panel__xterm" ref={containerRef} />
      {banner && (
        <div className="term-panel__banner">
          {BANNER_TEXT[banner]}
          {banner === 'connection-lost' && (
            <button
              type="button"
              className="term-panel__retry"
              onClick={() => retryRef.current()}
            >
              Retry
            </button>
          )}
        </div>
      )}
    </div>
  )
}
