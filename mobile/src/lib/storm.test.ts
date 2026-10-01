import type { ThreadInboxEntry } from '../api/types'
import { inboxCount, stormGroups, stormHref, stormLabel } from './storm'

let nextId = 1
function thread(over: Partial<ThreadInboxEntry>): ThreadInboxEntry {
  const id = nextId++
  return {
    local_ref: `1/Q${id}`, kind: 'task', task_id: 1, task_title: 'Metering', subject: 'task #1', id, ordinal: id,
    asked_by: 'orch', body: 'q', status: 'open', type: 'decision', participants: ['human', 'orch'],
    attention: ['human'], waiting_on: ['human'], your_turn: true, asked_at: 100, updated_at: 100,
    ...over,
  } as ThreadInboxEntry
}

describe('stormGroups', () => {
  it('groups open storm threads on your turn by task, oldest first', () => {
    const groups = stormGroups([
      thread({ type: 'brainstorm', task_id: 17, task_title: 'Billing', updated_at: 50 }),
      thread({ type: 'brainstorm', task_id: 17, task_title: 'Billing', updated_at: 70 }),
      thread({ type: 'brainstorm', task_id: 17, status: 'resolved', your_turn: false }),
      thread({ type: 'brainstorm', task_id: 17, your_turn: false }),
      thread({ type: 'brainstorm', task_id: 18, task_title: 'Ledger', updated_at: 10 }),
      thread({ type: 'decision', task_id: 19 }),
    ])
    expect(groups.map((g) => [g.taskId, g.count])).toEqual([
      [18, 1],
      [17, 2],
    ])
  })

  it('puts a stale storm first', () => {
    const groups = stormGroups([
      thread({ type: 'brainstorm', task_id: 1, updated_at: 1 }),
      thread({ type: 'brainstorm', task_id: 2, updated_at: 99, stale: true }),
    ])
    expect(groups.map((g) => g.taskId)).toEqual([2, 1])
  })
})

it('stormLabel names the task and pluralises', () => {
  expect(stormLabel({ taskId: 17, taskTitle: 'Billing', count: 1, stale: false, updatedAt: 0 })).toBe(
    'Storm #17 «Billing»: 1 question waiting',
  )
  expect(stormLabel({ taskId: 17, taskTitle: 'Billing', count: 2, stale: false, updatedAt: 0 })).toBe(
    'Storm #17 «Billing»: 2 questions waiting',
  )
})

it('stormHref opens the task on its Brainstorm chip', () => {
  expect(stormHref({ taskId: 17, count: 1, stale: false, updatedAt: 0 })).toBe('/task/17?tab=brainstorm')
})

it('inboxCount counts every task storm once', () => {
  expect(
    inboxCount([
      thread({}),
      thread({ your_turn: false }),
      thread({ type: 'brainstorm', task_id: 5 }),
      thread({ type: 'brainstorm', task_id: 5 }),
      thread({ type: 'brainstorm', task_id: 6 }),
      thread({ type: 'brainstorm', task_id: 7, status: 'resolved' }),
    ]),
  ).toBe(3)
})
