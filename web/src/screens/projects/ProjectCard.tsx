import { Link } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import type { Project } from '../../lib/types'
import './projects.css'

interface Stat {
  tone: BadgeTone
  label: string
}

/**
 * Derives the card's stat badges from `tasks{}` + `live_sessions`, matching
 * docs/design/Projects.dc.html: in-progress/review counts (when nonzero)
 * plus a live-sessions badge, or a single gray "idle" badge when there's
 * nothing active yet — a valid state until phase 3 lands.
 */
function projectStats(project: Project): Stat[] {
  const stats: Stat[] = []
  if (project.tasks.in_progress > 0) {
    stats.push({ tone: 'indigo', label: `${project.tasks.in_progress} in progress` })
  }
  if (project.tasks.review > 0) {
    stats.push({ tone: 'review', label: `${project.tasks.review} review` })
  }
  if (project.live_sessions > 0) {
    stats.push({ tone: 'ok', label: `● ${project.live_sessions} live` })
  }
  if (stats.length === 0) {
    stats.push({ tone: 'neutral', label: 'idle' })
  }
  return stats
}

export interface ProjectCardProps {
  project: Project
  /** Most recent activity timestamp (unix seconds) for the "updated" footer. */
  updatedAt: number
}

export function ProjectCard({ project, updatedAt }: ProjectCardProps) {
  const hasLinked = project.linked.length > 0
  const awaiting = (project.awaiting_questions ?? 0) > 0

  return (
    <Link to={`/p/${project.id}`} className="project-card">
      <div className="project-card__header">
        <div className="project-card__title">
          <span
            className={
              'project-card__dot ' +
              (project.live_sessions > 0 ? 'project-card__dot--live' : 'project-card__dot--idle')
            }
          />
          <span className="project-card__name">{project.name}</span>
        </div>
        <Badge tone="neutral" mono>
          {project.id}
        </Badge>
      </div>

      <div className="project-card__repos">
        <span className="project-card__repo project-card__repo--main">⌂ {project.main}</span>
        {hasLinked && (
          <>
            <span className="project-card__repo-sep">+</span>
            <span className="project-card__repo project-card__repo--linked">
              {project.linked.join(', ')}
            </span>
          </>
        )}
      </div>

      <div className="project-card__stats">
        {projectStats(project).map((stat) => (
          <Badge key={stat.label} tone={stat.tone}>
            {stat.label}
          </Badge>
        ))}
      </div>

      <div className="project-card__footer">
        <div className="project-card__signals">
          {awaiting && (
            <span className="project-card__awaiting">? awaiting you</span>
          )}
        </div>
        <span className="project-card__updated">updated {timeAgo(updatedAt)}</span>
      </div>
    </Link>
  )
}
