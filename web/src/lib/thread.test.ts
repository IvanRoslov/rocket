import { describe, expect, it } from 'vitest'
import { questionTitle, splitReplies, VISIBLE_REPLIES } from './thread'

describe('questionTitle', () => {
  it('uses the title the daemon derived', () => {
    expect(questionTitle({ title: 'Prorate refunds?', body: 'long body' })).toBe('Prorate refunds?')
  })

  it('falls back to the first line of the body when there is no title', () => {
    expect(questionTitle({ body: 'First line\nsecond line' })).toBe('First line')
  })

  it('truncates a long fallback on a word boundary', () => {
    const title = questionTitle({ body: 'слово '.repeat(40) })
    expect(title.length).toBeLessThanOrEqual(81)
    expect(title.endsWith('…')).toBe(true)
    expect(title).not.toContain('слов…')
  })

  it('is empty for an empty body', () => {
    expect(questionTitle({ body: '' })).toBe('')
  })
})

describe('splitReplies', () => {
  const messages = [1, 2, 3, 4, 5, 6]

  it('shows the last replies only, counting what it hid', () => {
    expect(splitReplies(messages, false)).toEqual({ hidden: 3, shown: [4, 5, 6] })
  })

  it('hides nothing once expanded', () => {
    expect(splitReplies(messages, true)).toEqual({ hidden: 0, shown: messages })
  })

  it('hides nothing when the thread is short', () => {
    const short = messages.slice(0, VISIBLE_REPLIES)
    expect(splitReplies(short, false)).toEqual({ hidden: 0, shown: short })
  })
})
