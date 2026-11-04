import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtemp, readFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { toReference, collectOutputs, outputName, saveOutputs, looksLikeMedia } from '../src/io.ts'

// saveOutputs refuses a body that is not media, so fixtures carry a real PNG signature.
const PNG_SIGNATURE = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
const png = (tail: string) => Buffer.concat([PNG_SIGNATURE, Buffer.from(tail)])

test('a URL reference passes through untouched', async () => {
  assert.equal(await toReference('https://x/a.png'), 'https://x/a.png')
})

test('a local reference becomes a data URI', async () => {
  const got = await toReference('/some/where/a.png', async () => Buffer.from('binary'))
  assert.equal(got, `data:image/png;base64,${Buffer.from('binary').toString('base64')}`)
})

test('output is normalised to a list of URLs', () => {
  assert.deepEqual(collectOutputs('https://x/1.mp4'), ['https://x/1.mp4'])
  assert.deepEqual(collectOutputs(['https://x/1.png', 'https://x/2.png']), ['https://x/1.png', 'https://x/2.png'])
  assert.deepEqual(collectOutputs(null), [])
})

test('names a file by date, label, prediction id and source extension', () => {
  const got = outputName(new Date('2026-08-30T10:00:00Z'), 'cover', 'abcdefghij', 'https://x/out.mp4?token=1')
  assert.equal(got, '2026-08-30-cover-abcdefgh.mp4')
})

test('a multi-output name carries an index suffix; a single-output name does not', () => {
  const got = outputName(new Date('2026-08-30T10:00:00Z'), 'cover', 'abcdefghij', 'https://x/out.png', 0)
  assert.equal(got, '2026-08-30-cover-abcdefgh-1.png')
  const solo = outputName(new Date('2026-08-30T10:00:00Z'), 'cover', 'abcdefghij', 'https://x/out.mp4')
  assert.equal(solo, '2026-08-30-cover-abcdefgh.mp4')
})

test('multiple outputs from one prediction get distinct filenames and a truthful sidecar', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'lepsto-io-'))
  const fetchImpl = (async (url: string) => new Response(png(`bytes-${url}`))) as unknown as typeof fetch

  const written = await saveOutputs(['https://x/a.png', 'https://x/b.png'], {
    dir,
    label: 'spot',
    id: 'abcdefghij',
    date: new Date('2026-08-30T10:00:00Z'),
    model: 'x/y',
    input: {},
    fetchImpl,
  })

  assert.equal(written.length, 2)
  assert.notEqual(written[0], written[1])
  assert.deepEqual(await readFile(written[0]!), png('bytes-https://x/a.png'))
  assert.deepEqual(await readFile(written[1]!), png('bytes-https://x/b.png'))

  const sidecar = JSON.parse(await readFile(join(dir, '2026-08-30-spot-abcdefgh.json'), 'utf8'))
  assert.deepEqual(sidecar.files, [
    '2026-08-30-spot-abcdefgh-1.png',
    '2026-08-30-spot-abcdefgh-2.png',
  ])
})

test('a data: URI in the sidecar input is replaced by a mime/byte-length placeholder', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'lepsto-io-'))
  const fetchImpl = (async () => new Response(png('bytes'))) as unknown as typeof fetch
  const dataUri = `data:image/png;base64,${Buffer.from('binary-payload').toString('base64')}`

  await saveOutputs(['https://x/out.png'], {
    dir,
    label: 'cover',
    id: 'abcdefghij',
    date: new Date('2026-08-30T10:00:00Z'),
    model: 'x/y',
    input: { image: dataUri, prompt: 'a key' },
    fetchImpl,
  })

  const sidecar = JSON.parse(await readFile(join(dir, '2026-08-30-cover-abcdefgh.json'), 'utf8'))
  assert.equal(sidecar.input.prompt, 'a key')
  assert.equal(sidecar.input.image, '<data: image/png, 15 bytes>')
  assert.ok(!JSON.stringify(sidecar).includes('binary-payload'))
})

test('writes every output plus one sidecar recording the run', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'lepsto-io-'))
  const fetchImpl = (async () => new Response(png('video-bytes'))) as unknown as typeof fetch

  const written = await saveOutputs(['https://x/out.mp4'], {
    dir,
    label: 'seedance-2.5',
    id: 'abcdefghij',
    date: new Date('2026-08-30T10:00:00Z'),
    model: 'bytedance/seedance-2.5',
    input: { prompt: 'a coral key' },
    fetchImpl,
  })

  assert.equal(written.length, 1)
  assert.deepEqual(await readFile(written[0]!), png('video-bytes'))

  const sidecar = JSON.parse(await readFile(join(dir, '2026-08-30-seedance-2.5-abcdefgh.json'), 'utf8'))
  assert.deepEqual(sidecar, {
    id: 'abcdefghij',
    model: 'bytedance/seedance-2.5',
    input: { prompt: 'a coral key' },
    outputs: ['https://x/out.mp4'],
    files: ['2026-08-30-seedance-2.5-abcdefgh.mp4'],
  })
})

test('looksLikeMedia accepts a png and rejects a JSON error page', () => {
  assert.equal(looksLikeMedia(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d])), true)
  assert.equal(looksLikeMedia(Buffer.from('{"message":"not enabled"}')), false)
})

test('saveOutputs refuses to write a non-media body and quotes it', async () => {
  const fetchImpl = (async () =>
    new Response('{"message":"The file upload/download system is not enabled."}', { status: 200 })) as unknown as typeof fetch
  await assert.rejects(
    saveOutputs(['https://host/a.png'], {
      dir: await mkdtemp(join(tmpdir(), 'io-')),
      label: 'x',
      id: 'abcdefgh',
      model: 'm',
      input: {},
      fetchImpl,
    }),
    /did not serve media: \{"message":"The file upload/,
  )
})
