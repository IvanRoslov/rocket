// One thread, in full: the card that fills the right half of Decide mode.
//
// Everything the human can do to a thread lives here — pick an option, write a
// resolution, ask back, push it to Later, dismiss it — but nothing is decided
// here: every control calls up into QuestionsScreen, which owns the deferred
// action behind the undo window.

import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Markdown } from '../../components/Markdown'
import { timeAgo } from '../../lib/format'
import { isHuman, threadParticipantLabel } from '../../lib/participants'
import { questionTitle, splitReplies } from '../../lib/thread'
import type { ThreadInboxEntry } from '../../lib/types'
import { statusChip } from './model'
import type { ThreadDetail } from './useThreadDetail'

/** The initial that stands in for a participant's avatar. */
function initial(id?: string): string {
  return isHuman(id) ? 'Y' : (id ?? '?').slice(0, 1).toUpperCase()
}

function Avatar({ id }: { id?: string }) {
  return (
    <span className={isHuman(id) ? 'q__avatar q__avatar--human' : 'q__avatar'} aria-hidden="true">
      {initial(id)}
    </span>
  )
}

export interface ThreadCardProps {
  entry: ThreadInboxEntry
  detail: ThreadDetail
  /** Set once the human has acted but the deferred API call has not gone out. */
  pendingResolution?: string
  draft: string
  onDraft: (value: string) => void
  picks: string[]
  onTogglePick: (participant: string) => void
  onChoose: (index: number) => void
  onAnswerClose: () => void
  onReply: () => void
  onSkip: () => void
  onDismiss: () => void
  onBackToQueue: () => void
  onBrowse: () => void
}

