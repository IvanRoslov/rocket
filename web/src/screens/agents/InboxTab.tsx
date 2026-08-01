// Role inbox: the event queue that wakes the role (docs/10-agents.md
// «Инбокс»). Events stay after they are handled, so this doubles as the
// "what has this role been asked to do" history.

import { useState } from 'react'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import { useAgentInbox } from '../../lib/queries'
import type { AgentInboxEvent } from '../../lib/types'
import './agents.css'

const STATUS_TONE: Record<AgentInboxEvent['status'], BadgeTone> = {
  queued: 'warn',
  delivered: 'indigo',
  done: 'neutral',
}

/**
 * One-line human summary of an inbox event. The payload shapes come from the
 * event producers: the wake API (`{text, from}`), the GitHub poller
 * (`{repo, number, title}`), the tasks layer (`{task_id, title, from, to}`)
 * and the Q&A threads (`{question_id, ordinal, entry, text}`).
 */
export function summarizeEvent(event: AgentInboxEvent): string {
  const p = (event.payload ?? {}) as Record<string, string | number | undefined>
  switch (event.kind) {
    case 'message':
      return p.from ? `${p.from}: ${p.text ?? ''}` : String(p.text ?? 'message')
    case 'issue_opened':
    case 'issue_comment':
      return `${p.repo}#${p.number}${p.title ? ` — ${p.title}` : ''}`
    case 'task_update':
      return `#${p.task_id} ${p.title ?? ''}: ${p.from} → ${p.to}`
    case 'question':
      return `Q${p.ordinal} ${p.entry ?? ''}: ${p.text ?? ''}`
    case 'snooze_expired':
      return `snooze expired: ${p.ref ?? ''}`
    case 'cron':
      return 'cron tick'
    case 'terminal_opened':
      return 'terminal opened'
    default:
      return event.kind
  }
}

const STATUS_FILTERS = ['all', 'queued', 'delivered', 'done'] as const

export interface InboxTabProps {
  roleId: string
}

export function InboxTab({ roleId }: InboxTabProps) {
  const [status, setStatus] = useState<(typeof STATUS_FILTERS)[number]>('all')
  const { data: events } = useAgentInbox(roleId, status === 'all' ? undefined : status)

  return (
    <div className="agent-inbox">
      <div className="agent-tab__toolbar">
        <label htmlFor="agent-inbox-status">Status</label>
        <select
          id="agent-inbox-status"
          className="agent-tab__select"
          value={status}
          onChange={(e) => setStatus(e.target.value as (typeof STATUS_FILTERS)[number])}
        >
          {STATUS_FILTERS.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>

      {events && events.length === 0 ? (
        <div className="agent-tab__empty">Nothing here — no event has woken this role yet.</div>
      ) : (
        <div>
          {events?.map((event) => (
            <div key={event.id} className="agent-inbox__row">
              <Badge tone="neutral" mono>
                {event.kind}
              </Badge>
              <span className="agent-inbox__summary">{summarizeEvent(event)}</span>
              <Badge tone={STATUS_TONE[event.status]}>{event.status}</Badge>
              <span className="agent-inbox__time">{timeAgo(event.created_at)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
