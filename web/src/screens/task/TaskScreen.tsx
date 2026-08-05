// Task card screen (docs/design/Task.dc.html): the richest screen in the
// dashboard — header, an open-question banner, five tabs (Questions /
// Overview / Docs / Journal / Messages), and a right rail of the task's
// orchestrator + worker sessions.

import { useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import { useProjects, useSessions, useTask, useTaskDocs, useTaskLog, useTaskQuestions, useMessages } from '../../lib/queries'
import type { TaskCreatedBy, TaskStatus } from '../../lib/types'
import { DocsTab } from './DocsTab'
import { JournalTab } from './JournalTab'
import { MessagesTab } from './MessagesTab'
import { OverviewTab } from './OverviewTab'
import { QuestionBanner } from './QuestionBanner'
import { QuestionsTab } from './QuestionsTab'
import { AgentRail } from '../milestones/AgentRail'
import { SessionRail } from './SessionRail'
import './TaskScreen.css'

type TabId = 'questions' | 'overview' | 'docs' | 'journal' | 'messages'

const STATUS_LABEL: Record<TaskStatus, string> = {
  backlog: 'Backlog',
  brainstorm: 'Brainstorm',
  in_progress: 'In Progress',
  review: 'Review',
  done: 'Done',
  cancelled: 'Cancelled',
}

const STATUS_TONE: Record<TaskStatus, BadgeTone> = {
  backlog: 'neutral',
  brainstorm: 'review',
  in_progress: 'indigo',
  review: 'review',
  done: 'ok',
  cancelled: 'neutral',
}

const CREATED_BY_LABEL: Record<TaskCreatedBy, string> = {
  user: 'you',
  orchestrator: 'orchestrator',
  agent: 'agent',
}

export function TaskScreen() {
  const { projectId, taskId: taskIdParam } = useParams<{ projectId: string; taskId: string }>()
  const taskId = taskIdParam ? Number(taskIdParam) : undefined

  const [tab, setTab] = useState<TabId>('overview')

  const { data: projects } = useProjects()
  const { data: task } = useTask(taskId)
  const { data: parentTask } = useTask(task?.parent_id)
  const { data: docs } = useTaskDocs(taskId)
  const { data: log } = useTaskLog(taskId)
  const { data: questions } = useTaskQuestions(taskId)
  const { data: sessions } = useSessions(projectId ? { project: projectId } : undefined)
  // Separate query, scoped to the Overview tab's subtask→PR join: a done
  // worker (PR merged) drops out of the live-only `sessions` above, so the
  // join needs `all: true` to still resolve its pr_number/pr_state. The
  // rail/orchestrator/workers above intentionally stay live-only.
  const { data: allSessions } = useSessions(projectId ? { project: projectId, all: true } : undefined)
  const { data: messages } = useMessages(task?.session?.id)

  const orchestrator = useMemo(
    () => (task?.session ? sessions?.find((s) => s.id === task.session!.id) : undefined),
    [sessions, task?.session],
  )
  const workers = useMemo(
    () => (orchestrator ? (sessions?.filter((s) => s.parent_id === orchestrator.id) ?? []) : []),
    [sessions, orchestrator],
  )

  const openQuestions = useMemo(
    () => (questions ?? []).filter((q) => q.status === 'open').sort((a, b) => a.ordinal - b.ordinal),
    [questions],
  )
  const bannerQuestion = openQuestions[0]

  if (!taskId || !task) return null

  // A milestone (task #1023, spec v2) belongs to no project: it is reached at
  // /milestones/:taskId, goes back to the milestones board, and shows the
  // agent holding it where a project task shows its feature branch.
  const isMilestone = task.milestone === true
  const project = projects?.find((p) => p.id === projectId)
  if (!projectId && !isMilestone) return null
  const backPath = isMilestone ? '/milestones' : `/p/${projectId}`
  const backLabel = isMilestone ? 'Milestones' : `${project?.name ?? projectId} board`
  const taskPath = (id: number) => (isMilestone ? `/milestones/${id}` : `/p/${projectId}/tasks/${id}`)

  const tabs: Array<{ id: TabId; label: string; count?: number; warn?: boolean }> = [
    { id: 'questions', label: 'Questions', count: openQuestions.length || undefined, warn: openQuestions.length > 0 },
    { id: 'overview', label: 'Overview' },
    { id: 'docs', label: 'Docs', count: docs?.length },
    { id: 'journal', label: 'Journal', count: log?.length },
    { id: 'messages', label: 'Messages', count: messages?.length },
  ]

  return (
    <div className="task-screen">
      <div className="task-screen__content">
        <div className="task-screen__inner">
          <div className="task-screen__crumbs">
            <Link to={backPath} className="task-screen__back">
              ← {backLabel}
            </Link>
            {task.parent_id !== undefined && (
              <Link to={taskPath(task.parent_id)} className="task-screen__back">
                ← #{task.parent_id} {parentTask?.title ?? '…'}
              </Link>
            )}
          </div>

          <div className="task-screen__title-row">
            <span className="task-screen__id">#{task.id}</span>
            <h1 className="task-screen__title">{task.title}</h1>
            <Badge tone={STATUS_TONE[task.status]}>{STATUS_LABEL[task.status]}</Badge>
          </div>

          <div className="task-screen__meta">
            {isMilestone && (
              <span className="task-screen__meta-mono">
                {task.assigned_role ? `◆ ${task.assigned_role}` : 'not taken'}
              </span>
            )}
            {task.feature_slug && <span className="task-screen__meta-mono">feature/{task.feature_slug}</span>}
            <span className="task-screen__meta-dot">·</span>
            <span>created by {CREATED_BY_LABEL[task.created_by] ?? task.created_by} · {timeAgo(task.created_at)}</span>
            <span className="task-screen__meta-dot">·</span>
            <span>updated {timeAgo(task.updated_at)}</span>
          </div>

          {bannerQuestion && (
            <QuestionBanner taskId={task.id} question={bannerQuestion} onOpen={() => setTab('questions')} />
          )}

          <div className="task-screen__tabs" role="tablist">
            {tabs.map((t) => {
              const active = tab === t.id
              return (
                <button
                  key={t.id}
                  type="button"
                  role="tab"
                  aria-selected={active}
                  className={active ? 'task-screen__tab task-screen__tab--active' : 'task-screen__tab'}
                  onClick={() => setTab(t.id)}
                  style={{ color: active ? undefined : t.warn ? 'var(--warn-text)' : undefined }}
                >
                  {t.label}
                  {t.count !== undefined && (
                    <span
                      className="task-screen__tab-count"
                      style={{ color: t.warn ? 'var(--warn-text)' : undefined }}
                    >
                      {t.count}
                    </span>
                  )}
                </button>
              )
            })}
          </div>

          {tab === 'questions' && (
            <QuestionsTab
              taskId={taskId}
              questions={questions ?? []}
              orchestratorName={orchestrator?.tmux_name}
              hasLiveOrchestrator={orchestrator?.state === 'running'}
            />
          )}
          {tab === 'overview' && (
            <OverviewTab
              projectId={projectId}
              taskPath={taskPath}
              task={task}
              subtasks={task.subtasks}
              sessions={allSessions}
              docs={docs}
            />
          )}
          {tab === 'docs' && <DocsTab docs={docs ?? []} />}
          {tab === 'journal' && <JournalTab log={log ?? []} />}
          {tab === 'messages' && <MessagesTab session={task.session} messages={messages ?? []} />}
        </div>
      </div>

      {isMilestone ? (
        <AgentRail agentId={task.assigned_role} />
      ) : (
        <SessionRail orchestrator={orchestrator} workers={workers} />
      )}
    </div>
  )
}
