// Who stormed and how the gate went (task #5019 spec §2, §2.1) — the same
// rules as the CLI's labels in internal/cli/stats.go, worded in English like
// the rest of the dashboard: the human is "you", an agent goes by its id.

import { HUMAN, participantLabel } from './participants'

/** A storm's participants in first-answer order: "you + cto"; "—" with no answers. */
export function stormWho(ids: string[]): string {
  return ids.length === 0 ? '—' : ids.map((id) => participantLabel(id)).join(' + ')
}

/** How the human received the spec gate. Without `has_gate` "no gates" and "pending, 0 changes" look alike. */
export function gateState(s: { go_at: number | null; spec_changes: number; has_gate: boolean }): string {
  if (s.go_at !== null) return s.spec_changes === 0 ? 'Go first try' : `Go after ${s.spec_changes} changes`
  if (s.has_gate) return `awaiting Go (changes: ${s.spec_changes})`
  return '—'
}

/** Unique participant ids, the human first, then the agents ascending — the weekly rows' order. */
export function orderParticipants(ids: string[]): string[] {
  const unique = [...new Set(ids)]
  const agents = unique.filter((id) => id !== HUMAN).sort()
  return unique.includes(HUMAN) ? [HUMAN, ...agents] : agents
}
