// Journal tab (docs/design/Task.dc.html "JOURNAL"): a vertical timeline of
// `GET /v1/tasks/{id}/log` entries, filterable by kind (pills) and author.

import { useMemo, useState } from 'react'
import { timeAgo } from '../../lib/format'
import type { TaskLogEntry, TaskLogKind } from '../../lib/types'
import './JournalTab.css'

export interface JournalTabProps {
  log: TaskLogEntry[]
}

const KIND_ORDER: TaskLogKind[] = ['decision', 'problem', 'note', 'status']

const KIND_TONE: Record<TaskLogKind, string> = {
  decision: 'journal-tab__kind--indigo',
  problem: 'journal-tab__kind--err',
  note: 'journal-tab__kind--neutral',
  status: 'journal-tab__kind--ok',
}

const DOT_TONE: Record<TaskLogKind, string> = {
  decision: 'journal-tab__dot--indigo',
  problem: 'journal-tab__dot--err',
  note: 'journal-tab__dot--neutral',
  status: 'journal-tab__dot--ok',
}

function authorLabel(author: string | undefined): string {
  return author && author.length > 0 ? author : 'you'
}

export function JournalTab({ log }: JournalTabProps) {
  const [kindFilter, setKindFilter] = useState<TaskLogKind | 'all'>('all')
  const [authorFilter, setAuthorFilter] = useState<string>('all')

  const kinds = useMemo(
    () => KIND_ORDER.filter((k) => log.some((entry) => entry.kind === k)),
    [log],
  )
  const authors = useMemo(
    () => Array.from(new Set(log.map((entry) => authorLabel(entry.author)))).sort(),
    [log],
  )

  const filtered = log
    .filter((entry) => kindFilter === 'all' || entry.kind === kindFilter)
    .filter((entry) => authorFilter === 'all' || authorLabel(entry.author) === authorFilter)
    .sort((a, b) => b.created_at - a.created_at)

  return (
    <div className="journal-tab">
      <div className="journal-tab__filters">
        <button
          type="button"
          className={kindFilter === 'all' ? 'journal-tab__pill journal-tab__pill--active' : 'journal-tab__pill'}
          onClick={() => setKindFilter('all')}
        >
          All
        </button>
        {kinds.map((k) => (
          <button
            key={k}
            type="button"
            className={kindFilter === k ? 'journal-tab__pill journal-tab__pill--active' : 'journal-tab__pill'}
            onClick={() => setKindFilter(k)}
          >
            {k}
          </button>
        ))}

        {authors.length > 1 && (
          <select
            aria-label="Filter by author"
            className="journal-tab__author-select"
            value={authorFilter}
            onChange={(e) => setAuthorFilter(e.target.value)}
          >
            <option value="all">All authors</option>
            {authors.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
        )}
      </div>

      <div className="journal-tab__timeline">
        <div className="journal-tab__line" />
        <div className="journal-tab__entries">
          {filtered.map((entry) => (
            <div key={entry.id} className="journal-tab__entry">
              <span className={`journal-tab__dot ${DOT_TONE[entry.kind]}`} />
              <div className="journal-tab__entry-head">
                <span className={`journal-tab__kind ${KIND_TONE[entry.kind]}`}>{entry.kind}</span>
                <span className="journal-tab__author">{authorLabel(entry.author)}</span>
                <span className="journal-tab__when">{timeAgo(entry.created_at)}</span>
              </div>
              <div className="journal-tab__body">{entry.body}</div>
            </div>
          ))}
          {filtered.length === 0 && <p className="journal-tab__empty">No entries match this filter.</p>}
        </div>
      </div>
    </div>
  )
}
