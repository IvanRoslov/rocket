import { brainstormCounts, brainstormHref, filterBrainstorms, isMidWorkSpecChange } from './brainstorms'
import type { BrainstormEntry } from './types'

function entry(over: Partial<BrainstormEntry>): BrainstormEntry {
  return {
    task_id: 1,
    project_id: 'p',
    task_title: 'T',
    task_status: 'brainstorm',
    questions_waiting: 0,
    questions_stale: false,
    pending_gate: null,
    waiting_since: 0,
    ...over,
  }
}
const gate = { id: 1, spec_version: 1, requested_at: 0 }

const list = [
  entry({ task_id: 1, questions_waiting: 2 }),
  entry({ task_id: 2, pending_gate: gate }),
  entry({ task_id: 3, questions_waiting: 1, pending_gate: gate }),
]

test('filterBrainstorms', () => {
  expect(filterBrainstorms(list, 'all').map((e) => e.task_id)).toEqual([1, 2, 3])
  expect(filterBrainstorms(list, 'answer').map((e) => e.task_id)).toEqual([1, 3])
  expect(filterBrainstorms(list, 'go').map((e) => e.task_id)).toEqual([2, 3])
})

test('brainstormCounts: a task with both lands in both', () => {
  expect(brainstormCounts(list)).toEqual({ all: 3, answer: 2, go: 2 })
  expect(brainstormCounts([])).toEqual({ all: 0, answer: 0, go: 0 })
})

test('isMidWorkSpecChange needs a gate and a working status', () => {
  expect(isMidWorkSpecChange(entry({ pending_gate: gate, task_status: 'in_progress' }))).toBe(true)
  expect(isMidWorkSpecChange(entry({ pending_gate: gate, task_status: 'blocked' }))).toBe(true)
  expect(isMidWorkSpecChange(entry({ pending_gate: gate, task_status: 'brainstorm' }))).toBe(false)
  expect(isMidWorkSpecChange(entry({ pending_gate: null, task_status: 'in_progress' }))).toBe(false)
})

test('brainstormHref opens the Brainstorm tab, milestones their own route', () => {
  expect(brainstormHref(entry({ task_id: 17, project_id: 'pay' }))).toBe('/p/pay/tasks/17?tab=brainstorm')
  expect(brainstormHref(entry({ task_id: 9, project_id: undefined }))).toBe('/milestones/9')
})
