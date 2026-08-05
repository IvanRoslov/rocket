// Overview tab (docs/design/Task.dc.html "OVERVIEW"): markdown description,
// the subtask decomposition (status + PR/CI derived from each subtask's
// worker session), and the final report doc if the orchestrator has written
// one (`kind: "report"`).

import { useState } from 'react'
import { Markdown } from '../../components/Markdown'
import { Link } from 'react-router-dom'
import { useUpdateTask } from '../../lib/queries'
import type { Session, Task, TaskDoc, TaskStatus } from '../../lib/types'
import './OverviewTab.css'

export interface OverviewTabProps {
  /** Undefined for a milestone — it belongs to no project (task #1023, spec v2). */
  projectId?: string
  /** Where a subtask card links; the task screen knows which route family it is on. */
  taskPath: (id: number) => string
  task: Task
  subtasks: Task[]
  sessions: Session[] | undefined
  docs: TaskDoc[] | undefined
}

const STATUS_LABEL: Record<TaskStatus, string> = {
  backlog: 'backlog',
  brainstorm: 'brainstorm',
  in_progress: 'in progress',
  review: 'review',
  done: 'done',
  cancelled: 'cancelled',
}

const STATUS_TONE: Record<TaskStatus, string> = {
  backlog: 'overview-tab__status--neutral',
  brainstorm: 'overview-tab__status--review',
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

export function OverviewTab({ task, subtasks, sessions, docs, taskPath }: OverviewTabProps) {
  const report = docs?.find((d) => d.kind === 'report')

  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(task.title)
  const [description, setDescription] = useState(task.description ?? '')
  const update = useUpdateTask()

  function startEdit() {
    setTitle(task.title)
    setDescription(task.description ?? '')
    update.reset()
    setEditing(true)
  }

  function cancelEdit() {
    update.reset()
    setEditing(false)
  }

  function handleSave() {
    const trimmedTitle = title.trim()
    if (!trimmedTitle) return
    update.mutate(
      { id: task.id, title: trimmedTitle, description },
      { onSuccess: () => setEditing(false) },
    )
  }

  return (
    <div className="overview-tab">
      {editing ? (
        <div className="overview-tab__edit-form">
          <input
            aria-label="Task title"
            className="overview-tab__edit-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            disabled={update.isPending}
          />
          <textarea
            aria-label="Task description"
            className="overview-tab__edit-description"
            placeholder="Markdown description…"
            rows={8}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            disabled={update.isPending}
          />
          {update.isError && <div className="overview-tab__edit-error">{update.error.message}</div>}
          <div className="overview-tab__edit-actions">
            <button
              type="button"
              className="overview-tab__edit-save"
              onClick={handleSave}
              disabled={update.isPending || !title.trim()}
            >
              Save
            </button>
            <button
              type="button"
              className="overview-tab__edit-cancel"
              onClick={cancelEdit}
              disabled={update.isPending}
            >
              Cancel
            </button>
          </div>
        </div>
      ) : (
        <>
          <button type="button" className="overview-tab__edit-toggle" onClick={startEdit} aria-label="Edit task">
            ✎ Edit
          </button>
          {task.description ? (
            <div className="overview-tab__description">
              <Markdown>{task.description}</Markdown>
            </div>
          ) : (
            <p className="overview-tab__description-empty">No description — click Edit to add one.</p>
          )}
        </>
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
                <Link key={s.id} to={taskPath(s.id)} className="overview-tab__subtask">
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
