import { test } from 'node:test'
import assert from 'node:assert/strict'
import { REGISTRY, findModel, buildInput, defaultTimeoutMs, checkInput } from '../src/registry.ts'

test('carries the three named video models', () => {
  const slugs = REGISTRY.map((e) => e.slug).sort()
  assert.deepEqual(slugs, ['alibaba/wan-3', 'bytedance/seedance-2.0', 'bytedance/seedance-2.5'])
  assert.ok(REGISTRY.every((e) => e.kind === 'video'))
})

test('finds a model by slug and misses unknown ones', () => {
  assert.equal(findModel('alibaba/wan-3')?.kind, 'video')
  assert.equal(findModel('nobody/nothing'), undefined)
})

test('overrides beat the preset and the prompt', () => {
  const entry = { slug: 'x/y', kind: 'video' as const, preset: { duration: 5, fps: 24 } }
  const got = buildInput(entry, 'a coral key', { duration: 10, prompt: 'explicit' })
  assert.deepEqual(got, { duration: 10, fps: 24, prompt: 'explicit' })
})

test('works with no registry entry at all', () => {
  assert.deepEqual(buildInput(undefined, 'a coral key', {}), { prompt: 'a coral key' })
})

test('video gets the long timeout, images the short one', () => {
  assert.equal(defaultTimeoutMs('video'), 600_000)
  assert.equal(defaultTimeoutMs('image'), 120_000)
})

test('every registry entry lists the input fields verified against the API', () => {
  for (const entry of REGISTRY) {
    assert.ok(entry.fields.includes('prompt'), `${entry.slug} should accept prompt`)
    assert.ok(entry.fields.includes('image'), `${entry.slug} should accept image`)
    assert.ok(entry.fields.includes('duration'), `${entry.slug} should accept duration`)
  }
})

test('the fields differ per model where the API says they differ', () => {
  assert.ok(findModel('bytedance/seedance-2.5')!.fields.includes('watermark'))
  assert.ok(!findModel('alibaba/wan-3')!.fields.includes('watermark'))
  assert.ok(findModel('alibaba/wan-3')!.fields.includes('negative_prompt'))
  assert.ok(!findModel('bytedance/seedance-2.0')!.fields.includes('negative_prompt'))
})

test('checkInput names an input key the model does not declare', () => {
  const entry = findModel('alibaba/wan-3')!
  assert.throws(() => checkInput(entry, { negtive_prompt: 'x' }), (err: unknown) => {
    const message = (err as Error).message
    assert.match(message, /alibaba\/wan-3 does not accept negtive_prompt/)
    assert.match(message, /It accepts:.*negative_prompt/)
    return true
  })
})

test('checkInput accepts declared keys and stays silent for unknown models', () => {
  const entry = findModel('bytedance/seedance-2.5')!
  assert.doesNotThrow(() => checkInput(entry, { duration: 10, last_frame_image: 'https://x/a.png' }))
  assert.doesNotThrow(() => checkInput(undefined, { anything_at_all: 1 }))
})
