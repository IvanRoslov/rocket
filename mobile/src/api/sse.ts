// Minimal SSE client over XMLHttpRequest for React Native.
//
// The daemon emits *named* events (`event: session.spawned`), and the
// off-the-shelf EventSource ports only dispatch listeners registered for
// each exact name. Event types are open-ended, so this client parses the
// stream itself and hands every event to a single wildcard callback.

export interface SseHandlers {
  onOpen: () => void
  onEvent: (type: string, data: string) => void
  onError: () => void
}

export interface SseConnection {
  close: () => void
}

/**
 * Extracts complete SSE frames (terminated by a blank line) from `buffer`
 * starting at offset `from`. Returns the events as `[type, data]` pairs and
 * the new offset past the last complete frame.
 */
export function parseSseFrames(
  buffer: string,
  from: number,
): { events: [string, string][]; parsed: number } {
  const events: [string, string][] = []
  let parsed = from
  let idx: number
  while ((idx = buffer.indexOf('\n\n', parsed)) !== -1) {
    const frame = buffer.slice(parsed, idx)
    parsed = idx + 2
    let type = 'message'
    const dataLines: string[] = []
    for (const line of frame.split('\n')) {
      if (line.startsWith('event:')) type = line.slice(6).trim()
      else if (line.startsWith('data:')) dataLines.push(line.slice(5).trim())
    }
    if (dataLines.length > 0 || type !== 'message') events.push([type, dataLines.join('\n')])
  }
  return { events, parsed }
}

/**
 * Opens `url` as an SSE stream and keeps reconnecting (delay
 * `reconnectMs`) until `close()` is called.
 */
export function connectSse(url: string, handlers: SseHandlers, reconnectMs = 4000): SseConnection {
  let xhr: XMLHttpRequest | null = null
  let timer: ReturnType<typeof setTimeout> | null = null
  let closed = false
  let parsed = 0
  let opened = false

  function scheduleReconnect() {
    if (closed || timer) return
    handlers.onError()
    timer = setTimeout(() => {
      timer = null
      open()
    }, reconnectMs)
  }

  function processBuffer(buffer: string) {
    const res = parseSseFrames(buffer, parsed)
    parsed = res.parsed
    for (const [type, data] of res.events) handlers.onEvent(type, data)
  }

  function open() {
    if (closed) return
    parsed = 0
    opened = false
    xhr = new XMLHttpRequest()
    xhr.open('GET', url)
    xhr.setRequestHeader('Accept', 'text/event-stream')
    xhr.onreadystatechange = () => {
      if (!xhr || closed) return
      if (xhr.readyState >= 2 && !opened) {
        if (xhr.status === 200) {
          opened = true
          handlers.onOpen()
        } else if (xhr.status > 0) {
          xhr.abort()
          scheduleReconnect()
          return
        }
      }
      if (xhr.readyState >= 3 && typeof xhr.responseText === 'string') {
        processBuffer(xhr.responseText)
      }
      if (xhr.readyState === 4) scheduleReconnect()
    }
    xhr.onerror = () => scheduleReconnect()
    xhr.ontimeout = () => scheduleReconnect()
    xhr.send()
  }

  open()

  return {
    close: () => {
      closed = true
      if (timer) clearTimeout(timer)
      if (xhr) {
        try {
          xhr.abort()
        } catch {
          // already dead
        }
      }
    },
  }
}
