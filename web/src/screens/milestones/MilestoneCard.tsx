import type { DragEvent } from 'react'
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Badge } from '../../components/Badge'
import type { Agent, Task } from '../../lib/types'
import { attachCommand } from '../agents/AgentCard'
import { chatPagePath } from '../chat/ChatScreen'
import { termPagePath } from '../term/TermScreen'
import './milestones.css'

const COPY_FEEDBACK_MS = 1500

export interface MilestoneCardProps {
  milestone: Task
  /** Every registered agent — the assign picker's options. */
  agents: Agent[] | undefined
  dragging: boolean
  onDragStart: (e: DragEvent<HTMLDivElement>) => void
  onDragEnd: () => void
  onAssign: (agentId: string | null) => void
}

/**
 * One milestone on the board. Deliberately built from the kanban card's
 * classes — a milestone is a task and should read like one — with the two
 * things only a milestone has on top: who holds it, and the way into that
 * agent's session (one tmux session serves all of its milestones, and its id
 * IS the agent id, docs/10-agents.md).
 */
export function MilestoneCard({
  milestone,
  agents,
  dragging,
  onDragStart,
  onDragEnd,
  onAssign,
}: MilestoneCardProps) {
  const [picking, setPicking] = useState(false)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), COPY_FEEDBACK_MS)
    return () => clearTimeout(timer)
  }, [copied])

  const holder = milestone.assigned_role
  const holderAgent = holder ? agents?.find((a) => a.id === holder) : undefined
  const live = holderAgent?.session_alive === true
  const draggable = milestone.status !== 'cancelled'

  async function copyAttach() {
    if (!holder) return
    try {
      await navigator.clipboard.writeText(attachCommand(holder))
      setCopied(true)
    } catch {
      // Clipboard access can fail (permissions, non-secure context); the
      // command is in the title attribute either way.
    }
  }

  return (
    <div
      className="kanban-card"
      draggable={draggable}
      onDragStart={draggable ? onDragStart : undefined}
      onDragEnd={onDragEnd}
      style={{ opacity: dragging ? 0.4 : 1 }}
    >
      <Link to={`/milestones/${milestone.id}`} className="kanban-card__title-row">
        <span className="kanban-card__id">#{milestone.id}</span>
        <span className="kanban-card__title">{milestone.title}</span>
      </Link>

      <div className="milestone-card__holder">
        {holder ? (
          <Badge tone="indigo">◆ {holder}</Badge>
        ) : (
          <Badge tone="neutral">not taken</Badge>
        )}
        {/* `quiet` arrives from the daemon (subtask #1032); until it does,
            its absence simply means the agent has been showing its work. */}
        {milestone.quiet && <Badge tone="warn">🤐 quiet</Badge>}
        {milestone.waiting_terminal && <Badge tone="warn">⏳ waiting for input</Badge>}
      </div>

      {milestone.questions_awaiting_user > 0 ? (
        <div className="kanban-card__questions">
          <Badge tone="warn">? {milestone.questions_awaiting_user} awaiting you</Badge>
        </div>
      ) : milestone.open_questions > 0 ? (
        <div className="kanban-card__questions">
          <Badge tone="neutral">? {milestone.open_questions} open</Badge>
        </div>
      ) : null}

      <div className="milestone-card__actions">
        <button
          type="button"
          className="milestone-card__action"
          onClick={() => setPicking((open) => !open)}
          aria-expanded={picking}
        >
          ◆ assign
        </button>
        {holder && live && (
          <>
            <a
              className="milestone-card__action"
              href={termPagePath(holder)}
              target="_blank"
              rel="noopener noreferrer"
            >
              ▣ term
            </a>
            <a
              className="milestone-card__action"
              href={chatPagePath(holder)}
              target="_blank"
              rel="noopener noreferrer"
            >
              💬 chat
            </a>
          </>
        )}
        {holder && (
          <button
            type="button"
            className="milestone-card__action"
            onClick={copyAttach}
            title={attachCommand(holder)}
          >
            {copied ? '✓ copied' : '⧉ attach'}
          </button>
        )}
      </div>

      {picking && (
        <div className="milestone-card__picker">
          {(agents ?? []).map((agent) => (
            <button
              key={agent.id}
              type="button"
              className="milestone-card__picker-option"
              disabled={agent.id === holder}
              onClick={() => {
                setPicking(false)
                onAssign(agent.id)
              }}
            >
              {agent.id}
            </button>
          ))}
          {holder && (
            <button
              type="button"
              className="milestone-card__picker-option milestone-card__picker-option--none"
              onClick={() => {
                setPicking(false)
                onAssign(null)
              }}
            >
              unassign
            </button>
          )}
        </div>
      )}
    </div>
  )
}
