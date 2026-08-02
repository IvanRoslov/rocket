import { HUMAN, isHuman, messageAuthorLabel, participantLabel } from './participants'

describe('isHuman', () => {
  it('accepts both the legacy empty author and the canonical "human"', () => {
    expect(isHuman('')).toBe(true)
    expect(isHuman(undefined)).toBe(true)
    expect(isHuman(HUMAN)).toBe(true)
    expect(isHuman('cto')).toBe(false)
    expect(isHuman('reply-answer-orch')).toBe(false)
  })
})

describe('participantLabel', () => {
  it('labels the human "you" for both wire spellings', () => {
    expect(participantLabel('')).toBe('you')
    expect(participantLabel(undefined)).toBe('you')
    expect(participantLabel(HUMAN)).toBe('you')
  })

  it('falls back to the raw participant id, or the display name when given one', () => {
    expect(participantLabel('cto')).toBe('cto')
    expect(participantLabel('s-orch', 'billing-orch')).toBe('billing-orch')
  })
})

describe('messageAuthorLabel', () => {
  it('uses the agent display name only while there is a single non-human participant', () => {
    expect(messageAuthorLabel('s-orch', 'billing-orch', ['human', 's-orch'])).toBe('billing-orch')
    expect(messageAuthorLabel('s-orch', 'billing-orch', undefined)).toBe('billing-orch')
    expect(messageAuthorLabel('s-orch', 'billing-orch', ['human', 's-orch', 'cto'])).toBe('s-orch')
    expect(messageAuthorLabel('cto', 'billing-orch', ['human', 's-orch', 'cto'])).toBe('cto')
  })

  it('labels the human "you" whichever wire spelling arrives', () => {
    expect(messageAuthorLabel('', 'billing-orch', ['human', 's-orch', 'cto'])).toBe('you')
    expect(messageAuthorLabel(HUMAN, 'billing-orch', ['human', 's-orch', 'cto'])).toBe('you')
  })
})
