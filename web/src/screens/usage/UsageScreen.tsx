// Usage (task #5138 spec §5): tokens the agents spent in a period — by model
// and by task — with ≈ $ from the prices kept in Settings. Read-only; the
// daemon counts everything from the agents' transcripts (GET /v1/stats/usage).
// The period and project live in the query string, so a view can be linked.

import { Link, useSearchParams } from 'react-router-dom'
import { Segmented } from '../../components/Segmented'
import { useProjects, useUsageStats } from '../../lib/queries'
import type { TaskStatus, UsageModelRow, UsageTaskRow, UsageTotals } from '../../lib/types'
import { formatCost, formatTokens, presetRange, validRange } from '../../lib/usage'
import './usage.css'

const PRESETS = [
  { id: '7', label: '7d' },
  { id: '30', label: '30d' },
  { id: '90', label: '90d' },
]
const DEFAULT_DAYS = 30

const STATUS_LABEL: Record<TaskStatus, string> = {
  backlog: 'Backlog',
  brainstorm: 'Brainstorm',
  in_progress: 'In Progress',
  review: 'Review',
  done: 'Done',
  cancelled: 'Cancelled',
}

export const PRICES_PATH = '/settings?section=prices'

/** "$12.40 (partial)" when some of the usage has no price. */
export function costText(cost: number | null, partial: boolean): string {
  return partial ? `${formatCost(cost)} (partial)` : formatCost(cost)
}

/** The four headline cards, shared with the task Usage tab. */
export function TotalsCards({ totals, sessionsHint }: { totals: UsageTotals; sessionsHint: string }) {
  const cards = [
    { label: 'Tokens', value: formatTokens(totals.tokens.billable), hint: 'input + cache write + output' },
    { label: 'Cache read', value: formatTokens(totals.tokens.cache_read), hint: 'not in Tokens' },
    { label: '≈ Cost', value: costText(totals.cost_usd, totals.cost_partial), hint: 'by the prices in Settings' },
    { label: 'Sessions', value: String(totals.sessions), hint: sessionsHint },
  ]
  return (
    <ul className="usage-cards" aria-label="Totals">
      {cards.map((c) => (
        <li key={c.label} className="usage-card">
          <span className="usage-card__label">{c.label}</span>
          <span className="usage-card__value">{c.value}</span>
          <span className="usage-card__hint">{c.hint}</span>
        </li>
      ))}
    </ul>
  )
}

