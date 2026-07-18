// Terminal overlay (docs/design/Task.dc.html «TERMINAL PANEL»): a full-bleed
// dark panel showing a live tmux attach for a session, on top of the Task
// screen. Focus/Escape/backdrop-click behavior mirrors components/Modal.tsx
// — the header markup here is custom (dot, session name, live cols×rows,
// attach-copy) so we don't reuse <Modal> directly, but we keep it
// accessible the same way.

import { useEffect, useRef, useState } from 'react'
import { TermPanel } from '../../components/TermPanel'
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

export function TermOverlay({ session, onClose }: TermOverlayProps) {
  const panelRef = useRef<HTMLDivElement>(null)
  const previouslyFocusedRef = useRef<HTMLElement | null>(null)
  const [geometry, setGeometry] = useState<{ cols: number; rows: number } | null>(null)
  const [copied, setCopied] = useState(false)

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
        <TermPanel sessionId={session.id} onResize={(cols, rows) => setGeometry({ cols, rows })} />
      </div>
    </div>
  )
}
