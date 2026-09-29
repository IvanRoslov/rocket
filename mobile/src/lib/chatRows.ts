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
  /** Two or more adjacent tool calls, collapsed into one expandable row. */
  | { kind: 'tools'; key: string; entries: ChatEntry[] }
  | { kind: 'outgoing'; key: string; body: string; status: string; reason?: string }

/** Tool calls and harness-injected user entries are hidden by default. */
export function isNoise(e: ChatEntry): boolean {
  return e.role === 'tool' || (e.role === 'user' && classifyUserEntry(e.text).kind === 'system')
}

const isQuizAsk = (e: ChatEntry) => e.role === 'tool' && e.tool_name === 'AskUserQuestion' && e.quiz !== undefined

/**
 * Builds the chat list: transcript entries plus optimistic bubbles for
 * messages the transcript hasn't picked up yet.
 *
 * Each optimistic bubble consumes at most one matching transcript entry, so
 * sending the same text twice keeps both bubbles — a plain "is this text in
 * the transcript" check would hide the second one forever. Only entries at
 * or after the send time can match, so an identical message from earlier in
 * the conversation doesn't swallow a fresh one.
 *
 * A large send never reaches the transcript as its text but as a
 * `[large message]` pointer (docs/13-chat.md). When no entry matches the
 * body, the first unclaimed pointer at or after the send time claims the
 * send and is rendered in its place with the text we sent.
 *
 * An AskUserQuestion round is two entries (the tool ask, then the
 * `quiz_answer`); when the answer follows, only the answer is shown.
 *
 * Adjacent tool entries (visible only with `showNoise`) collapse into one
 * `tools` row; a lone tool call stays an ordinary `entry` row.
 */
export function buildChatRows(params: {
  entries: ChatEntry[]
  outgoing: OutgoingMsg[]
  queueMessages?: Message[]
  showNoise: boolean
}): ChatRow[] {
  const { entries, outgoing, queueMessages, showNoise } = params

  // Transcript timestamps come from the agent's log and can lag a second
  // behind our own clock, so allow a small window when matching.
  const SKEW = 5
  const claimed = new Set<number>()
  const replaced = new Map<number, string>()
  const pendingOut: OutgoingMsg[] = []

  for (const o of outgoing) {
    const fresh = (e: ChatEntry, i: number) =>
      !claimed.has(i) && e.role === 'user' && (e.ts === 0 || e.ts >= o.sentAt - SKEW)
    let idx = entries.findIndex((e, i) => fresh(e, i) && e.text === o.body)
    if (idx === -1) {
      idx = entries.findIndex((e, i) => fresh(e, i) && e.text.startsWith('[large message]'))
      if (idx !== -1) replaced.set(idx, o.body)
    }
    if (idx !== -1) claimed.add(idx)
    else pendingOut.push(o)
  }

  const rows: ChatRow[] = []
  let toolRun: { i: number; e: ChatEntry }[] = []
  const flushTools = () => {
    if (toolRun.length === 1) rows.push({ kind: 'entry', key: `e${toolRun[0].i}`, entry: toolRun[0].e })
    else if (toolRun.length > 1) rows.push({ kind: 'tools', key: `t${toolRun[0].i}`, entries: toolRun.map((x) => x.e) })
    toolRun = []
  }

  entries.forEach((e, i) => {
    if (isQuizAsk(e) && entries[i + 1]?.role === 'quiz_answer') return
    const text = replaced.get(i)
    const entry = text !== undefined ? { ...e, text } : e
    if (text === undefined && !showNoise && isNoise(entry)) return
    if (entry.role === 'tool') {
      toolRun.push({ i, e: entry })
      return
    }
    flushTools()
    rows.push({ kind: 'entry', key: `e${i}`, entry })
  })
  flushTools()

  for (const o of pendingOut) {
    const m = queueMessages?.find((qm) => qm.id === o.msgId)
    rows.push({ kind: 'outgoing', key: `o${o.msgId}`, body: o.body, status: m?.status ?? 'queued', reason: m?.reason })
  }
  return rows
}
