import { inboxCount, isStorm, stormGroups, stormHref, stormLabel } from './storm'
import type { ThreadInboxEntry } from './types'

let nextId = 1
function thread(over: Partial<ThreadInboxEntry>): ThreadInboxEntry {
  const id = nextId++
  return {
    local_ref: `1/Q${id}`,
    kind: 'task',
    task_id: 1,
    project_id: 'p',
    task_title: 'Metering',
    subject: 'task #1',
    id,
    ordinal: id,
    asked_by: 'orch',
    body: 'q',
    status: 'open',
    type: 'decision',
    participants: ['human', 'orch'],
    attention: ['human'],
    waiting_on: ['human'],
    your_turn: true,
    asked_at: 100,
    updated_at: 100,
    ...over,
  }
}

describe('stormGroups', () => {
  test('one group per task, counting only open storm threads on your turn', () => {
    const groups = stormGroups([
      thread({ type: 'brainstorm', task_id: 17, task_title: 'Billing', project_id: 'pay', updated_at: 50 }),
      thread({ type: 'brainstorm', task_id: 17, task_title: 'Billing', project_id: 'pay', updated_at: 70 }),
      thread({ type: 'brainstorm', task_id: 17, status: 'resolved', your_turn: false }),
      thread({ type: 'brainstorm', task_id: 17, your_turn: false, attention: ['orch'] }),
      thread({ type: 'brainstorm', task_id: 18, task_title: 'Ledger', updated_at: 10 }),
      thread({ type: 'decision', task_id: 19 }),
    ])

    expect(groups.map((g) => [g.taskId, g.count])).toEqual([
      [18, 1],
      [17, 2],
    ])
    expect(groups[1]).toMatchObject({ taskId: 17, projectId: 'pay', taskTitle: 'Billing', updatedAt: 50 })
  })

  test('a stale storm leads', () => {
    const groups = stormGroups([
      thread({ type: 'brainstorm', task_id: 1, updated_at: 1 }),
      thread({ type: 'brainstorm', task_id: 2, updated_at: 99, stale: true }),
    ])
    expect(groups.map((g) => g.taskId)).toEqual([2, 1])
  })

  test('no storm on you, no groups', () => {
    expect(stormGroups([thread({}), thread({ type: 'brainstorm', status: 'resolved' })])).toEqual([])
  })
})

test('stormLabel names the task and pluralises', () => {
  expect(stormLabel({ taskId: 17, taskTitle: 'Billing', count: 1, stale: false, updatedAt: 0 })).toBe(
    'Storm #17 «Billing»: 1 question waiting',
  )
  expect(stormLabel({ taskId: 17, taskTitle: 'Billing', count: 3, stale: false, updatedAt: 0 })).toBe(
    'Storm #17 «Billing»: 3 questions waiting',
  )
})

test('stormHref opens the task on its Brainstorm tab', () => {
  expect(stormHref({ taskId: 17, projectId: 'pay', count: 1, stale: false, updatedAt: 0 })).toBe(
    '/p/pay/tasks/17?tab=brainstorm',
  )
})

test('inboxCount counts every task storm as one item', () => {
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

test('isStorm', () => {
  expect(isStorm({ type: 'brainstorm' })).toBe(true)
  expect(isStorm({ type: 'decision' })).toBe(false)
})
