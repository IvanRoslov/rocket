import { test } from 'node:test'
import assert from 'node:assert/strict'
import { checkPrompt, collectImages, generate, PROMPT_LIMIT, DEFAULT_BASE_URL } from '../src/laozhang.ts'
import { resolveLaozhangToken } from '../src/laozhang-auth.ts'
import { parseArgs, loadDotEnv } from '../src/laozhang-cli.ts'

test('checkPrompt accepts a prompt at the limit', () => {
  assert.doesNotThrow(() => checkPrompt('a'.repeat(PROMPT_LIMIT)))
})

test('checkPrompt refuses an over-long prompt and says by how much', () => {
  assert.throws(() => checkPrompt('a'.repeat(PROMPT_LIMIT + 1)), /32001 characters; this endpoint accepts 32000/)
})

test('checkPrompt passes a full frame prompt, which the docs claimed was too long', () => {
  assert.doesNotThrow(() => checkPrompt('a'.repeat(2950)))
})

test('checkPrompt counts characters, not UTF-16 code units', () => {
  assert.doesNotThrow(() => checkPrompt('🔑'.repeat(PROMPT_LIMIT)))
})

test('checkPrompt refuses an empty prompt', () => {
  assert.throws(() => checkPrompt(''), /empty/)
})

test('collectImages reads urls and b64 payloads, skipping empty entries', () => {
  const images = collectImages({
    data: [{ url: 'https://x/1.png' }, { b64_json: 'AAA' }, { url: '' }, {}],
  })
  assert.deepEqual(images, [{ url: 'https://x/1.png' }, { b64: 'AAA' }])
})

test('collectImages returns nothing when data is missing', () => {
  assert.deepEqual(collectImages({ created: 1 }), [])
})

test('generate posts to /images/generations with a bearer token', async () => {
  let seen: { url: string; init: RequestInit } | null = null
  const fetchImpl = (async (url: string | URL, init?: RequestInit) => {
    seen = { url: String(url), init: init! }
    return new Response(JSON.stringify({ data: [{ url: 'https://x/1.png' }] }), { status: 200 })
  }) as unknown as typeof fetch

  const images = await generate({ model: 'gpt-image-1', prompt: 'a key' }, { token: 'sk-test', fetchImpl })

  assert.deepEqual(images, [{ url: 'https://x/1.png' }])
  assert.equal(seen!.url, `${DEFAULT_BASE_URL}/images/generations`)
  assert.equal((seen!.init.headers as Record<string, string>).authorization, 'Bearer sk-test')
  assert.equal(JSON.parse(seen!.init.body as string).response_format, 'url')
})

test('generate trims a trailing slash off the base url', async () => {
  let seen = ''
  const fetchImpl = (async (url: string | URL) => {
    seen = String(url)
    return new Response(JSON.stringify({ data: [{ url: 'https://x/1.png' }] }), { status: 200 })
  }) as unknown as typeof fetch

  await generate({ model: 'gpt-image-1', prompt: 'a key' }, { token: 't', baseUrl: 'https://host/v1/', fetchImpl })
  assert.equal(seen, 'https://host/v1/images/generations')
})

test('generate surfaces an http error body', async () => {
  const fetchImpl = (async () => new Response('no credit', { status: 402 })) as unknown as typeof fetch
  await assert.rejects(
    generate({ model: 'gpt-image-1', prompt: 'a key' }, { token: 't', fetchImpl }),
    /402: no credit/,
  )
})

test('generate surfaces an error carried inside a 200', async () => {
  const fetchImpl = (async () =>
    new Response(JSON.stringify({ error: { message: 'model not available' } }), { status: 200 })) as unknown as typeof fetch
  await assert.rejects(
    generate({ model: 'gpt-image-2', prompt: 'a key' }, { token: 't', fetchImpl }),
    /model not available/,
  )
})

test('generate refuses an over-long prompt before it calls out', async () => {
  let called = false
  const fetchImpl = (async () => {
    called = true
    return new Response('{}', { status: 200 })
  }) as unknown as typeof fetch

  await assert.rejects(generate({ model: 'gpt-image-1', prompt: 'a'.repeat(PROMPT_LIMIT + 1) }, { token: 't', fetchImpl }))
  assert.equal(called, false)
})

test('resolveLaozhangToken falls back to the environment while the vault reference is a TODO', () => {
  assert.deepEqual(resolveLaozhangToken({ LAOZHANG_API_KEY: 'sk-env' }), { token: 'sk-env', from: 'env' })
})

test('resolveLaozhangToken prefers the vault once the reference is filled in', () => {
  assert.deepEqual(resolveLaozhangToken({ LAOZHANG_API_KEY: 'sk-env' }, () => 'sk-vault'), {
    token: 'sk-vault',
    from: 'op',
  })
})

test('resolveLaozhangToken names the variable to set when nothing is available', () => {
  assert.throws(() => resolveLaozhangToken({}), /LAOZHANG_API_KEY/)
})

test('parseArgs defaults quality and size to auto', () => {
  const opts = parseArgs(['--prompt', 'a key'])
  assert.equal(opts.quality, 'auto')
  assert.equal(opts.size, 'auto')
  assert.equal(opts.model, 'gpt-image-1')
})

test('parseArgs takes the base url from the environment', () => {
  assert.equal(parseArgs(['--prompt', 'x'], { LAOZHANG_BASE_URL: 'https://api.laozhang.ai/v1' }).baseUrl, 'https://api.laozhang.ai/v1')
})

test('parseArgs lets --base-url beat the environment', () => {
  assert.equal(
    parseArgs(['--prompt', 'x', '--base-url', 'https://flag/v1'], { LAOZHANG_BASE_URL: 'https://env/v1' }).baseUrl,
    'https://flag/v1',
  )
})

test('parseArgs rejects an unknown quality by name', () => {
  assert.throws(() => parseArgs(['--prompt', 'x', '--quality', 'draft']), /Unknown --quality "draft"/)
})

test('parseArgs rejects a size the endpoint does not offer', () => {
  assert.throws(() => parseArgs(['--prompt', 'x', '--size', '1920x1080']), /Unknown --size/)
})

test('parseArgs rejects --n outside 1..10', () => {
  assert.throws(() => parseArgs(['--prompt', 'x', '--n', '11']), /1 to 10/)
})

test('parseArgs needs a subject alongside --style', () => {
  assert.throws(() => parseArgs(['--style', 'cover']), /--style needs --subject/)
})

test('parseArgs needs either a prompt or a style', () => {
  assert.throws(() => parseArgs(['--size', 'auto']), /Give either --style or --prompt/)
})

test('loadDotEnv reports a missing file rather than throwing', () => {
  assert.equal(loadDotEnv(() => { throw new Error('ENOENT') }), false)
})

test('loadDotEnv reports success when the file loads', () => {
  assert.equal(loadDotEnv(() => {}), true)
})

test('parseArgs carries --response-format through', () => {
  assert.equal(parseArgs(['--prompt', 'x', '--response-format', 'b64_json']).responseFormat, 'b64_json')
})

test('parseArgs rejects a response format the endpoint does not offer', () => {
  assert.throws(() => parseArgs(['--prompt', 'x', '--response-format', 'binary']), /Unknown --response-format/)
})
