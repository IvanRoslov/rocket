import type { ThreadInboxEntry } from '../api/types'
import { otherOpen, questionPreview, questionTitle, threadSource, waitingOnYou } from './questions'

const t = (p: Partial<ThreadInboxEntry>): ThreadInboxEntry => ({
  local_ref: '1/Q1', kind: 'task', task_id: 1, subject: 'task #1', id: 1, ordinal: 1, asked_by: 'orch',
  body: 'b', status: 'open', type: 'decision', participants: [], attention: [], waiting_on: [],
  your_turn: false, asked_at: 0, updated_at: 0, ...p,
})

describe('waitingOnYou', () => {
  it('keeps open threads on you, stale first, then oldest movement first', () => {
    const list = [
      t({ id: 1, your_turn: true, updated_at: 30 }),
      t({ id: 2, your_turn: true, updated_at: 10 }),
      t({ id: 3, your_turn: true, updated_at: 50, stale: true }),
      t({ id: 4, your_turn: false }),
      t({ id: 5, your_turn: true, status: 'resolved' }),
    ]
    expect(waitingOnYou(list).map((x) => x.id)).toEqual([3, 2, 1])
  })
})

describe('otherOpen', () => {
  it('is open threads not on you, fyi excluded, newest movement first', () => {
    const list = [t({ id: 1, updated_at: 5 }), t({ id: 2, updated_at: 9 }), t({ id: 3, your_turn: true }), t({ id: 4, type: 'fyi' })]
    expect(otherOpen(list).map((x) => x.id)).toEqual([2, 1])
  })
})

describe('threadSource', () => {
  it('labels a task thread with its number and title and links to the task', () => {
    expect(threadSource(t({ task_id: 1023, task_title: 'Ship it' }))).toEqual({ label: '#1023 · Ship it', href: '/task/1023' })
  })
  it('labels a role thread with the agent and links to the agent', () => {
    expect(threadSource(t({ kind: 'role', task_id: undefined, role_id: 'cto' }))).toEqual({ label: 'agent cto', href: '/agent/cto' })
  })
})

describe('questionPreview', () => {
  it('prefers the brief, flattened to one plain-text line', () => {
    const brief = '**Проблема:** база тормозит.\n\n**Варианты:**\n- `pg`\n- sqlite\n\n**Рекомендация:** pg'
    expect(questionPreview({ brief, body: 'long wall' })).toBe('Проблема: база тормозит. Варианты: pg sqlite Рекомендация: pg')
  })
  it('falls back to the body unchanged when the brief is empty, blank or absent', () => {
    expect(questionPreview({ brief: '', body: 'raw **body**' })).toBe('raw **body**')
    expect(questionPreview({ brief: '  \n', body: 'raw' })).toBe('raw')
    expect(questionPreview({ body: 'raw' })).toBe('raw')
  })
})

describe('questionTitle', () => {
  it('prefers the title, falls back to the first non-empty body line cut at 80 chars on a word', () => {
    expect(questionTitle({ title: ' Pick a DB ', body: 'x' })).toBe('Pick a DB')
    expect(questionTitle({ body: '\n\nfirst line\nsecond' })).toBe('first line')
    expect(questionTitle({ body: 'word '.repeat(30) }).endsWith('…')).toBe(true)
  })
})
