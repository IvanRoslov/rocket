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
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { WebglAddon } from '@xterm/addon-webgl'
import { Terminal, type ITheme } from '@xterm/xterm'
import { useEffect, useRef, useState } from 'react'
import '@xterm/xterm/css/xterm.css'
import './TermPanel.css'

export interface TermPanelProps {
  sessionId: string
  readonly?: boolean
  /** Called whenever the fitted terminal geometry changes (for the overlay header). */
  onResize?: (cols: number, rows: number) => void
  /** Terminal font size in px (docs/design/SUMMARY.md: 13.5px min). Defaults to DEFAULT_FONT_SIZE. */
  fontSize?: number
}

const PING_INTERVAL_MS = 30_000
const RECONNECT_DELAYS_MS = [1_000, 2_000, 5_000]

/** Default terminal font size (px). Kept above the 13.5px design-doc floor
 * for readability at the panel widths TermOverlay now targets. */
export const DEFAULT_TERM_FONT_SIZE = 14

/**
 * xterm.js `Theme` — a plain object of hex/rgba strings, not CSS custom
 * properties: xterm renders to canvas, which can't resolve `var(...)` at
 * paint time, so the palette has to be literal color values here. This is
 * the single source of truth for the terminal's colors; keep it in sync
 * with the dark-surface tokens in styles/tokens.css (--dark-surface
 * #161618, --dark-text-2 #e4e4e7) and docs/design/SUMMARY.md ("терминал:
 * фон #161618, моно 13.5px, зелёный курсор"). Foreground and the ANSI
 * "bright" variants are pushed lighter/more saturated than the base tokens
 * so text and colored output stay legible on the dark background.
 */
const TERMINAL_THEME: ITheme = {
  background: '#161618',
  foreground: '#d6d6d9',
  cursor: '#22c55e',
  cursorAccent: '#161618',
  selectionBackground: 'rgba(129, 140, 248, 0.35)',
  black: '#3f3f46',
  red: '#f87171',
  green: '#4ade80',
  yellow: '#fbbf24',
  blue: '#60a5fa',
  magenta: '#c084fc',
  cyan: '#22d3ee',
  white: '#d6d6d9',
  brightBlack: '#71717a',
  brightRed: '#fca5a5',
  brightGreen: '#86efac',
  brightYellow: '#fde047',
  brightBlue: '#93c5fd',
  brightMagenta: '#d8b4fe',
  brightCyan: '#67e8f9',
  brightWhite: '#f4f4f5',
}

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

/**
 * Loads the Unicode 11 width tables into `term` and activates them.
 *
 * Root cause this fixes: tmux computes character cell widths via utf8proc
 * (Unicode-aware), so it treats most pictographic emoji (e.g. 🚀 U+1F680)
 * as occupying 2 columns. xterm.js's *default* width table implements only
 * the much older Unicode 6 rules and treats the same characters as 1
 * column wide. That single-column disagreement is enough to corrupt
 * rendering: tmux advances its cursor model 2 columns past such a glyph
 * before emitting the next one (typically a plain space), but xterm only
 * reserves 1 column for it, so xterm draws that next cell one column
 * early — landing on top of / merging into the emoji's cell instead of
 * the blank column tmux left — and the drift compounds across a line
 * (visible as glyphs glued to following text, shifted content, and
 * leftover fragments once a scroll-region redraw runs on top of it).
 * Loading Unicode 11's tables brings xterm's width computation much
 * closer to tmux's for ordinary emoji, eliminating that class of drift.
 *
 * Requires `allowProposedApi: true` on the Terminal (activeVersion is a
 * proposed API); the caller's Terminal options set that.
 */
export function configureUnicode11(term: Terminal): void {
  term.loadAddon(new Unicode11Addon())
  term.unicode.activeVersion = '11'
}

/**
 * Loads the WebGL renderer addon, falling back silently to xterm's default
 * DOM renderer if WebGL is unavailable (headless test envs, blocklisted
 * GPUs) or the context is later lost.
 *
 * Root cause this fixes: the DOM renderer draws each row as a separate DOM
 * line, so it can't tile box-drawing/block glyphs (or generally keep frames
 * pixel-aligned) cleanly across rows under fast, continuous writes — this is
 * exactly the "obryvki pervyh bukv strok, nalozheniya" (stray first-letter
 * fragments, overlapping text) symptom seen under heavy Claude Code TUI
 * output, which self-heals once output pauses and xterm catches up on a
 * clean repaint. The WebGL renderer custom-draws glyphs into a texture atlas
 * per cell instead, so frames stay connected under load. Returns the addon
 * (or null if it couldn't be loaded) so the caller can dispose it on
 * teardown.
 */
