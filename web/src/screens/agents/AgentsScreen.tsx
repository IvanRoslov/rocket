// Agents screen (docs/11-dashboard.md): the standing agents of a project —
// registrations you and other agents can address by id.

import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { EmptyState } from '../../components/EmptyState'
import { useAgents } from '../../lib/queries'
import { AgentCard } from './AgentCard'
import { AgentFormModal } from './AgentFormModal'
import './agents.css'

export function AgentsScreen() {
  const { projectId } = useParams<{ projectId: string }>()
  const navigate = useNavigate()
  const [creating, setCreating] = useState(false)

  const { data: agents } = useAgents(projectId)

  if (!projectId) return null

  return (
    <main className="agents-screen">
      <div className="agents-screen__header">
        <div>
          <h1 className="agents-screen__title">Agents</h1>
          <p className="agents-screen__subtitle">
            A standing agent — platform SRE, issue triage — you and other agents can address by
            id. Messages reach its live session or wait in its inbox.
          </p>
        </div>
        <button type="button" className="agents-screen__new-btn" onClick={() => setCreating(true)}>
          <span aria-hidden="true">＋</span> New agent
        </button>
      </div>

      {agents && agents.length === 0 ? (
        <EmptyState
          icon="◎"
          title="No agents in this project yet"
          action={
            <button
              type="button"
              className="agents-screen__new-btn"
              onClick={() => setCreating(true)}
            >
              <span aria-hidden="true">＋</span> Register agent
            </button>
          }
        />
      ) : (
        <div className="agents-grid">
          {agents?.map((agent) => (
            <AgentCard key={agent.id} projectId={projectId} agent={agent} />
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
