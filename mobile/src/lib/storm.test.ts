import type { ThreadInboxEntry } from '../api/types'
import { inboxCount } from './storm'

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

it('inboxCount counts ordinary threads on your turn and no storm thread', () => {
  expect(
    inboxCount([
      thread({}),
      thread({}),
      thread({ your_turn: false }),
      thread({ status: 'resolved' }),
      thread({ type: 'brainstorm', task_id: 5 }),
      thread({ type: 'brainstorm', task_id: 5 }),
      thread({ type: 'brainstorm', task_id: 6 }),
    ]),
  ).toBe(2)
})
