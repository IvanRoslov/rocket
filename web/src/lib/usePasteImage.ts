// Paste-to-upload for textareas: on image paste, uploads the clipboard
// image to POST /v1/attachments and inserts `![screenshot](url)` markdown
// at the cursor. While the upload runs a placeholder sits in the text; on
// failure the placeholder is removed and `error` set (cleared on the next
// paste). Text pastes pass through untouched.

import { useCallback, useState, type ClipboardEvent, type Dispatch, type SetStateAction } from 'react'
import { api } from './api'

const PLACEHOLDER = '![uploading…]()'

export function usePasteImage(setBody: Dispatch<SetStateAction<string>>): {
  onPaste: (e: ClipboardEvent<HTMLTextAreaElement>) => void
  error?: string
} {
  const [error, setError] = useState<string>()

  const onPaste = useCallback(
    (e: ClipboardEvent<HTMLTextAreaElement>) => {
      const item = Array.from(e.clipboardData.items).find((i) => i.type.startsWith('image/'))
      if (!item) return
      const file = item.getAsFile()
      if (!file) return
      e.preventDefault()

      const start = e.currentTarget.selectionStart ?? e.currentTarget.value.length
      const end = e.currentTarget.selectionEnd ?? start
      setError(undefined)
      setBody((prev) => prev.slice(0, start) + PLACEHOLDER + prev.slice(end))

      api.upload(file).then(
        ({ url }) => setBody((prev) => prev.replace(PLACEHOLDER, `![screenshot](${url})`)),
        (err: Error) => {
          setBody((prev) => prev.replace(PLACEHOLDER, ''))
          setError(err.message)
        },
      )
    },
    [setBody],
  )

  return { onPaste, error }
}
