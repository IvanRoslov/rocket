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

test('profileErrorText speaks Russian for the known codes, raw message otherwise', async () => {
  const { ApiError } = await import('./api')
  const { profileErrorText } = await import('./profiles')
  expect(profileErrorText(new ApiError(409, 'profile_exists', 'profile x already exists'))).toBe(
    'A profile with this name already exists',
  )
  expect(profileErrorText(new ApiError(409, 'profile_in_use', 'x is the default'))).toBe(
    'This profile is a default — pick another default first',
  )
  expect(profileErrorText(new ApiError(400, 'bad_effort', 'effort y'))).toBe(
    'This effort level is not supported for this model',
  )
  expect(profileErrorText(new ApiError(400, 'bad_request', 'name must match'))).toBe('name must match')
  expect(profileErrorText(new Error('network down'))).toBe('network down')
})
