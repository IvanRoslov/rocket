import type { Agent, AgentInboxEvent } from '../api/types'
import {
  agentBadges,
  awaitingUser,
  inboxKindBadge,
  inboxStatusBadge,
  inboxSummary,
  itemStateBadge,
  isRoleRun,
  subscriptionLabel,
} from './agents'

const base: Agent = {
  id: 'sre',
  project: 'platform',
  prompt_path: '/tmp/role.md',
  subscriptions: [],
  cron: '',
  agent: 'claude-code',
  enabled: true,
  inbox_queued: 0,
  items: 0,
  open_questions: 0,
  awaiting_user: 0,
  created_at: 1,
  updated_at: 1,
}

describe('awaitingUser', () => {
  it('sums awaiting_user across roles', () => {
    expect(awaitingUser([{ ...base, awaiting_user: 2 }, { ...base, id: 'triage', awaiting_user: 1 }])).toBe(3)
  })

  it('is 0 for no roles', () => {
    expect(awaitingUser([])).toBe(0)
  })
})

describe('agentBadges', () => {
  it('flags a disabled role', () => {
    expect(agentBadges({ ...base, enabled: false }).map((b) => b.label)).toContain('disabled')
  })

  it('shows queued inbox and awaiting-answer counts', () => {
    const labels = agentBadges({ ...base, inbox_queued: 3, awaiting_user: 1, open_questions: 2 }).map((b) => b.label)
    expect(labels).toContain('3 queued')
    expect(labels).toContain('? 1 awaiting you')
  })

  it('shows open questions without an awaiting badge when the role owes the answer', () => {
    const labels = agentBadges({ ...base, open_questions: 2 }).map((b) => b.label)
    expect(labels).toContain('2 open Q')
    expect(labels.some((l) => l.includes('awaiting'))).toBe(false)
  })

  it('says idle when there is nothing to show', () => {
    expect(agentBadges(base).map((b) => b.label)).toEqual(['idle'])
  })
})

describe('inboxKindBadge', () => {
  it('humanises known kinds', () => {
    expect(inboxKindBadge('issue_opened').label).toBe('issue opened')
  })

  it('passes unknown kinds through', () => {
    expect(inboxKindBadge('whatever').label).toBe('whatever')
  })
})

describe('inboxStatusBadge', () => {
  it('separates queued from done', () => {
    expect(inboxStatusBadge('queued').bg).not.toBe(inboxStatusBadge('done').bg)
  })
})

describe('itemStateBadge', () => {
  it('colours deferred differently from taken', () => {
    expect(itemStateBadge('deferred').bg).not.toBe(itemStateBadge('taken').bg)
  })

  it('labels an empty state as new', () => {
    expect(itemStateBadge('').label).toBe('new')
  })
})

describe('inboxSummary', () => {
  const ev = (payload: Record<string, unknown>, kind = 'message'): AgentInboxEvent =>
    ({ id: 1, kind, payload, status: 'queued', created_at: 1, updated_at: 1 }) as AgentInboxEvent

  it('uses the text field of a message', () => {
    expect(inboxSummary(ev({ text: 'blocked by X', from: 'orch' }))).toBe('blocked by X')
  })

  it('falls back to the issue title', () => {
    expect(inboxSummary(ev({ title: 'db is down' }, 'issue_opened'))).toBe('db is down')
  })

  it('renders the payload compactly when nothing is recognised', () => {
    expect(inboxSummary(ev({ n: 1 }, 'cron'))).toBe('{"n":1}')
  })

  it('is empty for an empty payload', () => {
    expect(inboxSummary(ev({}, 'cron'))).toBe('')
  })

  it('survives a missing payload', () => {
    expect(inboxSummary({ id: 1, kind: 'cron', status: 'queued', created_at: 1, updated_at: 1 } as AgentInboxEvent)).toBe(
      '',
    )
  })
})

describe('isRoleRun', () => {
  it('matches runs of the role itself', () => {
    expect(isRoleRun('sre-run-3', 'sre')).toBe(true)
  })

  it('does not match another role whose id shares a prefix', () => {
    expect(isRoleRun('sre-bot-run-1', 'sre')).toBe(false)
  })

  it('does not match the role id alone', () => {
    expect(isRoleRun('sre', 'sre')).toBe(false)
  })
})

describe('subscriptionLabel', () => {
  it('renders repo, labels and mention-only', () => {
    expect(subscriptionLabel({ repo: 'o/r', labels: ['bug'], mention_only: true })).toBe('o/r · bug · @mentions')
  })

  it('renders a bare repo', () => {
    expect(subscriptionLabel({ repo: 'o/r' })).toBe('o/r')
  })
})