export function loadWebglAddon(term: Terminal): WebglAddon | null {
  try {
    const addon = new WebglAddon()
    addon.onContextLoss(() => {
      addon.dispose()
    })
    term.loadAddon(addon)
    return addon
  } catch {
    // WebGL unavailable (e.g. jsdom in tests, no GPU) — xterm keeps using
    // the DOM renderer.
    return null
  }
}

export function TermPanel({ sessionId, readonly, onResize, fontSize }: TermPanelProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [banner, setBanner] = useState<Banner>(null)
  // Set by the effect below; the Retry button calls through this ref so a
  // retry reuses the existing terminal/socket machinery instead of
  // re-running the whole effect (which would recreate the xterm instance).
  const retryRef = useRef<() => void>(() => {})
  // Set by the effect below so the fontSize-follow effect can resize the
  // live terminal without recreating it (and without being in the main
  // effect's dependency array, which would tear down the WebSocket).
  const termRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)

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
      fontSize: fontSize ?? DEFAULT_TERM_FONT_SIZE,
      lineHeight: 1.25,
      fontWeight: 'normal',
      fontWeightBold: '600',
      theme: TERMINAL_THEME,
      // Required to reach term.unicode below (activeVersion is a proposed
      // API). See the Unicode11Addon load just after: without it, xterm's
      // default (Unicode 6) character-width table disagrees with tmux's
      // utf8proc-based width table for glyphs introduced since — notably
      // pictographic emoji (e.g. 🚀 U+1F680) that tmux renders 2 columns
      // wide but xterm renders 1. That single-column disagreement makes
      // xterm draw the next cell (typically the space after an emoji
      // status icon) one column early, so it lands on/merges into the
      // glyph tmux already placed there instead of the blank column tmux
      // left for it — visible as glyphs glued to the following text and,
      // as the drift compounds across a scroll region redraw, stale
      // fragments and horizontal shift elsewhere on the line. Loading the
      // Unicode 11 width tables here brings xterm's width table much
      // closer to tmux's, eliminating the mismatch for ordinary emoji.
      allowProposedApi: true,
      // JetBrains-Mono-class monospace fonts don't cover every glyph agent
      // TUIs emit (powerline separators, some emoji); a fallback-font glyph
      // wider than the cell bleeds into the next cell and overlaps
      // following text. Shrinks any overlapping glyph back to fit its cell.
      rescaleOverlappingGlyphs: true,
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    configureUnicode11(term)
    term.open(container)
    fitAddon.fit()
    onResizeRef.current?.(term.cols, term.rows)
    termRef.current = term
    fitAddonRef.current = fitAddon

    // WebGL renderer — deferred one frame past term.open() (matches the
    // reference implementation) so the initial viewport/atlas sizing has
    // settled before the addon measures it. See loadWebglAddon() for why
    // this specifically targets the mid-stream corruption symptom.
    let webglAddon: WebglAddon | null = null
    let webglMounted = true
    const webglRaf = requestAnimationFrame(() => {
      if (!webglMounted) return
      webglAddon = loadWebglAddon(term)
    })

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
      webglMounted = false
      cancelAnimationFrame(webglRaf)
      try {
        webglAddon?.dispose()
      } catch {
        // addon may already be disposed via the context-loss handler
      }
      clearTimers()
      resizeObserver.disconnect()
      ws?.close()
      term.dispose()
      termRef.current = null
      fitAddonRef.current = null
      retryRef.current = () => {}
    }
    // Intentionally omit `onResize` and `fontSize` — `onResize` is read via
    // `onResizeRef` above, and `fontSize` is applied live by the effect
    // below, both so their churn never tears down and reopens this
    // WebSocket. Only a real identity/mode change (a different session, or
    // flipping readonly) should reconnect.
  }, [sessionId, readonly])

  // Applies a fontSize change to the live terminal without recreating it
  // (and thus without touching the WebSocket connection). fitAddon.fit()
  // recomputes cols/rows for the new glyph size and, if they changed, fires
  // the terminal's onResize handler above — which both updates the overlay
  // header geometry and sends the WS resize control frame, so this reuses
  // the existing resize protocol rather than duplicating it.
  useEffect(() => {
    const term = termRef.current
    const fitAddon = fitAddonRef.current
    if (!term || !fitAddon || fontSize === undefined) return
    term.options.fontSize = fontSize
    fitAddon.fit()
  }, [fontSize])

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
