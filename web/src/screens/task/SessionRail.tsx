// Right rail (docs/design/Task.dc.html "RIGHT: session rail"): the task's
// orchestrator session plus its worker sessions, each with term/attach/kill
// actions. Errored workers get a "restore" action instead of relying on
// kill alone (`POST /v1/sessions/{id}/restore`, phase 4).

import { useEffect, useState } from 'react'
import { Badge, type BadgeTone } from '../../components/Badge'
import { Dot, type DotState } from '../../components/Dot'
import { useKillSession, useRestoreSession } from '../../lib/queries'
import type { Session } from '../../lib/types'
import './SessionRail.css'

export interface SessionRailProps {
  orchestrator?: Session
  workers: Session[]
  onOpenTerm: (session: { id: string; tmux_name: string }) => void
}

const COPY_FEEDBACK_MS = 2500

function dotState(session: Session): DotState {
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

function badgeTone(session: Session): BadgeTone {
  const label = session.activity ?? session.state
  if (label === 'active' || label === 'ready' || label === 'running') return 'ok'
  if (label === 'blocked' || label === 'waiting_input') return 'warn'
  if (label === 'errored' || label === 'exited' || label === 'killed') return 'err'
  if (label === 'spawning') return 'indigo'
  return 'neutral'
}

function prText(session: Session): { text: string; tone: string } {
  if (!session.pr_number) {
    if (session.activity === 'blocked') return { text: 'PR —', tone: 'session-rail__pr--err' }
    return { text: 'PR —', tone: 'session-rail__pr--neutral' }
  }
  if (session.ci_state === 'passing') return { text: `PR #${session.pr_number} ✔`, tone: 'session-rail__pr--ok' }
  if (session.ci_state === 'failing') return { text: `PR #${session.pr_number} ✗`, tone: 'session-rail__pr--err' }
  if (session.ci_state === 'pending') return { text: `PR #${session.pr_number} ⏳`, tone: 'session-rail__pr--warn' }
  return { text: `PR #${session.pr_number}`, tone: 'session-rail__pr--neutral' }
}

function useCopyAttach() {
  const [copiedId, setCopiedId] = useState<string | null>(null)

  useEffect(() => {
    if (!copiedId) return
    const timer = setTimeout(() => setCopiedId(null), COPY_FEEDBACK_MS)
    return () => clearTimeout(timer)
  }, [copiedId])

  async function copy(session: Session) {
    try {
      await navigator.clipboard.writeText(`rocket attach ${session.tmux_name}`)
      setCopiedId(session.id)
    } catch {
      // Clipboard access can fail (permissions, non-secure context); just
      // skip the "copied" confirmation.
    }
  }

  return { copiedId, copy }
}

export function SessionRail({ orchestrator, workers, onOpenTerm }: SessionRailProps) {
  const kill = useKillSession()
  const restore = useRestoreSession()
  const { copiedId, copy } = useCopyAttach()

  function handleKill(session: Session) {
    if (!window.confirm(`Kill session ${session.tmux_name}?`)) return
    kill.mutate({ id: session.id })
  }

  return (
    <aside className="session-rail">
      <div className="session-rail__heading">Sessions</div>

      {orchestrator && (
        <div className="session-rail__orch">
          <div className="session-rail__orch-head">
            <Dot state={dotState(orchestrator)} />
            <span className="session-rail__orch-label">Orchestrator</span>
            <div className="session-rail__spacer" />
            <Badge tone={badgeTone(orchestrator)}>{orchestrator.activity ?? orchestrator.state}</Badge>
          </div>
          <div className="session-rail__orch-name">{orchestrator.tmux_name}</div>
          <div className="session-rail__orch-actions">
            <button
              type="button"
              className="session-rail__term-btn"
              onClick={() => onOpenTerm({ id: orchestrator.id, tmux_name: orchestrator.tmux_name })}
            >
              ▣ term
            </button>
            <button type="button" className="session-rail__attach-btn" onClick={() => copy(orchestrator)}>
              attach ⧉
            </button>
            <button type="button" className="session-rail__kill-btn" onClick={() => handleKill(orchestrator)}>
              kill
            </button>
          </div>
          {copiedId === orchestrator.id && (
            <div className="session-rail__copied">copied: rocket attach {orchestrator.tmux_name}</div>
          )}
        </div>
      )}

      <div className="session-rail__workers-heading">Workers</div>
      <div className="session-rail__workers">
        {workers.length === 0 && <p className="session-rail__empty">No workers yet.</p>}
        {workers.map((w) => {
          const pr = prText(w)
          const errored = w.state === 'errored'
          return (
            <div key={w.id} className="session-rail__worker">
              <div className="session-rail__worker-head">
                <Dot state={dotState(w)} />
                <span className="session-rail__worker-name">{w.tmux_name}</span>
                <span className="session-rail__worker-repo">{w.repo_id}</span>
              </div>
              <div className="session-rail__worker-status">
                <Badge tone={badgeTone(w)}>{w.activity ?? w.state}</Badge>
                <span className={`session-rail__pr ${pr.tone}`}>{pr.text}</span>
              </div>
              <div className="session-rail__worker-actions">
                <button
                  type="button"
                  className="session-rail__worker-term"
                  onClick={() => onOpenTerm({ id: w.id, tmux_name: w.tmux_name })}
                >
                  ▣ term
                </button>
                <button type="button" className="session-rail__worker-attach" onClick={() => copy(w)}>
                  ⧉
                </button>
                {errored ? (
                  <button
                    type="button"
                    className="session-rail__worker-restore"
                    onClick={() => restore.mutate(w.id)}
                    disabled={restore.isPending}
                  >
                    restore
                  </button>
                ) : (
                  <button type="button" className="session-rail__worker-kill" onClick={() => handleKill(w)}>
                    kill
                  </button>
                )}
              </div>
              {copiedId === w.id && (
                <div className="session-rail__copied">copied: rocket attach {w.tmux_name}</div>
              )}
            </div>
          )
        })}
      </div>
    </aside>
  )
}
