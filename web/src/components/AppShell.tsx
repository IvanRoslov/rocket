import type { CSSProperties } from 'react'
import { NavLink, Outlet, useParams, useLocation } from 'react-router-dom'
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
          <NavLink
            to={projectId ? `/p/${projectId}` : '/'}
            style={({ isActive }) => navLinkStyle({ isActive: isActive && location.pathname.startsWith('/p/') })}
          >
            Kanban
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
