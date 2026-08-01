// Presentation helpers for agent roles (docs/10-agents.md). Kept pure so the
// screens stay declarative and the rules are unit-tested without rendering.

import type { Agent, AgentInboxEvent, AgentSubscription } from '../api/types'
import { colors } from '../theme'

export interface BadgeProps {
  label: string
  fg: string
  bg: string
}

/** Total number of role threads where the next word is the user's. */
export function awaitingUser(agents: Agent[]): number {
  return agents.reduce((n, a) => n + a.awaiting_user, 0)
}

/** Badges for a role card, most urgent first. */
export function agentBadges(a: Agent): BadgeProps[] {
  const out: BadgeProps[] = []
  if (!a.enabled) out.push({ label: 'disabled', fg: colors.slateFg, bg: colors.slateBg })
  if (a.awaiting_user > 0)
    out.push({ label: `? ${a.awaiting_user} awaiting you`, fg: colors.amberDeep, bg: colors.amberBg })
  if (a.open_questions > 0)
    out.push({ label: `${a.open_questions} open Q`, fg: colors.purpleFg, bg: colors.purpleBg })
  if (a.inbox_queued > 0) out.push({ label: `${a.inbox_queued} queued`, fg: colors.indigoFg, bg: colors.indigoBg })
  if (a.items > 0) out.push({ label: `${a.items} tracked`, fg: colors.slateFg, bg: colors.slateBg })
  if (out.length === 0) out.push({ label: 'idle', fg: colors.slateFg, bg: colors.slateBg })
  return out
}

const INBOX_KIND_LABEL: Record<string, string> = {
  message: 'message',
  issue_opened: 'issue opened',
  issue_comment: 'issue comment',
  task_update: 'task update',
  snooze_expired: 'snooze expired',
  cron: 'cron',
  question: 'question',
  terminal_opened: 'terminal',
}

const INBOX_KIND_COLOR: Record<string, [string, string]> = {
  message: [colors.indigoFg, colors.indigoBg],
  issue_opened: [colors.purpleFg, colors.purpleBg],
  issue_comment: [colors.purpleFg, colors.purpleBg],
  task_update: [colors.greenFg, colors.greenBg],
  question: [colors.amberDeep, colors.amberBg],
}

export function inboxKindBadge(kind: string): BadgeProps {
  const [fg, bg] = INBOX_KIND_COLOR[kind] ?? [colors.slateFg, colors.slateBg]
  return { label: INBOX_KIND_LABEL[kind] ?? kind, fg, bg }
}

const INBOX_STATUS_COLOR: Record<string, [string, string]> = {
  queued: [colors.amberDeep, colors.amberBg],
  delivered: [colors.indigoFg, colors.indigoBg],
  done: [colors.slateFg, colors.slateBg],
}

export function inboxStatusBadge(status: string): BadgeProps {
  const [fg, bg] = INBOX_STATUS_COLOR[status] ?? [colors.slateFg, colors.slateBg]
  return { label: status, fg, bg }
}

// Dossier states (spec): new → triaged → taken | deferred | waiting_team →
// in_work → resolved → closed. Not enforced by the daemon — it is the role's
// own notebook — so unknown values must render, not crash.
const ITEM_STATE_COLOR: Record<string, [string, string]> = {
  taken: [colors.indigoFg, colors.indigoBg],
  in_work: [colors.indigoFg, colors.indigoBg],
  triaged: [colors.indigoFg, colors.indigoBg],
  deferred: [colors.amberDeep, colors.amberBg],
  waiting_team: [colors.amberDeep, colors.amberBg],
  resolved: [colors.greenFg, colors.greenBg],
  closed: [colors.slateFg, colors.slateBg],
}

export function itemStateBadge(state: string): BadgeProps {
  const [fg, bg] = ITEM_STATE_COLOR[state] ?? [colors.slateFg, colors.slateBg]
  return { label: state || 'new', fg, bg }
}

/** One-line preview of an inbox event: its human-readable field, else the raw payload. */
export function inboxSummary(e: AgentInboxEvent): string {
  const p = (e.payload ?? {}) as Record<string, unknown>
  for (const key of ['text', 'title', 'body', 'comment', 'status']) {
    const v = p[key]
    if (typeof v === 'string' && v.trim()) return v.trim()
  }
  if (Object.keys(p).length === 0) return ''
  return JSON.stringify(p)
}

/**
 * True when `sessionID` is a run of `roleID`. Runs are named "<role>-run-<n>"
 * — the id is the only link between a session and its role.
 */
export function isRoleRun(sessionID: string, roleID: string): boolean {
  const rest = sessionID.startsWith(`${roleID}-run-`) ? sessionID.slice(`${roleID}-run-`.length) : ''
  return rest !== '' && /^\d+$/.test(rest)
}

export function subscriptionLabel(s: AgentSubscription): string {
  const parts = [s.repo]
  if (s.labels?.length) parts.push(s.labels.join(', '))
  if (s.mention_only) parts.push('@mentions')
  return parts.join(' · ')
}
