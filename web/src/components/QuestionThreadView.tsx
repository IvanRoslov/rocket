// The presentational half of a question thread: header with the turn chip,
// collapsible context, the reply thread and the composer with its three
// actions. Extracted from QuestionThread so role threads (docs/10-agents.md
// «Q&A-треды роли») render exactly the same UI as task threads and only differ
// in the mutations wired into the callbacks below.

import { useState } from 'react'
import { Markdown } from './Markdown'
import { timeAgo } from '../lib/format'
import { isHuman, threadParticipantLabel } from '../lib/participants'
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
  body: string
  context?: string
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
  placeholder?: string
  busy?: boolean
  /** `to` is who must RESPOND next; empty means "everyone but you". */
  onClarify: (body: string, to: string[]) => void
  onAnswer: (body: string, to: string[]) => void
  onDismiss: () => void
}

export function QuestionThreadView({
  ordinal,
  body: question,
  context,
  messages,
  turnLabel,
  turnWarn,
  askerLabel,
  participants,
  agentName,
  agentInitial = 'O',
  placeholder = 'Write a reply, ask for a rephrase, or give your final answer…',
  busy,
  onClarify,
  onAnswer,
  onDismiss,
}: QuestionThreadViewProps) {
  const [ctxOpen, setCtxOpen] = useState(true)
  const [body, setBody] = useState('')
  const [to, setTo] = useState<string[]>([])
  const paste = usePasteImage(setBody)

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
        <span className="question-thread__tag">Q{ordinal}</span>
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
        <div className="question-thread__question">
          <Markdown>{question}</Markdown>
        </div>

        {context &&
          (ctxOpen ? (
            <div className="question-thread__context">
              <div className="question-thread__context-header">
                <span>Context</span>
                <button type="button" onClick={() => setCtxOpen(false)}>
                  Hide ▴
                </button>
              </div>
              <div className="question-thread__context-body">
                <Markdown compact>{context}</Markdown>
              </div>
            </div>
          ) : (
            <button
              type="button"
              className="question-thread__context-toggle"
              onClick={() => setCtxOpen(true)}
            >
              ＋ Show context
            </button>
          ))}

        <div className="question-thread__discussion-label">
          Discussion · {messages.length} replies
        </div>
        <div className="question-thread__messages" aria-label="Discussion">
          {messages.map((m) => {
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

        <div className="question-thread__form">
          <textarea
            aria-label={`Reply to Q${ordinal}`}
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
