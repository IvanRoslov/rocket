import { useEffect, useState, type ReactNode } from 'react'
import { Badge, type BadgeTone } from './Badge'
import { Dot, type DotState } from './Dot'
import { sessionModelLabel } from '../lib/profiles'
import { chatPagePath, termPagePath } from '../lib/sessionPaths'
import type { Session } from '../lib/types'
import './SessionCard.css'

type CardSession = Pick<Session, 'id' | 'tmux_name' | 'agent' | 'state' | 'activity' | 'profile' | 'model' | 'effort'>
const COPY_FEEDBACK_MS = 2500

function dotState(session: CardSession): DotState {
  if (session.activity) return session.activity
  switch (session.state) {
    case 'spawning':
      return 'spawning'
    case 'errored':
      return 'errored'
    case 'done':
    case 'killed':
      return 'exited'
    default:
      return 'ready'
  }
}

function badgeTone(session: CardSession): BadgeTone {
  const label = session.activity ?? session.state
  if (label === 'active' || label === 'ready' || label === 'running') return 'ok'
  if (label === 'blocked' || label === 'waiting_input') return 'warn'
  if (label === 'errored' || label === 'exited' || label === 'killed') return 'err'
  if (label === 'spawning') return 'indigo'
  return 'neutral'
}

/** The session's model-profile snapshot (task #5026); nothing for a session
 * launched without a profile. */
function ModelLabel({ session }: { session: CardSession }) {
  const label = sessionModelLabel(session)
  if (!label) return null
  return (
    <span className="session-rail__model" title={`profile: ${label}`}>
      {label}
    </span>
  )
}


/** Presentation and existing access actions only; callers own lifecycle controls. */
export function SessionCard({ session, role, repo, beforeStatus, afterStatus, actions }: {
  session: CardSession
  role: 'orchestrator' | 'worker'
  repo?: string
  beforeStatus?: ReactNode
  afterStatus?: ReactNode
  actions?: ReactNode
}) {
  const orch = role === 'orchestrator'
  const prefix = orch ? 'session-rail__orch' : 'session-rail__worker'
  const [copied, setCopied] = useState(false)
  const [copying, setCopying] = useState(false)
  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), COPY_FEEDBACK_MS)
    return () => clearTimeout(timer)
  }, [copied])

  async function copyAttach() {
    setCopied(false)
    setCopying(true)
    try {
      await navigator.clipboard.writeText(`rocket attach ${session.tmux_name}`)
      setCopied(true)
    } catch {
      // Clipboard permissions/non-secure contexts must never show success.
    } finally {
      setCopying(false)
    }
  }

  return (
    <div className={prefix}>
      <div className={`${prefix}-head`}>
        <Dot state={dotState(session)} />
        <span className="session-rail__name" title={session.tmux_name}>{session.tmux_name}</span>
      </div>
      {repo && <div className="session-rail__worker-repo" title={repo}>{repo}</div>}
      <div className={orch ? 'session-rail__orch-meta' : 'session-rail__worker-status'}>
        {orch && <span className="session-rail__orch-label">Orchestrator</span>}
        <span className="session-rail__agent" title={`agent: ${session.agent}`}>{session.agent}</span>
        <ModelLabel session={session} />
        {orch && <div className="session-rail__spacer" />}
        {beforeStatus}
        <Badge tone={badgeTone(session)}>{session.activity ?? session.state}</Badge>
        {afterStatus}
      </div>
      <div className={`${prefix}-actions`}>
        <a className={orch ? 'session-rail__term-btn' : 'session-rail__worker-term'}
          href={termPagePath(session.id)} target="_blank" rel="noopener noreferrer">▣ term</a>
        <a className={orch ? 'session-rail__chat-btn' : 'session-rail__worker-chat'}
          href={chatPagePath(session.id)} target="_blank" rel="noopener noreferrer">💬 chat</a>
        <button type="button" className={orch ? 'session-rail__attach-btn' : 'session-rail__worker-attach'}
          aria-label={`Copy attach command for ${session.tmux_name}`} disabled={copying} onClick={copyAttach}>
          {orch ? 'attach ⧉' : '⧉'}
        </button>
        {actions}
      </div>
      {copied && <div className="session-rail__copied" role="status">copied: rocket attach {session.tmux_name}</div>}
    </div>
  )
}
