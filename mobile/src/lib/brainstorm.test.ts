import type { Question, TaskDoc, TaskGate } from '../api/types'
import {
  OUTCOME_LABEL,
  OUTCOMES,
  chosenLabel,
  docAt,
  exitState,
  gateHistoryLabel,
  gateState,
  isBrainstorm,
  isRecommended,
  latestDoc,
  pendingGate,
  showBrainstormTab,
  stormWho,
} from './brainstorm'

const doc = (p: Partial<TaskDoc>): TaskDoc => ({
  id: 1, task_id: 1, kind: 'spec', title: 't', body: 'b', version: 1, created_at: 0, ...p,
})

const gate = (p: Partial<TaskGate>): TaskGate => ({
  id: 1, task_id: 1, spec_version: 1, plan_version: 1, status: 'pending', comment: '', decided_by: '',
  requested_by: 'orch', requested_at: 0, decided_at: null, ...p,
})

const q = (p: Partial<Question>): Question => ({
  id: 1, task_id: 1, ordinal: 1, asked_by: 'orch', body: 'b', status: 'open', participants: [],
  waiting_on: [], your_turn: true, asked_at: 0, messages: [], ...p,
})

describe('OUTCOME_LABEL', () => {
  it('names every outcome', () => {
    expect(OUTCOMES.map((o) => OUTCOME_LABEL[o])).toEqual(['Accepted', 'Corrected', 'Wrong turn'])
  })
})

describe('isBrainstorm', () => {
  it('is true only for brainstorm threads', () => {
    expect(isBrainstorm({ type: 'brainstorm' })).toBe(true)
    expect(isBrainstorm({ type: 'decision' })).toBe(false)
    expect(isBrainstorm({})).toBe(false)
  })
})

describe('isRecommended', () => {
  it('matches the 1-based recommended option', () => {
    expect(isRecommended({ recommended_option: 2 }, 1)).toBe(true)
    expect(isRecommended({ recommended_option: 2 }, 0)).toBe(false)
  })
  it('is false when there is no recommendation (old thread)', () => {
    expect(isRecommended({ recommended_option: null }, 0)).toBe(false)
    expect(isRecommended({}, 0)).toBe(false)
  })
})

describe('chosenLabel', () => {
  it('is the text of the chosen 1-based option', () => {
    expect(chosenLabel({ options: ['a', 'b'], chosen_option: 2 })).toBe('b')
  })
  it('is empty for an own-words answer or an out-of-range option', () => {
    expect(chosenLabel({ options: ['a'], chosen_option: null })).toBe('')
    expect(chosenLabel({ options: ['a'], chosen_option: 5 })).toBe('')
  })
})

describe('latestDoc', () => {
  it('picks the newest doc of a kind by id, ignoring other kinds', () => {
    const docs = [
      doc({ id: 3, kind: 'problem', version: 1 }),
      doc({ id: 7, kind: 'problem', version: 2 }),
      doc({ id: 9, kind: 'spec' }),
    ]
    expect(latestDoc(docs, 'problem')?.id).toBe(7)
  })
  it('is undefined when there is none', () => {
    expect(latestDoc([doc({ kind: 'spec' })], 'problem')).toBeUndefined()
  })
})

describe('pendingGate', () => {
  it('returns the pending gate', () => {
    expect(pendingGate([gate({ id: 2, status: 'pending' }), gate({ id: 1, status: 'changes' })])?.id).toBe(2)
  })
  it('is undefined when every gate is decided', () => {
    expect(pendingGate([gate({ status: 'go' })])).toBeUndefined()
  })
})

describe('gateHistoryLabel', () => {
  it('describes each status', () => {
    expect(gateHistoryLabel(gate({ spec_version: 1, status: 'changes', comment: 'уточни метрику' }))).toBe(
      'v1 — Needs changes: “уточни метрику”',
    )
    expect(gateHistoryLabel(gate({ spec_version: 2, status: 'go' }))).toBe('v2 — Go')
    expect(gateHistoryLabel(gate({ spec_version: 3, status: 'superseded' }))).toBe('v3 — Superseded by a newer spec')
    expect(gateHistoryLabel(gate({ spec_version: 4, status: 'pending' }))).toBe('v4 — Awaiting decision')
  })
})

