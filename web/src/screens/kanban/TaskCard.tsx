import type { DragEvent } from 'react'
import { Link } from 'react-router-dom'
import { Badge, type BadgeTone } from '../../components/Badge'
import { Dot, type DotState } from '../../components/Dot'
import type { Session, Task } from '../../lib/types'
import './kanban.css'

export interface TaskCardProps {
  task: Task
  projectId: string
  /** The project's main repo id, or undefined if the project isn't loaded yet. */
  mainRepo?: string
  /** All sessions for the project (used to find the task's orchestrator + its workers). */
  sessions: Session[] | undefined
  dragging: boolean
  onDragStart: (e: DragEvent<HTMLDivElement>) => void
  onDragEnd: () => void
  onStart: () => void
}

/**
 * Maps a session onto the `Dot` state it should render. Prefers `activity`
 * (set once the session is running); falls back to a coarse mapping of
 * `state` for sessions that haven't reported activity yet.
 */
function orchDotState(session: Session): DotState {
  if (session.activity) return session.activity
  switch (session.state) {
    case 'spawning':
      return 'spawning'
    case 'errored':
      return 'errored'
    case 'done':
    case 'killed':
      return 'exited'
    default:
      return 'ready'
  }
}

type CiState = NonNullable<Session['ci_state']>

const CI_SEVERITY: Record<CiState, number> = { passing: 0, pending: 1, failing: 2 }
const CI_SYMBOL: Record<CiState, string> = { passing: '✔', pending: '⏳', failing: '✗' }
const CI_TONE: Record<CiState, BadgeTone> = { passing: 'ok', pending: 'warn', failing: 'err' }

/** worst-of a set of worker CI states: failing > pending > passing. */
export function worstCiState(states: CiState[]): CiState | undefined {
  return states.reduce<CiState | undefined>((worst, s) => {
    if (!worst || CI_SEVERITY[s] > CI_SEVERITY[worst]) return s
    return worst
  }, undefined)
}

/** PR summary across a task's worker sessions (docs/design/Kanban.dc.html PR badges). */
function prSummary(workers: Session[]): { openCount: number; mergedCount: number; ci?: CiState } {
  const openCount = workers.filter((w) => w.pr_state === 'open').length
  const mergedCount = workers.filter((w) => w.pr_state === 'merged').length
  const ciStates = workers.map((w) => w.ci_state).filter((c): c is CiState => c !== undefined)
  return { openCount, mergedCount, ci: worstCiState(ciStates) }
}

export function TaskCard({
  task,
  projectId,
  mainRepo,
  sessions,
  dragging,
  onDragStart,
  onDragEnd,
  onStart,
}: TaskCardProps) {
  // Orchestrator liveness: the task's own session (if it has one) is the
  // orchestrator; its workers are sessions whose parent_id points back to it.
  // Board responses don't include subtask data, so we can't show worker
  // repos here (docs/design/Kanban.dc.html's "api → web, infra" row) without
  // an N+1 fetch per card — skipped per task-12 brief.
  const orchestrator = task.session_id ? sessions?.find((s) => s.id === task.session_id) : undefined
  const workerSessions = orchestrator ? (sessions?.filter((s) => s.parent_id === orchestrator.id) ?? []) : []
  const workers = workerSessions.length
  const { openCount, mergedCount, ci } = prSummary(workerSessions)
  const draggable = task.status !== 'cancelled'

  return (
    <div
      className="kanban-card"
      draggable={draggable}
      onDragStart={draggable ? onDragStart : undefined}
      onDragEnd={onDragEnd}
      style={{ opacity: dragging ? 0.4 : 1 }}
    >
      <Link to={`/p/${projectId}/tasks/${task.id}`} className="kanban-card__title-row">
        <span className="kanban-card__id">#{task.id}</span>
        <span className="kanban-card__title">{task.title}</span>
      </Link>

      {mainRepo && <div className="kanban-card__repo">{mainRepo}</div>}

      {orchestrator && (
        <div className="kanban-card__orch">
          <Dot state={orchDotState(orchestrator)} />
          <span>orch: {orchestrator.activity ?? orchestrator.state}</span>
          <span className="kanban-card__orch-sep">·</span>
          <span>{workers} workers</span>
        </div>
      )}

      {(openCount > 0 || mergedCount > 0 || ci) && (
        <div className="kanban-card__pr-badges">
          {openCount > 0 && (
            <Badge tone="indigo">
              {openCount} PR open
            </Badge>
          )}
          {mergedCount > 0 && (
            <Badge tone="ok">
              {mergedCount} merged
            </Badge>
          )}
          {ci && <Badge tone={CI_TONE[ci]}>CI {CI_SYMBOL[ci]}</Badge>}
        </div>
      )}

      {/* Signals (open_questions): not on the list/board taskResponse, only
          per-task detail — skipping to avoid an N+1 fetch per card. Will
          show up once the task's own screen is open. */}

      {task.status === 'backlog' && (
        <button type="button" className="kanban-card__start" onClick={onStart}>
          Start ▸
        </button>
      )}
    </div>
  )
}
