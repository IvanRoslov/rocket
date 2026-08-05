import { useState, type DragEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { SearchInput } from '../../components/SearchInput'
import { useMoveTask, useProjects, useSessions, useTasksBoard, type TaskBoard } from '../../lib/queries'
import type { Task, TaskStatus } from '../../lib/types'
import { Column } from './Column'
import { NewTaskModal } from './NewTaskModal'
import { StartModal } from './StartModal'
import { TaskCard } from './TaskCard'
import './kanban.css'

interface ColumnSpec {
  key: TaskStatus
  title: string
  dot: string
}

// Backlog/In Progress/Review/Done — always shown. Cancelled is appended
// only when "Show cancelled" is checked, and is never a drop target
// (docs/design/Kanban.dc.html + task-12 brief: cancelling happens from the
// task screen, not by dragging a card here).
const COLUMNS: ColumnSpec[] = [
  { key: 'backlog', title: 'Backlog', dot: 'var(--text-4)' },
  { key: 'brainstorm', title: 'Brainstorm', dot: 'var(--review)' },
  { key: 'in_progress', title: 'In Progress', dot: 'var(--accent)' },
  { key: 'review', title: 'Review', dot: 'var(--review)' },
  { key: 'done', title: 'Done', dot: 'var(--ok)' },
]

const CANCELLED_COLUMN: ColumnSpec = { key: 'cancelled', title: 'Cancelled', dot: 'var(--text-4)' }

function matchesSearch(task: Task, query: string): boolean {
  if (!query.trim()) return true
  return task.title.toLowerCase().includes(query.trim().toLowerCase())
}

/** Moves `taskId` into `status` within a board snapshot, for optimistic drag-drop updates. */
function moveTaskInBoard(board: TaskBoard, taskId: number, status: TaskStatus): TaskBoard {
  let moved: Task | undefined
  const next: TaskBoard = { backlog: [], brainstorm: [], in_progress: [], review: [], done: [], cancelled: [] }
  for (const key of Object.keys(board) as TaskStatus[]) {
    for (const task of board[key]) {
      if (task.id === taskId) {
        moved = { ...task, status }
      } else {
        next[key].push(task)
      }
    }
  }
  if (moved) next[status].push(moved)
  return next
}

export function KanbanScreen() {
  const { projectId } = useParams<{ projectId: string }>()
  const queryClient = useQueryClient()
  const { data: projects } = useProjects()
  const { data: board } = useTasksBoard(projectId)
  // `all: true` — a task card's PR badges need to see done workers (merged
  // PRs) too, not just live spawning/running ones. No session rail lives on
  // this screen, so unlike TaskScreen there's no live-only view to protect.
  const { data: sessions } = useSessions({ project: projectId, all: true })
  const moveTask = useMoveTask()

  const [search, setSearch] = useState('')
  const [showCancelled, setShowCancelled] = useState(false)
  const [dragId, setDragId] = useState<number | null>(null)
  const [dragOverStatus, setDragOverStatus] = useState<TaskStatus | null>(null)
  const [newTaskOpen, setNewTaskOpen] = useState(false)
  const [startingTaskId, setStartingTaskId] = useState<number | null>(null)

  if (!projectId) return null

  const project = projects?.find((p) => p.id === projectId)
  const columns = showCancelled ? [...COLUMNS, CANCELLED_COLUMN] : COLUMNS

  function handleDrop(status: TaskStatus) {
    if (dragId == null) return
    const boardKey = ['tasks', projectId, 'board']
    const previous = queryClient.getQueryData<TaskBoard>(boardKey)
    const draggedTask = previous && (Object.values(previous).flat() as Task[]).find((t) => t.id === dragId)
    if (draggedTask?.status === status) {
      // Dropped back into its own column — no-op, don't PATCH.
      setDragId(null)
      setDragOverStatus(null)
      return
    }
    if (previous) {
      queryClient.setQueryData(boardKey, moveTaskInBoard(previous, dragId, status))
    }
    const id = dragId
    setDragId(null)
    setDragOverStatus(null)
    moveTask.mutate(
      { id, status },
      {
        onError: () => {
          if (previous) queryClient.setQueryData(boardKey, previous)
        },
      },
    )
  }

  return (
    <main className="kanban-screen">
      <div className="kanban-screen__subheader">
        <h1 className="kanban-screen__title">{project?.name ?? projectId}</h1>
        <div className="kanban-screen__search">
          <SearchInput value={search} onChange={setSearch} placeholder="Search tasks…" />
        </div>
        <div style={{ flex: 1 }} />
        <label className="kanban-screen__cancelled-toggle">
          <input
            type="checkbox"
            checked={showCancelled}
            onChange={(e) => setShowCancelled(e.target.checked)}
          />
          Show cancelled
        </label>
        <Link to={`/p/${projectId}/agents`} className="kanban-screen__cancelled-toggle">
          Agents
        </Link>
        <Link to="/settings" className="kanban-screen__settings-link" aria-label="Settings">
          ⚙
        </Link>
      </div>

      <div className="kanban-screen__board">
        {columns.map((col) => {
          const tasks = (board?.[col.key] ?? []).filter((t) => matchesSearch(t, search))
          const isDropTarget = col.key !== 'cancelled'
          return (
            <Column
              key={col.key}
              title={col.title}
              dotColor={col.dot}
              count={tasks.length}
              onAdd={col.key === 'backlog' ? () => setNewTaskOpen(true) : undefined}
              highlighted={isDropTarget && dragOverStatus === col.key}
              onDragOver={
                isDropTarget
                  ? (e: DragEvent<HTMLDivElement>) => {
                      e.preventDefault()
                      setDragOverStatus(col.key)
                    }
                  : undefined
              }
              onDragLeave={isDropTarget ? () => setDragOverStatus(null) : undefined}
              onDrop={
                isDropTarget
                  ? (e: DragEvent<HTMLDivElement>) => {
                      e.preventDefault()
                      handleDrop(col.key)
                    }
                  : undefined
              }
            >
              {tasks.map((task) => (
                <TaskCard
                  key={task.id}
                  task={task}
                  projectId={projectId}
                  mainRepo={project?.main}
                  sessions={sessions}
                  dragging={dragId === task.id}
                  onDragStart={() => setDragId(task.id)}
                  onDragEnd={() => {
                    setDragId(null)
                    setDragOverStatus(null)
                  }}
                  onStart={() => setStartingTaskId(task.id)}
                />
              ))}
            </Column>
          )
        })}
      </div>

      {newTaskOpen && <NewTaskModal projectId={projectId} onClose={() => setNewTaskOpen(false)} />}
      {startingTaskId != null && (
        <StartModal taskId={startingTaskId} onClose={() => setStartingTaskId(null)} />
      )}
    </main>
  )
}
