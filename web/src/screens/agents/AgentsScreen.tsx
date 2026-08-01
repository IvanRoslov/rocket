// Agents screen (docs/11-dashboard.md): the roles of a project — standing
// agents that wake on events, keep a dossier and answer you in threads.

import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { EmptyState } from '../../components/EmptyState'
import { useAgents, useSessions } from '../../lib/queries'
import { AgentCard, liveInstance } from './AgentCard'
import { AgentFormModal } from './AgentFormModal'
import './agents.css'

export function AgentsScreen() {
  const { projectId } = useParams<{ projectId: string }>()
  const navigate = useNavigate()
  const [creating, setCreating] = useState(false)

  const { data: agents } = useAgents(projectId)
  // Live role instances only — the runs journal on the role card is the place
  // for finished ones.
  const { data: sessions } = useSessions({ kind: 'agent', project: projectId })

  if (!projectId) return null

  return (
    <main className="agents-screen">
      <div className="agents-screen__header">
        <div>
          <h1 className="agents-screen__title">Agents</h1>
          <p className="agents-screen__subtitle">
            A role is a standing agent — platform SRE, issue triage — you and other agents can
            address. It wakes on events, keeps a dossier and answers in threads.
          </p>
        </div>
        <button type="button" className="agents-screen__new-btn" onClick={() => setCreating(true)}>
          <span aria-hidden="true">＋</span> New role
        </button>
      </div>

      {agents && agents.length === 0 ? (
        <EmptyState
          icon="◎"
          title="No roles in this project yet"
          action={
            <button
              type="button"
              className="agents-screen__new-btn"
              onClick={() => setCreating(true)}
            >
              <span aria-hidden="true">＋</span> Create role
            </button>
          }
        />
      ) : (
        <div className="agents-grid">
          {agents?.map((agent) => (
            <AgentCard
              key={agent.id}
              projectId={projectId}
              agent={agent}
              instance={liveInstance(sessions, agent.id)}
            />
          ))}
        </div>
      )}

      {creating && (
        <AgentFormModal
          projectId={projectId}
          onClose={() => setCreating(false)}
          onCreated={(id) => navigate(`/p/${projectId}/agents/${id}`)}
        />
      )}
    </main>
  )
}
