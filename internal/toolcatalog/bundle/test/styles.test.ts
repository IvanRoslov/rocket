import { test } from 'node:test'
import assert from 'node:assert/strict'
import { buildPrompt, SKIES } from '../src/styles.ts'

test('a cover carries the subject, the chosen sky, the bans and the ratio', () => {
  const got = buildPrompt('cover', 'a hand holding up a keyring with one key picked out in coral', 'twilight')
  assert.ok(got.startsWith('Subject: a hand holding up a keyring with one key picked out in coral.'))
  assert.ok(got.includes(SKIES.twilight))
  assert.ok(got.includes('NO text, NO letters, NO numbers, NO signage, NO watermark.'))
  assert.ok(got.endsWith('16:9.'))
})

test('a cover defaults to the twilight sky', () => {
  assert.ok(buildPrompt('cover', 'a coral key').includes(SKIES.twilight))
})

test('a spot is transparent and takes no sky', () => {
  const got = buildPrompt('spot', 'an empty mailbox')
  assert.ok(got.includes('fully transparent background'))
  assert.ok(!got.includes(SKIES.twilight))
})

test('a portrait edits a photograph rather than inventing a face', () => {
  const got = buildPrompt('portrait', 'ignored')
  assert.ok(got.startsWith('Transform this photograph'))
  assert.ok(got.endsWith('Square composition.'))
})

test('every sky mood from the brand page is present', () => {
  assert.deepEqual(Object.keys(SKIES).sort(), ['golden', 'midday', 'storm', 'teal', 'twilight'])
})

test('the House spot frame matches canonical guidance without changing universal spot', async () => {
  const { readFile } = await import('node:fs/promises')
  const doc = await readFile(new URL('./fixtures/imagery.md', import.meta.url), 'utf8')
  const section = doc.split('## 2b. House spot — transparent marketing illustration')[1]!.split('\n## ')[0]!
  const frame = section.split('\n').find(line => line.startsWith('> Subject:'))!
  const subject = 'a cobalt box of folders for a dark navy surface'
  assert.equal(buildPrompt('house-spot', subject), frame.slice(2).replace('`{subject}`', subject))
  assert.equal(buildPrompt('house-spot', subject, 'storm'), buildPrompt('house-spot', subject))
  assert.ok(buildPrompt('spot', subject).includes('No white, no pale fills, no dark navy.'))
})
