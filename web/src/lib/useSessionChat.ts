// Read-side data layer for the session chat page (`/chat/:sessionId`,
// docs/13-chat.md "Жизненный цикл экрана"):
//
//  1. Initial tail fetch — `GET .../chat` with no cursor, default `limit`.
//  2. SSE `session.chat_updated` — a content-less ping (no `data` field,
//     see sse.ts); on it, fetch by the last `next_cursor` and append.
//  3. A periodic fallback poll covers pings the monitor didn't publish
//     (best-effort per spec: no transcript signal that tick => no ping,
//     silently) or an SSE reconnect gap.
//
// Cursor-rollback dedup policy: an incremental response can legitimately
// re-deliver already-shown entries when the daemon falls back to a
// from-scratch transcript read (invalid/stale cursor — file rotated, or the
// adapter rejected a cursor outside its transcript dir). We detect that by
// checking whether the *first* entry of a non-empty incremental response
// already exists in what we're showing; if so the response is treated as
// authoritative for the whole feed and replaces it outright, rather than
// trying to splice at an unknown offset. This is the simplest-correct
// option the spec leaves up to the client ("дедуплицировать (или просто
// перерисовывать ленту) по своему усмотрению").

import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from './api'
import { useEventStream } from './sse'
import type { ChatEntry, ChatResponse, ChatSessionRef } from './types'

const FALLBACK_POLL_MS = 7000

export interface UseSessionChatResult {
  entries: ChatEntry[]
  session?: ChatSessionRef
  /** True until the first tail fetch resolves. */
  loading: boolean
}

function sameEntry(a: ChatEntry, b: ChatEntry): boolean {
  return a.role === b.role && a.ts === b.ts && a.text === b.text && a.tool_name === b.tool_name
}

export function useSessionChat(sessionId: string | undefined): UseSessionChatResult {
  const [entries, setEntries] = useState<ChatEntry[]>([])
  const [session, setSession] = useState<ChatSessionRef | undefined>(undefined)
  const [loading, setLoading] = useState(true)
  const entriesRef = useRef<ChatEntry[]>([])
  const cursorRef = useRef('')

  const fetchTail = useCallback(async (id: string) => {
    const res = await api.get<ChatResponse>(`/v1/sessions/${encodeURIComponent(id)}/chat`)
    entriesRef.current = res.entries
    cursorRef.current = res.next_cursor
    setEntries(res.entries)
    setSession(res.session)
    setLoading(false)
  }, [])

  const fetchIncremental = useCallback(async (id: string) => {
    const cursor = cursorRef.current
    const qs = cursor ? `?cursor=${encodeURIComponent(cursor)}` : ''
    const res = await api.get<ChatResponse>(`/v1/sessions/${encodeURIComponent(id)}/chat${qs}`)
    setSession(res.session)
    if (res.next_cursor) cursorRef.current = res.next_cursor
    if (res.entries.length === 0) return
    const rolledBack = entriesRef.current.some((e) => sameEntry(e, res.entries[0]))
    entriesRef.current = rolledBack ? res.entries : [...entriesRef.current, ...res.entries]
    setEntries(entriesRef.current)
  }, [])

  // (Re)load from scratch whenever the target session changes.
  useEffect(() => {
    entriesRef.current = []
    cursorRef.current = ''
    setEntries([])
    setSession(undefined)
    if (!sessionId) {
      setLoading(false)
      return
    }
    setLoading(true)
    fetchTail(sessionId)
  }, [sessionId, fetchTail])

  useEventStream(
    useCallback(
      (event) => {
        if (sessionId && event.type === 'session.chat_updated' && event.session_id === sessionId) {
          fetchIncremental(sessionId)
        }
      },
      [sessionId, fetchIncremental],
    ),
  )

  useEffect(() => {
    if (!sessionId) return
    const timer = setInterval(() => fetchIncremental(sessionId), FALLBACK_POLL_MS)
    return () => clearInterval(timer)
  }, [sessionId, fetchIncremental])

  return { entries, session, loading }
}
