// Right rail of a milestone card: where a project task has an orchestrator
// and its workers, a milestone has one persistent agent — and one tmux
// session serving all of its milestones (docs/10-agents.md «Живость и
// адопция», task #1023 spec v2). Reuses the session rail's styling so the
// two screens read as one.

import { useEffect, useState } from 'react'
import { Badge } from '../../components/Badge'
import { Dot } from '../../components/Dot'
import { useAgent } from '../../lib/queries'
import { attachCommand } from '../agents/AgentCard'
import { chatPagePath } from '../chat/ChatScreen'
import { termPagePath } from '../term/TermScreen'
import '../task/SessionRail.css'

const COPY_FEEDBACK_MS = 2500

export interface AgentRailProps {
  /** The holding agent's id, or undefined while nobody has taken the milestone. */
  agentId?: string
}

export function AgentRail({ agentId }: AgentRailProps) {
  const { data: agent } = useAgent(agentId)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), COPY_FEEDBACK_MS)
    return () => clearTimeout(timer)
  }, [copied])

  async function copyAttach() {
    if (!agentId) return
    try {
      await navigator.clipboard.writeText(attachCommand(agentId))
      setCopied(true)
    } catch {
      // Clipboard access can fail (permissions, non-secure context).
    }
  }

  if (!agentId) {
    return (
      <aside className="session-rail">
        <div className="session-rail__heading">Agent</div>
        <p className="session-rail__empty">Not taken — assign an agent from the board.</p>
      </aside>
    )
  }

  const live = agent?.session_alive === true

  return (
    <aside className="session-rail">
      <div className="session-rail__heading">Agent</div>
      <div className="session-rail__orch">
        <div className="session-rail__orch-head">
          <Dot state={live ? 'ready' : 'exited'} />
          <span className="session-rail__name" title={agentId}>
            {agentId}
          </span>
        </div>
        <div className="session-rail__orch-meta">
          <span className="session-rail__orch-label">Persistent agent</span>
          <div className="session-rail__spacer" />
          <Badge tone={live ? 'ok' : 'neutral'}>{live ? 'session live' : 'session down'}</Badge>
        </div>
        <div className="session-rail__orch-actions">
          <a
            className="session-rail__term-btn"
            href={termPagePath(agentId)}
            target="_blank"
            rel="noopener noreferrer"
          >
            ▣ term
          </a>
          <a
            className="session-rail__chat-btn"
            href={chatPagePath(agentId)}
            target="_blank"
            rel="noopener noreferrer"
          >
            💬 chat
          </a>
          <button
            type="button"
            className="session-rail__attach-btn"
            onClick={copyAttach}
            title={attachCommand(agentId)}
          >
            {copied ? '✓ copied' : '⧉ attach'}
          </button>
        </div>
      </div>
    </aside>
  )
}
