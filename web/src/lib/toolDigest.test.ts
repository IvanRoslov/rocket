import { describe, expect, it } from 'vitest'
import { groupChatEntries, summarizeToolEntry, type GroupableEntry } from './toolDigest'

describe('summarizeToolEntry', () => {
  it('extracts command from valid JSON', () => {
    const digest = summarizeToolEntry('Bash', '{"command":"go test ./internal/billing/...","description":"Run billing tests"}')
    expect(digest.command).toBe('go test ./internal/billing/...')
    expect(digest.description).toBe('Run billing tests')
  })

  it('extracts command from truncated JSON missing the closing quote/brace', () => {
    const digest = summarizeToolEntry('Bash', '{"command":"go test ./internal/billing/really/long/path/that/keeps/goi…')
    expect(digest.command).toBe('go test ./internal/billing/really/long/path/that/keeps/goi…')
  })

  it('unescapes escaped quotes and newlines in the command value', () => {
    const digest = summarizeToolEntry('Bash', '{"command":"echo \\"hi\\"\\ndone"}')
    expect(digest.command).toBe('echo "hi"⏎done')
  })

  it('extracts basename and keeps the full path for file_path entries', () => {
    const digest = summarizeToolEntry('Edit', '{"file_path":"/home/dev/rocket/internal/billing/reconcile.go","old_string":"x"}')
    expect(digest.fileName).toBe('reconcile.go')
    expect(digest.filePath).toBe('/home/dev/rocket/internal/billing/reconcile.go')
  })

  it('falls back to raw text for non-JSON payloads (e.g. apply_patch)', () => {
    const raw = '*** Begin Patch\n*** Update File: foo.go\n***'
    const digest = summarizeToolEntry('apply_patch', raw)
    expect(digest.raw).toBe(raw)
    expect(digest.command).toBeUndefined()
    expect(digest.fileName).toBeUndefined()
  })

  it('returns an empty digest for empty text', () => {
    const digest = summarizeToolEntry('Bash', '')
    expect(digest).toEqual({})
  })

  it('prefers command over file_path when both are present', () => {
    const digest = summarizeToolEntry('Bash', '{"command":"ls","file_path":"/tmp/x"}')
    expect(digest.command).toBe('ls')
    expect(digest.fileName).toBeUndefined()
  })
})

interface Item {
  id: string
  tool?: string
}

function entry(id: string, isTool: boolean): GroupableEntry<Item> {
  return { kind: 'item', key: id, isTool, value: { id, tool: isTool ? id : undefined } }
}

describe('groupChatEntries', () => {
  it('groups 3+ consecutive tool entries into a single tool-group', () => {
    const items = [entry('u1', false), entry('t1', true), entry('t2', true), entry('t3', true), entry('a1', false)]
    const grouped = groupChatEntries(items)
    expect(grouped).toHaveLength(3)
    expect(grouped[0]).toBe(items[0])
    expect(grouped[1]).toMatchObject({ kind: 'tool-group', items: [items[1], items[2], items[3]] })
    expect(grouped[2]).toBe(items[4])
  })

  it('breaks a group when a non-tool item sits between tool entries', () => {
    const items = [entry('t1', true), entry('t2', true), entry('u1', false), entry('t3', true), entry('t4', true)]
    const grouped = groupChatEntries(items)
    expect(grouped).toHaveLength(3)
    expect(grouped[0]).toMatchObject({ kind: 'tool-group', items: [items[0], items[1]] })
    expect(grouped[1]).toBe(items[2])
    expect(grouped[2]).toMatchObject({ kind: 'tool-group', items: [items[3], items[4]] })
  })

  it('leaves a lone tool entry ungrouped', () => {
    const items = [entry('u1', false), entry('t1', true), entry('a1', false)]
    const grouped = groupChatEntries(items)
    expect(grouped).toEqual(items)
  })

  it('returns an empty array for an empty input', () => {
    expect(groupChatEntries<Item>([])).toEqual([])
  })

  it('groups a run at the very start or end of the feed', () => {
    const items = [entry('t1', true), entry('t2', true), entry('u1', false)]
    const grouped = groupChatEntries(items)
    expect(grouped[0]).toMatchObject({ kind: 'tool-group', items: [items[0], items[1]] })
    expect(grouped[1]).toBe(items[2])
  })
})
