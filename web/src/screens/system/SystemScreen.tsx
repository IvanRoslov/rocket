// System screen (docs/design/System.dc.html): observability across the
// daemon — reconciled tmux sessions/agents, the message queue, worktrees on
// disk, daemon status, and a live-ish log tail. `useSystem()` polls
// `GET /v1/system` every 5s; the sessions table joins that against
// `useSessions()` so rows carry the richer session record (kind, agent,
// worktree path) while orphan/state flags come from the daemon's tmux
// reconciliation.

import { useQueries } from '@tanstack/react-query'
import { useState } from 'react'
import { Badge, type BadgeTone } from '../../components/Badge'
import { Button } from '../../components/Button'
import { Card } from '../../components/Card'
import { Modal } from '../../components/Modal'
import { api } from '../../lib/api'
import { formatBytes, formatUptime } from '../../lib/format'
import { useKillSession, useSessions, useSystem, useSystemCleanup } from '../../lib/queries'
import type { Message, Session, TmuxEntry } from '../../lib/types'
import { chatPagePath } from '../chat/ChatScreen'
import { termPagePath } from '../term/TermScreen'
import './system.css'

const stateTone: Record<string, BadgeTone> = {
  running: 'ok',
  spawning: 'indigo',
  done: 'neutral',
  killed: 'neutral',
  errored: 'err',
  orphan: 'warn',
}

function stateBadgeTone(state: string): BadgeTone {
  return stateTone[state] ?? 'neutral'
}

interface SessionRow {
  key: string
  name: string
  kind: string
  agent: string
  state: string
  session?: Session
  orphan: boolean
}

/**
 * Joins `useSessions()` records against `system.tmux` by tmux name to get
 * each row's orphan flag, then appends rows for tmux entries with no
 * matching store record at all (pure orphans, per internal/api/system.go's
 * `orphan` semantics).
 */
function buildSessionRows(sessions: Session[], tmux: TmuxEntry[]): SessionRow[] {
  const tmuxByName = new Map(tmux.map((t) => [t.name, t]))
  const matched = new Set<string>()

  const rows: SessionRow[] = sessions.map((s) => {
    const t = tmuxByName.get(s.tmux_name)
    if (t) matched.add(t.name)
    return {
      key: s.id,
      name: s.tmux_name,
      kind: s.kind,
      agent: s.agent,
      state: s.state,
      session: s,
      orphan: t?.orphan ?? false,
    }
  })

  for (const t of tmux) {
    if (t.orphan && !matched.has(t.name)) {
      rows.push({
        key: `tmux-${t.name}`,
        name: t.name,
        kind: '—',
        agent: '—',
        state: 'orphan',
        orphan: true,
      })
    }
  }

  return rows
}

function latestFailedMessage(messages: Message[]): Message | undefined {
  return messages
    .filter((m) => m.status === 'failed')
    .sort((a, b) => b.created_at - a.created_at)[0]
}

/**
 * `GET /v1/system` only reports the queue's aggregate queued/failed counts
 * (internal/api/system.go), not individual messages — there's no "list all
 * messages" endpoint. To surface the failed-message plate from the design
 * (docs/design/System.dc.html), fan out `GET /v1/messages?session=` across
 * every live (non-orphan) tmux session and pick the most recent failure.
 */
function useLatestFailedMessage(sessionIds: string[]): Message | undefined {
  const results = useQueries({
    queries: sessionIds.map((id) => ({
      queryKey: ['messages', id],
      queryFn: () => api.get<{ messages: Message[] }>(`/v1/messages?session=${encodeURIComponent(id)}`),
    })),
  })
  const all = results.flatMap((r) => r.data?.messages ?? [])
  return latestFailedMessage(all)
}

interface StatTileProps {
  label: string
  value: number
  unit: string
  highlight?: boolean
}

