// "Waiting on you": every storm that needs an answer or a Go, stale ones first,
// then the longest wait first, as the daemon sorts them. A row opens the task's Brainstorm tab.

import { useState } from 'react'
import { Link } from 'react-router-dom'
import { brainstormCounts, brainstormHref, filterBrainstorms, isMidWorkSpecChange, type BrainstormFilter } from '../../lib/brainstorms'
import { timeAgo } from '../../lib/format'
import { useBrainstorms } from '../../lib/queries'
import type { BrainstormEntry } from '../../lib/types'

const STATUS: Record<string, { label: string; dot: string }> = {
  backlog: { label: 'Backlog', dot: 'var(--text-4)' },
  brainstorm: { label: 'Brainstorm', dot: 'var(--warn)' },
  in_progress: { label: 'In Progress', dot: 'var(--accent)' },
  blocked: { label: 'Blocked', dot: 'var(--err)' },
}

function statusOf(s: string): { label: string; dot: string } {
  return STATUS[s] ?? { label: s.replaceAll('_', ' '), dot: 'var(--text-4)' }
}

function Needs({ e }: { e: BrainstormEntry }) {
  return (
    <span className="brainstorms__needs">
      {e.pending_gate && (
        <span className="bs-badge bs-badge--go">{`Go · spec v${e.pending_gate.spec_version}`}</span>
      )}
      {isMidWorkSpecChange(e) && (
        <span className="bs-badge bs-badge--midwork" title="The spec changed while the task was already in work">
          mid-work
        </span>
      )}
      {e.questions_waiting > 0 && (
        <span className="bs-badge bs-badge--answer">
          {e.questions_waiting === 1 ? '1 question' : `${e.questions_waiting} questions`}
        </span>
      )}
      {e.questions_waiting > 0 && e.questions_stale && <span className="bs-badge bs-badge--stale">STALE</span>}
    </span>
  )
}

export function BrainstormList() {
  const { data, isLoading, isError } = useBrainstorms()
  const [filter, setFilter] = useState<BrainstormFilter>('all')

  if (isLoading) return <p className="brainstorms__note">Loading…</p>
  if (isError || !data) return <p className="brainstorms__error">Could not load brainstorms.</p>
  if (data.length === 0) {
    return (
      <div className="brainstorms__empty">
        <span className="brainstorms__empty-mark" aria-hidden="true">
          ✓
        </span>
        <p className="brainstorms__empty-title">No brainstorms are waiting on you</p>
        <p className="brainstorms__note">New storm questions and Go requests show up here.</p>
      </div>
    )
  }

  const counts = brainstormCounts(data)
  const rows = filterBrainstorms(data, filter)
  const filters: Array<{ id: BrainstormFilter; label: string; count: number }> = [
    { id: 'all', label: 'All', count: counts.all },
    { id: 'answer', label: 'Needs answer', count: counts.answer },
    { id: 'go', label: 'Needs Go', count: counts.go },
  ]

  return (
    <>
      <div className="segmented brainstorms__filter" role="group" aria-label="Filter">
        {filters.map((f) => {
          const active = f.id === filter
          return (
            <button
              key={f.id}
              type="button"
              aria-pressed={active}
              className={active ? 'segmented__option segmented__option--active' : 'segmented__option'}
              onClick={() => setFilter(f.id)}
            >
              {f.label}{" "}
              <span className="brainstorms__count">{f.count}</span>
            </button>
          )
        })}
      </div>

      <div className="brainstorms__table">
        <div className="brainstorms__row brainstorms__row--head">
          <span>Waiting for</span>
          <span>Task</span>
          <span>Project</span>
          <span>Status</span>
          <span className="brainstorms__right">
            Waiting
          </span>
        </div>
        {rows.length === 0 && (
          <p className="brainstorms__none">{filter === 'go' ? 'Nothing needs a Go' : 'Nothing needs an answer'}</p>
        )}
        {rows.map((e) => {
          const status = statusOf(e.task_status)
          return (
            <Link key={e.task_id} to={brainstormHref(e)} className="brainstorms__row">
              <span>
                <Needs e={e} />
              </span>
              <span className="brainstorms__task">
                <span className="brainstorms__id">#{e.task_id}</span> {e.task_title}
              </span>
              <span className="brainstorms__muted">
                {e.project_id ?? 'milestone'}
              </span>
              <span className="brainstorms__status">
                <span className="brainstorms__dot" style={{ background: status.dot }} aria-hidden="true" />
                {status.label}
              </span>
              <span className="brainstorms__right brainstorms__muted">
                {timeAgo(e.waiting_since)}
              </span>
            </Link>
          )
        })}
      </div>
    </>
  )
}