function ModelsTable({ models }: { models: UsageModelRow[] }) {
  return (
    <table className="usage-table" aria-label="By model">
      <thead>
        <tr>
          <th>Model</th>
          <th>Agent</th>
          <th className="usage-num">Sessions</th>
          <th className="usage-num">Tokens</th>
          <th className="usage-num">Input</th>
          <th className="usage-num">Cache write</th>
          <th className="usage-num">Cache read</th>
          <th className="usage-num">Output</th>
          <th className="usage-num">≈ $</th>
        </tr>
      </thead>
      <tbody>
        {models.map((m) => (
          <tr key={`${m.agent}/${m.model}`}>
            <td className="usage-mono">{m.model}</td>
            <td>{m.agent}</td>
            <td className="usage-num">{m.sessions}</td>
            <td className="usage-num usage-strong">{formatTokens(m.tokens.billable)}</td>
            <td className="usage-num">{formatTokens(m.tokens.input)}</td>
            <td className="usage-num">{formatTokens(m.tokens.cache_write)}</td>
            <td className="usage-num usage-muted">{formatTokens(m.tokens.cache_read)}</td>
            <td className="usage-num">{formatTokens(m.tokens.output)}</td>
            <td className="usage-num">
              {m.cost_usd === null ? (
                <>
                  —{' '}
                  <Link className="usage-link" to={PRICES_PATH}>
                    Set price
                  </Link>
                </>
              ) : (
                formatCost(m.cost_usd)
              )}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function TasksTable({ tasks }: { tasks: UsageTaskRow[] }) {
  return (
    <table className="usage-table" aria-label="By task">
      <thead>
        <tr>
          <th>Task</th>
          <th>Project</th>
          <th>Status</th>
          <th className="usage-num">Sessions</th>
          <th className="usage-num">Tokens</th>
          <th className="usage-num">≈ $</th>
        </tr>
      </thead>
      <tbody>
        {tasks.map((t) => (
          <tr key={t.task_id ?? 'none'}>
            <td>
              {t.task_id === null ? (
                <span className="usage-muted">No task</span>
              ) : (
                <Link
                  className="usage-link"
                  // A task without a project is a milestone, reached outside any project.
                  to={t.project_id ? `/p/${t.project_id}/tasks/${t.task_id}?tab=usage` : `/milestones/${t.task_id}`}
                >
                  #{t.task_id} {t.title}
                </Link>
              )}
            </td>
            <td>{t.project_id}</td>
            <td>{t.status ? STATUS_LABEL[t.status] : ''}</td>
            <td className="usage-num">{t.sessions}</td>
            <td className="usage-num usage-strong">{formatTokens(t.tokens.billable)}</td>
            <td className="usage-num">{costText(t.cost_usd, t.cost_partial)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export function UsageScreen() {
  const [params, setParams] = useSearchParams()
  const fallback = presetRange(DEFAULT_DAYS)
  const from = params.get('from') ?? fallback.from
  const to = params.get('to') ?? fallback.to
  const project = params.get('project') ?? ''
  const valid = validRange(from, to)
  const { data: projects } = useProjects()
  const { data, isLoading, isError, error } = useUsageStats({ from, to, project }, valid)

  const activePreset = PRESETS.find((p) => {
    const r = presetRange(Number(p.id))
    return r.from === from && r.to === to
  })

  function update(next: { from?: string; to?: string; project?: string }) {
    const merged = { from, to, project, ...next }
    const qs = new URLSearchParams({ from: merged.from, to: merged.to })
    if (merged.project) qs.set('project', merged.project)
    setParams(qs, { replace: true })
  }

  const empty = data && data.models.length === 0 && data.tasks.length === 0

  return (
    <main className="usage">
      <h1 className="usage__title">Usage</h1>
      <p className="usage__lede">
        Tokens the agents spent, counted from their transcripts. Cache reads are shown apart and never count in
        Tokens.
      </p>

      <div className="usage-toolbar">
        <Segmented
          label="Period"
          options={PRESETS}
          activeId={activePreset?.id ?? ''}
          onChange={(id) => update(presetRange(Number(id)))}
        />
        <label className="usage-field">
          <span>From</span>
          <input
            type="date"
            aria-label="From"
            value={from}
            onChange={(e) => update({ from: e.target.value })}
          />
        </label>
        <label className="usage-field">
          <span>To</span>
          <input type="date" aria-label="To" value={to} onChange={(e) => update({ to: e.target.value })} />
        </label>
        <select
          className="usage-select"
          aria-label="Project"
          value={project}
          onChange={(e) => update({ project: e.target.value })}
        >
          <option value="">All projects</option>
          {(projects ?? []).map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </div>

      {!valid && <p className="usage-hint">Pick a start date on or before the end date.</p>}
      {valid && isLoading && <p className="usage-empty">Loading…</p>}
      {isError && (
        <p className="usage-error" role="alert">
          Could not load usage: {error instanceof Error ? error.message : 'unknown error'}
        </p>
      )}
      {valid && data && data.pending > 0 && (
        <p className="usage-banner" role="status">
          Counting history… {data.pending} sessions left
        </p>
      )}
      {valid && data &&
        (empty ? (
          <p className="usage-empty">No usage in this period</p>
        ) : (
          <>
            <TotalsCards totals={data.totals} sessionsHint="ended in this period" />
            <h2 className="usage__subtitle">By model</h2>
            <ModelsTable models={data.models} />
            <h2 className="usage__subtitle">By task</h2>
            <TasksTable tasks={data.tasks} />
          </>
        ))}
    </main>
  )
}
