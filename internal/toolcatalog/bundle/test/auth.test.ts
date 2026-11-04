import { test } from 'node:test'
import assert from 'node:assert/strict'
import { resolveToken, OP_REFERENCE } from '../src/auth.ts'

test('prefers the 1Password token over the environment', () => {
  const got = resolveToken({ REPLICATE_API_TOKEN: 'from-env' }, () => 'from-op')
  assert.deepEqual(got, { token: 'from-op', from: 'op' })
})

test('falls back to the environment when 1Password yields nothing', () => {
  const got = resolveToken({ REPLICATE_API_TOKEN: 'from-env' }, () => null)
  assert.deepEqual(got, { token: 'from-env', from: 'env' })
})

test('names both sources when neither has the token', () => {
  assert.throws(
    () => resolveToken({}, () => null),
    /REPLICATE_API_TOKEN/,
  )
})

test('the 1Password reference names a real vault and item', () => {
  assert.equal(OP_REFERENCE, 'op://Lessly Platform/Replicate/password')
})
