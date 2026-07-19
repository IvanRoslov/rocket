// Dedicated full-window terminal page (`/term/:sessionId`): the same live
// tmux attach as TermOverlay, but as its own route meant to be opened in a
// separate browser tab (the «▣ term» buttons target _blank at this page).
// Minimal dark header — session name, live cols×rows, A−/A+ font size,
// attach-copy — with «← back» instead of Close; the terminal fills the
// rest of the viewport.

import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { TermChatSwitch } from '../../components/TermChatSwitch'
import { TermPanel } from '../../components/TermPanel'
import { useSession } from '../../lib/queries'
import { useTermFontSize } from '../../lib/termFontSize'
import './TermScreen.css'

const COPY_FEEDBACK_MS = 2000

/** Builds the dashboard path for a session's dedicated terminal page. */
export function termPagePath(sessionId: string): string {
  return `/term/${encodeURIComponent(sessionId)}`
}

export function TermScreen() {
  const { sessionId } = useParams<{ sessionId: string }>()
  const navigate = useNavigate()
  const { data: session } = useSession(sessionId)
  const [geometry, setGeometry] = useState<{ cols: number; rows: number } | null>(null)
  const [copied, setCopied] = useState(false)
  const { fontSize, shrink, grow, canShrink, canGrow } = useTermFontSize()

  const name = session?.tmux_name ?? sessionId ?? ''

  useEffect(() => {
    const previous = document.title
    document.title = name ? `${name} — rocket term` : 'rocket term'
    return () => {
      document.title = previous
    }
  }, [name])

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), COPY_FEEDBACK_MS)
    return () => clearTimeout(timer)
  }, [copied])

  function handleBack() {
    // Opened in a fresh tab there is no history to go back to — fall back
    // to the task/dashboard root instead of a dead button.
    if (window.history.length > 1) {
      navigate(-1)
    } else {
      navigate('/')
    }
  }

  async function handleCopyAttach() {
    if (!session) return
    try {
      await navigator.clipboard.writeText(`rocket attach ${session.tmux_name}`)
      setCopied(true)
    } catch {
      // Clipboard access can fail (permissions, non-secure context); the
      // button simply won't show the "copied" confirmation.
    }
  }

  if (!sessionId) return null

  return (
    <div className="term-screen">
      <div className="term-screen__header">
        <button type="button" className="term-screen__back" onClick={handleBack}>
          ← back
        </button>
        <span className="term-screen__dot" />
        <span className="term-screen__name">{name}</span>
        <span className="term-screen__meta">tmux · live attach</span>
        <div className="term-screen__spacer" />
        <div className="term-screen__switch">
          <TermChatSwitch sessionId={sessionId} active="term" />
        </div>
        <span className="term-screen__geometry">
          {geometry ? `${geometry.cols}×${geometry.rows}` : '80×24'}
        </span>
        <div className="term-screen__fontsize" role="group" aria-label="Terminal font size">
          <button
            type="button"
            className="term-screen__fontsize-btn"
            onClick={shrink}
            disabled={!canShrink}
            aria-label="Decrease terminal font size"
          >
            A−
          </button>
          <button
            type="button"
            className="term-screen__fontsize-btn"
            onClick={grow}
            disabled={!canGrow}
            aria-label="Increase terminal font size"
          >
            A+
          </button>
        </div>
        <button type="button" className="term-screen__attach" onClick={handleCopyAttach}>
          attach ⧉
        </button>
      </div>
      {copied && session && (
        <div className="term-screen__copied">copied: rocket attach {session.tmux_name}</div>
      )}
      <TermPanel
        sessionId={sessionId}
        onResize={(cols, rows) => setGeometry({ cols, rows })}
        fontSize={fontSize}
      />
    </div>
  )
}
