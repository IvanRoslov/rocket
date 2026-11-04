import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createTask, taskStatus, waitForTask, withFlags, buildContent, baseFor, readOutputUrls, HOSTS } from '../src/byteplus.ts'
import { parseArgs, resolveByteplusToken } from '../src/byteplus-cli.ts'

const ok = (body: unknown) => new Response(JSON.stringify(body), { status: 200 })

test('withFlags appends settings to the prompt in a fixed order', () => {
  assert.equal(
    withFlags('a paper plane', { duration: 5, ratio: '16:9', resolution: '720p' }),
    'a paper plane --ratio 16:9 --resolution 720p --duration 5',
  )
})

test('withFlags leaves a prompt alone when there is nothing to add', () => {
  assert.equal(withFlags('  a paper plane  ', {}), 'a paper plane')
})

test('withFlags writes booleans as the model expects them', () => {
  assert.equal(withFlags('x', { watermark: false, camerafixed: true }), 'x --camerafixed true --watermark false')
})

test('withFlags refuses a prompt that already carries flags', () => {
  assert.throws(() => withFlags('a plane --resolution 1080p', { resolution: '720p' }), /already carries --flags/)
})

test('withFlags does not mistake a double dash in prose for a flag', () => {
  assert.doesNotThrow(() => withFlags('the window — not the page -- stays still', { duration: 5 }))
})

test('buildContent puts the prompt first and images after it', () => {
  assert.deepEqual(buildContent('x', [{ url: 'https://a.png' }]), [
    { type: 'text', text: 'x' },
    { type: 'image_url', image_url: { url: 'https://a.png' } },
  ])
})

test('buildContent carries a role when one is given', () => {
  const content = buildContent('x', [{ url: 'https://b.png', role: 'last_frame' }])
  assert.deepEqual(content[1], { type: 'image_url', image_url: { url: 'https://b.png' }, role: 'last_frame' })
})

test('baseFor resolves the two named hosts', () => {
  assert.equal(baseFor('byteplus'), HOSTS.byteplus)
  assert.equal(baseFor('volcengine'), HOSTS.volcengine)
})

test('baseFor accepts a full base URL and trims its trailing slash', () => {
  assert.equal(baseFor('https://host/api/v3/'), 'https://host/api/v3')
})

test('baseFor names the alternatives when the host is unknown', () => {
  assert.throws(() => baseFor('ark'), /Unknown host "ark".*byteplus, volcengine/s)
})

test('readOutputUrls finds the video in a finished task', () => {
  assert.deepEqual(readOutputUrls({ content: { video_url: 'https://cdn/a.mp4' } }), ['https://cdn/a.mp4'])
})

test('createTask posts model and content to the task endpoint', async () => {
  let seen: { url: string; body: Record<string, unknown> } | null = null
  const fetchImpl = (async (url: string | URL, init?: RequestInit) => {
    seen = { url: String(url), body: JSON.parse(init!.body as string) }
    return ok({ id: 'cgt-1' })
  }) as unknown as typeof fetch

  const id = await createTask('seedance-x', buildContent('x'), { token: 't', fetchImpl })

  assert.equal(id, 'cgt-1')
  assert.equal(seen!.url, `${HOSTS.byteplus}/contents/generations/tasks`)
  assert.equal(seen!.body.model, 'seedance-x')
  assert.deepEqual(seen!.body.content, [{ type: 'text', text: 'x' }])
})

test('createTask honours an alternate host', async () => {
  let seen = ''
  const fetchImpl = (async (url: string | URL) => {
    seen = String(url)
    return ok({ id: 'cgt-1' })
  }) as unknown as typeof fetch
  await createTask('m', buildContent('x'), { token: 't', host: 'volcengine', fetchImpl })
  // startsWith, not a regex built from the URL: the dots in a host are regex wildcards.
  assert.ok(seen.startsWith(HOSTS.volcengine), seen)
})

test('createTask surfaces the platform error code and message', async () => {
  const fetchImpl = (async () =>
    new Response(JSON.stringify({ error: { code: 'InvalidEndpointOrModel.NotFound', message: 'does not exist' } }), { status: 404 })) as unknown as typeof fetch
  await assert.rejects(createTask('nope', buildContent('x'), { token: 't', fetchImpl }), /InvalidEndpointOrModel.NotFound: does not exist/)
})

