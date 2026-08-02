import { Link } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import type { Agent } from '../../lib/types'
import './agents.css'

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

export interface AgentCardProps {
  projectId: string
  agent: Agent
}

export function AgentCard({ projectId, agent }: AgentCardProps) {
  return (
    <Link to={`/p/${projectId}/agents/${agent.id}`} className="agent-card">
      <div className="agent-card__header">
        <div className="agent-card__title">
          <span
            className={
              'agent-card__dot ' +
              (agent.session_alive ? 'agent-card__dot--live' : 'agent-card__dot--idle')
            }
          />
          <span className="agent-card__name">{agent.id}</span>
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

      <div className="agent-card__footer">
        <span className="agent-card__updated">updated {timeAgo(agent.updated_at)}</span>
      </div>
    </Link>
  )
}
