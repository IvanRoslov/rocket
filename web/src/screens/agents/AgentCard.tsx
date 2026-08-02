import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import type { Agent } from '../../lib/types'
import { chatPagePath } from '../chat/ChatScreen'
import { termPagePath } from '../term/TermScreen'
import './agents.css'

const COPY_FEEDBACK_MS = 1500

interface Stat {
  tone: BadgeTone
  label: string
}

/**
 * The card's signal badges, in priority order: what stops the agent working
 * (disabled), whether its session is up, what is waiting in its inbox and what
 * is waiting for YOU (a thread the agent asked about).
 *
 * Liveness comes from the daemon's `session_alive` — it watches tmux for a
 * session named after the agent (docs/10-agents.md «Живость и адопция»), so a
 * session started by hand counts exactly like one from `rocket agent start`.
 */
export function agentStats(agent: Agent): Stat[] {
  const stats: Stat[] = []
  if (!agent.enabled) stats.push({ tone: 'neutral', label: 'disabled' })
  if (agent.session_alive) stats.push({ tone: 'ok', label: '● live' })
  if (agent.unread > 0) stats.push({ tone: 'indigo', label: `${agent.unread} unread` })
  if (agent.awaiting_user > 0) stats.push({ tone: 'warn', label: 'awaiting you' })
  else if (agent.open_questions > 0) {
    stats.push({ tone: 'neutral', label: `${agent.open_questions} open Q` })
  }
  if (stats.length === 0) stats.push({ tone: 'neutral', label: 'idle' })
  return stats
}

/** What you paste into a shell to land in the agent's tmux session. */
export function attachCommand(agentId: string): string {
  return `rocket agent attach ${agentId}`
}

/** Where the card's title links — the project route inside a project, the
 *  global one otherwise (a project-less agent has no project route). */
export function agentPath(agent: Agent, projectId?: string): string {
  return projectId
    ? `/p/${projectId}/agents/${agent.id}`
    : `/agents/${encodeURIComponent(agent.id)}`
}

export interface AgentCardProps {
  /**
   * Set inside a project — the card then links into the project-scoped route.
   * Omitted in the global `/agents` view, where an agent may have no project
   * at all and its card lives at `/agents/:id`.
   */
  projectId?: string
  agent: Agent
}

export function AgentCard({ projectId, agent }: AgentCardProps) {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), COPY_FEEDBACK_MS)
    return () => clearTimeout(timer)
  }, [copied])

  async function copyAttach() {
    try {
      await navigator.clipboard.writeText(attachCommand(agent.id))
      setCopied(true)
    } catch {
      // Clipboard access can fail (permissions, non-secure context); just
      // skip the "copied" confirmation.
    }
  }

  const deadHint = 'Session is down — start the agent first'

  return (
    // Not one big <a>: the card carries its own term/chat links and a copy
    // button, and an anchor may not nest interactive content.
    <div className="agent-card">
      <div className="agent-card__header">
        <div className="agent-card__title">
          <span
            className={
              'agent-card__dot ' +
              (agent.session_alive ? 'agent-card__dot--live' : 'agent-card__dot--idle')
            }
          />
          <Link to={agentPath(agent, projectId)} className="agent-card__name">
            {agent.id}
          </Link>
        </div>
      </div>

      <div className="agent-card__desc">{agent.description || 'no description'}</div>

      <div className="agent-card__stats">
        {agentStats(agent).map((stat) => (
          <Badge key={stat.label} tone={stat.tone}>
            {stat.label}
          </Badge>
        ))}
      </div>

      {/* The agent's tmux session is named after the agent, so its session id
          IS the agent id (docs/10-agents.md «Живость и адопция») — the shared
          full-window /term and /chat pages take it as-is. */}
      <div className="agent-card__actions">
        {agent.session_alive ? (
          <a
            className="agent-card__action"
            href={termPagePath(agent.id)}
            target="_blank"
            rel="noopener noreferrer"
          >
            ▣ term
          </a>
        ) : (
          <span className="agent-card__action agent-card__action--off" title={deadHint}>
            ▣ term
          </span>
        )}
        {agent.session_alive ? (
          <a
            className="agent-card__action"
            href={chatPagePath(agent.id)}
            target="_blank"
            rel="noopener noreferrer"
          >
            💬 chat
          </a>
        ) : (
          <span className="agent-card__action agent-card__action--off" title={deadHint}>
            💬 chat
          </span>
        )}
        {/* Always available: it is what you paste into a terminal, and it is
            just as useful before you start the session. */}
        <button
          type="button"
          className="agent-card__action"
          onClick={copyAttach}
          title={attachCommand(agent.id)}
        >
          {copied ? '✓ copied' : '⧉ attach'}
        </button>
      </div>

      <div className="agent-card__footer">
        <span className="agent-card__updated">updated {timeAgo(agent.updated_at)}</span>
      </div>
    </div>
  )
}
