import { test } from 'node:test'
import assert from 'node:assert/strict'
import { uploadImage, createTask, taskStatus, waitForTask, readResultUrls, API_BASE, UPLOAD_URL } from '../src/kie.ts'
import { parseArgs, resolveKieToken } from '../src/kie-cli.ts'

const ok = (body: unknown) => new Response(JSON.stringify(body), { status: 200 })

test('uploadImage posts a data URI and returns the download link', async () => {
  let seen: { url: string; body: Record<string, unknown> } | null = null
  const fetchImpl = (async (url: string | URL, init?: RequestInit) => {
    seen = { url: String(url), body: JSON.parse(init!.body as string) }
    return ok({ success: true, code: 200, data: { downloadUrl: 'https://tempfile/x.png' } })
  }) as unknown as typeof fetch

  const url = await uploadImage('card.png', { token: 't', fetchImpl, read: async () => Buffer.from([1, 2, 3]) })

  assert.equal(url, 'https://tempfile/x.png')
  assert.equal(seen!.url, UPLOAD_URL)
  assert.equal(seen!.body.base64Data, 'data:image/png;base64,AQID')
  assert.equal(seen!.body.fileName, 'card.png')
})

test('uploadImage refuses a file type the endpoint will not take', async () => {
  await assert.rejects(
    uploadImage('card.gif', { token: 't', fetchImpl: (async () => ok({})) as unknown as typeof fetch, read: async () => Buffer.from([1]) }),
    /Unsupported image type/,
  )
})

test('uploadImage surfaces a refusal carried inside a 200', async () => {
  const fetchImpl = (async () => ok({ success: false, code: 400, msg: 'Missing required parameter: uploadPath' })) as unknown as typeof fetch
  await assert.rejects(
    uploadImage('card.png', { token: 't', fetchImpl, read: async () => Buffer.from([1]) }),
    /Missing required parameter: uploadPath/,
  )
})

test('createTask sends model and input and returns the task id', async () => {
  let seen: { url: string; body: Record<string, unknown> } | null = null
  const fetchImpl = (async (url: string | URL, init?: RequestInit) => {
    seen = { url: String(url), body: JSON.parse(init!.body as string) }
    return ok({ code: 200, msg: 'success', data: { taskId: 'abc123' } })
  }) as unknown as typeof fetch

  const id = await createTask('bytedance/seedance-2-5', { prompt: 'x', duration: 25 }, { token: 't', fetchImpl })

  assert.equal(id, 'abc123')
  assert.equal(seen!.url, `${API_BASE}/api/v1/jobs/createTask`)
  assert.equal(seen!.body.model, 'bytedance/seedance-2-5')
  assert.deepEqual(seen!.body.input, { prompt: 'x', duration: 25 })
})

test('createTask surfaces a non-200 code carried inside a 200 response', async () => {
  const fetchImpl = (async () => ok({ code: 422, msg: 'The model name you specified is not supported.' })) as unknown as typeof fetch
  await assert.rejects(createTask('nope', {}, { token: 't', fetchImpl }), /model name you specified is not supported/)
})

test('readResultUrls digs the video out of a stringified resultJson', () => {
  assert.deepEqual(
    readResultUrls({ resultJson: JSON.stringify({ resultUrls: ['https://cdn/a.mp4'] }) }),
    ['https://cdn/a.mp4'],
  )
})

test('readResultUrls finds urls nested at any depth', () => {
  assert.deepEqual(readResultUrls({ result: { output: { videos: [{ url: 'https://cdn/b.mp4' }] } } }), ['https://cdn/b.mp4'])
})

test('readResultUrls returns nothing when there is nothing to find', () => {
  assert.deepEqual(readResultUrls({ state: 'generating' }), [])
})

test('taskStatus reports state and urls', async () => {
  const fetchImpl = (async (url: string | URL) => {
    assert.match(String(url), /recordInfo\?taskId=abc123$/)
    return ok({ code: 200, data: { state: 'success', resultJson: '{"resultUrls":["https://cdn/a.mp4"]}' } })
  }) as unknown as typeof fetch

  const status = await taskStatus('abc123', { token: 't', fetchImpl })
  assert.equal(status.state, 'success')
  assert.deepEqual(status.urls, ['https://cdn/a.mp4'])
})

