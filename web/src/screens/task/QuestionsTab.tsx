// Questions tab (docs/design/Task.dc.html "QUESTIONS"): one thread card per
// open question (yellow header, collapsible context, reply thread, reply
// form) plus a collapsed list of resolved threads below.

import { useState } from 'react'
import { Markdown } from '../../components/Markdown'
import { QuestionThread, authorLabel } from '../../components/QuestionThread'
import { timeAgo } from '../../lib/format'
import { useAskOrchestrator } from '../../lib/queries'
import type { Question } from '../../lib/types'
import './QuestionsTab.css'

export interface QuestionsTabProps {
  taskId: number
  questions: Question[]
  orchestratorName?: string
  /** Whether the task has a live orchestrator session to receive a new question. */
  hasLiveOrchestrator?: boolean
}

function resolutionLabel(question: Question): string {
  if (question.resolution === 'dismissed') return 'dismissed'
  return 'resolved'
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
        <QuestionThread key={q.id} taskId={taskId} question={q} orchestratorName={orchestratorName} />
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
