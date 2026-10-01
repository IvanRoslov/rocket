// Storm questions in the inbox (task #4901, spec v2 §3.1) — the same rules as
// the web dashboard (web/src/lib/storm.ts). A storm question read on its own
// is torn out of its context, so it is answered only in the task's Brainstorm
// tab: the inbox lists no storm thread, only one row per task with storm
// questions waiting on you, and that row is one item in every count.
import type { ThreadInboxEntry } from '../api/types'
import { isBrainstorm } from './brainstorm'

export interface StormGroup {
  taskId: number
  taskTitle?: string
  /** Open storm questions on your turn. */
  count: number
  stale: boolean
  /** The oldest movement among them — the order's tie-break. */
  updatedAt: number
}

function waitingOnYou(t: ThreadInboxEntry): boolean {
  return t.status === 'open' && t.your_turn
}

/** One group per task with storm questions on your turn: stale first, then longest idle. */
export function stormGroups(threads: ThreadInboxEntry[]): StormGroup[] {
  const byTask = new Map<number, StormGroup>()
  for (const t of threads) {
    if (!isBrainstorm(t) || !waitingOnYou(t) || t.task_id == null) continue
    const g = byTask.get(t.task_id)
    if (g) {
      g.count++
      g.stale ||= t.stale ?? false
      g.updatedAt = Math.min(g.updatedAt, t.updated_at)
    } else {
      byTask.set(t.task_id, {
        taskId: t.task_id,
        taskTitle: t.task_title,
        count: 1,
        stale: t.stale ?? false,
        updatedAt: t.updated_at,
      })
    }
  }
  return [...byTask.values()].sort((a, b) => Number(b.stale) - Number(a.stale) || a.updatedAt - b.updatedAt)
}

export function stormLabel(g: StormGroup): string {
  const title = g.taskTitle ? ` «${g.taskTitle}»` : ''
  return `Storm #${g.taskId}${title}: ${g.count} ${g.count === 1 ? 'question' : 'questions'} waiting`
}

/** The task screen with its Brainstorm chip selected. */
export function stormHref(g: StormGroup): string {
  return `/task/${g.taskId}?tab=brainstorm`
}

/** The Questions tab badge: threads on your turn, each task's storm counted once. */
export function inboxCount(threads: ThreadInboxEntry[]): number {
  return threads.filter((t) => waitingOnYou(t) && !isBrainstorm(t)).length + stormGroups(threads).length
}
