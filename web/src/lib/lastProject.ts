// Last visited project: the header's Kanban/Agents tabs need a project id even
// on project-less pages (Projects list, /questions, /system, /settings), where
// `useParams()` has no `:projectId`. We remember the last project the user was
// in and fall back to the first project from the projects query.

import { useEffect } from 'react'
import { useProjects } from './queries'

export const LAST_PROJECT_STORAGE_KEY = 'rocket.lastProjectId'

export function loadStoredProjectId(): string | undefined {
  if (typeof window === 'undefined') return undefined
  return window.localStorage.getItem(LAST_PROJECT_STORAGE_KEY) ?? undefined
}

/** Project id for project-scoped navigation: the current one when the URL has
 * it (also persisting it), otherwise the last visited one, otherwise the first
 * known project. `undefined` while no project is known yet. */
export function useLastProjectId(currentProjectId?: string): string | undefined {
  const { data: projects } = useProjects()

  useEffect(() => {
    if (!currentProjectId) return
    window.localStorage.setItem(LAST_PROJECT_STORAGE_KEY, currentProjectId)
  }, [currentProjectId])

  if (currentProjectId) return currentProjectId
  const stored = loadStoredProjectId()
  if (stored) return stored
  return projects?.[0]?.id
}
