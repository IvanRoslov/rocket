import { Link } from 'react-router-dom'
import { EmptyState } from '../../components/EmptyState'
import { useProjects, useSessions } from '../../lib/queries'
import type { Project, Session } from '../../lib/types'
import { ProjectCard } from './ProjectCard'
import './projects.css'

/** Latest activity for a project: the newest session update, or its creation time. */
function lastActivity(project: Project, sessions: Session[] | undefined): number {
  const projectSessions = sessions?.filter((s) => s.project_id === project.id) ?? []
  const latestSession = projectSessions.reduce(
    (max, s) => Math.max(max, s.updated_at),
    0,
  )
  return Math.max(project.created_at, latestSession)
}

export function ProjectsScreen() {
  const { data: projects } = useProjects()
  const { data: sessions } = useSessions()

  return (
    <main className="projects-screen">
      <div className="projects-screen__header">
        <div>
          <h1 className="projects-screen__title">Projects</h1>
          <p className="projects-screen__subtitle">
            Each project is a product: a main repo plus linked repos where workers run.
          </p>
        </div>
        <Link to="/projects/new" className="projects-screen__new-btn">
          <span aria-hidden="true">＋</span> New project
        </Link>
      </div>

      {projects && projects.length === 0 ? (
        <EmptyState
          icon="＋"
          title="No projects yet"
          action={
            <Link to="/projects/new" className="projects-screen__new-btn">
              <span aria-hidden="true">＋</span> Create project
            </Link>
          }
        />
      ) : (
        <div className="projects-grid">
          {projects?.map((project) => (
            <ProjectCard
              key={project.id}
              project={project}
              updatedAt={lastActivity(project, sessions)}
            />
          ))}
          <Link to="/projects/new" className="projects-grid__create">
            <span className="projects-grid__create-icon" aria-hidden="true">
              ＋
            </span>
            <span className="projects-grid__create-label">Create project</span>
          </Link>
        </div>
      )}
    </main>
  )
}
