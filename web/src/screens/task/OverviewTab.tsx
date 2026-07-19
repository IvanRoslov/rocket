// Overview tab (docs/design/Task.dc.html "OVERVIEW"): markdown description,
// the subtask decomposition (status + PR/CI derived from each subtask's
// worker session), and the final report doc if the orchestrator has written
// one (`kind: "report"`).

import { Markdown } from '../../components/Markdown'
import { Link } from 'react-router-dom'
import type { Session, Task, TaskDoc, TaskStatus } from '../../lib/types'
import './OverviewTab.css'

export interface OverviewTabProps {
  projectId: string
  task: Task
  subtasks: Task[]
  sessions: Session[] | undefined
  docs: TaskDoc[] | undefined
}

const STATUS_LABEL: Record<TaskStatus, string> = {
  backlog: 'backlog',
  in_progress: 'in progress',
  review: 'review',
  done: 'done',
  cancelled: 'cancelled',
}

const STATUS_TONE: Record<TaskStatus, string> = {
  backlog: 'overview-tab__status--neutral',
  in_progress: 'overview-tab__status--indigo',
  review: 'overview-tab__status--review',
  done: 'overview-tab__status--ok',
  cancelled: 'overview-tab__status--neutral',
}

function subtaskDot(session: Session | undefined): string {
  if (!session) return 'overview-tab__dot--neutral'
  if (session.state === 'errored' || session.activity === 'blocked') return 'overview-tab__dot--err'
  if (session.activity === 'active' || session.activity === 'ready') return 'overview-tab__dot--ok'
  return 'overview-tab__dot--neutral'
}

function prLabel(session: Session | undefined): { text: string; tone: string } {
  if (!session) return { text: 'PR —', tone: 'overview-tab__pr--neutral' }
  if (!session.pr_number) {
    if (session.activity === 'blocked') return { text: 'PR — blocked', tone: 'overview-tab__pr--err' }
    return { text: 'PR —', tone: 'overview-tab__pr--neutral' }
  }
  // pr_state 'closed'/'merged' aren't CI-relevant — show them directly
  // instead of falling through to ci_state.
  if (session.pr_state === 'closed') return { text: `PR #${session.pr_number} closed`, tone: 'overview-tab__pr--neutral' }
  if (session.pr_state === 'merged') return { text: `PR #${session.pr_number} merged`, tone: 'overview-tab__pr--ok' }
  if (session.ci_state === 'passing') return { text: `PR #${session.pr_number} ✔`, tone: 'overview-tab__pr--ok' }
  if (session.ci_state === 'failing') return { text: `PR #${session.pr_number} ✗`, tone: 'overview-tab__pr--err' }
  if (session.ci_state === 'pending') return { text: `PR #${session.pr_number} running`, tone: 'overview-tab__pr--warn' }
  return { text: `PR #${session.pr_number}`, tone: 'overview-tab__pr--neutral' }
}

export function OverviewTab({ projectId, task, subtasks, sessions, docs }: OverviewTabProps) {
  const report = docs?.find((d) => d.kind === 'report')

  return (
    <div className="overview-tab">
      {task.description ? (
        <div className="overview-tab__description">
          <Markdown>{task.description}</Markdown>
        </div>
      ) : (
        <p className="overview-tab__description-empty">No description.</p>
      )}

      {task.parent_id === undefined && (
        <>
          <h3 className="overview-tab__heading">Subtasks · decomposition</h3>
          <div className="overview-tab__subtasks">
            {subtasks.length === 0 && <p className="overview-tab__empty">No subtasks yet.</p>}
            {subtasks.map((s) => {
              const session = s.session_id ? sessions?.find((sess) => sess.id === s.session_id) : undefined
              const pr = prLabel(session)
              return (
                <Link key={s.id} to={`/p/${projectId}/tasks/${s.id}`} className="overview-tab__subtask">
                  <span className={`overview-tab__dot ${subtaskDot(session)}`} />
                  <span className="overview-tab__subtask-id">#{s.id}</span>
                  <div className="overview-tab__subtask-main">
                    <div className="overview-tab__subtask-title">{s.title}</div>
                    <div className="overview-tab__subtask-meta">
                      {session ? session.tmux_name : '—'} · {s.repo_id ?? '—'}
                    </div>
                  </div>
                  <span className={`overview-tab__status ${STATUS_TONE[s.status]}`}>{STATUS_LABEL[s.status]}</span>
                  <span className={`overview-tab__pr ${pr.tone}`}>{pr.text}</span>
                </Link>
              )
            })}
          </div>
        </>
      )}

      <div className="overview-tab__report">
        <div className="overview-tab__report-label">Final report</div>
        {report ? (
          <div className="overview-tab__report-body">
            <Markdown>{report.body}</Markdown>
          </div>
        ) : (
          <div className="overview-tab__report-empty">
            Not available yet — orchestrator writes <code>report</code> when the feature is ready.
          </div>
        )}
      </div>
    </div>
  )
}
