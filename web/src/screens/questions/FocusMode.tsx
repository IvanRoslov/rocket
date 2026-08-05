// Decide mode: the queue rail on the left, one thread card on the right.
//
// The rail is the whole point of the mode — it says how much is on you and in
// what order, so deciding is a pass down a list rather than a hunt through an
// inbox.

import type { ReactNode } from 'react'
import { timeAgo } from '../../lib/format'
import type { ThreadInboxEntry } from '../../lib/types'

/** "2h ago" -> "2h": the rail has room for the number, not the sentence. */
function shortAge(ts: number): string {
  const ago = timeAgo(ts)
  return ago === 'just now' ? 'now' : ago.replace(' ago', '')
}

export interface FocusModeProps {
  queue: ThreadInboxEntry[]
  currentId?: number
  onSelect: (id: number) => void
  waitingOnAgents: number
  notes: number
  onBrowse: () => void
  onNotes: () => void
  clearedToday: number
  /** The card, or nothing when the queue is empty. */
  card: ReactNode
}

export function FocusMode(props: FocusModeProps) {
  const { queue, card } = props

  return (
    <div className="q__focus">
      <div className="q__rail q__scroll">
        <div className="q__rail-head">
          <span className="q__eyebrow">Queue · your turn</span>
          <span className="q__count">{queue.length}</span>
        </div>
        {queue.map((t) => {
          const on = t.id === props.currentId
          const options = t.options?.length ?? 0
          return (
            <button
              key={t.id}
              type="button"
              aria-current={on ? 'true' : undefined}
              className={[
                'q__qrow',
                on ? 'q__qrow--on' : '',
                t.stale ? 'q__qrow--stale' : '',
              ]
                .filter(Boolean)
                .join(' ')}
              onClick={() => props.onSelect(t.id)}
            >
              <span className="q__qrow-head">
                <span className="q__qrow-ref">{t.local_ref}</span>
                {t.stale && <span className="q__stale-tag">STALE</span>}
                <span className="q__spacer" />
                <span className="q__qrow-age">{shortAge(t.updated_at)}</span>
              </span>
              <span className="q__qrow-body">{t.body}</span>
              {options > 0 && (
                <span className="q__opt-hint">{options} options · one tap</span>
              )}
            </button>
          )
        })}
        <div className="q__rail-foot">
          <button type="button" className="q__rail-link" onClick={props.onBrowse}>
            {props.waitingOnAgents} threads waiting on agents →
          </button>
          <button type="button" className="q__rail-link q__rail-link--dim" onClick={props.onNotes}>
            {props.notes} notes · nothing to answer →
          </button>
        </div>
      </div>

      <div className="q__pane q__scroll">
        {card ?? (
          <div className="q__clear">
            <div className="q__clear-mark" aria-hidden="true">
              ✓
            </div>
            <div className="q__clear-title">Queue clear</div>
            <div className="q__clear-text">
              {props.clearedToday > 0
                ? `You cleared ${props.clearedToday} threads. ${props.waitingOnAgents} are waiting on agents — they will come back to you if they need you.`
                : 'Nothing is blocked on you right now.'}
            </div>
            <button type="button" className="q__btn-light-sm" onClick={props.onBrowse}>
              Browse all threads
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
