// Storm questions in the inbox (task #4901, spec v2 §3.1).
//
// A storm question read on its own is torn out of its context — the Problem,
// the earlier answers, the spec taking shape — so the brainstorm happens only
// in the task's Brainstorm tab. The inbox lists no storm thread; it shows one
// row per task with storm questions waiting on the human, and that row is one
// item in every count, however many questions it stands for.

import type { ThreadInboxEntry } from './types'

export interface StormGroup {
  taskId: number
  projectId?: string
  taskTitle?: string
  /** Open storm questions on the human's turn. */
  count: number
  stale: boolean
  /** The oldest movement among them — the queue's tie-break. */
  updatedAt: number
}

export function isStorm(t: { type?: string }): boolean {
  return t.type === 'brainstorm'
}

function waitingOnYou(t: ThreadInboxEntry): boolean {
  return t.status === 'open' && t.your_turn
}

/** One group per task with storm questions on your turn: stale first, then longest idle. */
export function stormGroups(threads: ThreadInboxEntry[]): StormGroup[] {
  const byTask = new Map<number, StormGroup>()
  for (const t of threads) {
    if (!isStorm(t) || !waitingOnYou(t) || t.task_id === undefined) continue
    const g = byTask.get(t.task_id)
    if (g) {
      g.count++
      g.stale ||= t.stale ?? false
      g.updatedAt = Math.min(g.updatedAt, t.updated_at)
    } else {
      byTask.set(t.task_id, {
        taskId: t.task_id,
        projectId: t.project_id,
        taskTitle: t.task_title,
        count: 1,
        stale: t.stale ?? false,
        updatedAt: t.updated_at,
      })
    }
  }
  return [...byTask.values()].sort(
    (a, b) => Number(b.stale) - Number(a.stale) || a.updatedAt - b.updatedAt,
  )
}

export function stormLabel(g: StormGroup): string {
  const title = g.taskTitle ? ` «${g.taskTitle}»` : ''
  return `Storm #${g.taskId}${title}: ${g.count} ${g.count === 1 ? 'question' : 'questions'} waiting`
}

/** The task screen, landed on its Brainstorm tab. */
export function stormHref(g: StormGroup): string {
  return `/p/${g.projectId ?? ''}/tasks/${g.taskId}?tab=brainstorm`
}

/** What the inbox badge shows: threads on your turn, each task's storm counted once. */
export function inboxCount(threads: ThreadInboxEntry[]): number {
  const plain = threads.filter((t) => waitingOnYou(t) && !isStorm(t)).length
  return plain + stormGroups(threads).length
}
