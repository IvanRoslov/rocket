import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useProjects } from '../lib/queries'
import type { Project } from '../lib/types'
import './uikit.css'

export interface ProjectSwitcherProps {
  currentProjectId?: string
}

export function ProjectSwitcher({ currentProjectId }: ProjectSwitcherProps) {
  const { data: projects } = useProjects()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const navigate = useNavigate()
  const rootRef = useRef<HTMLDivElement>(null)

  const current = projects?.find((p) => p.id === currentProjectId)

  const filtered = useMemo(() => {
    if (!projects) return []
    const q = query.trim().toLowerCase()
    if (!q) return projects
    return projects.filter(
      (p) => p.name.toLowerCase().includes(q) || p.id.toLowerCase().includes(q),
    )
  }, [projects, query])

  useEffect(() => {
    if (!open) return
    function handlePointerDown(e: PointerEvent) {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open])

  function select(project: Project) {
    setOpen(false)
    setQuery('')
    navigate(`/p/${project.id}`)
  }

  return (
    <div className="project-switcher" ref={rootRef}>
      <button
        type="button"
        className="project-switcher__trigger"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <span
          className={
            'project-switcher__dot ' +
            ((current?.live_sessions ?? 0) > 0
              ? 'project-switcher__dot--live'
              : 'project-switcher__dot--idle')
          }
        />
        <span>{current ? current.name : 'Select project'}</span>
        {current && <span className="project-switcher__id">{current.id}</span>}
        <span className="project-switcher__caret">▾</span>
      </button>
      {open && (
        <div className="project-switcher__panel">
          <input
            className="project-switcher__search"
            type="text"
            autoFocus
            placeholder="Search projects…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          {filtered.map((p) => (
            <button
              key={p.id}
              type="button"
              className="project-switcher__item"
              onClick={() => select(p)}
            >
              <span
                className={
                  'project-switcher__dot ' +
                  (p.live_sessions > 0
                    ? 'project-switcher__dot--live'
                    : 'project-switcher__dot--idle')
                }
              />
              <span className="project-switcher__item-name">{p.name}</span>
              <span className="project-switcher__id">{p.id}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
