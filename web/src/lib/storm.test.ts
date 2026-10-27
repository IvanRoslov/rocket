import { inboxCount, isStorm, stormsByTask } from './storm'
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

test('inboxCount ignores storm threads', () => {
  expect(
    inboxCount([
      thread({}),
      thread({ your_turn: false }),
      thread({ type: 'brainstorm', task_id: 5 }),
      thread({ type: 'brainstorm', task_id: 5 }),
      thread({ type: 'brainstorm', task_id: 6 }),
    ]),
  ).toBe(1)
})

test('isStorm', () => {
  expect(isStorm({ type: 'brainstorm' })).toBe(true)
  expect(isStorm({ type: 'decision' })).toBe(false)
})

test('stormsByTask counts open storm questions per task, and those on you', () => {
  const byTask = stormsByTask([
    thread({ type: 'brainstorm', task_id: 5 }),
    thread({ type: 'brainstorm', task_id: 5, your_turn: false }),
    thread({ type: 'brainstorm', task_id: 5, status: 'resolved', your_turn: false }),
    thread({ type: 'decision', task_id: 5 }),
  ])
  expect(byTask.get(5)).toEqual({ open: 2, onYou: 1 })
  expect(byTask.get(6)).toBeUndefined()
})
