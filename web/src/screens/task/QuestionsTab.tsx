// Questions tab (docs/design/Task.dc.html "QUESTIONS"): one thread card per
// open question (yellow header, collapsible context, reply thread, reply
// form) plus a collapsed list of resolved threads below.

import { useState } from 'react'
import { Markdown } from '../../components/Markdown'
import { QuestionThread, authorLabel } from '../../components/QuestionThread'
import { isHuman } from '../../lib/participants'
import { timeAgo } from '../../lib/format'
import { questionTitle } from '../../lib/thread'
import { useAskOrchestrator } from '../../lib/queries'
import { usePasteImage } from '../../lib/usePasteImage'
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
  // An fyi thread is born closed and nobody ever answered it — calling it
  // "resolved" would claim somebody did (spec v1 §«Тип треда»).
  if (question.type === 'fyi' || question.resolution === 'fyi') return 'fyi'
  if (question.resolution === 'dismissed') return 'dismissed'
  return 'resolved'
}

interface AskOrchestratorFormProps {
  taskId: number
  disabled: boolean
}

function AskOrchestratorForm({ taskId, disabled }: AskOrchestratorFormProps) {
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const ask = useAskOrchestrator(taskId)
  const pasteBody = usePasteImage(setBody)

  function reset() {
    setOpen(false)
    setTitle('')
    setBody('')
  }

  function handleSubmit() {
    if (!body.trim()) return
    // The title is optional: the daemon derives one from the body when it is
    // left empty (task #1264).
    ask.mutate({ body, title: title.trim() || undefined }, { onSuccess: reset })
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
      <input
        type="text"
        className="questions-tab__ask-title"
        aria-label="Question heading (optional)"
        placeholder="Heading — one line, optional"
        value={title}
        onChange={(e) => setTitle(e.target.value)}
      />
      <textarea
        aria-label="Ask the orchestrator"
        placeholder="What do you want to ask the orchestrator? Markdown is fine."
        rows={4}
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onPaste={pasteBody.onPaste}
      />
      <div className="questions-tab__ask-actions">
        <button
          type="button"
          className="questions-tab__ask-submit"
          onClick={handleSubmit}
          disabled={ask.isPending || pasteBody.uploading || !body.trim()}
        >
          Ask
        </button>
        <button type="button" className="questions-tab__ask-cancel" onClick={reset} disabled={ask.isPending}>
          Cancel
        </button>
      </div>
      {pasteBody.error && (
        <div className="question-thread__paste-error">Upload failed: {pasteBody.error}</div>
      )}
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
        <span className="questions-tab__resolved-tag">
          {question.local_ref ?? `Q${question.ordinal}`}
        </span>
        <span className="questions-tab__resolved-badge">{resolutionLabel(question)}</span>
        <span className="questions-tab__resolved-text">{questionTitle(question)}</span>
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

          {question.messages.length > 0 && (
            <div className="question-thread__messages">
              {question.messages.map((m) => {
                const isOrchestrator = !isHuman(m.author)
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
