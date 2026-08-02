import type { Agent } from '../api/types'
import { agentBadges, awaitingUser, inboxStatusBadge } from './agents'

const base: Agent = {
  id: 'sre',
  description: 'platform on-call',
  project: 'platform',
  dir: '/home/agents/sre',
  command: 'claude',
  enabled: true,
  session_alive: false,
  unread: 0,
  open_questions: 0,
  awaiting_user: 0,
  created_at: 1,
  updated_at: 1,
}

describe('awaitingUser', () => {
  it('sums awaiting_user across agents', () => {
    expect(awaitingUser([{ ...base, awaiting_user: 2 }, { ...base, id: 'triage', awaiting_user: 1 }])).toBe(3)
  })

  it('is 0 for no agents', () => {
    expect(awaitingUser([])).toBe(0)
  })
})

describe('agentBadges', () => {
  it('flags a live session first', () => {
    expect(agentBadges({ ...base, session_alive: true })[0].label).toBe('● live')
  })

  it('flags a disabled agent', () => {
    expect(agentBadges({ ...base, enabled: false }).map((b) => b.label)).toContain('disabled')
  })

  it('shows unread and awaiting-answer counts', () => {
    const labels = agentBadges({ ...base, unread: 3, awaiting_user: 1, open_questions: 2 }).map((b) => b.label)
    expect(labels).toContain('3 unread')
    expect(labels).toContain('? 1 awaiting you')
  })

  it('shows open questions without an awaiting badge when the agent owes the answer', () => {
    const labels = agentBadges({ ...base, open_questions: 2 }).map((b) => b.label)
    expect(labels).toContain('2 open Q')
    expect(labels.some((l) => l.includes('awaiting'))).toBe(false)
  })

  it('says idle when there is nothing to show', () => {
    expect(agentBadges(base).map((b) => b.label)).toEqual(['idle'])
  })
})

describe('inboxStatusBadge', () => {
  it('separates unread from read', () => {
    expect(inboxStatusBadge('unread').bg).not.toBe(inboxStatusBadge('read').bg)
  })

  it('labels the status as it comes', () => {
    expect(inboxStatusBadge('unread').label).toBe('unread')
  })
})
