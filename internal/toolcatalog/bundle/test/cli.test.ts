import { test } from 'node:test'
import assert from 'node:assert/strict'
import { parseArgs, main } from '../src/cli.ts'

test('parses a styled image request', () => {
  const got = parseArgs(['image', '--style', 'cover', '--subject', 'a coral key', '--sky', 'storm', '--model', 'x/y'])
  assert.deepEqual(got, {
    kind: 'image',
    model: 'x/y',
    style: 'cover',
    subject: 'a coral key',
    sky: 'storm',
    prompt: undefined,
    image: undefined,
    out: 'out',
    input: {},
    timeoutMs: undefined,
    help: false,
  })
})

test('parses a raw video request with an input override and a reference', () => {
  const got = parseArgs([
    'video',
    '--model', 'bytedance/seedance-2.5',
    '--prompt', 'a key turning',
    '--input', '{"duration":10}',
    '--image', './ref.png',
    '--timeout', '900',
  ])
  assert.equal(got.kind, 'video')
  assert.equal(got.prompt, 'a key turning')
  assert.deepEqual(got.input, { duration: 10 })
  assert.equal(got.image, './ref.png')
  assert.equal(got.timeoutMs, 900_000)
})

test('rejects a command that is neither image nor video', () => {
  assert.throws(() => parseArgs(['audio', '--model', 'x/y']), /image or video/)
})

test('rejects a request with no model', () => {
  assert.throws(() => parseArgs(['image', '--prompt', 'a']), /--model/)
})

test('rejects a request with neither a style nor a prompt', () => {
  assert.throws(() => parseArgs(['image', '--model', 'x/y']), /--style or --prompt/)
})

test('rejects malformed --input JSON by name', () => {
  assert.throws(() => parseArgs(['image', '--model', 'x/y', '--prompt', 'a', '--input', '{oops']), /--input/)
})

test('rejects an unknown --style, naming the accepted values', () => {
  assert.throws(
    () => parseArgs(['image', '--model', 'x/y', '--style', 'bogus', '--subject', 'a']),
    /Unknown --style "bogus".*cover.*spot.*portrait/,
  )
})

test('rejects an unknown --sky, naming the accepted values', () => {
  assert.throws(
    () =>
      parseArgs(['image', '--model', 'x/y', '--style', 'cover', '--subject', 'a', '--sky', 'nonsense']),
    /Unknown --sky "nonsense".*twilight.*midday.*storm.*teal.*golden/,
  )
})

test('rejects a non-numeric --timeout', () => {
  assert.throws(
    () => parseArgs(['image', '--model', 'x/y', '--prompt', 'a', '--timeout', 'abc']),
    /--timeout/,
  )
})

test('rejects a non-positive --timeout', () => {
  assert.throws(
    () => parseArgs(['image', '--model', 'x/y', '--prompt', 'a', '--timeout', '0']),
    /--timeout/,
  )
  assert.throws(
    () => parseArgs(['image', '--model', 'x/y', '--prompt', 'a', '--timeout', '-5']),
    /--timeout/,
  )
})

test('a prediction with no downloadable output fails loudly, naming the prediction id', async () => {
  const originalFetch = globalThis.fetch
  const originalToken = process.env.REPLICATE_API_TOKEN
  const originalErrorLog = console.error
  process.env.REPLICATE_API_TOKEN = 'test-token'
  const errors: string[] = []
  console.error = (msg?: unknown) => {
    errors.push(String(msg))
  }
  globalThis.fetch = (async () =>
    new Response(JSON.stringify({ id: 'pred-zero-output', status: 'succeeded', output: { url: 'not-a-string' } }), {
      status: 200,
    })) as unknown as typeof fetch

  try {
    const code = await main(['image', '--model', 'x/y', '--prompt', 'a coral key'])
    assert.equal(code, 1)
    assert.ok(errors.some((line) => line.includes('pred-zero-output')))
    assert.ok(errors.some((line) => line.includes('no downloadable output')))
  } finally {
    globalThis.fetch = originalFetch
    console.error = originalErrorLog
    if (originalToken === undefined) delete process.env.REPLICATE_API_TOKEN
    else process.env.REPLICATE_API_TOKEN = originalToken
  }
})

test('both styled image entry points accept House spot', async () => {
  const { parseArgs: parseLaozhangArgs } = await import('../src/laozhang-cli.ts')
  const args = ['--style', 'house-spot', '--subject', 'a blue mailbox on a dark surface']
  assert.equal(parseArgs(['image', '--model', 'x/y', ...args]).style, 'house-spot')
  assert.equal(parseLaozhangArgs(args).style, 'house-spot')
})
