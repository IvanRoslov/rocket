import { test } from 'node:test'
import assert from 'node:assert/strict'
import { run } from '../src/replicate.ts'

const ok = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'content-type': 'application/json' } })

const base = { token: 't', timeoutMs: 60_000, sleep: async () => {}, log: () => {} }

test('returns immediately when the create call already succeeded', async () => {
  const calls: string[] = []
  const fetchImpl = (async (url: string) => {
    calls.push(String(url))
    return ok({ id: 'p1', status: 'succeeded', output: ['https://x/1.png'] })
  }) as unknown as typeof fetch

  const got = await run('bytedance/seedance-2.5', { prompt: 'a' }, { ...base, fetchImpl })

  assert.equal(got.status, 'succeeded')
  assert.deepEqual(calls, ['https://api.replicate.com/v1/models/bytedance/seedance-2.5/predictions'])
})

test('polls until the prediction reaches a terminal status', async () => {
  const statuses = ['starting', 'processing', 'succeeded']
  let i = 0
  const fetchImpl = (async () => ok({ id: 'p2', status: statuses[i++] ?? 'succeeded', output: [] })) as unknown as typeof fetch

  const got = await run('alibaba/wan-3', {}, { ...base, fetchImpl })

  assert.equal(got.status, 'succeeded')
  assert.equal(i, 3)
})

test('throws the Replicate detail on a failed prediction', async () => {
  const fetchImpl = (async () => ok({ id: 'p3', status: 'failed', error: 'NSFW content detected' })) as unknown as typeof fetch

  await assert.rejects(
    () => run('alibaba/wan-3', {}, { ...base, fetchImpl }),
    /failed: NSFW content detected/,
  )
})

test('throws the HTTP status and body on a non-ok response', async () => {
  const fetchImpl = (async () =>
    new Response(JSON.stringify({ detail: 'Invalid version' }), { status: 422 })) as unknown as typeof fetch

  await assert.rejects(
    () => run('alibaba/wan-3', {}, { ...base, fetchImpl }),
    /422: Invalid version/,
  )
})

test('gives up once the timeout has passed', async () => {
  let clock = 0
  const fetchImpl = (async () => ok({ id: 'p4', status: 'processing' })) as unknown as typeof fetch

  await assert.rejects(
    () =>
      run('alibaba/wan-3', {}, {
        ...base,
        timeoutMs: 5_000,
        fetchImpl,
        now: () => (clock += 3_000),
      }),
    /timeout: prediction p4 still processing/,
  )
})
