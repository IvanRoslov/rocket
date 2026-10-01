import { ApiError } from '../api/client'
import type { PendingQuiz } from '../api/types'
import {
  isPermissionQuiz,
  permissionErrorText,
  resolvedPermissionLine,
  splitPermissionQuestion,
} from './permission'

const quiz = (source?: string): PendingQuiz => ({
  asked_at: 1,
  source,
  questions: [{ question: 'Q?', multi_select: false, options: [] }],
})

describe('isPermissionQuiz', () => {
  it('is true only for source "permission"', () => {
    expect(isPermissionQuiz(undefined)).toBe(false)
    expect(isPermissionQuiz(quiz())).toBe(false)
    expect(isPermissionQuiz(quiz('hook'))).toBe(false)
    expect(isPermissionQuiz(quiz('permission'))).toBe(true)
  })
})

describe('splitPermissionQuestion', () => {
  it('splits the title from the context at the first blank line', () => {
    expect(splitPermissionQuestion('Do you want to run this?\n\nBash(ls ~)\n  list home')).toEqual({
      title: 'Do you want to run this?',
      context: 'Bash(ls ~)\n  list home',
    })
  })

  it('treats a question without a blank line as a bare title', () => {
    expect(splitPermissionQuestion('Do you want to proceed?')).toEqual({
      title: 'Do you want to proceed?',
      context: '',
    })
  })
})

describe('resolvedPermissionLine', () => {
  it('names the chosen option for a chat answer', () => {
    expect(resolvedPermissionLine({ title: 'Edit settings.json?', answer_label: 'Yes', answered_via: 'chat' })).toBe(
      'Разрешение: Edit settings.json? → Yes',
    )
  })

  it('says the answer came from the terminal', () => {
    expect(resolvedPermissionLine({ title: 'Edit settings.json?', answered_via: 'terminal' })).toBe(
      'Разрешение: Edit settings.json? → отвечено в терминале',
    )
  })

  it('falls back to a neutral word when a chat answer has no label', () => {
    expect(resolvedPermissionLine({ title: 'T?', answered_via: 'chat' })).toBe('Разрешение: T? → отвечено')
  })
})

describe('permissionErrorText', () => {
  it('explains a changed dialog', () => {
    expect(permissionErrorText(new ApiError('prompt_changed', 'prompt changed', 409))).toBe(
      'Диалог изменился, обновляю…',
    )
  })

  it('passes other errors through', () => {
    expect(permissionErrorText(new ApiError('no_pending_quiz', 'no pending quiz', 409))).toBe('no pending quiz')
    expect(permissionErrorText(new Error('Network request failed'))).toBe('Network request failed')
  })
})
