// Role card (docs/11-dashboard.md): everything about one role — its
// definition, the actions that drive it (wake, terminal, enable/disable) and
// five tabs over its durable state.

import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Badge } from '../../components/Badge'
import { Button } from '../../components/Button'
import { timeAgo } from '../../lib/format'
import {
  useAgent,
  useAgentQuestions,
  useDeleteAgent,
  useProjects,
  useSessions,
  useSetAgentEnabled,
  useWakeAgent,
} from '../../lib/queries'
import { TermOverlay, type TermOverlaySession } from '../task/TermOverlay'
import { AgentFormModal } from './AgentFormModal'
import { AgentQuestionsTab } from './AgentQuestionsTab'
import { DossierTab } from './DossierTab'
import { InboxTab } from './InboxTab'
import { liveInstance } from './AgentCard'
import { MemoryTab } from './MemoryTab'
import { RunsTab } from './RunsTab'
import './agents.css'

type TabId = 'questions' | 'inbox' | 'dossier' | 'memory' | 'runs'

export function AgentScreen() {
  const { projectId, roleId } = useParams<{ projectId: string; roleId: string }>()
  const navigate = useNavigate()

  const [tab, setTab] = useState<TabId>('questions')
  const [ping, setPing] = useState('')
  const [editing, setEditing] = useState(false)
  const [termSession, setTermSession] = useState<TermOverlaySession | null>(null)
  // True between "Terminal" on a role with no live instance and the instance
  // actually appearing — the daemon debounces wakes (30s by default,
  // docs/10-agents.md), so this state can last a while and must be visible.
  const [waking, setWaking] = useState(false)

  const { data: projects } = useProjects()
  const { data: agent } = useAgent(roleId)
  const { data: questions } = useAgentQuestions(roleId)
  const { data: sessions } = useSessions({ kind: 'agent', project: projectId })

  const wake = useWakeAgent()
  const setEnabled = useSetAgentEnabled()
  const remove = useDeleteAgent()

  const instance = roleId ? liveInstance(sessions, roleId) : undefined

  // The wake engine spawns the instance asynchronously; `agent.instance_
  // spawned` invalidates the sessions query (lib/queries.ts), so the instance
  // simply shows up here and the pending terminal opens on it.
  useEffect(() => {
    if (waking && instance) {
      setTermSession({ id: instance.id, tmux_name: instance.tmux_name })
      setWaking(false)
    }
  }, [waking, instance])

  if (!projectId || !roleId || !agent) return null

  const project = projects?.find((p) => p.id === projectId)
  const openQuestions = (questions ?? []).filter((q) => q.status === 'open').length

  function handleWake() {
    wake.mutate(
      { id: roleId!, text: ping.trim() || undefined },
      { onSuccess: () => setPing('') },
    )
  }

  function handleTerminal() {
    if (instance) {
      setTermSession({ id: instance.id, tmux_name: instance.tmux_name })
      return
    }
    setWaking(true)
    wake.mutate({ id: roleId!, kind: 'terminal_opened' }, { onError: () => setWaking(false) })
  }

  function handleDelete() {
    if (!window.confirm(`Delete role ${roleId}? Its inbox and dossier go with it.`)) return
    remove.mutate(roleId!, { onSuccess: () => navigate(`/p/${projectId}/agents`) })
  }

  const tabs: Array<{ id: TabId; label: string; count?: number; warn?: boolean }> = [
    {
      id: 'questions',
      label: 'Questions',
      count: openQuestions || undefined,
      warn: agent.awaiting_user > 0,
    },
    { id: 'inbox', label: 'Inbox', count: agent.inbox_queued || undefined },
    { id: 'dossier', label: 'Dossier', count: agent.items || undefined },
    { id: 'memory', label: 'Memory' },
    { id: 'runs', label: 'Runs' },
  ]

  return (
    <main className="agent-screen">
      <Link to={`/p/${projectId}/agents`} className="agent-screen__back">
        ← {project?.name ?? projectId} agents
      </Link>

      <div className="agent-screen__title-row">
        <span
          className={
            'agent-card__dot ' + (instance ? 'agent-card__dot--live' : 'agent-card__dot--idle')
          }
        />
        <h1 className="agent-screen__title">{agent.id}</h1>
        <Badge tone="neutral" mono>
          {agent.agent}
        </Badge>
        {agent.enabled ? (
          <Badge tone="ok">enabled</Badge>
        ) : (
          <Badge tone="neutral">disabled</Badge>
        )}
        {instance && <Badge tone="ok">● {instance.id}</Badge>}
      </div>

      <div className="agent-screen__meta">
        <span className="agent-screen__meta-mono">{agent.prompt_path}</span>
        <span>·</span>
        <span>{agent.cron ? `cron ${agent.cron}` : 'no cron'}</span>
        <span>·</span>
        <span>
          {agent.subscriptions.length > 0
            ? agent.subscriptions.map((s) => s.repo).join(', ')
            : 'no GitHub subscriptions'}
        </span>
        <span>·</span>
        <span>updated {timeAgo(agent.updated_at)}</span>
      </div>

      <div className="agent-screen__actions">
        <input
          className="agent-screen__ping"
          aria-label="Ping the role"
          placeholder="Optional message — leave empty for a bare wake"
          value={ping}
          onChange={(e) => setPing(e.target.value)}
        />
        <Button variant="primary" size="sm" onClick={handleWake} disabled={wake.isPending}>
          Wake
        </Button>
        <Button variant="secondary" size="sm" onClick={handleTerminal} disabled={waking}>
          {instance ? 'Terminal' : 'Wake & open terminal'}
        </Button>
        {waking && (
          <>
            <span className="agent-screen__waking">waking… the terminal opens on spawn</span>
            <Button variant="secondary" size="sm" onClick={() => setWaking(false)}>
              Cancel
            </Button>
          </>
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

      {wake.isError && <p className="agent-screen__error">{wake.error.message}</p>}
      {remove.isError && <p className="agent-screen__error">{remove.error.message}</p>}

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
      {tab === 'inbox' && <InboxTab roleId={agent.id} />}
      {tab === 'dossier' && <DossierTab roleId={agent.id} projectId={projectId} />}
      {tab === 'memory' && <MemoryTab roleId={agent.id} />}
      {tab === 'runs' && <RunsTab roleId={agent.id} projectId={projectId} />}

      {editing && (
        <AgentFormModal projectId={projectId} agent={agent} onClose={() => setEditing(false)} />
      )}

      {termSession && (
        <TermOverlay session={termSession} onClose={() => setTermSession(null)} />
      )}
    </main>
  )
}
