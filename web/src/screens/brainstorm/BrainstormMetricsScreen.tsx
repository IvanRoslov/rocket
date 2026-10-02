// Brainstorm metrics (task #4901 spec §3.2): how often the human took the
// orchestrator's recommendation, week by week and per storm skill, plus every
// storm with its counters. Read-only; the numbers are computed by the daemon
// from storm answers (GET /v1/stats/brainstorm).

import { Link } from 'react-router-dom'
import { useBrainstormStats } from '../../lib/queries'
import type { BrainstormStorm, BrainstormWeek } from '../../lib/types'
import './brainstorm-metrics.css'

// Color follows the skill, never its rank: each known skill (versions of
// our own skill included) owns a categorical slot; anything else (newer
// versions, "unknown") takes the last one.
const KNOWN_SKILLS = ['orchestrator-brainstorming@1.0', 'orchestrator-brainstorming@1.1', 'superpowers:brainstorming']
const SERIES_COLORS = ['var(--viz-series-1)', 'var(--viz-series-2)', 'var(--viz-series-3)', 'var(--viz-series-4)']

function skillColor(skill: string): string {
  const i = KNOWN_SKILLS.indexOf(skill)
  return SERIES_COLORS[i === -1 ? SERIES_COLORS.length - 1 : i]
}

function orderSkills(skills: string[]): string[] {
  const known = KNOWN_SKILLS.filter((s) => skills.includes(s))
  const rest = [...new Set(skills.filter((s) => !KNOWN_SKILLS.includes(s)))].sort()
  return [...known, ...rest]
}

function percent(accepted: number, answered: number): number {
  return Math.round((accepted / answered) * 100)
}

function WeeklyChart({ weeks }: { weeks: BrainstormWeek[] }) {
  const rows = weeks.filter((w) => w.answered > 0)
  const skills = orderSkills(rows.map((w) => w.skill))
  const weekIds = [...new Set(rows.map((w) => w.week))].sort()

  return (
    <figure className="bm-chart" aria-label="Accepted recommendations by week">
      <figcaption className="bm-chart__title">Accepted recommendations, share of answered storm questions</figcaption>
      <ul className="bm-chart__legend" aria-label="Legend">
        {skills.map((s) => (
          <li key={s}>
            <span className="bm-chart__swatch" style={{ background: skillColor(s) }} aria-hidden="true" />
            {s}
          </li>
        ))}
      </ul>
      {weekIds.length === 0 ? (
        <p className="bm-empty">No answered storm questions in this period</p>
      ) : (
        <div className="bm-chart__plot">
          <div className="bm-chart__grid" aria-hidden="true">
            <span style={{ bottom: '100%' }}>100%</span>
            <span style={{ bottom: '50%' }}>50%</span>
            <span style={{ bottom: '0%' }}>0%</span>
          </div>
          {weekIds.map((week) => (
            <div key={week} className="bm-chart__group">
              <div className="bm-chart__bars">
                {skills.map((skill) => {
                  const w = rows.find((r) => r.week === week && r.skill === skill)
                  if (!w) return <span key={skill} className="bm-chart__slot" />
                  const p = percent(w.accepted, w.answered)
                  const label = `${week} · ${skill}: ${p}% (${w.accepted} of ${w.answered})`
                  return (
                    <span key={skill} className="bm-chart__slot" aria-label={label} title={label} role="img">
                      <span
                        className="bm-chart__bar"
                        style={{ height: `${Math.max(p, 1)}%`, background: skillColor(skill) }}
                      >
                        <span className="bm-chart__value">{p}%</span>
                      </span>
                    </span>
                  )
                })}
              </div>
              <div className="bm-chart__week">{week}</div>
            </div>
          ))}
        </div>
      )}
    </figure>
  )
}

/** YYYY-MM-DD in the viewer's local time — the day the human pressed Go. */
function goDate(ts: number | null): string {
  if (ts === null) return '—'
  const d = new Date(ts * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function StormsTable({ storms }: { storms: BrainstormStorm[] }) {
  return (
    <table className="bm-table">
      <thead>
        <tr>
          <th>Task</th>
          <th>Skill</th>
          <th>Questions</th>
          <th title="Accepted / with comment / Corrected / Wrong turn">Accepted / with comment / Corrected / Wrong turn</th>
          <th>Spec changes</th>
          <th>Go</th>
        </tr>
      </thead>
      <tbody>
        {storms.map((s) => (
          <tr key={s.task_id}>
            <td>
              <Link to={`/p/${s.project_id}/tasks/${s.task_id}?tab=brainstorm`}>
                #{s.task_id} {s.title}
              </Link>
            </td>
            <td>{s.skill}</td>
            <td>{s.questions}</td>
            <td>{`${s.accepted} / ${s.accepted_with_comment} / ${s.corrected} / ${s.wrong_turn}`}</td>
            <td>{s.spec_changes}</td>
            <td>{goDate(s.go_at)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export function BrainstormMetricsScreen() {
  const { data, isLoading, isError, error } = useBrainstormStats()

  return (
    <main className="bm">
      <h1 className="bm__title">Brainstorm metrics</h1>
      <p className="bm__lede">
        How often the orchestrator's recommendation was taken — counted from storm answers, per storm skill.
      </p>
      {isLoading && <p className="bm-empty">Loading…</p>}
      {isError && (
        <p className="bm-error" role="alert">
          Could not load the metric: {error instanceof Error ? error.message : 'unknown error'}
        </p>
      )}
      {data &&
        (data.storms.length === 0 && data.weeks.length === 0 ? (
          <p className="bm-empty">No storms yet</p>
        ) : (
          <>
            <WeeklyChart weeks={data.weeks} />
            <h2 className="bm__subtitle">Storms</h2>
            <StormsTable storms={data.storms} />
          </>
        ))}
    </main>
  )
}
