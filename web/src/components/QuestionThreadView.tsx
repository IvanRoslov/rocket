// The presentational half of a question thread: header with the turn chip,
// the title, the markdown question, the reply thread and the composer with its
// three actions. Extracted from QuestionThread so role threads (docs/10-agents.md
// «Q&A-треды роли») render exactly the same UI as task threads and only differ
// in the mutations wired into the callbacks below.

import { useState } from 'react'
import { Markdown } from './Markdown'
import { timeAgo } from '../lib/format'
import { isHuman, threadParticipantLabel } from '../lib/participants'
import { questionTitle, splitReplies } from '../lib/thread'
import { usePasteImage } from '../lib/usePasteImage'
import './questionthread.css'

/**
 * One thread entry. `author` is a participant id — `''`/undefined today and
 * `'human'` after subtask #736 both mean the human, so it is only ever read
 * through `isHuman()`.
 */
export interface ThreadEntry {
  id: number
  author?: string
  body: string
  created_at: number
  /** Who must respond to this entry. Absent/empty = everyone but the author. */
  addressed_to?: string[]
}

export interface QuestionThreadViewProps {
  ordinal: number
  /**
   * The one thread id a human sees — "1023/Q2" for a task thread, "cto/Q1"
   * for a role thread. Absent on a daemon older than task #1023, where the
   * bare `Q<ordinal>` is all there is.
   */
  localRef?: string
  /** One-line heading; derived from the body when the daemon sent none. */
  title?: string
  body: string
  messages: ThreadEntry[]
  /** Empty when nobody is waiting (a resolved thread). */
  turnLabel: string
  /** true renders the turn chip in the warning tone ("awaiting you"). */
  turnWarn: boolean
  askerLabel: string
  /**
   * Everyone taking part in the thread. Drives the participants row and the
   * addressee picker; absent (a pre-participants API) hides both.
   */
  participants?: string[]
  /** Display name for agent-authored entries (orchestrator name / role id). */
  agentName?: string
  /** Avatar letter for agent-authored entries: 'O' orchestrator, 'A' role. */
  agentInitial?: string
  /**
   * Answer choices. Each renders as a button that CLOSES the thread with that
   * option as the resolution — the cheapest possible answer, which is what
   * threads hang for want of (spec v1 §«Варианты ответа»).
   */
  options?: string[]
  /**
   * An open thread nobody has moved for longer than `question_stale_after`.
   * The daemon decides this: the threshold is configurable, so the client
   * cannot honestly derive it from `asked_at`.
   */
  stale?: boolean
  placeholder?: string
  busy?: boolean
  /** `to` is who must RESPOND next; empty means "everyone but you". */
  onClarify: (body: string, to: string[]) => void
  onAnswer: (body: string, to: string[]) => void
  onDismiss: () => void
  /** Close the thread by picking option `choose` — a **1-based** index. */
  onChoose?: (choose: number) => void
}

