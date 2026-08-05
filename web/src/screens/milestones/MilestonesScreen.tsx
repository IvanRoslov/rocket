// The Milestones page (task #1023, spec v2 «Дашборд и mobile»): the board of
// work the persistent agents hold. Same kanban shape as a project board —
// milestones live outside every project, so this page is never scoped by the
// project switcher and its cards carry an agent instead of an orchestrator.

import { useState, type DragEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { SearchInput } from '../../components/SearchInput'
import {
  useAgents,
  useAssignMilestone,
  useMilestonesBoard,
  useMoveTask,
  type TaskBoard,
} from '../../lib/queries'
import type { Task, TaskStatus } from '../../lib/types'
import { Column } from '../kanban/Column'
import '../kanban/kanban.css'
import { MilestoneCard } from './MilestoneCard'
import { NewMilestoneModal } from './NewMilestoneModal'
import './milestones.css'

interface ColumnSpec {
  key: TaskStatus
  title: string
  dot: string
}

const COLUMNS: ColumnSpec[] = [
  { key: 'backlog', title: 'Backlog', dot: 'var(--text-4)' },
  { key: 'brainstorm', title: 'Brainstorm', dot: 'var(--review)' },
  { key: 'in_progress', title: 'In Progress', dot: 'var(--accent)' },
  { key: 'review', title: 'Review', dot: 'var(--review)' },
  { key: 'done', title: 'Done', dot: 'var(--ok)' },
]

const CANCELLED_COLUMN: ColumnSpec = { key: 'cancelled', title: 'Cancelled', dot: 'var(--text-4)' }

const BOARD_KEY = ['tasks', 'milestones', 'board']

function matchesSearch(task: Task, query: string): boolean {
  if (!query.trim()) return true
  const q = query.trim().toLowerCase()
  return task.title.toLowerCase().includes(q) || (task.assigned_role ?? '').toLowerCase().includes(q)
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

export function MilestonesScreen() {
  const queryClient = useQueryClient()
  const { data: board } = useMilestonesBoard()
  const { data: agents } = useAgents()
  const moveTask = useMoveTask()
  const assign = useAssignMilestone()

  const [search, setSearch] = useState('')
  const [showCancelled, setShowCancelled] = useState(false)
  const [dragId, setDragId] = useState<number | null>(null)
  const [dragOverStatus, setDragOverStatus] = useState<TaskStatus | null>(null)
  const [newOpen, setNewOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const columns = showCancelled ? [...COLUMNS, CANCELLED_COLUMN] : COLUMNS

  function handleDrop(status: TaskStatus) {
    if (dragId == null) return
    const previous = queryClient.getQueryData<TaskBoard>(BOARD_KEY)
    const dragged = previous && (Object.values(previous).flat() as Task[]).find((t) => t.id === dragId)
    if (dragged?.status === status) {
      setDragId(null)
      setDragOverStatus(null)
      return
    }
    if (previous) {
      queryClient.setQueryData(BOARD_KEY, moveTaskInBoard(previous, dragId, status))
    }
    const id = dragId
    setDragId(null)
    setDragOverStatus(null)
    setError(null)
    moveTask.mutate(
      { id, status },
      {
        // The daemon refuses an empty milestone at review and reserves done
        // for the human (internal/api/milestones.go). Show what it said and
        // put the card back where it was.
        onError: (err) => {
          if (previous) queryClient.setQueryData(BOARD_KEY, previous)
          setError(err.message)
        },
      },
    )
  }

  function handleAssign(id: number, agentId: string | null) {
    setError(null)
    assign.mutate({ id, agentId }, { onError: (err) => setError(err.message) })
  }

  return (
    <main className="kanban-screen">
      <div className="kanban-screen__subheader">
        <h1 className="kanban-screen__title">Milestones</h1>
        <div className="kanban-screen__search">
          <SearchInput value={search} onChange={setSearch} placeholder="Search milestones…" />
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
      </div>

      {error && (
        <p className="milestones-screen__error" role="alert">
          {error}
        </p>
      )}

      <div className="kanban-screen__board">
        {columns.map((col) => {
          const items = (board?.[col.key] ?? []).filter((t) => matchesSearch(t, search))
          const isDropTarget = col.key !== 'cancelled'
          return (
            <Column
              key={col.key}
              title={col.title}
              dotColor={col.dot}
              count={items.length}
              onAdd={col.key === 'backlog' ? () => setNewOpen(true) : undefined}
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
              {items.map((milestone) => (
                <MilestoneCard
                  key={milestone.id}
                  milestone={milestone}
                  agents={agents}
                  dragging={dragId === milestone.id}
                  onDragStart={() => setDragId(milestone.id)}
                  onDragEnd={() => {
                    setDragId(null)
                    setDragOverStatus(null)
                  }}
                  onAssign={(agentId) => handleAssign(milestone.id, agentId)}
                />
              ))}
            </Column>
          )
        })}
      </div>

      {newOpen && <NewMilestoneModal onClose={() => setNewOpen(false)} />}
    </main>
  )
}
