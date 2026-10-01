import { useQueryClient } from '@tanstack/react-query'
import { createContext, useContext, useEffect, useRef, useState } from 'react'
import { useServers } from '../servers/ServerContext'
import { authHeaders, notifyUnauthorized } from './client'
import { connectSse } from './sse'

/**
 * Maps a daemon event type (e.g. "session.state_changed") to the query-key
 * segments (after the baseUrl segment) that must be refetched.
 */
export function parseEventType(type: string): string[] {
  // chat_updated fires every activity tick while an agent is talking; the
  // chat screen polls its own cursor, so it must not invalidate anything.
  if (type === 'session.chat_updated') return []
  const domain = type.split('.')[0]
  switch (domain) {
    case 'session':
      return ['sessions', 'system', 'task', 'projects']
    case 'task':
      return ['tasks', 'task', 'projects', 'threads']
    case 'message':
      return ['messages', 'system']
    case 'pr':
      return ['tasks', 'task', 'sessions']
    case 'orchestrator':
      return ['sessions']
    case 'agent':
      return ['agents', 'agent', 'threads']
    case 'repo':
      return ['repos', 'system']
    case 'workspace':
      return ['system']
    default:
      return []
  }
}

/**
 * True when `queryKey` (shape `[baseUrl, segment, ...rest]`) belongs to one
 * of the invalidated segments.
 */
export function keyMatches(queryKey: readonly unknown[], segments: string[]): boolean {
  return typeof queryKey[1] === 'string' && segments.includes(queryKey[1])
}

type DaemonEventListener = (type: string, data: string) => void

const listeners = new Set<DaemonEventListener>()

/**
 * Raw daemon events for screens that react to an event itself rather than
 * to refetched data (e.g. session.quiz_answer_unconfirmed has no state to
 * refetch). Returns the unsubscribe function.
 */
export function subscribeDaemonEvents(cb: DaemonEventListener): () => void {
  listeners.add(cb)
  return () => {
    listeners.delete(cb)
  }
}

export function emitDaemonEvent(type: string, data: string): void {
  listeners.forEach((cb) => cb(type, data))
}

/** Calls `handler` whenever the stream delivers `type` for `sessionId`. */
export function useSessionEvent(sessionId: string, type: string, handler: () => void): void {
  const handlerRef = useRef(handler)
  handlerRef.current = handler
  useEffect(
    () =>
      subscribeDaemonEvents((t, data) => {
        if (t !== type) return
        let ev: { session_id?: string }
        try {
          ev = JSON.parse(data)
        } catch {
          return
        }
        if (ev?.session_id === sessionId) handlerRef.current()
      }),
    [sessionId, type],
  )
}

export const ConnectionContext = createContext<{ sse: boolean }>({ sse: false })

export function useConnection() {
  return useContext(ConnectionContext)
}

/**
 * Subscribes to `GET /v1/events/stream` on the active server and translates
 * daemon events into query invalidations. Returns the connection state so
 * screens can fall back to faster polling when the stream is down.
 */
export function useEventStream(): { connected: boolean } {
  const { baseUrl, authLost } = useServers()
  const qc = useQueryClient()
  const [connected, setConnected] = useState(false)

  useEffect(() => {
    // While auth is lost there is no valid token to stream with; re-pairing flips
    // authLost back and this effect reconnects.
    if (!baseUrl || authLost) return
    const conn = connectSse(
      `${baseUrl}/v1/events/stream`,
      {
        onOpen: () => setConnected(true),
        onError: () => setConnected(false),
        onUnauthorized: () => {
          setConnected(false)
          notifyUnauthorized(baseUrl)
        },
        onEvent: (type, data) => {
          emitDaemonEvent(type, data)
          const segments = parseEventType(type)
          if (segments.length === 0) return
          qc.invalidateQueries({ predicate: (q) => keyMatches(q.queryKey, segments) })
        },
      },
      4000,
      authHeaders(baseUrl),
    )

    return () => {
      setConnected(false)
      conn.close()
    }
  }, [baseUrl, authLost, qc])

  return { connected }
}