export function QuestionThreadView({
  ordinal,
  localRef,
  title,
  body: question,
  messages,
  turnLabel,
  turnWarn,
  askerLabel,
  participants,
  agentName,
  agentInitial = 'O',
  options,
  stale,
  placeholder = 'Write a reply, ask for a rephrase, or give your final answer…',
  busy,
  onClarify,
  onAnswer,
  onDismiss,
  onChoose,
}: QuestionThreadViewProps) {
  const [body, setBody] = useState('')
  const [allReplies, setAllReplies] = useState(false)
  const [to, setTo] = useState<string[]>([])
  const paste = usePasteImage(setBody)
  const ref = localRef ?? `Q${ordinal}`
  const replies = splitReplies(messages, allReplies)

  // You are never your own addressee, so the human is not a candidate.
  const addressees = (participants ?? []).filter((p) => !isHuman(p))
  const label = (id: string | undefined) => threadParticipantLabel(id, agentName, participants)

  function toggleAddressee(id: string) {
    setTo((prev) => (prev.includes(id) ? prev.filter((p) => p !== id) : [...prev, id]))
  }

  function submit(handler: (body: string, to: string[]) => void) {
    if (!body.trim()) return
    handler(body, to)
    setBody('')
  }

  return (
    <div className="question-thread">
      <div className="question-thread__header">
        <span className="question-thread__tag">{ref}</span>
        {stale && (
          <span className="question-thread__stale" title="Nobody has moved this thread in over a day">
            stale
          </span>
        )}
        {turnLabel && (
          <span
            className={
              turnWarn
                ? 'question-thread__turn question-thread__turn--warn'
                : 'question-thread__turn question-thread__turn--neutral'
            }
          >
            {turnLabel}
          </span>
        )}
        <div className="question-thread__spacer" />
        <span className="question-thread__asker">{askerLabel}</span>
      </div>
      {participants && participants.length > 0 && (
        <div className="question-thread__participants" aria-label="Participants">
          <span className="question-thread__participants-label">In this thread</span>
          {participants.map((id) => (
            <span key={id} className="question-thread__participant">
              {label(id)}
            </span>
          ))}
        </div>
      )}
      <div className="question-thread__body">
        <h2 className="question-thread__title">{questionTitle({ title, body: question })}</h2>
        <div className="question-thread__question">
          <Markdown>{question}</Markdown>
        </div>

        <div className="question-thread__discussion-label">
          Discussion · {messages.length} replies
        </div>
        <div className="question-thread__messages" aria-label="Discussion">
          {replies.hidden > 0 && (
            <button
              type="button"
              className="question-thread__more"
              onClick={() => setAllReplies(true)}
            >
              Show {replies.hidden} earlier {replies.hidden === 1 ? 'reply' : 'replies'}
            </button>
          )}
          {replies.shown.map((m) => {
            const fromAgent = !isHuman(m.author)
            return (
              <div key={m.id} className="question-thread__message">
                <div className="question-thread__message-head">
                  <span
                    className={
                      fromAgent
                        ? 'question-thread__avatar question-thread__avatar--orch'
                        : 'question-thread__avatar question-thread__avatar--you'
                    }
                  >
                    {fromAgent ? agentInitial : 'Y'}
                  </span>
                  <span className="question-thread__message-author">
                    {label(m.author)}
                  </span>
                  {m.addressed_to && m.addressed_to.length > 0 && (
                    <span className="question-thread__addressees">
                      {`\u2192 ${m.addressed_to.map((id) => label(id)).join(', ')}`}
                    </span>
                  )}
                  <span className="question-thread__message-meta">{timeAgo(m.created_at)}</span>
                </div>
                <div className="question-thread__message-body">
                  <Markdown compact>{m.body}</Markdown>
                </div>
              </div>
            )
          })}
        </div>

        {options && options.length > 0 && onChoose && (
          <div className="question-thread__options" aria-label="Answer options">
            {options.map((label, i) => (
              <button
                key={label}
                type="button"
                className="question-thread__option"
                onClick={() => onChoose(i + 1)}
                disabled={busy}
              >
                {label}
              </button>
            ))}
            <span className="question-thread__options-hint">
              Picking an option closes the thread with that answer.
            </span>
          </div>
        )}

        <div className="question-thread__form">
          <textarea
            aria-label={`Reply to ${ref}`}
            placeholder={placeholder}
            rows={4}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            onPaste={paste.onPaste}
          />
          {paste.error && (
            <div className="question-thread__paste-error">Upload failed: {paste.error}</div>
          )}
          {addressees.length > 0 && (
            <div className="question-thread__to">
              <span className="question-thread__to-label">Must respond</span>
              {addressees.map((id) => (
                <label key={id} className="question-thread__to-option">
                  <input
                    type="checkbox"
                    checked={to.includes(id)}
                    onChange={() => toggleAddressee(id)}
                  />
                  {label(id)}
                </label>
              ))}
              <span className="question-thread__to-hint">
                Everyone is notified either way — this picks who must answer.
              </span>
            </div>
          )}
          <div className="question-thread__actions">
            <button
              type="button"
              className="question-thread__clarify"
              onClick={() => submit(onClarify)}
              disabled={busy || paste.uploading || !body.trim()}
            >
              Clarify — keep open
            </button>
            <button
              type="button"
              className="question-thread__answer"
              onClick={() => submit(onAnswer)}
              disabled={busy || paste.uploading || !body.trim()}
            >
              Answer &amp; close
            </button>
            <div className="question-thread__spacer" />
            <button
              type="button"
              className="question-thread__dismiss"
              onClick={onDismiss}
              disabled={busy}
            >
              Dismiss as not relevant
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
