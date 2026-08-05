import type { CSSProperties } from 'react'
import { Link, NavLink, Outlet, useParams, useLocation } from 'react-router-dom'
import { useLastProjectId } from '../lib/lastProject'
import { useThreads } from '../lib/queries'
import { ProjectSwitcher } from './ProjectSwitcher'

const navLinkStyle = ({ isActive }: { isActive: boolean }): CSSProperties => ({
  padding: '7px 11px',
  borderRadius: 'var(--radius-btn-sm)',
  font: '500 13px var(--font-ui)',
  color: isActive ? 'var(--text)' : 'var(--text-3)',
  background: isActive ? 'var(--surface-2)' : 'transparent',
})

export function AppShell() {
  const { projectId } = useParams()
  const location = useLocation()
  const navProjectId = useLastProjectId(projectId)
  const { data: threads } = useThreads()
  // Project-scoped tabs use plain <Link>: NavLink's own prefix matching would
  // light up Kanban on /p/:id/agents too, so we derive both the highlight and
  // aria-current from the pathname ourselves.
  // Agents is global (`/agents`): agents may be registered without a project,
  // so the tab must not funnel you into one. It still lights up on the
  // project-scoped agents routes.
  const inProject = location.pathname.startsWith('/p/')
  const agentsActive =
    location.pathname.startsWith('/agents') || (inProject && location.pathname.includes('/agents'))
  const kanbanActive = inProject && !location.pathname.includes('/agents')
  // Counted over the unified inbox, not GET /v1/questions: the latter knows
  // only task threads, so a role thread waiting on the human never reached
  // this badge. `your_turn` is the caller-relative field; `whose_turn` cannot
  // distinguish "waiting on you" from "waiting on another participant".
  const awaitingCount = (threads ?? []).filter((t) => t.your_turn).length

  return (
    <div style={{ minHeight: '100vh' }}>
      <header
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 18,
          height: 52,
          padding: '0 20px',
          background: 'var(--surface)',
          borderBottom: '1px solid var(--border)',
          position: 'sticky',
          top: 0,
          zIndex: 30,
        }}
      >
        <NavLink to="/" style={{ display: 'flex', alignItems: 'center', gap: 9 }}>
          <div
            style={{
              width: 22,
              height: 22,
              borderRadius: 6,
              background: 'var(--text)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--surface)',
              font: '700 12px var(--font-mono)',
            }}
          >
            R
          </div>
          <span style={{ font: '600 14px var(--font-mono)', color: 'var(--text)', letterSpacing: '-.02em' }}>
            rocket
          </span>
        </NavLink>
        <div style={{ width: 1, height: 20, background: 'var(--border)' }} />
        <ProjectSwitcher currentProjectId={projectId} />
        <div style={{ flex: 1 }} />
        <nav style={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <NavLink to="/" end style={navLinkStyle}>
            Projects
          </NavLink>
          <Link
            to={navProjectId ? `/p/${navProjectId}` : '/'}
            aria-current={kanbanActive ? 'page' : undefined}
            style={navLinkStyle({ isActive: kanbanActive })}
          >
            Kanban
          </Link>
          <Link
            to="/agents"
            aria-current={agentsActive ? 'page' : undefined}
            style={navLinkStyle({ isActive: agentsActive })}
          >
            Agents
          </Link>
          {/* Milestones are the persistent agents' work and belong to no
              project (task #1023, spec v2) — a global tab, like Agents. */}
          <NavLink to="/milestones" style={navLinkStyle}>
            Milestones
          </NavLink>
          <NavLink to="/questions" style={navLinkStyle}>
            Questions
            {awaitingCount > 0 && (
              <span
                style={{
                  // Filled amber, not the pale --warn-bg chip: this counter is
                  // the one thing on the page that says work is blocked on the
                  // human, and the v3 design gives it the loudest badge.
                  marginLeft: 6,
                  padding: '1px 7px',
                  borderRadius: 999,
                  background: 'var(--q-amber)',
                  color: 'var(--surface)',
                  font: '700 11px var(--font-mono)',
                }}
              >
                {awaitingCount}
              </span>
            )}
          </NavLink>
          <NavLink to="/system" style={navLinkStyle}>
            System
          </NavLink>
          <NavLink to="/settings" style={navLinkStyle}>
            Settings
          </NavLink>
        </nav>
      </header>
      <Outlet />
    </div>
  )
}
