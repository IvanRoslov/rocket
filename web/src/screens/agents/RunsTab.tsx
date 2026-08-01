// Runs journal: the role's instances. A role has no dedicated runs endpoint —
// an instance IS a session of kind `agent` named `<role>-run-<n>`
// (docs/10-agents.md), so this reads `GET /v1/sessions?kind=agent&all=true`
// and filters by that prefix.

import { Link } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import { useSessions } from '../../lib/queries'
import type { Session, SessionState } from '../../lib/types'
import './agents.css'

const STATE_TONE: Record<SessionState, BadgeTone> = {
  spawning: 'indigo',
  running: 'ok',
  done: 'neutral',
  killed: 'neutral',
  errored: 'err',
}

/** Runs of a role, newest first. */
export function roleRuns(sessions: Session[] | undefined, roleId: string): Session[] {
  return (sessions ?? [])
    .filter((s) => s.kind === 'agent' && s.id.startsWith(`${roleId}-run-`))
    .sort((a, b) => b.created_at - a.created_at)
}

export interface RunsTabProps {
  roleId: string
  projectId: string
}

export function RunsTab({ roleId, projectId }: RunsTabProps) {
  const { data: sessions } = useSessions({ kind: 'agent', project: projectId, all: true })
  const runs = roleRuns(sessions, roleId)

  if (runs.length === 0) {
    return <div className="agent-tab__empty">This role has not run yet.</div>
  }

  return (
    <table className="agent-tab__table">
      <thead>
        <tr>
          <th>Run</th>
          <th>State</th>
          <th>Activity</th>
          <th>Started</th>
          <th>Updated</th>
          <th />
        </tr>
      </thead>
      <tbody>
        {runs.map((run) => {
          const live = run.state === 'running' || run.state === 'spawning'
          return (
            <tr key={run.id}>
              <td className="agent-tab__mono">{run.id}</td>
              <td>
                <Badge tone={STATE_TONE[run.state]}>{run.state}</Badge>
              </td>
              <td>{run.activity ?? '—'}</td>
              <td>{timeAgo(run.created_at)}</td>
              <td>{timeAgo(run.updated_at)}</td>
              <td>
                {live && (
                  <Link to={`/term/${run.id}`} target="_blank" rel="noreferrer">
                    term ▣
                  </Link>
                )}
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
