// Covers usePasteImage: image paste uploads via POST /v1/attachments (mocked
// in ../mocks/handlers, same msw scaffolding as queries.test.tsx) and inserts
// markdown at the cursor; a non-image paste is left untouched.

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { useState } from 'react'
import { afterAll, afterEach, beforeAll, expect, test } from 'vitest'
import { handlers } from '../mocks/handlers'
import { usePasteImage } from './usePasteImage'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

function Harness() {
  const [body, setBody] = useState('before after')
  const paste = usePasteImage(setBody)
  return (
    <>
      <textarea aria-label="field" value={body} onChange={(e) => setBody(e.target.value)} onPaste={paste.onPaste} />
      <div data-testid="body">{body}</div>
      <div data-testid="uploading">{String(paste.uploading)}</div>
      {paste.error && <div role="alert">{paste.error}</div>}
    </>
  )
}

function pasteImage(el: HTMLElement) {
  const file = new File([new Uint8Array([1, 2, 3])], 'shot.png', { type: 'image/png' })
  fireEvent.paste(el, {
    clipboardData: {
      items: [{ type: 'image/png', getAsFile: () => file }],
    },
  })
}

test('pasting an image uploads it and inserts markdown at the cursor', async () => {
  render(<Harness />)
  const field = screen.getByLabelText<HTMLTextAreaElement>('field')
  field.setSelectionRange(7, 7) // between "before " and "after"
  pasteImage(field)
  await waitFor(() =>
    expect(field.value).toBe('before ![screenshot](/v1/attachments/1)after'),
  )
})

test('non-image paste is ignored', () => {
  render(<Harness />)
  const field = screen.getByLabelText<HTMLTextAreaElement>('field')
  fireEvent.paste(field, { clipboardData: { items: [] } })
  expect(field.value).toBe('before after')
})

test('uploading is true while the upload is in flight and false once it resolves', async () => {
  let release: () => void = () => {}
  server.use(
    http.post('/v1/attachments', () => {
      return new Promise((resolve) => {
        release = () => resolve(HttpResponse.json({ id: 1, url: '/v1/attachments/1' }, { status: 201 }))
      })
    }),
  )

  render(<Harness />)
  const field = screen.getByLabelText<HTMLTextAreaElement>('field')
  expect(screen.getByTestId('uploading').textContent).toBe('false')

  field.setSelectionRange(7, 7)
  pasteImage(field)
  await waitFor(() => expect(screen.getByTestId('uploading').textContent).toBe('true'))

  release()
  await waitFor(() => expect(screen.getByTestId('uploading').textContent).toBe('false'))
  expect(field.value).toBe('before ![screenshot](/v1/attachments/1)after')
})

test('two pastes before either upload resolves each get a unique placeholder and both land', async () => {
  let callCount = 0
  server.use(
    http.post('/v1/attachments', () => {
      callCount += 1
      const id = callCount
      return HttpResponse.json({ id, url: `/v1/attachments/${id}` }, { status: 201 })
    }),
  )

  render(<Harness />)
  const field = screen.getByLabelText<HTMLTextAreaElement>('field')

  field.setSelectionRange(0, 0)
  pasteImage(field) // paste #1, inserted at the start
  field.setSelectionRange(field.value.length, field.value.length)
  pasteImage(field) // paste #2, inserted at the end, before #1's upload resolves

  // Both placeholders are present with distinct tokens (no leftmost-match
  // cross-talk) immediately after both synchronous pastes, before either
  // upload has resolved.
  const placeholders = field.value.match(/uploading-\d+…/g) ?? []
  expect(placeholders).toHaveLength(2)
  expect(new Set(placeholders).size).toBe(2)

  await waitFor(() => {
    expect(field.value).toContain('/v1/attachments/1')
    expect(field.value).toContain('/v1/attachments/2')
  })
  expect(field.value).not.toContain('uploading')
})
