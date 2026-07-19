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
import { UnicodeGraphemesAddon } from '@xterm/addon-unicode-graphemes'
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

/**
 * Concrete font-family string handed to xterm.js — deliberately NOT the
 * `--font-mono` CSS custom property (see tokens.css). xterm's internal
 * char-size measurement ultimately calls canvas `ctx.font = ...`, and the
 * Canvas 2D API cannot resolve `var(...)` tokens: handing it
 * `var(--font-mono, ...)` makes the browser silently fall back to its
 * default canvas font for glyph-atlas measurement while xterm still lays
 * out cells assuming the intended metrics. The DOM renderer masked this
 * (real DOM text nodes resolve custom properties normally via the
 * cascade), but the WebGL addon (058bdf7) measures via canvas and
 * regressed to unusable tiny glyphs with huge letter-spacing the moment
 * it was enabled. Also omits a leading generic family (`ui-monospace`)
 * since generic families aren't guaranteed to resolve to a concrete font
 * for canvas measurement either — list concrete faces first, matching
 * tokens.css's `--font-mono` stack.
 */
const TERMINAL_FONT = '"SFMono-Regular", Menlo, Monaco, "Courier New", monospace'

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
 * Loads the grapheme-cluster-aware Unicode width provider ('15-graphemes')
 * into `term` and activates it.
 *
 * Root cause this fixes (byte-level proof, round 6 debug session): tmux
 * computes character cell widths via utf8proc, which gives VS16
 * emoji-presentation sequences — a base char followed by U+FE0F, e.g.
 * Claude Code's spinner ✳️ (U+2733 U+FE0F) — width 2. xterm's per-codepoint
 * width tables (both the default Unicode 6 and the Unicode11 addon used
 * previously) give U+2733 width 1 and U+FE0F width 0, so the same sequence
 * occupies only 1 column in xterm. tmux keeps a per-attached-client cell
 * model and sends *minimal diffs* against it: a captured attach byte
 * stream shows tmux emitting `CSI row;3 H` — cursor to column 3, skipping
 * columns 1-2 it believes still hold the unchanged 2-wide spinner — before
 * rewriting the rest of a spinner line. In xterm the spinner only covered
 * column 1, so column 2 (and, as the desync compounds across scroll-region
 * redraws, columns 0-1 of other rows) keeps stale glyphs from earlier
 * frames until something forces a full repaint. That is exactly the
 * "stale 1-2 letter fragments in the first columns during streaming"
 * corruption.
 *
 * The UnicodeGraphemesAddon registers a grapheme-cluster-aware provider:
 * U+FE0F *joins* the preceding base char and widens the cluster to 2
 * columns (verified: charProperties(U+FE0F, after U+2733) reports width 2,
 * and plain wide emoji like 🚀 U+1F680 stay width 2), matching tmux's
 * utf8proc model and keeping the two cell grids in lockstep.
 *
 * Requires `allowProposedApi: true` on the Terminal (activeVersion is a
 * proposed API); the caller's Terminal options set that.
 */
export function configureUnicodeGraphemes(term: Terminal): void {
  term.loadAddon(new UnicodeGraphemesAddon())
  term.unicode.activeVersion = '15-graphemes'
}

/**
 * Terminal options for the tmux-attach terminal. Exported so tests can
 * assert the invariants that keep xterm's grid byte-for-byte in lockstep
 * with tmux's per-client cell model.
 *
 * CRITICAL: `convertEol` must stay OFF (xterm's default). tmux talks to
 * its attached client with minimal diffs and optimizes cursor movement: a
 * captured attach byte stream (round 6 debug session) shows sequences
 * like `CSI 24;28 H "9" LF "10"` — write "9" at row 24 col 28, then a
 * *bare LF*, which in a real terminal means "move down one row, KEEP the
 * column", then write "10" in the same column of row 25 (tmux updating a
 * column of line numbers across consecutive rows with a 1-byte move).
 * With `convertEol: true` xterm rewrites that LF into CR+LF, resetting
 * the column to 0, so "10 — …" lands at the line start — reproducibly
 * yielding the exact "stale 1-2 char fragments in the first columns +
 * overlapped text during streaming, healed by the next full repaint"
 * corruption (verified by replaying the same captured stream into a
 * headless xterm with the flag on vs off: off matches tmux's grid
 * exactly, on corrupts). The daemon side is a real PTY, so output
 * newlines already arrive as CR+LF where a column reset is intended —
 * convertEol buys nothing here and must never come back.
 */
export function buildTerminalOptions(
  fontSize?: number
): NonNullable<ConstructorParameters<typeof Terminal>[0]> {
  return {
    fontFamily: TERMINAL_FONT,
    fontSize: fontSize ?? DEFAULT_TERM_FONT_SIZE,
    lineHeight: 1.25,
    fontWeight: 'normal',
    fontWeightBold: '600',
    theme: TERMINAL_THEME,
    // Required to reach term.unicode (activeVersion is a proposed API).
    // See configureUnicodeGraphemes(): xterm's width table must match
    // tmux's utf8proc widths — including VS16 emoji-presentation
    // sequences like Claude Code's spinner ✳️ (U+2733 U+FE0F), 2 columns
    // in tmux — or tmux's minimal per-client diffs address cells xterm
    // laid out one column over.
    allowProposedApi: true,
    // JetBrains-Mono-class monospace fonts don't cover every glyph agent
    // TUIs emit (powerline separators, some emoji); a fallback-font glyph
    // wider than the cell bleeds into the next cell and overlaps
    // following text. Shrinks any overlapping glyph back to fit its cell.
    rescaleOverlappingGlyphs: true,
  }
}

// NOTE — no WebGL renderer, deliberately (round 6). Round 5 added
// @xterm/addon-webgl (058bdf7) chasing the mid-stream corruption, but the
// byte-level investigation proved that corruption lived in the GRID, not
// the paint: tmux's minimal per-client diffs desynced from xterm's cell
// model via (a) VS16 emoji widths (see configureUnicodeGraphemes) and
// (b) convertEol rewriting tmux's bare-LF cursor moves (see
// buildTerminalOptions). With both fixed the DOM renderer is pixel-clean
// under heavy streaming — verified live against the real Claude Code TUI.
// The WebGL beta addon, meanwhile, has a demonstrated devicePixelRatio
// bug: after a dpr change (browser zoom, moving the window to a
// non-retina display) it sizes its canvas backing store for the old dpr
// but paints a viewport for the new one, squeezing the whole terminal
// into a quarter of the canvas until remount. Correct > fast; the DOM
// renderer stays.

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

    const term = new Terminal(buildTerminalOptions(fontSize))
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    configureUnicodeGraphemes(term)
    term.open(container)
    // Debug handle for live-buffer inspection (used by the round-6 renderer
    // investigation and any future "is the grid right but the paint stale?"
    // question). Harmless in production.
    ;(window as unknown as { __rocketTerm?: Terminal }).__rocketTerm = term
    fitAddon.fit()
    onResizeRef.current?.(term.cols, term.rows)
    termRef.current = term
    fitAddonRef.current = fitAddon

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
