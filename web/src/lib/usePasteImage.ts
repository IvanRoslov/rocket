// Paste-to-upload for textareas: on image paste, uploads the clipboard
// image to POST /v1/attachments and inserts `![screenshot](url)` markdown
// at the cursor. While the upload runs a placeholder sits in the text; on
// failure the placeholder is removed and `error` set (cleared on the next
// paste). Text pastes pass through untouched.
//
// Each paste gets its own placeholder token (`![uploading-<n>…]()`, `n` from
// a module-level counter) so two concurrent pastes in the same field don't
// cross-talk: `.replace()` targets the exact token for the upload that
// resolved/rejected, never the leftmost placeholder in the text.

import { useCallback, useState, type ClipboardEvent, type Dispatch, type SetStateAction } from 'react'
import { api } from './api'

let placeholderCounter = 0

function nextPlaceholder(): string {
  placeholderCounter += 1
  return `![uploading-${placeholderCounter}…]()`
}

export function usePasteImage(setBody: Dispatch<SetStateAction<string>>): {
  onPaste: (e: ClipboardEvent<HTMLTextAreaElement>) => void
  error?: string
  /** True while at least one paste's upload is still in flight. */
  uploading: boolean
} {
  const [error, setError] = useState<string>()
  const [inFlight, setInFlight] = useState(0)

  const onPaste = useCallback(
    (e: ClipboardEvent<HTMLTextAreaElement>) => {
      const item = Array.from(e.clipboardData.items).find((i) => i.type.startsWith('image/'))
      if (!item) return
      const file = item.getAsFile()
      if (!file) return
      e.preventDefault()

      const placeholder = nextPlaceholder()
      const start = e.currentTarget.selectionStart ?? e.currentTarget.value.length
      const end = e.currentTarget.selectionEnd ?? start
      setError(undefined)
      setBody((prev) => prev.slice(0, start) + placeholder + prev.slice(end))
      setInFlight((n) => n + 1)

      api.upload(file).then(
        ({ url }) => {
          setBody((prev) => prev.replace(placeholder, `![screenshot](${url})`))
          setInFlight((n) => n - 1)
        },
        (err: Error) => {
          setBody((prev) => prev.replace(placeholder, ''))
          setError(err.message)
          setInFlight((n) => n - 1)
        },
      )
    },
    [setBody],
  )

  return { onPaste, error, uploading: inFlight > 0 }
}
