import { test } from 'node:test'
import assert from 'node:assert/strict'
import { submit, taskStatus, waitForTask, endpointFor, checkMediaUrls, readOutputUrls, API_BASE } from '../src/reapi.ts'
import { parseArgs, resolveReapiToken, uploadToGcs } from '../src/reapi-cli.ts'

const ok = (body: unknown) => new Response(JSON.stringify(body), { status: 200 })

test('endpointFor maps each kind to its path', () => {
  assert.equal(endpointFor('image'), `${API_BASE}/images/generations`)
  assert.equal(endpointFor('video'), `${API_BASE}/videos/generations`)
  assert.equal(endpointFor('audio'), `${API_BASE}/audio/generations`)
})

test('endpointFor trims a trailing slash off an overridden base', () => {
  assert.equal(endpointFor('video', 'https://host/api/v1/'), 'https://host/api/v1/videos/generations')
})

test('checkMediaUrls refuses local paths and names the way out', () => {
  assert.throws(() => checkMediaUrls(['cards/01.png']), /public URLs for media.*kie\.ai uploader/s)
})

test('checkMediaUrls refuses a data URI', () => {
  assert.throws(() => checkMediaUrls(['data:image/png;base64,AAA']), /public URLs/)
})

test('checkMediaUrls passes https links through', () => {
  assert.doesNotThrow(() => checkMediaUrls(['https://cdn/a.png', 'http://cdn/b.png']))
})

test('readOutputUrls finds the video urls in a completed task', () => {
  assert.deepEqual(readOutputUrls({ output: { video_urls: ['https://cdn.reapi.ai/x/video.mp4'] } }), [
    'https://cdn.reapi.ai/x/video.mp4',
  ])
})

test('readOutputUrls returns nothing while output is still null', () => {
  assert.deepEqual(readOutputUrls({ output: null }), [])
})

test('submit posts to the endpoint for its kind and returns the task id', async () => {
  let seen: { url: string; body: Record<string, unknown>; auth: string } | null = null
  const fetchImpl = (async (url: string | URL, init?: RequestInit) => {
    seen = {
      url: String(url),
      body: JSON.parse(init!.body as string),
      auth: (init!.headers as Record<string, string>).authorization,
    }
    return ok({ id: 'task-1', status: 'processing' })
  }) as unknown as typeof fetch

  const id = await submit('video', { model: 'doubao-seedance-2.5-face', prompt: 'x' }, { token: 'rk_live_x', fetchImpl })

  assert.equal(id, 'task-1')
  assert.equal(seen!.url, `${API_BASE}/videos/generations`)
  assert.equal(seen!.auth, 'Bearer rk_live_x')
  assert.equal(seen!.body.model, 'doubao-seedance-2.5-face')
})

test('submit surfaces the error message from a rejected request', async () => {
  const fetchImpl = (async () =>
    new Response(JSON.stringify({ error: { code: 'invalid_model', message: 'unknown model' } }), { status: 400 })) as unknown as typeof fetch
  await assert.rejects(submit('video', { model: 'nope' }, { token: 't', fetchImpl }), /400: unknown model/)
})

test('submit complains when the response carries no task id', async () => {
  const fetchImpl = (async () => ok({ status: 'processing' })) as unknown as typeof fetch
  await assert.rejects(submit('image', {}, { token: 't', fetchImpl }), /no task id/)
})

test('taskStatus reads state and output urls', async () => {
  const fetchImpl = (async (url: string | URL) => {
    assert.equal(String(url), `${API_BASE}/tasks/task-1`)
    return ok({ id: 'task-1', status: 'completed', output: { video_urls: ['https://cdn/a.mp4'] }, error: null })
  }) as unknown as typeof fetch

  const status = await taskStatus('task-1', { token: 't', fetchImpl })
  assert.equal(status.state, 'completed')
  assert.deepEqual(status.urls, ['https://cdn/a.mp4'])
})

test('waitForTask polls until the task completes', async () => {
  const states = ['processing', 'processing', 'completed']
  let calls = 0
  const fetchImpl = (async () =>
    ok({ status: states[Math.min(calls++, 2)], output: { video_urls: ['https://cdn/a.mp4'] } })) as unknown as typeof fetch

  const done = await waitForTask('task-1', { token: 't', fetchImpl, pollMs: 0, sleep: async () => {} })
  assert.equal(done.state, 'completed')
  assert.equal(calls, 3)
})

