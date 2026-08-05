import type { ThreadInboxEntry } from '../../lib/types'
import { browseGroups, matchesQuery, queueOf, statusChip } from './model'

const HOUR = 3600
const now = 1_800_000_000

function thread(over: Partial<ThreadInboxEntry> & { id: number }): ThreadInboxEntry {
  return {
    local_ref: `100${over.id}/Q1`,
    kind: 'task',
    task_id: 1000 + over.id,
    subject: `task #${1000 + over.id}`,
    ordinal: 1,
    asked_by: 'orch',
    body: 'body',
    status: 'open',
    type: 'decision',
    participants: ['human', 'orch'],
    attention: ['human'],
    waiting_on: ['human'],
    your_turn: true,
    asked_at: now - HOUR,
    updated_at: now - HOUR,
    ...over,
  }
}

describe('queueOf', () => {
  test('keeps only open threads whose turn is yours', () => {
    const rows = queueOf(
      [
        thread({ id: 1 }),
        thread({ id: 2, your_turn: false, attention: ['orch'] }),
        thread({ id: 3, status: 'resolved' }),
      ],
      new Set(),
    )

    expect(rows.map((r) => r.id)).toEqual([1])
  })

  // The queue is a work order, not a feed: what has been rotting longest goes
  // first, and anything the daemon marked stale jumps ahead of all of it.
  test('sorts stale first, then longest without movement first', () => {
    const rows = queueOf(
      [
        thread({ id: 1, updated_at: now - HOUR }),
        thread({ id: 2, updated_at: now - 5 * HOUR }),
        thread({ id: 3, updated_at: now - 2 * HOUR, stale: true }),
      ],
      new Set(),
    )

    expect(rows.map((r) => r.id)).toEqual([3, 2, 1])
  })

  test('hides threads pushed to Later — until nothing else is left', () => {
    const all = [thread({ id: 1 }), thread({ id: 2, updated_at: now - 5 * HOUR })]

    expect(queueOf(all, new Set([2])).map((r) => r.id)).toEqual([1])
    // Everything skipped: showing an empty queue would be a lie, so the full
    // list comes back rather than a false "Queue clear".
    expect(queueOf(all, new Set([1, 2])).map((r) => r.id)).toEqual([2, 1])
  })
})

describe('statusChip', () => {
  test('an open thread on you says so', () => {
    expect(statusChip(thread({ id: 1 }))).toEqual({ label: 'your turn', tone: 'turn' })
  })

  test('an open thread on somebody else names them', () => {
    const chip = statusChip(thread({ id: 1, your_turn: false, attention: ['billing-orch'] }))
    expect(chip).toEqual({ label: 'waiting on billing-orch', tone: 'waiting' })
  })

  // `attention: null` from an older daemon crashed this route once already
  // (commit f2f073e) — it must degrade, not throw.
  test('an open thread with nobody on it does not throw', () => {
    const chip = statusChip(
      thread({ id: 1, your_turn: false, attention: undefined as unknown as string[] }),
    )
    expect(chip).toEqual({ label: 'waiting on nobody', tone: 'waiting' })
  })

  // The inbox knows only HOW a thread ended, never the words it ended with.
  test('a resolved decision falls back to the resolution enum', () => {
    const chip = statusChip(thread({ id: 1, status: 'resolved', resolution: 'dismissed' }))
    expect(chip).toEqual({ label: 'closed: dismissed', tone: 'closed' })
  })

  test('a caller holding the full thread shows the real resolution, truncated', () => {
    const chip = statusChip(
      thread({ id: 1, status: 'resolved', resolution: 'answered' }),
      'B — carry the delta into the next invoice, matching v1.',
    )
    expect(chip.tone).toBe('closed')
    expect(chip.label).toBe('closed: B — carry the delta into the n…')
  })

  test('an fyi note is a note, never a closed decision', () => {
    expect(statusChip(thread({ id: 1, type: 'fyi', status: 'resolved' }))).toEqual({
      label: 'note',
      tone: 'note',
    })
  })
})

describe('matchesQuery', () => {
  const t = thread({ id: 1, local_ref: '1023/Q2', body: 'Webhook retries', task_title: 'Billing' })

  test('matches the ref, the title and the body, case-insensitively', () => {
    expect(matchesQuery(t, '1023/q2')).toBe(true)
    expect(matchesQuery(t, 'billing')).toBe(true)
    expect(matchesQuery(t, 'WEBHOOK')).toBe(true)
    expect(matchesQuery(t, 'referral')).toBe(false)
  })

  test('an empty query matches everything', () => {
    expect(matchesQuery(t, '   ')).toBe(true)
  })
})

describe('browseGroups', () => {
  const all = [
    thread({ id: 1 }),
    thread({ id: 2, your_turn: false, attention: ['orch'] }),
    thread({ id: 3, status: 'resolved', resolution: 'answered' }),
  ]

  test('"All open" shows both open groups and no history', () => {
    const groups = browseGroups(all, 'open', '')
    expect(groups.map((g) => g.label)).toEqual(['Your turn', 'Waiting on agents'])
    expect(groups[0].rows.map((r) => r.id)).toEqual([1])
    expect(groups[1].rows.map((r) => r.id)).toEqual([2])
  })

  test('"Your turn" drops the agents group', () => {
    expect(browseGroups(all, 'mine', '').map((g) => g.label)).toEqual(['Your turn'])
  })

  test('"Closed" shows history only', () => {
    const groups = browseGroups(all, 'closed', '')
    expect(groups.map((g) => g.label)).toEqual(['Closed & notes'])
    expect(groups[0].rows.map((r) => r.id)).toEqual([3])
  })

  test('"Everything" shows all three groups', () => {
    expect(browseGroups(all, 'all', '').map((g) => g.label)).toEqual([
      'Your turn',
      'Waiting on agents',
      'Closed & notes',
    ])
  })

  test('the search narrows every group and empties the list when nothing matches', () => {
    expect(browseGroups(all, 'all', 'nothing-here')).toEqual([])
  })

  test('a group with no rows is dropped rather than rendered empty', () => {
    const groups = browseGroups([thread({ id: 1 })], 'all', '')
    expect(groups.map((g) => g.label)).toEqual(['Your turn'])
  })
})