function StatTile({ label, value, unit, highlight }: StatTileProps) {
  return (
    <div className="stat-tile" data-testid="stat-tile" data-highlight={highlight ? 'true' : 'false'}>
      <div className="stat-tile__label">{label}</div>
      <div className="stat-tile__value-row">
        <span className="stat-tile__value">{value}</span>
        <span className="stat-tile__unit">{unit}</span>
      </div>
    </div>
  )
}

interface KillConfirmModalProps {
  session: Session
  onClose: () => void
  onConfirm: (cleanup: boolean) => void
  pending: boolean
}

function KillConfirmModal({ session, onClose, onConfirm, pending }: KillConfirmModalProps) {
  const [cleanup, setCleanup] = useState(false)
  return (
    <Modal title="Kill session?" onClose={onClose}>
      <p className="kill-confirm__body">
        This will kill tmux session <strong>{session.tmux_name}</strong>.
      </p>
      <label className="kill-confirm__checkbox">
        <input
          type="checkbox"
          checked={cleanup}
          onChange={(e) => setCleanup(e.target.checked)}
        />
        Also remove its worktree
      </label>
      <div className="kill-confirm__actions">
        <Button variant="secondary" size="sm" onClick={onClose} disabled={pending}>
          Cancel
        </Button>
        <Button
          variant="danger"
          size="sm"
          onClick={() => onConfirm(cleanup)}
          disabled={pending}
        >
          Confirm kill
        </Button>
      </div>
    </Modal>
  )
}

