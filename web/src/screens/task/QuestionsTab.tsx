// Questions tab (docs/design/Task.dc.html "QUESTIONS"): one thread card per
// open question (yellow header, collapsible context, reply thread, reply
// form) plus a collapsed list of resolved threads below.

import { useState } from 'react'
import { Markdown } from '../../components/Markdown'
import { timeAgo } from '../../lib/format'
import { useAnswerQuestion, useAskOrchestrator, useReplyQuestion } from '../../lib/queries'
import type { Question } from '../../lib/types'
import './QuestionsTab.css'

export interface QuestionsTabProps {
  taskId: number
  questions: Question[]
  orchestratorName?: string
  /** Whether the task has a live orchestrator session to receive a new question. */
  hasLiveOrchestrator?: boolean
}

function whoseTurnLabel(question: Question): string {
  if (question.whose_turn === 'user') return 'awaiting you'
  if (question.whose_turn === 'orchestrator') return 'awaiting orchestrator'
  return ''
}

function authorLabel(author: string | undefined, orchestratorName?: string): string {
  if (!author) return 'you'
  return orchestratorName ?? author
}

function resolutionLabel(question: Question): string {
  if (question.resolution === 'dismissed') return 'dismissed'
  return 'resolved'
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

interface AskOrchestratorFormProps {
  taskId: number
  disabled: boolean
}

function AskOrchestratorForm({ taskId, disabled }: AskOrchestratorFormProps) {
  const [open, setOpen] = useState(false)
  const [body, setBody] = useState('')
  const [context, setContext] = useState('')
  const [ctxOpen, setCtxOpen] = useState(false)
  const ask = useAskOrchestrator(taskId)

  function reset() {
    setOpen(false)
    setBody('')
    setContext('')
    setCtxOpen(false)
  }

  function handleSubmit() {
    if (!body.trim()) return
    ask.mutate({ body, context: context.trim() || undefined }, { onSuccess: reset })
  }

  if (!open) {
    return (
      <button
        type="button"
        className="questions-tab__ask-toggle"
        onClick={() => setOpen(true)}
        disabled={disabled}
        title={disabled ? 'No live orchestrator for this task' : undefined}
      >
        + Ask the orchestrator
      </button>
    )
  }

  return (
    <div className="questions-tab__ask-form">
      <textarea
        aria-label="Ask the orchestrator"
        placeholder="What do you want to ask the orchestrator?"
        rows={3}
        value={body}
        onChange={(e) => setBody(e.target.value)}
      />
      {ctxOpen ? (
        <textarea
          aria-label="Context (optional, markdown)"
          placeholder="Optional context (markdown)…"
          rows={3}
          value={context}
          onChange={(e) => setContext(e.target.value)}
        />
      ) : (
        <button type="button" className="questions-tab__ask-context-toggle" onClick={() => setCtxOpen(true)}>
          ＋ Add context
        </button>
      )}
      <div className="questions-tab__ask-actions">
        <button type="button" className="questions-tab__ask-submit" onClick={handleSubmit} disabled={ask.isPending || !body.trim()}>
          Ask
        </button>
        <button type="button" className="questions-tab__ask-cancel" onClick={reset} disabled={ask.isPending}>
          Cancel
        </button>
      </div>
    </div>
  )
}

interface ThreadCardProps {
  taskId: number
  question: Question
  orchestratorName?: string
}

function ThreadCard({ taskId, question, orchestratorName }: ThreadCardProps) {
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

interface ResolvedThreadRowProps {
  question: Question
  orchestratorName?: string
}

function ResolvedThreadRow({ question, orchestratorName }: ResolvedThreadRowProps) {
  const [open, setOpen] = useState(false)

  return (
    <div className="questions-tab__resolved">
      <button
        type="button"
        className="questions-tab__resolved-row"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="questions-tab__resolved-tag">Q{question.ordinal}</span>
        <span className="questions-tab__resolved-badge">{resolutionLabel(question)}</span>
        <span className="questions-tab__resolved-text">{question.body}</span>
        <span className="questions-tab__resolved-when">
          {question.resolved_at ? timeAgo(question.resolved_at) : ''}
        </span>
        <span className="questions-tab__resolved-chevron">{open ? '▴' : '▾'}</span>
      </button>

      {open && (
        <div className="questions-tab__resolved-detail">
          <div className="questions-tab__resolved-question">
            <Markdown>{question.body}</Markdown>
          </div>

          {question.context && (
            <div className="question-thread__context">
              <div className="question-thread__context-header">
                <span>Context</span>
              </div>
              <div className="question-thread__context-body">
                <Markdown compact>{question.context}</Markdown>
              </div>
            </div>
          )}

          {question.messages.length > 0 && (
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
          )}
        </div>
      )}
    </div>
  )
}

export function QuestionsTab({ taskId, questions, orchestratorName, hasLiveOrchestrator }: QuestionsTabProps) {
  const open = questions.filter((q) => q.status === 'open').sort((a, b) => a.ordinal - b.ordinal)
  const resolved = questions
    .filter((q) => q.status === 'resolved')
    .sort((a, b) => b.ordinal - a.ordinal)

  return (
    <div className="questions-tab">
      <AskOrchestratorForm taskId={taskId} disabled={!hasLiveOrchestrator} />

      {open.map((q) => (
        <ThreadCard key={q.id} taskId={taskId} question={q} orchestratorName={orchestratorName} />
      ))}

      {resolved.length > 0 && (
        <>
          <div className="questions-tab__resolved-label">Resolved</div>
          {resolved.map((q) => (
            <ResolvedThreadRow key={q.id} question={q} orchestratorName={orchestratorName} />
          ))}
        </>
      )}

      {open.length === 0 && resolved.length === 0 && (
        <div className="questions-tab__empty">No questions yet.</div>
      )}
    </div>
  )
}
