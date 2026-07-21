// One open-question thread card (yellow header, collapsible context, reply
// thread, reply form). Extracted from QuestionsTab so the global /questions
// page can render the same threads outside a task screen.

import { useState } from 'react'
import { Markdown } from './Markdown'
import { timeAgo } from '../lib/format'
import { useAnswerQuestion, useReplyQuestion } from '../lib/queries'
import type { Question } from '../lib/types'
import './questionthread.css'

function whoseTurnLabel(question: Question): string {
  if (question.whose_turn === 'user') return 'awaiting you'
  if (question.whose_turn === 'orchestrator') return 'awaiting orchestrator'
  return ''
}

export function authorLabel(author: string | undefined, orchestratorName?: string): string {
  if (!author) return 'you'
  return orchestratorName ?? author
}

/**
 * `asked_by === ""` means the human opened this thread TO the orchestrator
 * (docs/12-tasks.md); anything else is the existing orchestrator-opened
 * direction. The asker slot must never show `orchestratorName` for a
 * user-opened thread — that would misattribute the question.
 */
function askerLabel(question: Question, orchestratorName?: string): string {
  if (question.asked_by === '') return 'you asked the orchestrator'
  return `${orchestratorName ?? question.asked_by} asked`
}

export interface QuestionThreadProps {
  taskId: number
  question: Question
  orchestratorName?: string
}

export function QuestionThread({ taskId, question, orchestratorName }: QuestionThreadProps) {
  const [ctxOpen, setCtxOpen] = useState(true)
  const [body, setBody] = useState('')
  const reply = useReplyQuestion()
  const answer = useAnswerQuestion()

  const busy = reply.isPending || answer.isPending
  const turnLabel = whoseTurnLabel(question)

  function handleClarify() {
    if (!body.trim()) return
    reply.mutate(
      { id: question.id, body, taskId },
      { onSuccess: () => setBody('') },
    )
  }

  function handleAnswer() {
    if (!body.trim()) return
    answer.mutate(
      { id: question.id, body, taskId },
      { onSuccess: () => setBody('') },
    )
  }

  function handleDismiss() {
    answer.mutate({ id: question.id, dismiss: true, taskId })
  }

  return (
    <div className="question-thread">
      <div className="question-thread__header">
        <span className="question-thread__tag">Q{question.ordinal}</span>
        {turnLabel && (
          <span
            className={
              question.whose_turn === 'user'
                ? 'question-thread__turn question-thread__turn--warn'
                : 'question-thread__turn question-thread__turn--neutral'
            }
          >
            {turnLabel}
          </span>
        )}
        <div className="question-thread__spacer" />
        <span className="question-thread__asker">{askerLabel(question, orchestratorName)}</span>
      </div>
      <div className="question-thread__body">
        <div className="question-thread__question">
          <Markdown>{question.body}</Markdown>
        </div>

        {question.context &&
          (ctxOpen ? (
            <div className="question-thread__context">
              <div className="question-thread__context-header">
                <span>Context</span>
                <button type="button" onClick={() => setCtxOpen(false)}>
                  Hide ▴
                </button>
              </div>
              <div className="question-thread__context-body">
                <Markdown compact>{question.context}</Markdown>
              </div>
            </div>
          ) : (
            <button type="button" className="question-thread__context-toggle" onClick={() => setCtxOpen(true)}>
              ＋ Show context
            </button>
          ))}

        <div className="question-thread__discussion-label">Discussion · {question.messages.length} replies</div>
        <div className="question-thread__messages">
          {question.messages.map((m) => {
            const isOrchestrator = !!m.author
            return (
              <div key={m.id} className="question-thread__message">
                <div className="question-thread__message-head">
                  <span
                    className={
                      isOrchestrator
                        ? 'question-thread__avatar question-thread__avatar--orch'
                        : 'question-thread__avatar question-thread__avatar--you'
                    }
                  >
                    {isOrchestrator ? 'O' : 'Y'}
                  </span>
                  <span className="question-thread__message-author">
                    {authorLabel(m.author, orchestratorName)}
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
            aria-label={`Reply to Q${question.ordinal}`}
            placeholder="Write a reply, ask the orchestrator to rephrase, or give your final answer…"
            rows={4}
            value={body}
            onChange={(e) => setBody(e.target.value)}
          />
          <div className="question-thread__actions">
            <button type="button" className="question-thread__clarify" onClick={handleClarify} disabled={busy || !body.trim()}>
              Clarify — keep open
            </button>
            <button type="button" className="question-thread__answer" onClick={handleAnswer} disabled={busy || !body.trim()}>
              Answer &amp; close
            </button>
            <div className="question-thread__spacer" />
            <button type="button" className="question-thread__dismiss" onClick={handleDismiss} disabled={busy}>
              Dismiss as not relevant
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
