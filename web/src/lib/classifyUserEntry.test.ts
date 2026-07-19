import { describe, expect, it } from 'vitest'
import { classifyUserEntry, fromEntryParts, systemEntryTitle } from './classifyUserEntry'

describe('classifyUserEntry', () => {
  it('classifies task-notification hook injects as system', () => {
    expect(classifyUserEntry('<task-notification>done</task-notification>')).toBe('system')
  })

  it('classifies SYSTEM NOTIFICATION markers anywhere in the text as system', () => {
    expect(classifyUserEntry('preamble [SYSTEM NOTIFICATION - NOT USER INPUT] body')).toBe('system')
  })

  it('classifies any <system-...> tag prefix as system', () => {
    expect(classifyUserEntry('<system-reminder>careful</system-reminder>')).toBe('system')
  })

  it('classifies [large message] pointer entries as system', () => {
    expect(classifyUserEntry('[large message] stored as file, 4.2kb')).toBe('system')
  })

  it('classifies [task #N] QM reply/answer injects as system', () => {
    expect(classifyUserEntry('[task #42] answered: yes')).toBe('system')
  })

  it('classifies [rocket heartbeat] daemon injects as system', () => {
    expect(classifyUserEntry('[rocket heartbeat] Feature x status: ready')).toBe('system')
  })

  it('classifies [rocket] daemon injects as system', () => {
    expect(classifyUserEntry('[rocket] CI failing on PR #5: build error')).toBe('system')
  })

  it('classifies [from <session>] queue injects as from', () => {
    expect(classifyUserEntry('[from billing-v2-w1] status update')).toBe('from')
  })

  it('classifies plain human text as user', () => {
    expect(classifyUserEntry('почему упал тест biling_test.go?')).toBe('user')
  })

  it('treats empty text as user', () => {
    expect(classifyUserEntry('')).toBe('user')
  })

  it('does not treat an unterminated [from prefix (no closing bracket) as a from-inject', () => {
    expect(classifyUserEntry('[from billing-v2-w1 status update without a closing bracket')).toBe('user')
  })
})

describe('fromEntryParts', () => {
  it('splits the sender and remaining body out of a [from ...] inject', () => {
    expect(fromEntryParts('[from billing-v2-w1] статус готов')).toEqual({
      session: 'billing-v2-w1',
      body: 'статус готов',
    })
  })

  it('returns undefined for text with no [from ...] prefix', () => {
    expect(fromEntryParts('обычное сообщение')).toBeUndefined()
  })

  it('returns undefined for an unterminated [from prefix', () => {
    expect(fromEntryParts('[from billing-v2-w1 без закрывающей скобки')).toBeUndefined()
  })
})

describe('systemEntryTitle', () => {
  it('prefers a <summary> tag body when present', () => {
    expect(
      systemEntryTitle('<task-notification><summary>Задача #4 закрыта</summary>детали...</task-notification>'),
    ).toBe('Задача #4 закрыта')
  })

  it('falls back to the first ~60 chars of tag-stripped text', () => {
    const long = `<system-reminder>${'x'.repeat(100)}</system-reminder>`
    const title = systemEntryTitle(long)
    expect(title.endsWith('…')).toBe(true)
    expect(title.length).toBe(61) // 60 chars + ellipsis
  })

  it('handles empty text', () => {
    expect(systemEntryTitle('')).toBe('')
  })
})
