// Agent card (docs/11-dashboard.md): one agent — its registration, the
// actions you have over it (write to it, attach to its session, start or stop
// it) and the two things rocket keeps for it: Q&A threads and the inbox.

import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { Button } from '../../components/Button'
import { timeAgo } from '../../lib/format'
import {
  useAgent,
  useAgentQuestions,
  useDeleteAgent,
  useProjects,
  useSendAgentMessage,
  useSetAgentEnabled,
  useStartAgent,
  useStopAgent,
} from '../../lib/queries'
import type { TaskStatus } from '../../lib/types'
import { TermOverlay } from '../task/TermOverlay'
import { AgentFormModal } from './AgentFormModal'
import { AgentQuestionsTab } from './AgentQuestionsTab'
import { InboxTab } from './InboxTab'
import './agents.css'

type TabId = 'questions' | 'inbox'

const MILESTONE_STATUS_LABEL: Record<TaskStatus, string> = {
  backlog: 'Backlog',
  brainstorm: 'Brainstorm',
  in_progress: 'In Progress',
  review: 'Review',
  done: 'Done',
  cancelled: 'Cancelled',
}

const MILESTONE_STATUS_TONE: Record<TaskStatus, BadgeTone> = {
  backlog: 'neutral',
  brainstorm: 'review',
  in_progress: 'indigo',
  review: 'review',
  done: 'ok',
  cancelled: 'neutral',
}

