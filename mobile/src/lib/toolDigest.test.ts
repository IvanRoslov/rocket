import { summarizeToolEntry, toolLine } from './toolDigest'

describe('summarizeToolEntry', () => {
  it('pulls command and description out of full JSON', () => {
    const d = summarizeToolEntry('Bash', '{"command":"git status","description":"list changes"}')
    expect(toolLine(d)).toBe('git status — list changes')
  })
  it('survives a truncated digest', () => {
    const d = summarizeToolEntry('Bash', '{"command":"go test ./internal/…')
    expect(d.command).toBe('go test ./internal/…')
  })
  it('shows a file basename for file tools', () => {
    expect(toolLine(summarizeToolEntry('Edit', '{"file_path":"/a/b/chat.go","old_string":"x"}'))).toBe('chat.go')
  })
  it('falls back to raw text', () => {
    expect(toolLine(summarizeToolEntry('Grep', '{"pattern":"foo"}'))).toBe('{"pattern":"foo"}')
  })
})
