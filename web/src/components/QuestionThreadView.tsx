// The presentational half of a question thread: header with the turn chip,
// collapsible context, the reply thread and the composer with its three
// actions. Extracted from QuestionThread so role threads (docs/10-agents.md
// «Q&A-треды роли») render exactly the same UI as task threads and only differ
// in the mutations wired into the callbacks below.

import { useState } from 'react'
import { Markdown } from './Markdown'
import { timeAgo } from '../lib/format'
import { usePasteImage } from '../lib/usePasteImage'
import './questionthread.css'

/** One thread entry. `author === ''` (or undefined) means the human. */
export interface ThreadEntry {
  id: number
  author?: string
  body: string
  created_at: number
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
  /** Display name for agent-authored entries (orchestrator name / role id). */
  agentName?: string
  /** Avatar letter for agent-authored entries: 'O' orchestrator, 'A' role. */
  agentInitial?: string
  placeholder?: string
  busy?: boolean
  onClarify: (body: string) => void
  onAnswer: (body: string) => void
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
  const paste = usePasteImage(setBody)

  function submit(handler: (body: string) => void) {
    if (!body.trim()) return
    handler(body)
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
        <div className="question-thread__messages">
          {messages.map((m) => {
            const fromAgent = !!m.author
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
                    {fromAgent ? (agentName ?? m.author) : 'you'}
                  </span>
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
