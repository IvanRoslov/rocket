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

  it('drops entries re-delivered after a cursor fallback', () => {
    const a = e(1, 'user', 'hi')
    const b = e(2, 'assistant', 'hello')
    const merged = mergeEntries([a, b], [b, e(3, 'assistant', 'more')])
    expect(merged.map((x) => x.text)).toEqual(['hi', 'hello', 'more'])
  })

  it('returns the same array when nothing is new', () => {
    const existing = [e(1, 'user', 'hi')]
    expect(mergeEntries(existing, [e(1, 'user', 'hi')])).toBe(existing)
    expect(mergeEntries(existing, [])).toBe(existing)
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
