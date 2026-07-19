// Task card screen (docs/design/Task.dc.html): the richest screen in the
// dashboard — header, an open-question banner, five tabs (Questions /
// Overview / Docs / Journal / Messages), and a right rail of the task's
// orchestrator + worker sessions.

import { useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import { useProjects, useSessions, useTask, useTaskDocs, useTaskLog, useTaskQuestions, useMessages } from '../../lib/queries'
import type { TaskStatus } from '../../lib/types'
import { DocsTab } from './DocsTab'
import { JournalTab } from './JournalTab'
import { MessagesTab } from './MessagesTab'
import { OverviewTab } from './OverviewTab'
import { QuestionBanner } from './QuestionBanner'
import { QuestionsTab } from './QuestionsTab'
import { SessionRail } from './SessionRail'
import './TaskScreen.css'
import { TermOverlay } from './TermOverlay'

type TabId = 'questions' | 'overview' | 'docs' | 'journal' | 'messages'

const STATUS_LABEL: Record<TaskStatus, string> = {
  backlog: 'Backlog',
  in_progress: 'In Progress',
  review: 'Review',
  done: 'Done',
  cancelled: 'Cancelled',
}

const STATUS_TONE: Record<TaskStatus, BadgeTone> = {
  backlog: 'neutral',
  in_progress: 'indigo',
  review: 'review',
  done: 'ok',
  cancelled: 'neutral',
}

export function TaskScreen() {
  const { projectId, taskId: taskIdParam } = useParams<{ projectId: string; taskId: string }>()
  const taskId = taskIdParam ? Number(taskIdParam) : undefined

  const [tab, setTab] = useState<TabId>('overview')
  const [termSession, setTermSession] = useState<{ id: string; tmux_name: string } | null>(null)

  const { data: projects } = useProjects()
  const { data: task } = useTask(taskId)
  const { data: docs } = useTaskDocs(taskId)
  const { data: log } = useTaskLog(taskId)
  const { data: questions } = useTaskQuestions(taskId)
  const { data: sessions } = useSessions(projectId ? { project: projectId } : undefined)
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

  if (!projectId || !taskId || !task) return null

  const project = projects?.find((p) => p.id === projectId)

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
          <Link to={`/p/${projectId}`} className="task-screen__back">
            ← {project?.name ?? projectId} board
          </Link>

          <div className="task-screen__title-row">
            <span className="task-screen__id">#{task.id}</span>
            <h1 className="task-screen__title">{task.title}</h1>
            <Badge tone={STATUS_TONE[task.status]}>{STATUS_LABEL[task.status]}</Badge>
          </div>

          <div className="task-screen__meta">
            {task.feature_slug && <span className="task-screen__meta-mono">feature/{task.feature_slug}</span>}
            <span className="task-screen__meta-dot">·</span>
            <span>created by {task.created_by === 'user' ? 'you' : 'orchestrator'} · {timeAgo(task.created_at)}</span>
            <span className="task-screen__meta-dot">·</span>
            <span>updated {timeAgo(task.updated_at)}</span>
          </div>

          {bannerQuestion && (
            <QuestionBanner question={bannerQuestion} onOpen={() => setTab('questions')} />
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
            <QuestionsTab taskId={taskId} questions={questions ?? []} orchestratorName={orchestrator?.tmux_name} />
          )}
          {tab === 'overview' && (
            <OverviewTab
              projectId={projectId}
              task={task}
              subtasks={task.subtasks}
              sessions={sessions}
              docs={docs}
            />
          )}
          {tab === 'docs' && <DocsTab docs={docs ?? []} />}
          {tab === 'journal' && <JournalTab log={log ?? []} />}
          {tab === 'messages' && <MessagesTab session={task.session} messages={messages ?? []} />}
        </div>
      </div>

      <SessionRail
        orchestrator={orchestrator}
        workers={workers}
        onOpenTerm={(session) => setTermSession(session)}
      />

      {termSession && <TermOverlay session={termSession} onClose={() => setTermSession(null)} />}
    </div>
  )
}
