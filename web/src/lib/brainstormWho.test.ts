// Who stormed and how the gate went (task #5019 spec §2): the labels the
// metrics screen and the storm tab share with the CLI.

import { describe, expect, it } from 'vitest'
import { gateState, orderParticipants, participantLabel, stormWho } from './brainstormWho'

describe('participantLabel', () => {
  it.each([
    ['human', 'Иван'],
    ['', 'Иван'],
    ['cto', 'cto'],
  ])('%j → %s', (id, label) => {
    expect(participantLabel(id)).toBe(label)
  })
})

describe('stormWho', () => {
  it.each<[string[], string]>([
    [[], '—'],
    [['cto'], 'cto'],
    [['human', 'cto'], 'Иван + cto'],
    [['cto', 'human'], 'cto + Иван'],
  ])('%j → %s', (ids, label) => {
    expect(stormWho(ids)).toBe(label)
  })
})

describe('gateState', () => {
  it.each<[{ go_at: number | null; spec_changes: number; has_gate: boolean }, string]>([
    [{ go_at: 100, spec_changes: 0, has_gate: true }, 'Go с 1-го раза'],
    [{ go_at: 100, spec_changes: 2, has_gate: true }, 'Go после 2 правок'],
    [{ go_at: null, spec_changes: 1, has_gate: true }, 'ждёт Go (правок: 1)'],
    [{ go_at: null, spec_changes: 0, has_gate: true }, 'ждёт Go (правок: 0)'],
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
