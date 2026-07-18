// Embedded terminal panel: attaches an xterm.js instance to the daemon's
// per-session terminal WebSocket (Task 8, internal/api/term.go).
//
// Server->client binary frames are raw PTY output written straight into
// the terminal; client->server binary frames carry keystrokes/input.
// Resize is a JSON text control frame; ping/pong keeps the connection
// (and any intermediate proxy) alive. On an unexpected close we show a
// "reconnecting…" banner and retry with capped exponential backoff.

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
  const [reconnecting, setReconnecting] = useState(false)

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
    onResize?.(term.cols, term.rows)

    let ws: WebSocket | null = null
    let pingTimer: ReturnType<typeof setInterval> | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    let attempt = 0
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
      setReconnecting(true)
      const delay = RECONNECT_DELAYS_MS[Math.min(attempt, RECONNECT_DELAYS_MS.length - 1)]
      attempt += 1
      reconnectTimer = setTimeout(connect, delay)
    }

    function connect() {
      const socket = new WebSocket(termUrl(sessionId, readonly))
      socket.binaryType = 'arraybuffer'
      ws = socket

      socket.onopen = () => {
        if (cancelled) {
          socket.close()
          return
        }
        attempt = 0
        setReconnecting(false)
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

      socket.onclose = () => {
        if (pingTimer !== null) {
          clearInterval(pingTimer)
          pingTimer = null
        }
        if (!cancelled) scheduleReconnect()
      }

      socket.onerror = () => {
        socket.close()
      }
    }

    if (!readonly) {
      term.onData((data) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(new TextEncoder().encode(data))
        }
      })
    }

    term.onResize(({ cols, rows }) => {
      onResize?.(cols, rows)
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
    }
  }, [sessionId, readonly, onResize])

  return (
    <div className="term-panel">
      <div className="term-panel__xterm" ref={containerRef} />
      {reconnecting && <div className="term-panel__banner">reconnecting…</div>}
    </div>
  )
}