test('waitForTask polls until the task succeeds', async () => {
  const states = ['waiting', 'generating', 'success']
  let calls = 0
  const fetchImpl = (async () =>
    ok({ code: 200, data: { state: states[Math.min(calls++, 2)], resultJson: '{"resultUrls":["https://cdn/a.mp4"]}' } })) as unknown as typeof fetch

  const done = await waitForTask('abc123', { token: 't', fetchImpl, pollMs: 0, sleep: async () => {} })
  assert.equal(done.state, 'success')
  assert.equal(calls, 3)
})

test('waitForTask reports the reason a task failed', async () => {
  const fetchImpl = (async () => ok({ code: 200, data: { state: 'fail', failMsg: 'content policy' } })) as unknown as typeof fetch
  await assert.rejects(
    waitForTask('abc123', { token: 't', fetchImpl, pollMs: 0, sleep: async () => {} }),
    /failed: content policy/,
  )
})

test('waitForTask gives up after the timeout and names the task so it can be resumed', async () => {
  const fetchImpl = (async () => ok({ code: 200, data: { state: 'generating' } })) as unknown as typeof fetch
  await assert.rejects(
    waitForTask('abc123', { token: 't', fetchImpl, pollMs: 0, timeoutMs: -1, sleep: async () => {} }),
    /--status abc123/,
  )
})

test('parseArgs defaults to seedance 2.5 without audio', () => {
  const opts = parseArgs(['--prompt', 'x'])
  assert.equal(opts.model, 'bytedance/seedance-2-5')
  assert.equal(opts.audio, false)
  assert.equal(opts.aspect, 'adaptive')
})

test('parseArgs refuses a first frame and references together', () => {
  assert.throws(
    () => parseArgs(['--prompt', 'x', '--first-frame', 'a.png', '--reference', 'b.png']),
    /mutually exclusive/,
  )
})

test('parseArgs refuses a last frame without a first', () => {
  assert.throws(() => parseArgs(['--prompt', 'x', '--last-frame', 'b.png']), /cannot travel alone/)
})

test('parseArgs holds duration to the 4..30 the endpoint accepts', () => {
  assert.throws(() => parseArgs(['--prompt', 'x', '--duration', '31']), /from 4 to 30/)
  assert.throws(() => parseArgs(['--prompt', 'x', '--duration', '3']), /from 4 to 30/)
  assert.equal(parseArgs(['--prompt', 'x', '--duration', '25']).duration, 25)
})

test('parseArgs rejects an aspect ratio the endpoint does not offer', () => {
  assert.throws(() => parseArgs(['--prompt', 'x', '--aspect', '3:2']), /Unknown --aspect "3:2"/)
})

test('parseArgs splits a comma-separated reference list', () => {
  assert.deepEqual(parseArgs(['--prompt', 'x', '--reference', 'a.png, b.png ,c.png']).references, ['a.png', 'b.png', 'c.png'])
})

test('parseArgs accepts --status on its own, for resuming', () => {
  assert.equal(parseArgs(['--status', 'abc123']).status, 'abc123')
})

test('parseArgs needs something to do', () => {
  assert.throws(() => parseArgs(['--duration', '5']), /Give --prompt, or --input <json>, or --status/)
})

test('resolveKieToken names the variable to set', () => {
  assert.throws(() => resolveKieToken({}), /KIE_API_KEY/)
  assert.equal(resolveKieToken({ KIE_API_KEY: 'k' }), 'k')
})

test('parseArgs accepts a raw --input for a non-video model', () => {
  const opts = parseArgs(['--model', 'gpt-image-2-text-to-image', '--input', '{"prompt":"x","aspect_ratio":"3:2"}'])
  assert.deepEqual(opts.input, { prompt: 'x', aspect_ratio: '3:2' })
})

test('parseArgs rejects --input that is not JSON', () => {
  assert.throws(() => parseArgs(['--input', '{oops']), /--input is not valid JSON/)
})

test('parseArgs keeps its poll and timeout defaults in milliseconds', () => {
  const opts = parseArgs(['--prompt', 'x'])
  assert.equal(opts.pollMs, 10_000)
  assert.equal(opts.timeoutMs, 1_800_000)
})

test('parseArgs reads --poll and --timeout as seconds', () => {
  const opts = parseArgs(['--prompt', 'x', '--poll', '5', '--timeout', '60'])
  assert.equal(opts.pollMs, 5_000)
  assert.equal(opts.timeoutMs, 60_000)
})
