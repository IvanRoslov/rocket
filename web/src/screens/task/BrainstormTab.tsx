// Brainstorm tab (task #4901 spec §3.1): the whole storm in one place — its
// counters, the problem it starts from, the storm questions in the order they
// were asked, and the exit gate the human decides with Go / Needs changes.

import { useState, type ReactNode } from 'react'
import { BrainstormResult } from '../../components/BrainstormResult'
import { Button } from '../../components/Button'
import { Markdown } from '../../components/Markdown'
import { QuestionContent } from '../../components/QuestionContent'
import { QuestionThread } from '../../components/QuestionThread'
import { ApiError } from '../../lib/api'
import { timeAgo } from '../../lib/format'
import {
  useDecideGate,
  useSetOutcome,
  useTaskBrainstormStats,
  useTaskDocHistory,
  useTaskGates,
} from '../../lib/queries'
import { questionTitle } from '../../lib/thread'
import type { BrainstormStorm, Question, TaskDoc, TaskDocKind, TaskGate } from '../../lib/types'
import './BrainstormTab.css'

export interface BrainstormTabProps {
  taskId: number
  docs: TaskDoc[]
  questions: Question[]
  orchestratorName?: string
}

/** The newest doc of `kind` — the most recently written, whatever its title. */
function latestDoc(docs: TaskDoc[], kind: TaskDocKind): TaskDoc | undefined {
  return docs.filter((d) => d.kind === kind).sort((a, b) => b.id - a.id)[0]
}

/**
 * The doc a gate pinned. A gate records only kind + version (the newest doc
 * of that kind when it was requested), so among every version in the history
 * it is the latest doc of that kind and version written by request time.
 */
function pinnedDoc(history: TaskDoc[], kind: TaskDocKind, version: number, requestedAt: number): TaskDoc | undefined {
  return history
    .filter((d) => d.kind === kind && d.version === version && d.created_at <= requestedAt)
    .sort((a, b) => b.id - a.id)[0]
}

function Counters({ stats }: { stats?: BrainstormStorm }) {
  const v = (n: number | undefined) => (n === undefined ? '—' : String(n))
  const items: Array<[string, string]> = [
    ['Questions', v(stats?.questions)],
    [
      'Accepted',
      stats ? `${stats.accepted} (with comment ${stats.accepted_with_comment})` : '—',
    ],
    ['Corrected', v(stats?.corrected)],
    ['Wrong turn', v(stats?.wrong_turn)],
    ['Spec changes', v(stats?.spec_changes)],
  ]
  return (
    <dl className="brainstorm-tab__counters">
      {items.map(([label, value]) => (
        <div key={label} className="brainstorm-tab__counter">
          <dt className="brainstorm-tab__counter-label">{label}</dt>
          <dd className="brainstorm-tab__counter-value" data-testid="value">
            {value}
          </dd>
        </div>
      ))}
    </dl>
  )
}