export function AgentScreen() {
  // Reached from both routes: `/p/:projectId/agents/:roleId` (inside a
  // project) and `/agents/:roleId` (the global list, the only way in for an
  // agent registered without a project).
  const { projectId, roleId: agentId } = useParams<{ projectId?: string; roleId: string }>()
  const navigate = useNavigate()

  const [tab, setTab] = useState<TabId>('questions')
  const [message, setMessage] = useState('')
  const [editing, setEditing] = useState(false)
  const [termOpen, setTermOpen] = useState(false)

  const { data: projects } = useProjects()
  const { data: agent } = useAgent(agentId)
  const { data: questions } = useAgentQuestions(agentId)

  const send = useSendAgentMessage()
  const start = useStartAgent()
  const stop = useStopAgent()
  const setEnabled = useSetAgentEnabled()
  const remove = useDeleteAgent()

  if (!agentId || !agent) return null

  const listPath = projectId ? `/p/${projectId}/agents` : '/agents'
  const project = projects?.find((p) => p.id === (projectId ?? agent.project))
  const openQuestions = (questions ?? []).filter((q) => q.status === 'open').length

  function handleSend() {
    if (!message.trim()) return
    send.mutate({ id: agentId!, body: message.trim() }, { onSuccess: () => setMessage('') })
  }

  function handleDelete() {
    if (!window.confirm(`Delete agent ${agentId}? Its inbox and threads go with it.`)) return
    remove.mutate(agentId!, { onSuccess: () => navigate(listPath) })
  }

  const tabs: Array<{ id: TabId; label: string; count?: number; warn?: boolean }> = [
    {
      id: 'questions',
      label: 'Questions',
      count: openQuestions || undefined,
      warn: agent.awaiting_user > 0,
    },
    { id: 'inbox', label: 'Inbox', count: agent.unread || undefined },
  ]

  return (
    <main className="agent-screen">
      <Link to={listPath} className="agent-screen__back">
        ← {projectId ? `${project?.name ?? projectId} agents` : 'all agents'}
      </Link>

      <div className="agent-screen__title-row">
        <span
          className={
            'agent-card__dot ' +
            (agent.session_alive ? 'agent-card__dot--live' : 'agent-card__dot--idle')
          }
        />
        <h1 className="agent-screen__title">{agent.id}</h1>
        {agent.enabled ? (
          <Badge tone="ok">enabled</Badge>
        ) : (
          <Badge tone="neutral">disabled</Badge>
        )}
        {agent.session_alive && <Badge tone="ok">● session live</Badge>}
      </div>

      {agent.description && <p className="agent-screen__desc">{agent.description}</p>}

      <div className="agent-screen__meta">
        {/* Coming from the global list the project isn't in the URL, so name it
            here — «No project» is a legitimate registration. */}
        {!projectId && <span>{agent.project ? (project?.name ?? agent.project) : 'No project'}</span>}
        {!projectId && <span>·</span>}
        <span className="agent-screen__meta-mono">{agent.dir || 'no dir — start it yourself'}</span>
        <span>·</span>
        <span className="agent-screen__meta-mono">{agent.command || 'interactive shell'}</span>
        <span>·</span>
        <span>updated {timeAgo(agent.updated_at)}</span>
      </div>

      <div className="agent-screen__actions">
        <input
          className="agent-screen__ping"
          aria-label="Message the agent"
          placeholder={
            agent.session_alive
              ? 'Goes straight into the live session'
              : 'Session is down — this waits in the inbox'
          }
          value={message}
          onChange={(e) => setMessage(e.target.value)}
        />
        <Button
          variant="primary"
          size="sm"
          onClick={handleSend}
          disabled={!message.trim() || send.isPending}
        >
          Send
        </Button>
        {agent.session_alive ? (
          <>
            <Button variant="secondary" size="sm" onClick={() => setTermOpen(true)}>
              Terminal
            </Button>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => stop.mutate(agent.id)}
              disabled={stop.isPending}
            >
              Stop
            </Button>
          </>
        ) : (
          <Button
            variant="secondary"
            size="sm"
            onClick={() => start.mutate(agent.id)}
            disabled={start.isPending}
          >
            Start
          </Button>
        )}
        <Button
          variant="secondary"
          size="sm"
          onClick={() => setEnabled.mutate({ id: agent.id, enabled: !agent.enabled })}
          disabled={setEnabled.isPending}
        >
          {agent.enabled ? 'Disable' : 'Enable'}
        </Button>
        <Button variant="secondary" size="sm" onClick={() => setEditing(true)}>
          Edit
        </Button>
        <Button variant="danger" size="sm" onClick={handleDelete} disabled={remove.isPending}>
          Delete
        </Button>
      </div>

      {send.isError && <p className="agent-screen__error">{send.error.message}</p>}
      {start.isError && <p className="agent-screen__error">{start.error.message}</p>}
      {stop.isError && <p className="agent-screen__error">{stop.error.message}</p>}
      {remove.isError && <p className="agent-screen__error">{remove.error.message}</p>}

      {/* What the agent has taken on (task #1023, spec v2): milestones are
          root tasks outside every project, so they link to /milestones/:id. */}
      <div className="agent-screen__milestones">
        <h2 className="agent-screen__milestones-title">Milestones</h2>
        {(agent.milestones ?? []).length === 0 ? (
          <p className="agent-screen__milestones-empty">No milestones — nothing taken yet.</p>
        ) : (
          <ul className="agent-screen__milestones-list">
            {(agent.milestones ?? []).map((m) => (
              <li key={m.id} className="agent-screen__milestone">
                <Link to={`/milestones/${m.id}`} className="agent-screen__milestone-link">
                  <span className="agent-screen__milestone-id">#{m.id}</span>
                  {m.title}
                </Link>
                <Badge tone={MILESTONE_STATUS_TONE[m.status]}>{MILESTONE_STATUS_LABEL[m.status]}</Badge>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="agent-screen__tabs" role="tablist">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            className={
              tab === t.id ? 'agent-screen__tab agent-screen__tab--active' : 'agent-screen__tab'
            }
            onClick={() => setTab(t.id)}
          >
            {t.label}
            {t.count !== undefined && (
              <span
                className={
                  t.warn
                    ? 'agent-screen__tab-count agent-screen__tab-count--warn'
                    : 'agent-screen__tab-count'
                }
              >
                {t.count}
              </span>
            )}
          </button>
        ))}
      </div>

      {tab === 'questions' && <AgentQuestionsTab roleId={agent.id} />}
      {tab === 'inbox' && <InboxTab agentId={agent.id} />}

      {editing && (
        <AgentFormModal
          projectId={projectId}
          agent={agent}
          onClose={() => setEditing(false)}
        />
      )}

      {/* The agent's tmux session is named after the agent, so its session row
          carries the same id (docs/10-agents.md «Живость и адопция»). */}
      {termOpen && (
        <TermOverlay
          session={{ id: agent.id, tmux_name: agent.id }}
          onClose={() => setTermOpen(false)}
        />
      )}
    </main>
  )
}
