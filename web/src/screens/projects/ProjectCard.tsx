import { Link } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { timeAgo } from '../../lib/format'
import { useProjectTasks } from '../../lib/queries'
import type { Agent, Project, Task } from '../../lib/types'
import './projects.css'

interface Stat {
  tone: BadgeTone
  label: string
}

/**
 * Derives the card's stat badges from the project's task list + live_sessions,
 * matching docs/design/Projects.dc.html: in-progress/review counts (when
 * nonzero) plus a live-sessions badge, or a single gray "idle" badge when
 * there's nothing active yet. The real `GET /v1/projects` has no task
 * counters (.superpowers/sdd/phase3-contract.md), so counts are computed
 * client-side from `GET /v1/tasks?project=<id>`.
 */
function projectStats(project: Project, tasks: Task[] | undefined, agents: Agent[] | undefined): Stat[] {
  const stats: Stat[] = []
  const inProgress = tasks?.filter((t) => t.status === 'in_progress').length ?? 0
  const review = tasks?.filter((t) => t.status === 'review').length ?? 0
  if (inProgress > 0) {
    stats.push({ tone: 'indigo', label: `${inProgress} in progress` })
  }
  if (review > 0) {
    stats.push({ tone: 'review', label: `${review} review` })
  }
  if (project.live_sessions > 0) {
    stats.push({ tone: 'ok', label: `● ${project.live_sessions} live` })
  }
  // Agents are the other thing that can be waiting on you (docs/10-agents.md):
  // a thread the agent opened that nobody has answered yet. Same visual
  // language as the task "awaiting you" signal.
  const agentsAwaiting = (agents ?? []).filter((a) => a.awaiting_user > 0).length
  if (agentsAwaiting > 0) {
    stats.push({
      tone: 'warn',
      label: `？${agentsAwaiting} agent${agentsAwaiting > 1 ? 's' : ''} awaiting you`,
    })
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
  /** The roles of THIS project — the screen fetches them once for the grid. */
  agents?: Agent[]
}

/** Formats the linked-repos suffix as `first, two +N more` so the repos
 * line never grows past one line even for projects with dozens of linked
 * repos. */
function linkedSummary(linked: string[]): string {
  const shown = linked.slice(0, 2).join(', ')
  const rest = linked.length - 2
  return rest > 0 ? `${shown} +${rest} more` : shown
}

export function ProjectCard({ project, updatedAt, agents }: ProjectCardProps) {
  const hasLinked = project.linked.length > 0
  const { data: projectTasks } = useProjectTasks(project.id)
  // awaiting_questions badge removed: not exposed on GET /v1/tasks list,
  // only per-task detail. Add when API exposes it aggregated.

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

      <div
        className="project-card__repos"
        title={hasLinked ? `${project.main} + ${project.linked.join(', ')}` : project.main}
      >
        <span className="project-card__repo project-card__repo--main">⌂ {project.main}</span>
        {hasLinked && (
          <>
            <span className="project-card__repo-sep">+</span>
            <span className="project-card__repo project-card__repo--linked">
              {linkedSummary(project.linked)}
            </span>
          </>
        )}
      </div>

      <div className="project-card__stats">
        {projectStats(project, projectTasks, agents).map((stat) => (
          <Badge key={stat.label} tone={stat.tone}>
            {stat.label}
          </Badge>
        ))}
      </div>

      <div className="project-card__footer">
        <div className="project-card__signals" />
        <span className="project-card__updated">updated {timeAgo(updatedAt)}</span>
      </div>
    </Link>
  )
}
