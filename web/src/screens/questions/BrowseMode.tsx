// Browse mode: everything, searchable — the counterpart to Decide.
//
// Decide answers "what is on me, now"; Browse answers "where did that thread
// go". A row's CTA never resolves anything in place: it drops you into Decide
// on that thread, so there is exactly one place where a thread gets answered.

import { timeAgo } from '../../lib/format'
import type { ThreadInboxEntry } from '../../lib/types'
import { browseCounts, browseGroups, statusChip, type BrowseFilter } from './model'

const FILTERS: { key: BrowseFilter; label: string }[] = [
  { key: 'mine', label: 'Your turn' },
  { key: 'open', label: 'All open' },
  { key: 'closed', label: 'Closed' },
  { key: 'all', label: 'Everything' },
]

function shortAge(ts: number): string {
  const ago = timeAgo(ts)
  return ago === 'just now' ? 'now' : ago.replace(' ago', '')
}

export interface BrowseModeProps {
  threads: ThreadInboxEntry[]
  query: string
  onQuery: (value: string) => void
  filter: BrowseFilter
  onFilter: (filter: BrowseFilter) => void
  onOpen: (id: number) => void
}

export function BrowseMode(props: BrowseModeProps) {
  const groups = browseGroups(props.threads, props.filter, props.query)
  const counts = browseCounts(props.threads, props.query)
  const shown = groups.reduce((n, g) => n + g.rows.length, 0)

  return (
    <div className="q__browse">
      <div className="q__browse-bar">
        <input
          className="q__search"
          value={props.query}
          aria-label="Filter threads"
          placeholder="Filter by ref, task, project or text…"
          onChange={(e) => props.onQuery(e.target.value)}
        />
        {FILTERS.map((f) => (
          <button
            key={f.key}
            type="button"
            aria-pressed={props.filter === f.key}
            className={props.filter === f.key ? 'q__filter q__filter--on' : 'q__filter'}
            onClick={() => props.onFilter(f.key)}
          >
            {f.label}
            <span className="q__filter-count">{counts[f.key]}</span>
          </button>
        ))}
        <div className="q__spacer" />
        <span className="q__browse-count">{shown} shown</span>
      </div>

      <div className="q__browse-body q__scroll">
        <div className="q__browse-inner">
          {groups.map((g) => (
            <div key={g.label} className="q__group">
              <div className="q__group-head">
                <span className={`q__group-label q__group-label--${g.tone}`}>{g.label}</span>
                <span className="q__count">{g.rows.length}</span>
                <span className="q__group-sub">{g.sub}</span>
              </div>
              <div className="q__rows">
                {g.rows.map((t) => {
                  const open = t.status === 'open'
                  const chip = statusChip(t)
                  const decide = open && t.your_turn
                  const rule = !open
                    ? 'closed'
                    : t.stale
                      ? 'stale'
                      : t.your_turn
                        ? 'turn'
                        : 'plain'
                  return (
                    <div key={t.id} className="q__row">
                      <span className={`q__row-rule q__row-rule--${rule}`} />
                      <span className="q__row-ref">{t.local_ref}</span>
                      <span className="q__row-subject">{t.subject}</span>
                      <span className={open ? 'q__row-body' : 'q__row-body q__row-body--closed'}>
                        {t.body}
                      </span>
                      {open && (t.options?.length ?? 0) > 0 && (
                        <span className="q__row-opts">{t.options?.length} opts</span>
                      )}
                      <span className={`q__row-chip q__chip--${chip.tone}`}>{chip.label}</span>
                      <span
                        className={
                          t.stale && open ? 'q__row-age q__row-age--stale' : 'q__row-age'
                        }
                      >
                        {shortAge(t.updated_at)}
                      </span>
                      <button
                        type="button"
                        className={decide ? 'q__row-cta q__row-cta--decide' : 'q__row-cta'}
                        onClick={() => props.onOpen(t.id)}
                      >
                        {decide ? 'Decide' : 'Open'}
                      </button>
                    </div>
                  )
                })}
              </div>
            </div>
          ))}

          {groups.length === 0 && (
            <div className="q__empty">
              <div className="q__empty-title">Nothing matches</div>
              <div className="q__empty-text">Clear the search or switch a filter.</div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
