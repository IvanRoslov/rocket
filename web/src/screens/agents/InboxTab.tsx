// Agent inbox: what was written to the agent while its session was down
// (docs/10-agents.md «Доставка и разбор»). Read messages stay, so this also
// reads as the history of what the agent has been asked to do. Marking a
// message read is the agent's own move (`rocket inbox next`) — the dashboard
// only looks.

import { useState } from 'react'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import { useAgentInbox } from '../../lib/queries'
import type { AgentInboxMessage } from '../../lib/types'
import './agents.css'

const STATUS_TONE: Record<AgentInboxMessage['status'], BadgeTone> = {
  unread: 'warn',
  read: 'neutral',
}

const STATUS_FILTERS = ['all', 'unread', 'read'] as const

export interface InboxTabProps {
  agentId: string
}

export function InboxTab({ agentId }: InboxTabProps) {
  const [status, setStatus] = useState<(typeof STATUS_FILTERS)[number]>('all')
  const { data: messages } = useAgentInbox(agentId, status === 'all' ? undefined : status)

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

      {messages && messages.length === 0 ? (
        <div className="agent-tab__empty">
          Nothing here — every message so far reached the agent's live session.
        </div>
      ) : (
        <div>
          {messages?.map((message) => (
            <div key={message.id} className="agent-inbox__row">
              <Badge tone="neutral" mono>
                {message.from || 'you'}
              </Badge>
              <span className="agent-inbox__summary">{message.body}</span>
              <Badge tone={STATUS_TONE[message.status]}>{message.status}</Badge>
              <span className="agent-inbox__time">{timeAgo(message.created_at)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