export function SystemScreen() {
  const { data: system } = useSystem()
  const { data: sessions } = useSessions()
  const cleanup = useSystemCleanup()
  const killSession = useKillSession()
  const [killTarget, setKillTarget] = useState<Session | undefined>(undefined)

  const rows = buildSessionRows(sessions ?? [], system?.tmux ?? [])
  const orphanCount = system?.tmux.filter((t) => t.orphan).length ?? 0
  const liveCount = system?.tmux.filter((t) => !t.orphan && t.state === 'running').length ?? 0
  const totalWorktreeBytes = system?.worktrees.reduce((sum, w) => sum + w.size_bytes, 0) ?? 0
  const liveSessionIds = (system?.tmux ?? [])
    .filter((t) => !t.orphan && t.session_id)
    .map((t) => t.session_id as string)
  const failedPlate = useLatestFailedMessage(liveSessionIds)

  function handleConfirmKill(cleanupWorktree: boolean) {
    if (!killTarget) return
    killSession.mutate(
      { id: killTarget.id, cleanup: cleanupWorktree },
      { onSuccess: () => setKillTarget(undefined) },
    )
  }

  return (
    <main className="system-screen">
      <div className="system-screen__header">
        <div>
          <h1 className="system-screen__title">System</h1>
          <p className="system-screen__subtitle">
            Observability across the daemon — sessions, worktrees, message queue.
          </p>
        </div>
        <Button variant="secondary" onClick={() => cleanup.mutate()} disabled={cleanup.isPending}>
          ⌦ Cleanup orphans
        </Button>
      </div>

      <div className="system-stats">
        {/* liveCount is intentionally used for both tiles until the API distinguishes session vs agent counts */}
        <StatTile label="Live sessions" value={liveCount} unit="tmux" />
        <StatTile label="Agents running" value={liveCount} unit="claude/codex" />
        <StatTile label="Orphans" value={orphanCount} unit="reconcile" highlight={orphanCount > 0} />
        <StatTile
          label="Queue depth"
          value={system?.queue.queued ?? 0}
          unit={system && system.queue.failed > 0 ? `+${system.queue.failed} failed` : ''}
        />
      </div>

      <div className="system-sessions">
        <div className="system-sessions__head">
          <span className="system-sessions__head-title">tmux sessions &amp; agents</span>
          <span className="system-sessions__head-note">reconciled with daemon state</span>
        </div>
        <div className="session-cards">
          {rows.map((row) => (
            <div
              key={row.key}
              className="session-card"
              data-testid="session-row"
              data-orphan={row.orphan ? 'true' : 'false'}
            >
              <div className="session-card__row1">
                <span className={`session-card__dot session-card__dot--${stateBadgeTone(row.state)}`} />
                <span className="session-card__name" title={row.name}>
                  {row.name}
                </span>
                {row.kind !== '—' && <span className="session-card__kind">{row.kind}</span>}
              </div>
              <div className="session-card__row2">
                <span className="session-card__agent">{row.agent}</span>
                <Badge tone={stateBadgeTone(row.state)}>{row.state}</Badge>
              </div>
              <div className="session-card__row3">
                {row.session && (
                  <a
                    className="session-card__term"
                    href={termPagePath(row.session.id)}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    ▣ term
                  </a>
                )}
                {row.session && (
                  <a
                    className="session-card__chat"
                    href={chatPagePath(row.session.id)}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    💬 chat
                  </a>
                )}
                {row.session && (row.state === 'running' || row.state === 'spawning') && (
                  <Button variant="danger" size="sm" onClick={() => setKillTarget(row.session)}>
                    kill
                  </Button>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="system-grid">
          <Card className="panel">
            <div className="panel__title">Message queue</div>
            <div className="queue-tiles">
              <div className="queue-tile queue-tile--queued">
                <div className="queue-tile__value">{system?.queue.queued ?? 0}</div>
                <div className="queue-tile__label">queued</div>
              </div>
              <div className="queue-tile queue-tile--failed">
                <div className="queue-tile__value">{system?.queue.failed ?? 0}</div>
                <div className="queue-tile__label">failed</div>
              </div>
            </div>
            {/* Shows failures of live sessions only; disagrees with queue.failed tile if queue has
                messages from sessions no longer in tmux */}
            {failedPlate && (
              <div className="queue-plate" title="shows failures of live sessions only">
                msg#{failedPlate.id} → {failedPlate.to}
                <br />
                failed ×{failedPlate.attempts} · {failedPlate.reason}
              </div>
            )}
          </Card>

          <Card className="panel">
            <div className="worktrees-head">
              <span className="panel__title">Worktrees on disk</span>
              <span className="worktrees-total">{formatBytes(totalWorktreeBytes)}</span>
            </div>
            <div className="worktree-list">
              {system?.worktrees.map((w) => (
                <div className="worktree-row" key={w.path}>
                  <span className="worktree-row__path">{w.path}</span>
                  <span className="worktree-row__size">{formatBytes(w.size_bytes)}</span>
                </div>
              ))}
            </div>
          </Card>

          <Card className="panel">
            <div className="panel__title">Daemon</div>
            <div className="daemon-kv">
              <div className="daemon-kv__row">
                <span className="daemon-kv__key">status</span>
                <span className="daemon-kv__status">● running</span>
              </div>
              <div className="daemon-kv__row">
                <span className="daemon-kv__key">version</span>
                <span className="daemon-kv__val">{system?.daemon.version}</span>
              </div>
              <div className="daemon-kv__row">
                <span className="daemon-kv__key">socket</span>
                <span className="daemon-kv__val">{system?.daemon.socket}</span>
              </div>
              <div className="daemon-kv__row">
                <span className="daemon-kv__key">uptime</span>
                <span className="daemon-kv__val">
                  {system ? formatUptime(system.daemon.uptime_s) : ''}
                </span>
              </div>
            </div>
          </Card>
      </div>

      <div className="log-tail">
        <div className="log-tail__head">
          <span className="log-tail__title">rocketd.log</span>
          <span className="log-tail__note">tail -f</span>
        </div>
        <pre className="log-tail__body">{system?.log_tail.join('\n')}</pre>
      </div>

      {killTarget && (
        <KillConfirmModal
          session={killTarget}
          onClose={() => setKillTarget(undefined)}
          onConfirm={handleConfirmKill}
          pending={killSession.isPending}
        />
      )}
    </main>
  )
}
