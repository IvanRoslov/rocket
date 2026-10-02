// Who stormed and how the gate went (task #5019 spec §2): the labels the
// metrics screen and the storm tab share with the CLI.

import { describe, expect, it } from 'vitest'
import { gateState, orderParticipants, stormWho } from './brainstormWho'

describe('stormWho', () => {
  it.each<[string[], string]>([
    [[], '—'],
    [['cto'], 'cto'],
    [['human', 'cto'], 'you + cto'],
    [['cto', 'human'], 'cto + you'],
  ])('%j → %s', (ids, label) => {
    expect(stormWho(ids)).toBe(label)
  })
})

describe('gateState', () => {
  it.each<[{ go_at: number | null; spec_changes: number; has_gate: boolean }, string]>([
    [{ go_at: 100, spec_changes: 0, has_gate: true }, 'Go first try'],
    [{ go_at: 100, spec_changes: 2, has_gate: true }, 'Go after 2 changes'],
    [{ go_at: null, spec_changes: 1, has_gate: true }, 'awaiting Go (changes: 1)'],
    [{ go_at: null, spec_changes: 0, has_gate: true }, 'awaiting Go (changes: 0)'],
    [{ go_at: null, spec_changes: 0, has_gate: false }, '—'],
  ])('%j → %s', (s, label) => {
    expect(gateState(s)).toBe(label)
  })
})

describe('orderParticipants', () => {
  it('puts the human first, then the agents ascending, each once', () => {
    expect(orderParticipants(['cto', 'architect', 'human', 'cto'])).toEqual(['human', 'architect', 'cto'])
  })
})
