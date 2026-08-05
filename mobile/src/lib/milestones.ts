// Presentation helpers for milestones (task #1023, spec v2): root tasks
// outside every project, held by a persistent agent. Kept pure so the rules
// are unit-tested without rendering.

import type { Task } from '../api/types'
import { colors } from '../theme'
import type { BadgeProps } from './agents'

/**
 * Badges for a milestone card, most urgent first: who holds it (the whole
 * point of the page), whether it has gone quiet, and what is waiting on you.
 */
export function milestoneBadges(m: Task): BadgeProps[] {
  const out: BadgeProps[] = []
  out.push(
    m.assigned_role
      ? { label: `◆ ${m.assigned_role}`, fg: colors.indigoFg, bg: colors.indigoBg }
      : { label: 'not taken', fg: colors.slateFg, bg: colors.slateBg },
  )
  // `quiet` is written by the daemon (subtask #1032); absent means the agent
  // has been showing its work.
  if (m.quiet) out.push({ label: '🤐 quiet', fg: colors.amberDeep, bg: colors.amberBg })
  if (m.questions_awaiting_user && m.questions_awaiting_user > 0) {
    out.push({
      label: `? ${m.questions_awaiting_user} awaiting you`,
      fg: colors.amberDeep,
      bg: colors.amberBg,
    })
  } else if (m.open_questions && m.open_questions > 0) {
    out.push({ label: `${m.open_questions} open Q`, fg: colors.purpleFg, bg: colors.purpleBg })
  }
  return out
}
