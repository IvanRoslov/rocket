// Global Questions page (/questions): every open question across all
// projects, grouped by whose turn it is, answerable in place via the shared
// QuestionThread. Spec: docs/superpowers/specs/2026-07-21-questions-
// visibility-design.md §2.

import { useState } from 'react'
import { Link } from 'react-router-dom'
import { QuestionThread } from '../../components/QuestionThread'
import { useOpenQuestions } from '../../lib/queries'
import type { GlobalQuestion } from '../../lib/types'
import './QuestionsScreen.css'

function GlobalThread({ q }: { q: GlobalQuestion }) {
  return (
    <div className="questions-screen__item">
      <Link to={`/p/${q.project_id}/tasks/${q.task_id}`} className="questions-screen__task">
        {q.project_name || q.project_id} · #{q.task_id} {q.task_title}
      </Link>
      <QuestionThread taskId={q.task_id} question={q} orchestratorName={q.orchestrator_name} />
    </div>
  )
}

export function QuestionsScreen() {
  const { data: questions, isLoading } = useOpenQuestions()
  // Off by default on purpose: the human sees every open thread until they
  // ask for less. Threads are grouped by `your_turn`, the caller-relative
  // field — a thread can wait on another participant without waiting on you.
  const [onlyMine, setOnlyMine] = useState(false)
  const awaitingYou = (questions ?? []).filter((q) => q.your_turn)
  const awaitingOthers = onlyMine ? [] : (questions ?? []).filter((q) => !q.your_turn)

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
      </div>
      {isLoading && <p className="questions-screen__empty">Loading…</p>}
      {!isLoading && (questions ?? []).length === 0 && (
        <p className="questions-screen__empty">No open questions.</p>
      )}
      {awaitingYou.length > 0 && (
        <>
          <div className="questions-screen__label">Awaiting you</div>
          {awaitingYou.map((q) => (
            <GlobalThread key={q.id} q={q} />
          ))}
        </>
      )}
      {awaitingOthers.length > 0 && (
        <>
          <div className="questions-screen__label">Awaiting others</div>
          {awaitingOthers.map((q) => (
            <GlobalThread key={q.id} q={q} />
          ))}
        </>
      )}
    </div>
  )
}
