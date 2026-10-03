// Task card › Usage (task #5138 spec §5): what the feature's agents spent —
// the total, then one row per session (orchestrator and workers) with its
// subtask, PR, duration and tokens. A session that used several models
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

/** Nothing collected yet: a live session is counted when it ends. */
function isRunning(s: UsageSession): boolean {
  return s.status === ''
}

function StatusLabel({ session: s }: { session: UsageSession }) {
  if (isRunning(s)) return <span className="usage-muted">running — counted when finished</span>
  if (s.status === 'missing') return <span className="usage-muted">no transcript</span>
  if (s.status === 'error') {
    return (
      <span className="usage-status--error" title={s.error || 'collection failed'}>
        error
      </span>
    )
  }
  if (!s.final) return <span className="usage-status--snapshot">live snapshot</span>
  return null
}

function SessionRows({ session: s, taskPath }: { session: UsageSession; taskPath: (id: number) => string }) {
  const running = isRunning(s)
  const single = s.models.length === 1 ? s.models[0] : undefined
  const dash = (text: string) => (running ? '—' : text)
  return (
    <>
      <tr data-testid={`usage-session-${s.session_id}`}>
        <td>{s.role === 'orchestrator' ? 'Orchestrator' : 'Worker'}</td>
        <td>
          {s.subtask_id !== null && (
            <Link className="usage-link" to={taskPath(s.subtask_id)}>
              #{s.subtask_id} {s.subtask_title}
            </Link>
          )}
        </td>
        <td className="usage-nowrap">
          {s.pr_number !== null && (
            <>
              {s.pr_url ? (
                <a className="usage-link" href={s.pr_url} target="_blank" rel="noreferrer">
                  #{s.pr_number}
                </a>
              ) : (
                `#${s.pr_number}`
              )}
              {s.pr_state && <span className="usage-muted"> {s.pr_state}</span>}
            </>
          )}
        </td>
        <td>
          {s.agent}
          {single && <span className="usage-mono"> · {single.model}</span>}
        </td>
        <td>{s.effort}</td>
        <td className="usage-num">{formatDuration(s.duration_s)}</td>
        <td className="usage-num usage-strong">{dash(formatTokens(s.tokens.billable))}</td>
        <td className="usage-num usage-muted">{dash(formatTokens(s.tokens.cache_read))}</td>
        <td className="usage-num">{running ? '—' : formatCost(s.cost_usd)}</td>
        <td>
          <StatusLabel session={s} />
        </td>
      </tr>
      {!single &&
        s.models.map((m) => (
          <tr key={m.model} className="usage-subrow" data-testid={`usage-model-${s.session_id}`}>
            <td />
            <td />
            <td />
            <td className="usage-mono">{m.model}</td>
            <td />
            <td />
            <td className="usage-num">{formatTokens(m.tokens.billable)}</td>
            <td className="usage-num usage-muted">{formatTokens(m.tokens.cache_read)}</td>
            <td className="usage-num">{formatCost(m.cost_usd)}</td>
            <td />
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
      <table className="usage-table" aria-label="Sessions">
        <thead>
          <tr>
            <th>Role</th>
            <th>Subtask</th>
            <th>PR</th>
            <th>Agent / Model</th>
            <th>Effort</th>
            <th className="usage-num">Duration</th>
            <th className="usage-num">Tokens</th>
            <th className="usage-num">Cache read</th>
            <th className="usage-num">≈ $</th>
            <th>Status</th>
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
  )
}