describe('showBrainstormTab', () => {
  it('shows while the task is in brainstorm', () => {
    expect(showBrainstormTab({ status: 'brainstorm' }, [], [])).toBe(true)
  })
  it('shows after the storm when the task remembered its skill', () => {
    expect(showBrainstormTab({ status: 'in_progress', brainstorm_skill: 'superpowers:brainstorming' }, [], [])).toBe(true)
  })
  it('shows when there are storm threads or gates', () => {
    expect(showBrainstormTab({ status: 'done' }, [q({ type: 'brainstorm' })], [])).toBe(true)
    expect(showBrainstormTab({ status: 'done' }, [], [gate({})])).toBe(true)
  })
  it('hides on an old task without any storm', () => {
    expect(showBrainstormTab({ status: 'in_progress', brainstorm_skill: '' }, [q({ type: 'decision' })], [])).toBe(false)
  })
})

describe('docAt', () => {
  const docs = [
    doc({ id: 1, kind: 'spec', version: 1, body: 'old spec', created_at: 10 }),
    doc({ id: 2, kind: 'spec', version: 2, body: 'new spec', created_at: 20 }),
    // Same version number under another title, written after the request.
    doc({ id: 6, kind: 'spec', version: 1, title: 'other', body: 'later spec', created_at: 50 }),
    doc({ id: 3, kind: 'plan', version: 1, created_at: 15 }),
  ]
  it('finds the pinned version written by request time', () => {
    expect(docAt(docs, 'spec', 1, 30)?.body).toBe('old spec')
  })
  it('is undefined for a missing version, a null pin or a doc written after the request', () => {
    expect(docAt(docs, 'spec', 9, 30)).toBeUndefined()
    expect(docAt(docs, 'plan', null, 30)).toBeUndefined()
    expect(docAt(docs, 'spec', 2, 15)).toBeUndefined()
  })
})

describe('exitState', () => {
  it('waits for a spec when there is none', () => {
    expect(exitState([], undefined)).toEqual({ kind: 'waiting_spec' })
  })
  it('waits for a gate request once a spec exists and no gate was asked', () => {
    expect(exitState([], 1)).toEqual({ kind: 'waiting_request' })
  })
  it('waits for a request when the spec is newer than the last gate', () => {
    expect(exitState([gate({ spec_version: 1, status: 'changes', comment: 'x' })], 2)).toEqual({
      kind: 'waiting_request',
    })
  })
  it('waits for a request when the last gate was superseded', () => {
    expect(exitState([gate({ spec_version: 1, status: 'superseded' })], 1)).toEqual({ kind: 'waiting_request' })
  })
  it('waits for a revised spec after Needs changes on the current spec', () => {
    expect(exitState([gate({ spec_version: 2, status: 'changes', comment: 'x' })], 2)).toEqual({
      kind: 'waiting_spec',
    })
  })
  it('offers the decision on a pending gate', () => {
    const g = gate({ id: 4, status: 'pending' })
    expect(exitState([g, gate({ id: 3, status: 'changes' })], 1)).toEqual({ kind: 'pending', gate: g })
  })
  it('reports Go when the newest gate passed', () => {
    const g = gate({ id: 5, status: 'go' })
    expect(exitState([g], 1)).toEqual({ kind: 'go', gate: g })
  })
})

describe('who stormed (task #5019)', () => {
  it.each<[string[], string]>([
    [[], '—'],
    [['cto'], 'cto'],
    [['human', 'cto'], 'you + cto'],
  ])('stormWho(%j) is %s', (ids, label) => {
    expect(stormWho(ids)).toBe(label)
  })

  it.each<[{ go_at: number | null; spec_changes: number; has_gate: boolean }, string]>([
    [{ go_at: 100, spec_changes: 0, has_gate: true }, 'Go first try'],
    [{ go_at: 100, spec_changes: 2, has_gate: true }, 'Go after 2 changes'],
    [{ go_at: null, spec_changes: 1, has_gate: true }, 'awaiting Go (changes: 1)'],
    [{ go_at: null, spec_changes: 0, has_gate: false }, '—'],
  ])('gateState(%j) is %s', (s, label) => {
    expect(gateState(s)).toBe(label)
  })
})
