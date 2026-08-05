// Global Questions page (/questions): the unified thread inbox — every open
// thread of every task AND every role in one list, grouped by whose turn it
// is and answerable in place. Spec: task #1023 spec v1 §«Единый инбокс».
//
// It reads GET /v1/threads rather than GET /v1/questions because the latter
// knows only about task threads: a role thread waiting on the human was
// invisible here, which is exactly how threads got left hanging.

import { useState } from 'react'
import { Link } from 'react-router-dom'
import { AgentQuestionThread } from '../../components/AgentQuestionThread'
import { QuestionThread } from '../../components/QuestionThread'
import { timeAgo } from '../../lib/format'
import { isHuman, participantLabel } from '../../lib/participants'
import { useAgentQuestions, useTaskQuestions, useThreads } from '../../lib/queries'
import type { ThreadInboxEntry } from '../../lib/types'
import './QuestionsScreen.css'

/**
 * The full task thread behind an expanded row. The inbox carries the question
 * only, so the conversation, the context and the composer come from the task's
 * own endpoint — the same data the task page renders, so the two can never
 * disagree.
 */
function TaskThreadDetail({ taskId, questionId }: { taskId: number; questionId: number }) {
  const { data: questions, isLoading } = useTaskQuestions(taskId)
  const question = (questions ?? []).find((q) => q.id === questionId)

  if (isLoading) return <div className="questions-screen__detail-loading">Loading…</div>
  if (!question) return <div className="questions-screen__detail-loading">Thread is gone.</div>
  return <QuestionThread taskId={taskId} question={question} />
}

/** TaskThreadDetail for a role thread (docs/10-agents.md «Q&A-треды роли»). */
function RoleThreadDetail({ roleId, questionId }: { roleId: string; questionId: number }) {
  const { data: questions, isLoading } = useAgentQuestions(roleId)
  const question = (questions ?? []).find((q) => q.id === questionId)

  if (isLoading) return <div className="questions-screen__detail-loading">Loading…</div>
  if (!question) return <div className="questions-screen__detail-loading">Thread is gone.</div>
  return <AgentQuestionThread roleId={roleId} question={question} />
}

/**
 * Who a thread is waiting for, in words. An fyi note and a resolved thread
 * wait for nobody and get no chip at all — putting one there is exactly the
 * "21 open questions" inflation this redesign is undoing.
 */
function turnLabel(entry: ThreadInboxEntry): string {
  if (entry.status !== 'open' || entry.type === 'fyi') return ''
  if (entry.your_turn) return 'awaiting you'
  const others = entry.attention.filter((p) => !isHuman(p))
  if (others.length === 0) return ''
  return `awaiting ${others.map((id) => participantLabel(id)).join(', ')}`
}

interface ThreadRowProps {
  entry: ThreadInboxEntry
}

function ThreadRow({ entry }: ThreadRowProps) {
  const [open, setOpen] = useState(false)
  const turn = turnLabel(entry)

  return (
    <div className="questions-screen__row">
      <div className="questions-screen__row-head">
        <button
          type="button"
          className="questions-screen__row-main"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          <span className="questions-screen__ref">{entry.local_ref}</span>
          {entry.type === 'fyi' && <span className="questions-screen__fyi">fyi</span>}
          {entry.stale && (
            <span
              className="questions-screen__stale"
              title="Nobody has moved this thread in over a day"
            >
              stale
            </span>
          )}
          {turn && (
            <span
              className={
                entry.your_turn
                  ? 'questions-screen__turn questions-screen__turn--warn'
                  : 'questions-screen__turn'
              }
            >
              {turn}
            </span>
          )}
          <span className="questions-screen__body">{entry.body}</span>
          <span className="questions-screen__age">{timeAgo(entry.updated_at)}</span>
          <span className="questions-screen__chevron">{open ? '▴' : '▾'}</span>
        </button>
      </div>

      {/* The subject line: a task thread links to its task, a role thread to
          the role. Built from the id fields, never parsed out of `subject`. */}
      <div className="questions-screen__subject">
        {entry.kind === 'task' && entry.project_id && entry.task_id !== undefined ? (
          <Link to={`/p/${entry.project_id}/tasks/${entry.task_id}`}>
            #{entry.task_id} {entry.task_title}
          </Link>
        ) : entry.kind === 'role' && entry.role_id ? (
          <Link to={`/agents/${entry.role_id}`}>role {entry.role_id}</Link>
        ) : (
          <span>{entry.subject}</span>
        )}
      </div>

      {open && entry.kind === 'task' && entry.task_id !== undefined && (
        <TaskThreadDetail taskId={entry.task_id} questionId={entry.id} />
      )}
      {open && entry.kind === 'role' && entry.role_id && (
        <RoleThreadDetail roleId={entry.role_id} questionId={entry.id} />
      )}
    </div>
  )
}

export function QuestionsScreen() {
  // Off by default on purpose: the human sees every open thread until they
  // ask for less. Grouping is by `your_turn`, the caller-relative field — a
  // thread can wait on another participant without waiting on you.
  const [onlyMine, setOnlyMine] = useState(false)
  const [showResolved, setShowResolved] = useState(false)
  const { data: threads, isLoading } = useThreads({ all: showResolved })

  const rows = (threads ?? []).filter((t) => (onlyMine ? t.your_turn : true))
  const open = rows.filter((t) => t.status === 'open')
  const history = rows.filter((t) => t.status !== 'open')
  const awaitingYou = open.filter((t) => t.your_turn)
  const awaitingOthers = open.filter((t) => !t.your_turn)

  return (
    <div className="questions-screen">
      <div className="questions-screen__head">
        <h1 className="questions-screen__title">Open questions</h1>
        <label className="questions-screen__filter">
          <input
            type="checkbox"
            checked={onlyMine}
            onChange={(e) => setOnlyMine(e.target.checked)}
          />
          Waiting on me
        </label>
        <label className="questions-screen__filter">
          <input
            type="checkbox"
            checked={showResolved}
            onChange={(e) => setShowResolved(e.target.checked)}
          />
          Show resolved
        </label>
      </div>
      {isLoading && <p className="questions-screen__empty">Loading…</p>}
      {!isLoading && rows.length === 0 && <p className="questions-screen__empty">No open threads.</p>}
      {awaitingYou.length > 0 && (
        <>
          <div className="questions-screen__label">Awaiting you</div>
          {awaitingYou.map((t) => (
            <ThreadRow key={t.id} entry={t} />
          ))}
        </>
      )}
      {awaitingOthers.length > 0 && (
        <>
          <div className="questions-screen__label">Awaiting others</div>
          {awaitingOthers.map((t) => (
            <ThreadRow key={t.id} entry={t} />
          ))}
        </>
      )}
      {history.length > 0 && (
        <>
          <div className="questions-screen__label">History</div>
          {history.map((t) => (
            <ThreadRow key={t.id} entry={t} />
          ))}
        </>
      )}
    </div>
  )
}
