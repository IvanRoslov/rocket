// Development session rail. Shared cards own identity/status/access; this
// screen retains its quiz, PR and lifecycle controls.
import { Badge } from '../../components/Badge'
import { SessionCard } from '../../components/SessionCard'
import { useKillSession, useRestoreSession } from '../../lib/queries'
import type { PendingQuiz, Session } from '../../lib/types'
import './SessionRail.css'

export interface SessionRailProps {
  orchestrator?: Session
  workers: Session[]
}

function prText(session: Session): { text: string; tone: string } {
  if (!session.pr_number) {
    if (session.activity === 'blocked') return { text: 'PR —', tone: 'session-rail__pr--err' }
    return { text: 'PR —', tone: 'session-rail__pr--neutral' }
  }
  // pr_state 'closed' (declined without merging) and 'merged' aren't
  // CI-relevant anymore — show them plainly instead of falling through to
  // the (possibly stale) ci_state.
  if (session.pr_state === 'closed') return { text: `PR #${session.pr_number} closed`, tone: 'session-rail__pr--neutral' }
  if (session.pr_state === 'merged') return { text: `PR #${session.pr_number} merged`, tone: 'session-rail__pr--ok' }
  if (session.ci_state === 'passing') return { text: `PR #${session.pr_number} ✔`, tone: 'session-rail__pr--ok' }
  if (session.ci_state === 'failing') return { text: `PR #${session.pr_number} ✗`, tone: 'session-rail__pr--err' }
  if (session.ci_state === 'pending') return { text: `PR #${session.pr_number} ⏳`, tone: 'session-rail__pr--warn' }
  return { text: `PR #${session.pr_number}`, tone: 'session-rail__pr--neutral' }
}

/** «quiz» for an AskUserQuestion quiz, «Разрешение» for a TUI permission prompt (task #4881). */
function QuizBadge({ quiz }: { quiz: PendingQuiz }) {
  return quiz.source === 'permission' ? (
    <Badge tone="warn">Разрешение</Badge>
  ) : (
    <Badge tone="indigo">quiz</Badge>
  )
}

export function SessionRail({ orchestrator, workers }: SessionRailProps) {
  const kill = useKillSession()
  const restore = useRestoreSession()

  function handleKill(session: Session) {
    if (!window.confirm(`Kill session ${session.tmux_name}?`)) return
    kill.mutate({ id: session.id })
  }

  return (
    <aside className="session-rail">
      <div className="session-rail__heading">Sessions</div>
      {orchestrator && (
        <SessionCard session={orchestrator} role="orchestrator"
          beforeStatus={orchestrator.pending_quiz && <QuizBadge quiz={orchestrator.pending_quiz} />}
          actions={<button type="button" className="session-rail__kill-btn" onClick={() => handleKill(orchestrator)}>kill</button>}
        />
      )}
      <div className="session-rail__workers-heading">Workers</div>
      <div className="session-rail__workers">
        {workers.length === 0 && <p className="session-rail__empty">No workers yet.</p>}
        {workers.map((w) => {
          const pr = prText(w)
          return (
            <SessionCard key={w.id} session={w} role="worker" repo={w.repo_id}
              beforeStatus={w.pending_quiz && <QuizBadge quiz={w.pending_quiz} />}
              afterStatus={<span className={`session-rail__pr ${pr.tone}`}>{pr.text}</span>}
              actions={w.state === 'errored' ? (
                <button type="button" className="session-rail__worker-restore"
                  onClick={() => restore.mutate(w.id)} disabled={restore.isPending}>restore</button>
              ) : (
                <button type="button" className="session-rail__worker-kill" onClick={() => handleKill(w)}>kill</button>
              )}
            />
          )
        })}
      </div>
    </aside>
  )
}