test('waitForTask reports the failure and that credits come back', async () => {
  const fetchImpl = (async () =>
    ok({ status: 'failed', output: null, error: { code: 'moderation', message: 'blocked' } })) as unknown as typeof fetch
  await assert.rejects(
    waitForTask('task-1', { token: 't', fetchImpl, pollMs: 0, sleep: async () => {} }),
    /moderation: blocked.*refunded automatically/s,
  )
})

test('waitForTask names the task when it gives up, so it can be resumed', async () => {
  const fetchImpl = (async () => ok({ status: 'processing' })) as unknown as typeof fetch
  await assert.rejects(
    waitForTask('task-1', { token: 't', fetchImpl, pollMs: 0, timeoutMs: -1, sleep: async () => {} }),
    /--status task-1/,
  )
})

test('waitForTask defaults to a 2s poll, inside the published rate limit', async () => {
  const waits: number[] = []
  let calls = 0
  const fetchImpl = (async () => ok({ status: calls++ === 0 ? 'processing' : 'completed', output: { video_urls: ['https://cdn/a.mp4'] } })) as unknown as typeof fetch
  await waitForTask('task-1', { token: 't', fetchImpl, sleep: async (ms) => { waits.push(ms) } })
  assert.deepEqual(waits, [2_000])
})

test('parseArgs takes the kind as a leading word and defaults to video', () => {
  assert.equal(parseArgs(['image', '--model', 'm', '--prompt', 'x']).kind, 'image')
  assert.equal(parseArgs(['--model', 'm', '--prompt', 'x']).kind, 'video')
})

test('parseArgs rejects a kind the API has no endpoint for', () => {
  assert.throws(() => parseArgs(['music', '--model', 'm']), /Unknown command "music"/)
})

test('parseArgs insists on a model and points at where to find the wire id', () => {
  assert.throws(() => parseArgs(['video', '--prompt', 'x']), /llms\.txt/)
})

test('parseArgs accepts a prompt carried inside --input', () => {
  assert.doesNotThrow(() => parseArgs(['video', '--model', 'm', '--input', '{"prompt":"x"}']))
})

test('parseArgs needs a prompt from somewhere', () => {
  assert.throws(() => parseArgs(['video', '--model', 'm']), /Give --prompt/)
})

test('parseArgs splits a comma-separated image list', () => {
  assert.deepEqual(parseArgs(['video', '--model', 'm', '--prompt', 'x', '--image', 'https://a, https://b']).images, [
    'https://a',
    'https://b',
  ])
})

test('parseArgs accepts --status on its own', () => {
  assert.equal(parseArgs(['--status', 'task-1']).status, 'task-1')
})

test('parseArgs rejects --input that is not JSON', () => {
  assert.throws(() => parseArgs(['video', '--model', 'm', '--input', '{oops']), /not valid JSON/)
})

test('resolveReapiToken names the variable and where a key comes from', () => {
  assert.throws(() => resolveReapiToken({}), /REAPI_API_KEY.*settings\/apikeys/s)
  assert.equal(resolveReapiToken({ REAPI_API_KEY: 'rk_live_x' }), 'rk_live_x')
})

test('parseArgs reads --host as gcs, kie (true) or off', () => {
  const host = (v?: string) => parseArgs(['image', '--model', 'm', '--prompt', 'x', ...(v ? ['--host', v] : [])]).host
  assert.equal(host(), false)
  assert.equal(host('gcs'), 'gcs')
  assert.equal(host('true'), 'kie')
  assert.throws(() => host('s3'), /--host must be gcs/)
})

test('uploadToGcs copies to the refs bucket under a random name and returns its public URL', () => {
  const calls: string[][] = []
  const url = uploadToGcs('/tmp/Ref.PNG', ((_cmd: string, args: string[]) => (calls.push(args), Buffer.from(''))) as never)
  assert.match(url, /^https:\/\/storage\.googleapis\.com\/lepsto-marketing-gen-refs\/[0-9a-f]{24}\.png$/)
  assert.deepEqual(calls[0]!.slice(0, 4), ['storage', 'cp', '--quiet', '/tmp/Ref.PNG'])
  assert.equal(calls[0]![4], url.replace('https://storage.googleapis.com/', 'gs://'))
})
