import { Link } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import type { Agent, Session } from '../../lib/types'
import './agents.css'

/**
 * The live run of a role, if any. Role instances are sessions of kind `agent`
 * named `<role>-run-<n>` (docs/10-agents.md) — that id is the ONLY link
 * between a run and its role, so match the prefix exactly (`sre-run-` must not
 * match `sre-x-run-1`) and require a non-terminal state.
 */
export function liveInstance(sessions: Session[] | undefined, roleId: string): Session | undefined {
  return sessions?.find(
    (s) =>
      s.kind === 'agent' &&
      s.id.startsWith(`${roleId}-run-`) &&
      (s.state === 'running' || s.state === 'spawning'),
  )
}

interface Stat {
  tone: BadgeTone
  label: string
}

/**
 * The card's signal badges, in priority order: what stops the role working
 * (disabled), what it is doing (live instance), what is waiting for it (inbox,
 * dossier) and what is waiting for YOU (an open thread the role asked about).
 */
export function agentStats(agent: Agent, instance?: Session): Stat[] {
  const stats: Stat[] = []
  if (!agent.enabled) stats.push({ tone: 'neutral', label: 'disabled' })
  if (instance) stats.push({ tone: 'ok', label: `● ${instance.id}` })
  if (agent.inbox_queued > 0) stats.push({ tone: 'indigo', label: `${agent.inbox_queued} queued` })
  if (agent.items > 0) stats.push({ tone: 'neutral', label: `${agent.items} in dossier` })
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
  instance?: Session
}

export function AgentCard({ projectId, agent, instance }: AgentCardProps) {
  return (
    <Link to={`/p/${projectId}/agents/${agent.id}`} className="agent-card">
      <div className="agent-card__header">
        <div className="agent-card__title">
          <span
            className={
              'agent-card__dot ' + (instance ? 'agent-card__dot--live' : 'agent-card__dot--idle')
            }
          />
          <span className="agent-card__name">{agent.id}</span>
        </div>
        <Badge tone="neutral" mono>
          {agent.agent}
        </Badge>
      </div>

      <div className="agent-card__subs">
        {agent.subscriptions.length > 0
          ? agent.subscriptions.map((s) => s.repo).join(', ')
          : 'no GitHub subscriptions'}
        {agent.cron && <span className="agent-card__cron"> · cron {agent.cron}</span>}
      </div>

      <div className="agent-card__stats">
        {agentStats(agent, instance).map((stat) => (
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
