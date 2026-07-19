// Client side of the daemon's WebSocket terminal protocol
// (docs/03-daemon-api.md): binary frames carry terminal bytes both ways,
// text frames carry control JSON ({type:"resize",cols,rows} / {type:"ping"}).

/** ws:// URL for a session's live terminal. */
export function buildWsUrl(baseUrl: string, sessionId: string, readonly = false): string {
  const ws = baseUrl.replace(/^http/, 'ws')
  return `${ws}/v1/sessions/${encodeURIComponent(sessionId)}/term${readonly ? '?readonly=true' : ''}`
}

export interface SpecialKey {
  label: string
  seq: string
}

/** Toolbar keys for the mobile keyboard, raw byte sequences. */
export const SPECIAL_KEYS: SpecialKey[] = [
  { label: 'esc', seq: '\x1b' },
  { label: 'tab', seq: '\t' },
  { label: '^C', seq: '\x03' },
  { label: '^D', seq: '\x04' },
  { label: '←', seq: '\x1b[D' },
  { label: '↑', seq: '\x1b[A' },
  { label: '↓', seq: '\x1b[B' },
  { label: '→', seq: '\x1b[C' },
  { label: '⏎', seq: '\r' },
]
