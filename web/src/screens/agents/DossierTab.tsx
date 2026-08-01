// Role dossier: what the role is tracking and in which state
// (docs/10-agents.md «Досье»). States are the role's own notebook, not a
// daemon state machine, so the filter offers the conventional values but the
// column renders whatever the role wrote.

import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Badge } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import { useAgentItems } from '../../lib/queries'
import './agents.css'

const STATES = [
  'all',
  'new',
  'triaged',
  'taken',
  'deferred',
  'waiting_team',
  'in_work',
  'resolved',
  'closed',
] as const

/** Absolute local date for a snooze deadline — a relative "in 2d" would be
 * ambiguous against the `updated` column right beside it. */
function snoozeLabel(until: number): string {
  return new Date(until * 1000).toLocaleString()
}

export interface DossierTabProps {
  roleId: string
  projectId: string
}

export function DossierTab({ roleId, projectId }: DossierTabProps) {
  const [state, setState] = useState<(typeof STATES)[number]>('all')
  const { data: items } = useAgentItems(roleId, state === 'all' ? undefined : state)

  return (
    <div className="agent-dossier">
      <div className="agent-tab__toolbar">
        <label htmlFor="agent-dossier-state">State</label>
        <select
          id="agent-dossier-state"
          className="agent-tab__select"
          value={state}
          onChange={(e) => setState(e.target.value as (typeof STATES)[number])}
        >
          {STATES.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
      </div>

      {items && items.length === 0 ? (
        <div className="agent-tab__empty">The dossier is empty.</div>
      ) : (
        <table className="agent-tab__table">
          <thead>
            <tr>
              <th>Kind</th>
              <th>Ref</th>
              <th>State</th>
              <th>Note</th>
              <th>Task</th>
              <th>Snoozed until</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {items?.map((item) => (
              <tr key={item.id}>
                <td>
                  <Badge tone="neutral" mono>
                    {item.kind}
                  </Badge>
                </td>
                <td className="agent-tab__mono">{item.ref}</td>
                <td>{item.state || '—'}</td>
                <td className="agent-tab__note">{item.note || '—'}</td>
                <td>
                  {item.task_id > 0 ? (
                    <Link to={`/p/${projectId}/tasks/${item.task_id}`}>#{item.task_id}</Link>
                  ) : (
                    '—'
                  )}
                </td>
                <td>{item.snooze_until > 0 ? snoozeLabel(item.snooze_until) : '—'}</td>
                <td>{timeAgo(item.updated_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
