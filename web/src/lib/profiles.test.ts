import { expect, test } from 'vitest'
import { profileSummary, sessionModelLabel } from './profiles'

test('sessionModelLabel joins the snapshot, empty for a legacy session', () => {
  expect(sessionModelLabel({})).toBe('')
  expect(sessionModelLabel({ profile: 'claude-opus', model: 'opus', effort: 'high' })).toBe('claude-opus (opus, high)')
  expect(sessionModelLabel({ profile: 'codex' })).toBe('codex')
  expect(sessionModelLabel({ model: 'opus' })).toBe('opus')
})

test('profileSummary names the default model when none is set', () => {
  expect(profileSummary({ agent: 'codex', model: '', effort: '' })).toBe('codex · default model')
  expect(profileSummary({ agent: 'claude-code', model: 'opus', effort: 'high' })).toBe('claude-code · opus · high')
})
