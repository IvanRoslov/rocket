// Storm (brainstorm) threads. They never appear in the Questions inbox — they
// live in the Brainstorms section — so the helpers here only identify them,
// keep them out of the inbox badge, and count them per task for kanban cards.

import type { ThreadInboxEntry } from './types'

export function isStorm(t: { type?: string }): boolean {
  return t.type === 'brainstorm'
}

function waitingOnYou(t: ThreadInboxEntry): boolean {
  return t.status === 'open' && t.your_turn
}

export interface TaskStorm {
  /** Open storm questions, whoever has the turn. */
  open: number
  /** Of those, the ones on your turn. */
  onYou: number
}

/** Open storm questions per task id — what a task card subtracts from its question counts. */
export function stormsByTask(threads: ThreadInboxEntry[]): Map<number, TaskStorm> {
  const byTask = new Map<number, TaskStorm>()
  for (const t of threads) {
    if (!isStorm(t) || t.status !== 'open' || t.task_id === undefined) continue
    const s = byTask.get(t.task_id) ?? { open: 0, onYou: 0 }
    s.open++
    if (t.your_turn) s.onYou++
    byTask.set(t.task_id, s)
  }
  return byTask
}

/** What the inbox badge shows: threads on your turn. Storms live in the Brainstorms section. */
export function inboxCount(threads: ThreadInboxEntry[]): number {
  return threads.filter((t) => waitingOnYou(t) && !isStorm(t)).length
}
