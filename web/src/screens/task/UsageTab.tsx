// Task card › Usage (task #5138 spec §5): what the feature's agents spent —
// the total, then one row per session (orchestrator and workers) with its
// subtask, PR, duration and tokens; the collection status sits under the role. A session that used several models
// (subagents, /model) gets a sub-row per model. Root tasks only: the daemon
// counts usage per feature (GET /v1/tasks/{id}/usage).

import { Fragment } from 'react'
import { Link } from 'react-router-dom'
import { useTaskUsage } from '../../lib/queries'
import type { UsageSession } from '../../lib/types'
import { formatCost, formatDuration, formatTokens } from '../../lib/usage'
import { TotalsCards } from '../usage/UsageScreen'
import '../usage/usage.css'

interface UsageTabProps {
  taskId: number
  taskPath: (id: number) => string
}

function StatusLabel({ session: s }: { session: UsageSession }) {
  const label = (className: string, text: string, title?: string) => (
    <div className="usage-line2">
      <span className={className} title={title} data-testid="usage-status">
        {text}
      </span>
    </div>
  )
  if (s.status === 'running') return label('usage-muted', 'running — counted when finished')
  if (s.status === 'pending') return label('usage-muted', 'pending count')
  if (s.status === 'missing') return label('usage-muted', 'no transcript')
  if (s.status === 'error') return label('usage-status--error', 'error', s.error || 'collection failed')
  if (!s.final) return label('usage-status--snapshot', 'live snapshot')
  return null
}

function SessionRows({ session: s, taskPath }: { session: UsageSession; taskPath: (id: number) => string }) {
  // No numbers to show: not collected yet, or the transcript gave nothing.
  const blank = s.status !== 'ok'
  const single = s.models.length === 1 ? s.models[0] : undefined
  const dash = (text: string) => (blank ? '—' : text)
  return (
    <>
      <tr data-testid={`usage-session-${s.session_id}`}>
        <td className="usage-role">
          {s.role === 'orchestrator' ? 'Orchestrator' : 'Worker'}
          <StatusLabel session={s} />
        </td>
        <td className="usage-subtask">
          {s.subtask_id !== null && (
            <Link className="usage-link" to={taskPath(s.subtask_id)}>
              #{s.subtask_id} {s.subtask_title}
            </Link>
          )}
          {s.pr_number !== null && (
            <div className="usage-line2">
              {s.pr_url ? (
                <a className="usage-link" href={s.pr_url} target="_blank" rel="noreferrer">
                  PR #{s.pr_number}
                </a>
              ) : (
                `PR #${s.pr_number}`
              )}
              {s.pr_state && <span className="usage-muted"> {s.pr_state}</span>}
            </div>
          )}
        </td>
        <td className="usage-nowrap">
          {s.agent}
          {single && <div className="usage-line2 usage-mono">{single.model}</div>}
        </td>
        <td>{s.effort}</td>
        <td className="usage-num">{formatDuration(s.duration_s)}</td>
        <td className="usage-num usage-strong">{dash(formatTokens(s.tokens.billable))}</td>
        <td className="usage-num usage-muted">{dash(formatTokens(s.tokens.cache_read))}</td>
        <td className="usage-num">{dash(formatCost(s.cost_usd))}</td>
      </tr>
      {!single &&
        s.models.map((m) => (
          <tr key={m.model} className="usage-subrow" data-testid={`usage-model-${s.session_id}`}>
            <td />
            <td />
            <td className="usage-mono usage-nowrap">{m.model}</td>
            <td />
            <td />
            <td className="usage-num">{formatTokens(m.tokens.billable)}</td>
            <td className="usage-num usage-muted">{formatTokens(m.tokens.cache_read)}</td>
            <td className="usage-num">{formatCost(m.cost_usd)}</td>
          </tr>
        ))}
    </>
  )
}

export function UsageTab({ taskId, taskPath }: UsageTabProps) {
  const { data, isLoading, isError, error } = useTaskUsage(taskId)

  if (isLoading) return <p className="usage-empty">Loading…</p>
  if (isError) {
    return (
      <p className="usage-error" role="alert">
        Could not load usage: {error instanceof Error ? error.message : 'unknown error'}
      </p>
    )
  }
  if (!data) return null
  if (data.sessions.length === 0) return <p className="usage-empty">No agent sessions yet</p>

  return (
    <div className="usage-tab">
      <TotalsCards totals={data.totals} sessionsHint="orchestrator and workers" />
      <div className="usage-scroll">
        <table className="usage-table usage-table--compact" aria-label="Sessions">
          <thead>
            <tr>
              <th>Session</th>
              <th>Subtask / PR</th>
              <th>Agent / Model</th>
              <th>Effort</th>
              <th className="usage-num">Duration</th>
              <th className="usage-num">Tokens</th>
              <th className="usage-num">Cache read</th>
              <th className="usage-num">≈ $</th>
            </tr>
          </thead>
          <tbody>
            {data.sessions.map((s) => (
              <Fragment key={s.session_id}>
                <SessionRows session={s} taskPath={taskPath} />
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
