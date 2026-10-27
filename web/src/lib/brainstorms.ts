// Pure helpers for the Brainstorms section: the storms waiting on the human
// (`GET /v1/brainstorms`, already sorted by the server).

import type { BrainstormEntry } from './types'

export type BrainstormFilter = 'all' | 'answer' | 'go'

export function filterBrainstorms(list: BrainstormEntry[], f: BrainstormFilter): BrainstormEntry[] {
  if (f === 'answer') return list.filter((e) => e.questions_waiting > 0)
  if (f === 'go') return list.filter((e) => e.pending_gate !== null)
  return list
}

/** A task waiting on both an answer and a go-ahead counts in both. */
export function brainstormCounts(list: BrainstormEntry[]): { all: number; answer: number; go: number } {
  return {
    all: list.length,
    answer: filterBrainstorms(list, 'answer').length,
    go: filterBrainstorms(list, 'go').length,
  }
}

/** A spec change requested while the task is already being worked on. */
export function isMidWorkSpecChange(e: BrainstormEntry): boolean {
  return e.pending_gate !== null && (e.task_status === 'in_progress' || e.task_status === 'blocked')
}

/**
 * The task screen, landed on its Brainstorm tab. A task without a project is
 * a milestone, reached at /milestones/:id.
 */
export function brainstormHref(e: BrainstormEntry): string {
  if (!e.project_id) return `/milestones/${e.task_id}`
  return `/p/${e.project_id}/tasks/${e.task_id}?tab=brainstorm`
}
