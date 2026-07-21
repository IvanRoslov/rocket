// Covers usePasteImage: image paste uploads via POST /v1/attachments (mocked
// in ../mocks/handlers, same msw scaffolding as queries.test.tsx) and inserts
// markdown at the cursor; a non-image paste is left untouched.

import { fireEvent, render, screen, waitFor } from '@testing-library/react'
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
