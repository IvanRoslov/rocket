import { entryKey, mergeEntries } from './chat'
import type { ChatEntry } from './types'

const e = (ts: number, role: ChatEntry['role'], text: string, tool_name?: string): ChatEntry => ({
  ts,
  role,
  text,
  ...(tool_name ? { tool_name } : {}),
})

describe('mergeEntries', () => {
  it('appends new entries', () => {
    const existing = [e(1, 'user', 'hi')]
    const merged = mergeEntries(existing, [e(2, 'assistant', 'hello')])
    expect(merged).toHaveLength(2)
    expect(merged[1].text).toBe('hello')
  })

  it('skips the overlap the daemon re-delivered after a cursor fallback', () => {
    const a = e(1, 'user', 'hi')
    const b = e(2, 'assistant', 'hello')
    const merged = mergeEntries([a, b], [a, b, e(3, 'assistant', 'more')])
    expect(merged.map((x) => x.text)).toEqual(['hi', 'hello', 'more'])
  })

  it('returns the same array when the batch adds nothing after our tail', () => {
    const existing = [e(1, 'user', 'hi'), e(2, 'assistant', 'yo')]
    expect(mergeEntries(existing, existing.slice())).toBe(existing)
    expect(mergeEntries(existing, [])).toBe(existing)
  })

  it('keeps identical messages sent in the same second', () => {
    // Two "ок" with the same ts are separate entries, not a duplicate —
    // content-based dedupe used to silently drop the second one. The batch
    // does not repeat our tail, so both are appended.
    const existing = [
      e(1, 'user', 'a'),
      e(2, 'assistant', 'b'),
      e(3, 'user', 'c'),
      e(4, 'assistant', 'd'),
      e(5, 'assistant', 'e'),
    ]
    const merged = mergeEntries(existing, [e(10, 'user', 'ок'), e(10, 'user', 'ок')])
    expect(merged).toHaveLength(7)
    expect(merged.slice(5).map((x) => x.text)).toEqual(['ок', 'ок'])
  })

  it('distinguishes tool entries by tool_name', () => {
    const merged = mergeEntries([e(5, 'tool', '{}', 'Bash')], [e(5, 'tool', '{}', 'Edit')])
    expect(merged).toHaveLength(2)
  })
})

describe('entryKey', () => {
  it('is stable for identical entries', () => {
    expect(entryKey(e(1, 'user', 'x'))).toBe(entryKey(e(1, 'user', 'x')))
  })
})
