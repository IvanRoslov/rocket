// Presentation helpers for agents (docs/10-agents.md). Kept pure so the
// screens stay declarative and the rules are unit-tested without rendering.

import type { Agent } from '../api/types'
import { colors } from '../theme'

export interface BadgeProps {
  label: string
  fg: string
  bg: string
}

/** Total number of agent threads where the next word is the user's. */
export function awaitingUser(agents: Agent[]): number {
  return agents.reduce((n, a) => n + a.awaiting_user, 0)
}

/** Badges for an agent card, most urgent first. */
export function agentBadges(a: Agent): BadgeProps[] {
  const out: BadgeProps[] = []
  if (a.session_alive) out.push({ label: '● live', fg: colors.greenFg, bg: colors.greenBg })
  if (!a.enabled) out.push({ label: 'disabled', fg: colors.slateFg, bg: colors.slateBg })
  if (a.awaiting_user > 0)
    out.push({ label: `? ${a.awaiting_user} awaiting you`, fg: colors.amberDeep, bg: colors.amberBg })
  if (a.open_questions > 0)
    out.push({ label: `${a.open_questions} open Q`, fg: colors.purpleFg, bg: colors.purpleBg })
  if (a.unread > 0) out.push({ label: `${a.unread} unread`, fg: colors.indigoFg, bg: colors.indigoBg })
  if (out.length === 0) out.push({ label: 'idle', fg: colors.slateFg, bg: colors.slateBg })
  return out
}

// The inbox holds one kind of row — a message — in one of two states: unread
// until the agent pulls it with `rocket inbox next`, read afterwards.
const INBOX_STATUS_COLOR: Record<string, [string, string]> = {
  unread: [colors.amberDeep, colors.amberBg],
  read: [colors.slateFg, colors.slateBg],
}

export function inboxStatusBadge(status: string): BadgeProps {
  const [fg, bg] = INBOX_STATUS_COLOR[status] ?? [colors.slateFg, colors.slateBg]
  return { label: status, fg, bg }
}
