// Classifies a `role: "user"` chat transcript entry (docs/13-chat.md) into
// how the feed should render it. Not every "user" transcript line was
// actually typed by a human: orchestrators receive queue injects (`[from
// ...]`), synthetic hook notifications (`<task-notification>...`,
// `<system-...>`), and pointer entries for content the client already
// rendered elsewhere (`[large message]`, `[task #...]`, QM replies). Only
// the last bucket ("user") is a genuine human message and gets the
// right-aligned "own" bubble.
export type UserEntryKind = 'system' | 'from' | 'user'

/** Detects a queue inject prefix like `[from some-session]`, requiring a closing `]`. */
const FROM_PREFIX_RE = /^\[from ([^\]]+)\]\s*/

export function classifyUserEntry(text: string): UserEntryKind {
  const value = text ?? ''
  if (
    value.startsWith('<task-notification>') ||
    value.includes('[SYSTEM NOTIFICATION - NOT USER INPUT]') ||
    value.startsWith('<system-') ||
    value.startsWith('[large message]') ||
    value.startsWith('[task #')
  ) {
    return 'system'
  }
  if (FROM_PREFIX_RE.test(value)) {
    return 'from'
  }
  return 'user'
}

/** Splits a `[from <session>]` inject into the sender and the remaining body text. */
export function fromEntryParts(text: string): { session: string; body: string } | undefined {
  const match = text.match(FROM_PREFIX_RE)
  if (!match) return undefined
  return { session: match[1], body: text.slice(match[0].length) }
}

const TITLE_MAX_LEN = 60

/**
 * Heuristic short label for a collapsed system row: prefers a `<summary>`
 * tag's contents (task-notification hooks tend to carry one), otherwise
 * strips tags and takes the first ~60 characters.
 */
export function systemEntryTitle(text: string): string {
  const value = text ?? ''
  const summaryMatch = value.match(/<summary>([\s\S]*?)<\/summary>/i)
  const source = summaryMatch ? summaryMatch[1] : value.replace(/<[^>]*>/g, ' ')
  const collapsed = source.replace(/\s+/g, ' ').trim()
  return collapsed.length > TITLE_MAX_LEN
    ? `${collapsed.slice(0, TITLE_MAX_LEN).trimEnd()}…`
    : collapsed
}
