// Terminal overlay (docs/design/Task.dc.html «TERMINAL PANEL»): a full-bleed
// dark panel showing a live tmux attach for a session, on top of the Task
// screen. Focus/Escape/backdrop-click behavior mirrors components/Modal.tsx
// — the header markup here is custom (dot, session name, live cols×rows,
// attach-copy) so we don't reuse <Modal> directly, but we keep it
// accessible the same way.

import { useCallback, useEffect, useRef, useState } from 'react'
import { DEFAULT_TERM_FONT_SIZE, TermPanel } from '../../components/TermPanel'
import './TermOverlay.css'

export interface TermOverlaySession {
  id: string
  tmux_name: string
}

export interface TermOverlayProps {
  session: TermOverlaySession
  onClose: () => void
}

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

const COPY_FEEDBACK_MS = 2000

/** localStorage key the A−/A+ control persists the chosen terminal font size under. */
const FONT_SIZE_STORAGE_KEY = 'rocket.term.fontSize'
const FONT_SIZE_STEP = 1
const FONT_SIZE_MIN = 12
const FONT_SIZE_MAX = 20

function loadStoredFontSize(): number {
  if (typeof window === 'undefined') return DEFAULT_TERM_FONT_SIZE
  const raw = window.localStorage.getItem(FONT_SIZE_STORAGE_KEY)
  const parsed = raw ? Number(raw) : NaN
  if (!Number.isFinite(parsed)) return DEFAULT_TERM_FONT_SIZE
  return Math.min(FONT_SIZE_MAX, Math.max(FONT_SIZE_MIN, parsed))
}

export function TermOverlay({ session, onClose }: TermOverlayProps) {
  const panelRef = useRef<HTMLDivElement>(null)
  const previouslyFocusedRef = useRef<HTMLElement | null>(null)
  const [geometry, setGeometry] = useState<{ cols: number; rows: number } | null>(null)
  const [copied, setCopied] = useState(false)
  const [fontSize, setFontSize] = useState<number>(loadStoredFontSize)

  useEffect(() => {
    window.localStorage.setItem(FONT_SIZE_STORAGE_KEY, String(fontSize))
  }, [fontSize])

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  useEffect(() => {
    previouslyFocusedRef.current = document.activeElement as HTMLElement | null

    const panel = panelRef.current
    const focusables = panel ? panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR) : null
    const first = focusables && focusables.length > 0 ? focusables[0] : panel
    first?.focus()

    return () => {
      previouslyFocusedRef.current?.focus()
    }
  }, [])

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), COPY_FEEDBACK_MS)
    return () => clearTimeout(timer)
  }, [copied])

  function handleKeyDownTrap(e: React.KeyboardEvent<HTMLDivElement>) {
    if (e.key !== 'Tab') return
    const panel = panelRef.current
    if (!panel) return
    const focusables = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
    if (focusables.length === 0) {
      e.preventDefault()
      panel.focus()
      return
    }
    const first = focusables[0]
    const last = focusables[focusables.length - 1]
    const active = document.activeElement as HTMLElement | null

    if (e.shiftKey) {
      if (active === first || !panel.contains(active)) {
        e.preventDefault()
        last.focus()
      }
    } else {
      if (active === last || !panel.contains(active)) {
        e.preventDefault()
        first.focus()
      }
    }
  }

  // Defense in depth: TermPanel already guards against callback-identity
  // churn internally (see TermPanel.tsx's onResizeRef), but keeping this
  // stable too means TermOverlay never contributes to unnecessary effect
  // teardown/reopen cycles.
  const handleTermResize = useCallback((cols: number, rows: number) => {
    setGeometry({ cols, rows })
  }, [])

  function handleShrinkFont() {
    setFontSize((size) => Math.max(FONT_SIZE_MIN, size - FONT_SIZE_STEP))
  }

  function handleGrowFont() {
    setFontSize((size) => Math.min(FONT_SIZE_MAX, size + FONT_SIZE_STEP))
  }

  async function handleCopyAttach() {
    const cmd = `rocket attach ${session.tmux_name}`
    try {
      await navigator.clipboard.writeText(cmd)
      setCopied(true)
    } catch {
      // Clipboard access can fail (permissions, non-secure context); the
      // button simply won't show the "copied" confirmation.
    }
  }

  return (
    <div className="term-overlay" onClick={onClose}>
      <div
        className="term-overlay__panel"
        role="dialog"
        aria-modal="true"
        aria-label={`Terminal — ${session.tmux_name}`}
        tabIndex={-1}
        ref={panelRef}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={handleKeyDownTrap}
      >
        <div className="term-overlay__header">
          <span className="term-overlay__dot" />
          <span className="term-overlay__name">{session.tmux_name}</span>
          <span className="term-overlay__meta">tmux · live attach</span>
          <div className="term-overlay__spacer" />
          <span className="term-overlay__geometry">
            {geometry ? `${geometry.cols}×${geometry.rows}` : '80×24'}
          </span>
          <div className="term-overlay__fontsize" role="group" aria-label="Terminal font size">
            <button
              type="button"
              className="term-overlay__fontsize-btn"
              onClick={handleShrinkFont}
              disabled={fontSize <= FONT_SIZE_MIN}
              aria-label="Decrease terminal font size"
            >
              A−
            </button>
            <button
              type="button"
              className="term-overlay__fontsize-btn"
              onClick={handleGrowFont}
              disabled={fontSize >= FONT_SIZE_MAX}
              aria-label="Increase terminal font size"
            >
              A+
            </button>
          </div>
          <button type="button" className="term-overlay__attach" onClick={handleCopyAttach}>
            attach ⧉
          </button>
          <button type="button" className="term-overlay__close" onClick={onClose}>
            Close ✕
          </button>
        </div>
        {copied && (
          <div className="term-overlay__copied">copied: rocket attach {session.tmux_name}</div>
        )}
        <TermPanel sessionId={session.id} onResize={handleTermResize} fontSize={fontSize} />
      </div>
    </div>
  )
}