test('createTask treats an error carried inside a 200 as a failure', async () => {
  const fetchImpl = (async () => ok({ error: { code: 'ModelNotOpen', message: 'not activated' } })) as unknown as typeof fetch
  await assert.rejects(createTask('m', buildContent('x'), { token: 't', fetchImpl }), /ModelNotOpen: not activated/)
})

test('taskStatus reads state and output', async () => {
  const fetchImpl = (async (url: string | URL) => {
    assert.equal(String(url), `${HOSTS.byteplus}/contents/generations/tasks/cgt-1`)
    return ok({ id: 'cgt-1', status: 'succeeded', content: { video_url: 'https://cdn/a.mp4' } })
  }) as unknown as typeof fetch

  const status = await taskStatus('cgt-1', { token: 't', fetchImpl })
  assert.equal(status.state, 'succeeded')
  assert.deepEqual(status.urls, ['https://cdn/a.mp4'])
})

test('waitForTask polls through queued and running to succeeded', async () => {
  const states = ['queued', 'running', 'succeeded']
  let calls = 0
  const fetchImpl = (async () =>
    ok({ status: states[Math.min(calls++, 2)], content: { video_url: 'https://cdn/a.mp4' } })) as unknown as typeof fetch

  const done = await waitForTask('cgt-1', { token: 't', fetchImpl, pollMs: 0, sleep: async () => {} })
  assert.equal(done.state, 'succeeded')
  assert.equal(calls, 3)
})

test('waitForTask stops on a cancelled task, not only a failed one', async () => {
  const fetchImpl = (async () => ok({ status: 'cancelled' })) as unknown as typeof fetch
  await assert.rejects(waitForTask('cgt-1', { token: 't', fetchImpl, pollMs: 0, sleep: async () => {} }), /cgt-1 cancelled/)
})

test('waitForTask names the task when it gives up', async () => {
  const fetchImpl = (async () => ok({ status: 'running' })) as unknown as typeof fetch
  await assert.rejects(
    waitForTask('cgt-1', { token: 't', fetchImpl, pollMs: 0, timeoutMs: -1, sleep: async () => {} }),
    /--status cgt-1/,
  )
})

test('parseArgs collects settings as flags rather than JSON', () => {
  const opts = parseArgs(['--model', 'm', '--prompt', 'x', '--ratio', '16:9', '--resolution', '480p', '--duration', '25'])
  assert.deepEqual(opts.settings.ratio, '16:9')
  assert.equal(opts.settings.resolution, '480p')
  assert.equal(opts.settings.duration, 25)
})

test('parseArgs reads booleans strictly', () => {
  assert.equal(parseArgs(['--model', 'm', '--prompt', 'x', '--watermark', 'false']).settings.watermark, false)
  assert.throws(() => parseArgs(['--model', 'm', '--prompt', 'x', '--watermark', 'no']), /--watermark must be true or false/)
})

test('parseArgs insists on a model, shows the shape of one, and says where ids come from', () => {
  assert.throws(() => parseArgs(['--prompt', 'x']), /dreamina-seedance-2-5-260628.*ModelArk console/s)
})

test('parseArgs rejects an unknown host before anything is sent', () => {
  assert.throws(() => parseArgs(['--model', 'm', '--prompt', 'x', '--host', 'ark']), /Unknown host/)
})

test('parseArgs accepts --status on its own', () => {
  assert.equal(parseArgs(['--status', 'cgt-1']).status, 'cgt-1')
})

test('parseArgs keeps poll and timeout defaults in milliseconds', () => {
  const opts = parseArgs(['--model', 'm', '--prompt', 'x'])
  assert.equal(opts.pollMs, 5_000)
  assert.equal(opts.timeoutMs, 1_800_000)
})

test('resolveByteplusToken names the variable to set', () => {
  assert.throws(() => resolveByteplusToken({}), /BYTEPLUS_API_KEY/)
  assert.equal(resolveByteplusToken({ BYTEPLUS_API_KEY: 'k' }), 'k')
})
