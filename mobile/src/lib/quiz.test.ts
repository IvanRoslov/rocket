import type { PendingQuizQuestion } from '../api/types'
import { buildQuizAnswers, emptySelection, toggleOption } from './quiz'

const single: PendingQuizQuestion = {
  question: 'Merge strategy?',
  multi_select: false,
  options: [{ label: 'Merge commit' }, { label: 'Squash' }],
}
const multi: PendingQuizQuestion = {
  question: 'Include in release?',
  multi_select: true,
  options: [{ label: 'Docs' }, { label: 'Migrations' }, { label: 'CLI' }],
}

describe('toggleOption', () => {
  it('single select replaces the choice', () => {
    let s = toggleOption(emptySelection(), 0, false)
    s = toggleOption(s, 1, false)
    expect(s.indices).toEqual([1])
  })
  it('multi select flips and keeps sorted order', () => {
    let s = toggleOption(emptySelection(), 2, true)
    s = toggleOption(s, 0, true)
    expect(s.indices).toEqual([0, 2])
    s = toggleOption(s, 2, true)
    expect(s.indices).toEqual([0])
  })
  it('clears free text when an option is picked', () => {
    const s = toggleOption({ indices: [], text: 'x', useText: true }, 0, false)
    expect(s.useText).toBe(false)
    expect(s.text).toBe('')
  })
})

describe('buildQuizAnswers', () => {
  it('builds indices and text answers', () => {
    const r = buildQuizAnswers(
      [single, multi],
      [
        { indices: [1], text: '', useText: false },
        { indices: [], text: 'своё', useText: true },
      ],
    )
    expect(r).toEqual({
      ok: true,
      answers: [
        { question_index: 0, option_indices: [1] },
        { question_index: 1, text: 'своё' },
      ],
    })
  })
  it('rejects unanswered questions', () => {
    const r = buildQuizAnswers([single], [emptySelection()])
    expect(r.ok).toBe(false)
  })
  it('rejects empty free text', () => {
    const r = buildQuizAnswers([single], [{ indices: [], text: '  ', useText: true }])
    expect(r.ok).toBe(false)
  })
  it('rejects multiple indices for single select', () => {
    const r = buildQuizAnswers([single], [{ indices: [0, 1], text: '', useText: false }])
    expect(r.ok).toBe(false)
  })
  it('accepts several indices for multi select', () => {
    const r = buildQuizAnswers([multi], [{ indices: [0, 2], text: '', useText: false }])
    expect(r).toEqual({ ok: true, answers: [{ question_index: 0, option_indices: [0, 2] }] })
  })
})
