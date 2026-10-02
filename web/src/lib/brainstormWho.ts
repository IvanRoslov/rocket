// Who stormed and how the gate went (task #5019 spec §2) — the TS twins of the
// CLI's labels in internal/cli/stats.go, with the same exact wording.

import { HUMAN, isHuman } from './participants'

/** "Иван" for the human (also the legacy `""`), an agent by its id, verbatim. */
export function participantLabel(id: string): string {
  return isHuman(id) ? 'Иван' : id
}

/** A storm's participants in first-answer order: "Иван + cto"; "—" with no answers. */
export function stormWho(ids: string[]): string {
  return ids.length === 0 ? '—' : ids.map(participantLabel).join(' + ')
}

/** How the human received the spec gate. Without `has_gate` "no gates" and "pending, 0 changes" look alike. */
export function gateState(s: { go_at: number | null; spec_changes: number; has_gate: boolean }): string {
  if (s.go_at !== null) return s.spec_changes === 0 ? 'Go с 1-го раза' : `Go после ${s.spec_changes} правок`
  if (s.has_gate) return `ждёт Go (правок: ${s.spec_changes})`
  return '—'
}

/** Unique participant ids, the human first, then the agents ascending — the weekly rows' order. */
export function orderParticipants(ids: string[]): string[] {
  const unique = [...new Set(ids)]
  const agents = unique.filter((id) => id !== HUMAN).sort()
  return unique.includes(HUMAN) ? [HUMAN, ...agents] : agents
}