export function ThreadCard(props: ThreadCardProps) {
  const { entry, detail, pendingResolution } = props
  // Resets itself per thread: QuestionsScreen keys the card by thread id.
  const [allReplies, setAllReplies] = useState(false)
  const closed = entry.status !== 'open' || pendingResolution !== undefined
  const resolutionText = pendingResolution ?? detail.resolutionText
  const chip = statusChip(entry, resolutionText)
  const options = entry.options ?? []
  const others = (entry.participants ?? []).filter((p) => !isHuman(p))
  const label = (id?: string) => threadParticipantLabel(id, undefined, entry.participants)
  const replies = splitReplies(detail.messages, allReplies)

  return (
    <div className="q__card">
      <div className="q__card-head">
        <span className="q__ref">{entry.local_ref}</span>
        {entry.kind === 'task' && entry.project_id && entry.task_id !== undefined ? (
          <Link className="q__subject" to={`/p/${entry.project_id}/tasks/${entry.task_id}`}>
            #{entry.task_id} {entry.task_title} →
          </Link>
        ) : entry.kind === 'role' && entry.role_id ? (
          <Link className="q__subject" to={`/agents/${entry.role_id}`}>
            role {entry.role_id} →
          </Link>
        ) : (
          <span className="q__subject">{entry.subject}</span>
        )}
        <div className="q__spacer" />
        <span className={`q__chip q__chip--${chip.tone}`}>{chip.label}</span>
        <span className="q__age">{timeAgo(entry.updated_at)}</span>
      </div>

      {entry.type === 'fyi' && (
        <div className="q__banner">
          <span className="q__banner-tag">FYI</span>
          <span className="q__banner-text">
            Informational note. Nobody has the turn, nothing is blocked, and it never enters your
            queue or the counters — it is here so the decision history is complete.
          </span>
        </div>
      )}

      <h2 className="q__question">{questionTitle(entry)}</h2>
      <div className="q__body">
        <Markdown>{entry.body}</Markdown>
      </div>
      <div className="q__asked-by">
        <Avatar id={entry.asked_by} />
        <span>asked by {label(entry.asked_by)}</span>
      </div>

      {!closed && options.length > 0 && (
        <>
          <div className="q__options-label">One tap closes this thread</div>
          <div className="q__options">
            {options.map((option, i) => (
              <button
                key={option}
                type="button"
                className="q__option"
                onClick={() => props.onChoose(i)}
              >
                <span className="q__option-num">{i + 1}</span>
                <span className="q__option-label">{option}</span>
                <span className="q__option-note">closes thread</span>
              </button>
            ))}
          </div>
        </>
      )}

      {detail.messages.length > 0 && (
        <div className="q__conversation">
          {replies.hidden > 0 && (
            <button
              type="button"
              className="q__more"
              onClick={() => setAllReplies(true)}
            >
              Show {replies.hidden} earlier {replies.hidden === 1 ? 'reply' : 'replies'}
            </button>
          )}
          {replies.shown.map((m) => (
            <div key={m.id} className="q__msg">
              <div className="q__msg-head">
                <Avatar id={m.author} />
                <span className="q__msg-author">{label(m.author)}</span>
                {m.addressed_to && m.addressed_to.length > 0 && (
                  <span className="q__msg-to">→ {m.addressed_to.map(label).join(', ')}</span>
                )}
                <span className="q__msg-when">{timeAgo(m.created_at)}</span>
              </div>
              <div className="q__msg-body">
                <Markdown compact>{m.body}</Markdown>
              </div>
            </div>
          ))}
        </div>
      )}

      {!closed && (
        <div className="q__act">
          <textarea
            aria-label="Your answer"
            className="q__textarea"
            rows={2}
            value={props.draft}
            placeholder={
              options.length > 0
                ? 'None of the options fit? Write your own resolution…'
                : 'Write the decision, or ask the agent for a clarification…'
            }
            onChange={(e) => props.onDraft(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
                e.preventDefault()
                props.onAnswerClose()
              }
            }}
          />
          <div className="q__act-row">
            <button type="button" className="q__answer" onClick={props.onAnswerClose}>
              Answer &amp; close<span className="q__answer-key">⌘↩</span>
            </button>
            <button type="button" className="q__reply" onClick={props.onReply}>
              Ask back — keep open
            </button>
            <div className="q__spacer" />
            <button type="button" className="q__quiet" onClick={props.onSkip}>
              Later <span className="q__quiet-key">S</span>
            </button>
            <button
              type="button"
              className="q__quiet q__quiet--danger"
              onClick={props.onDismiss}
            >
              Not relevant <span className="q__quiet-key">X</span>
            </button>
          </div>
          {others.length > 0 && (
            <div className="q__turn">
              <span className="q__turn-label">Turn passes to</span>
              {others.map((p) => {
                const on = props.picks.includes(p)
                return (
                  <button
                    key={p}
                    type="button"
                    aria-pressed={on}
                    className={on ? 'q__turn-pick q__turn-pick--on' : 'q__turn-pick'}
                    onClick={() => props.onTogglePick(p)}
                  >
                    <span className="q__turn-mark" aria-hidden="true">
                      {on ? '●' : '○'}
                    </span>
                    {label(p)}
                  </button>
                )
              })}
              <span className="q__turn-note">everyone is notified either way</span>
            </div>
          )}
        </div>
      )}

      {closed && (
        <div className="q__closed">
          {entry.type !== 'fyi' && (
            <div className="q__closed-box">
              <span className="q__closed-tick" aria-hidden="true">
                ✓
              </span>
              <div>
                <div className="q__closed-by">
                  Closed by {label(detail.closedBy)}
                  {entry.resolved_at ? ` · ${timeAgo(entry.resolved_at)}` : ''}
                </div>
                <div className="q__closed-text">{resolutionText}</div>
              </div>
            </div>
          )}
          <div className="q__closed-actions">
            <button type="button" className="q__btn-dark-sm" onClick={props.onBackToQueue}>
              Back to the queue
            </button>
            <button type="button" className="q__btn-light-sm" onClick={props.onBrowse}>
              Browse history
            </button>
            <span className="q__readonly-note">
              Closed threads are read-only for you. Only an agent can reopen one, and only if the
              answer turned out to be wrong.
            </span>
          </div>
        </div>
      )}
    </div>
  )
}
