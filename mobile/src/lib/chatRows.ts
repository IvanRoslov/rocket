import type { ChatEntry, Message } from '../api/types'
import { classifyUserEntry } from './chatDisplay'

/** A message we sent that may not be in the transcript yet. */
export interface OutgoingMsg {
  msgId: number
  body: string
  /** Unix seconds when we sent it — used to match the transcript entry. */
  sentAt: number
}

export type ChatRow =
  | { kind: 'entry'; key: string; entry: ChatEntry }
  | { kind: 'outgoing'; key: string; body: string; status: string; reason?: string }

/** Tool calls and harness-injected user entries are hidden by default. */
export function isNoise(e: ChatEntry): boolean {
  return e.role === 'tool' || (e.role === 'user' && classifyUserEntry(e.text).kind === 'system')
}

/**
 * Builds the chat list: transcript entries plus optimistic bubbles for
 * messages the transcript hasn't picked up yet.
 *
 * Each optimistic bubble consumes at most one matching transcript entry, so
 * sending the same text twice keeps both bubbles — a plain "is this text in
 * the transcript" check would hide the second one forever. Only entries at
 * or after the send time can match, so an identical message from earlier in
 * the conversation doesn't swallow a fresh one.
 */
export function buildChatRows(params: {
  entries: ChatEntry[]
  outgoing: OutgoingMsg[]
  queueMessages?: Message[]
  showNoise: boolean
}): ChatRow[] {
  const { entries, outgoing, queueMessages, showNoise } = params

  const rows: ChatRow[] = entries
    .map((entry, i) => ({ entry, i }))
    .filter(({ entry }) => showNoise || !isNoise(entry))
    .map(({ entry, i }) => ({ kind: 'entry' as const, key: `e${i}`, entry }))

  // Transcript timestamps come from the agent's log and can lag a second
  // behind our own clock, so allow a small window when matching.
  const SKEW = 5
  const unclaimed = entries.filter((e) => e.role === 'user')
  const claimed = new Set<number>()

  for (const o of outgoing) {
    const idx = unclaimed.findIndex(
      (e, i) => !claimed.has(i) && e.text === o.body && (e.ts === 0 || e.ts >= o.sentAt - SKEW),
    )
    if (idx !== -1) {
      claimed.add(idx)
      continue // landed in the transcript — the real entry is already rendered
    }
    const m = queueMessages?.find((qm) => qm.id === o.msgId)
    rows.push({
      kind: 'outgoing',
      key: `o${o.msgId}`,
      body: o.body,
      status: m?.status ?? 'queued',
      reason: m?.reason,
    })
  }

  return rows
}
