// "Waiting on you": every storm that needs an answer or a Go, stale ones first,
// then the longest wait first, as the daemon sorts them. A row opens the task's Brainstorm tab.

import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Segmented } from '../../components/Segmented'
import { brainstormCounts, brainstormHref, filterBrainstorms, isMidWorkSpecChange, type BrainstormFilter } from '../../lib/brainstorms'
import { timeAgo } from '../../lib/format'
import { useBrainstorms } from '../../lib/queries'

export function BrainstormList() {
  const { data, isLoading, isError } = useBrainstorms()
  const [filter, setFilter] = useState<BrainstormFilter>('all')

  if (isLoading) return <p className="brainstorms__note">Loading…</p>
  if (isError || !data) return <p className="brainstorms__note">Could not load brainstorms.</p>
  if (data.length === 0) return <p className="brainstorms__note">No brainstorms are waiting on you</p>

  const counts = brainstormCounts(data)
  const rows = filterBrainstorms(data, filter)

  return (
    <div className="brainstorms__list">
      <Segmented
        label="Filter"
        activeId={filter}
        onChange={(id) => setFilter(id as BrainstormFilter)}
        options={[
          { id: 'all', label: 'All' },
          { id: 'answer', label: `Needs answer (${counts.answer})` },
          { id: 'go', label: `Needs Go (${counts.go})` },
        ]}
      />
      {rows.length === 0 && (
        <p className="brainstorms__note">{filter === 'go' ? 'Nothing needs a Go' : 'Nothing needs an answer'}</p>
      )}
      <ul className="brainstorms__rows">
        {rows.map((e) => (
          <li key={e.task_id}>
            <Link to={brainstormHref(e)} className="brainstorms__row">
              <span className="brainstorms__title">{`#${e.task_id} ${e.task_title}`}</span>
              <span className="brainstorms__meta">
                {e.project_id && <span>{e.project_id}</span>}
                <span>{e.task_status.replace('_', ' ')}</span>
                <span>{timeAgo(e.waiting_since)}</span>
              </span>
              <span className="brainstorms__waits">
                {e.questions_waiting > 0 && (
                  <span>{e.questions_waiting === 1 ? '1 question waiting' : `${e.questions_waiting} questions waiting`}</span>
                )}
                {e.questions_waiting > 0 && e.questions_stale && <span className="brainstorms__tag">STALE</span>}
                {e.pending_gate && <span>{`Needs Go · spec v${e.pending_gate.spec_version}`}</span>}
                {isMidWorkSpecChange(e) && <span className="brainstorms__tag">spec change mid-work</span>}
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  )
}
