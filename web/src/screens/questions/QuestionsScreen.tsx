// Global Questions page (/questions): every open question across all
// projects, grouped by whose turn it is, answerable in place via the shared
// QuestionThread. Spec: docs/superpowers/specs/2026-07-21-questions-
// visibility-design.md §2.

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
  const awaitingYou = (questions ?? []).filter((q) => q.whose_turn === 'user')
  const awaitingOrch = (questions ?? []).filter((q) => q.whose_turn !== 'user')

  return (
    <div className="questions-screen">
      <h1 className="questions-screen__title">Open questions</h1>
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
      {awaitingOrch.length > 0 && (
        <>
          <div className="questions-screen__label">Awaiting orchestrator</div>
          {awaitingOrch.map((q) => (
            <GlobalThread key={q.id} q={q} />
          ))}
        </>
      )}
    </div>
  )
}