/** An answered (or dismissed) storm question: what was decided stays in view. */
function ClosedStormQuestion({ taskId, question }: { taskId: number; question: Question }) {
  const [open, setOpen] = useState(false)
  const setOutcome = useSetOutcome()

  return (
    <div className="brainstorm-tab__closed">
      <div className="brainstorm-tab__closed-head">
        <span className="brainstorm-tab__ref">{question.local_ref ?? `Q${question.ordinal}`}</span>
        <span className="brainstorm-tab__closed-title">{questionTitle(question)}</span>
        {question.resolution === 'dismissed' && <span className="brainstorm-tab__dismissed">dismissed</span>}
        <span className="brainstorm-tab__when">
          {question.resolved_at ? timeAgo(question.resolved_at) : ''}
        </span>
      </div>
      <BrainstormResult
        question={question}
        busy={setOutcome.isPending}
        error={setOutcome.error?.message}
        onOverride={(outcome) => setOutcome.mutate({ id: question.id, taskId, outcome })}
      />
      <button
        type="button"
        className="brainstorm-tab__toggle"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        {open ? 'Hide question and discussion' : `Question and discussion · ${question.messages.length}`}
      </button>
      {open && (
        <div className="brainstorm-tab__closed-detail">
          <QuestionContent brief={question.brief} body={question.body} bodyClassName="questions-tab__resolved-question" />
          {question.messages.map((m) => (
            <div key={m.id} className="brainstorm-tab__message">
              <Markdown compact>{m.body}</Markdown>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function gateHistoryLine(g: TaskGate): string {
  switch (g.status) {
    case 'go':
      return `v${g.spec_version} — Go`
    case 'changes':
      return `v${g.spec_version} — changes: “${g.comment}”`
    case 'superseded':
      return `v${g.spec_version} — superseded by a newer spec`
    default:
      return `v${g.spec_version} — pending`
  }
}

/** Why a decision was refused, in words a human can act on. */
function decideError(err: unknown): string {
  if (err instanceof ApiError && err.status === 409) {
    return 'This gate is no longer current: the spec changed after it was requested, or it was already decided. Refreshed.'
  }
  return err instanceof Error ? `Failed: ${err.message}` : 'Failed'
}

/** What the exit block waits for when no gate is pending. */
function waitingText(latest: TaskGate | undefined, specVersion: number | undefined): string {
  if (specVersion === undefined) return 'Waiting for spec'
  // A spec newer than the last gate (or no gate yet) only needs a request;
  // after "changes" on the current spec, a revised spec comes first.
  if (!latest || specVersion > latest.spec_version || latest.status === 'superseded') {
    return 'Waiting for gate request'
  }
  return 'Waiting for spec'
}

function GateBlock({ taskId, docs }: { taskId: number; docs: TaskDoc[] }) {
  const gatesQuery = useTaskGates(taskId)
  const { data: history } = useTaskDocHistory(taskId)
  const decide = useDecideGate()
  const [shown, setShown] = useState<'spec' | 'plan' | null>(null)
  const [changesOpen, setChangesOpen] = useState(false)
  const [comment, setComment] = useState('')

  const list = gatesQuery.data ?? []
  const pending = list.find((g) => g.status === 'pending')
  const latest = list[0]
  const decided = list.filter((g) => g.status !== 'pending')

  // A new pending gate is a new question: drop the old answer-in-progress
  // and the old refusal.
  const [seenPendingId, setSeenPendingId] = useState(pending?.id)
  if (seenPendingId !== pending?.id) {
    setSeenPendingId(pending?.id)
    setShown(null)
    setChangesOpen(false)
    setComment('')
    decide.reset()
  }

  const shownDoc =
    pending && shown
      ? shown === 'spec'
        ? pinnedDoc(history ?? [], 'spec', pending.spec_version, pending.requested_at)
        : pending.plan_version !== null
          ? pinnedDoc(history ?? [], 'plan', pending.plan_version, pending.requested_at)
          : undefined
      : undefined

  function send(decision: 'go' | 'changes') {
    if (!pending) return
    decide.mutate(
      { gateId: pending.id, taskId, decision, comment: decision === 'changes' ? comment.trim() : '' },
      {
        onSuccess: () => {
          setChangesOpen(false)
          setComment('')
        },
      },
    )
  }

  let state: ReactNode
  if (gatesQuery.isError) {
    state = (
      <p className="brainstorm-tab__error" role="alert">
        Could not load the gates: {gatesQuery.error.message}
      </p>
    )
  } else if (gatesQuery.isLoading) {
    state = <p className="brainstorm-tab__empty">Loading…</p>
  } else if (pending) {
    state = (
      <>
        <div className="brainstorm-tab__gate-versions">
          <button type="button" className="brainstorm-tab__doc-link" onClick={() => setShown(shown === 'spec' ? null : 'spec')}>
            Spec v{pending.spec_version}
          </button>
          {pending.plan_version !== null && (
            <>
              <span aria-hidden="true">·</span>
              <button type="button" className="brainstorm-tab__doc-link" onClick={() => setShown(shown === 'plan' ? null : 'plan')}>
                Plan v{pending.plan_version}
              </button>
            </>
          )}
          <span className="brainstorm-tab__when">requested {timeAgo(pending.requested_at)}</span>
        </div>
        {shown && (
          <div className="brainstorm-tab__doc">
            {shownDoc ? (
              <Markdown>{shownDoc.body}</Markdown>
            ) : history === undefined ? (
              <p>Loading…</p>
            ) : (
              <p>This document version is not available.</p>
            )}
          </div>
        )}
        <div className="brainstorm-tab__gate-actions">
          <Button variant="primary" onClick={() => send('go')} disabled={decide.isPending}>
            Go
          </Button>
          <Button variant="secondary" onClick={() => setChangesOpen((o) => !o)} disabled={decide.isPending}>
            Needs changes
          </Button>
        </div>
        {changesOpen && (
          <div className="brainstorm-tab__changes">
            <textarea
              aria-label="What to change"
              placeholder="What should change in the spec — sent to the orchestrator"
              rows={3}
              value={comment}
              onChange={(e) => setComment(e.target.value)}
            />
            <Button
              variant="secondary"
              onClick={() => send('changes')}
              disabled={decide.isPending || !comment.trim()}
            >
              Send changes
            </Button>
          </div>
        )}
      </>
    )
  } else if (latest?.status === 'go') {
    state = (
      <p className="brainstorm-tab__gate-state">
        Go on spec v{latest.spec_version}
        {latest.plan_version !== null ? ` · plan v${latest.plan_version}` : ''}
      </p>
    )
  } else {
    state = <p className="brainstorm-tab__gate-state">{waitingText(latest, latestDoc(docs, 'spec')?.version)}</p>
  }

  return (
    <section className="brainstorm-tab__section brainstorm-tab__gate" aria-label="Storm exit">
      <h3 className="brainstorm-tab__heading">Storm exit</h3>
      {state}

      {decide.isError && (
        <p className="brainstorm-tab__error" role="alert">
          {decideError(decide.error)}
        </p>
      )}

      {decided.length > 0 && (
        <ul className="brainstorm-tab__history" aria-label="Gate history">
          {[...decided].reverse().map((g) => (
            <li key={g.id}>{gateHistoryLine(g)}</li>
          ))}
        </ul>
      )}
    </section>
  )
}

export function BrainstormTab({ taskId, docs, questions, orchestratorName }: BrainstormTabProps) {
  const { data: stats } = useTaskBrainstormStats(taskId)
  const problem = latestDoc(docs, 'problem')
  const storm = questions.filter((q) => q.type === 'brainstorm').sort((a, b) => a.ordinal - b.ordinal)

  return (
    <section className="brainstorm-tab" aria-label="Brainstorm">
      <Counters stats={stats} />

      <section className="brainstorm-tab__section" aria-label="Problem">
        <h3 className="brainstorm-tab__heading">
          Problem{problem ? <span className="brainstorm-tab__when"> v{problem.version}</span> : null}
        </h3>
        {problem ? (
          <div className="brainstorm-tab__doc">
            <Markdown>{problem.body}</Markdown>
          </div>
        ) : (
          <p className="brainstorm-tab__empty">Problem not written yet</p>
        )}
      </section>

      <section className="brainstorm-tab__section" aria-label="Storm questions">
        <h3 className="brainstorm-tab__heading">Questions</h3>
        {storm.length === 0 && <p className="brainstorm-tab__empty">No storm questions yet</p>}
        {storm.map((q) =>
          q.status === 'open' ? (
            <QuestionThread key={q.id} taskId={taskId} question={q} orchestratorName={orchestratorName} />
          ) : (
            <ClosedStormQuestion key={q.id} taskId={taskId} question={q} />
          ),
        )}
      </section>

      <GateBlock taskId={taskId} docs={docs} />
    </section>
  )
}
