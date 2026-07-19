import { useQueryClient } from '@tanstack/react-query'
import { createContext, useContext, useEffect, useState } from 'react'
import { useServers } from '../servers/ServerContext'
import { connectSse } from './sse'

/**
 * Maps a daemon event type (e.g. "session.state_changed") to the query-key
 * segments (after the baseUrl segment) that must be refetched.
 */
export function parseEventType(type: string): string[] {
  const domain = type.split('.')[0]
  switch (domain) {
    case 'session':
      return ['sessions', 'system', 'task', 'projects']
    case 'task':
      return ['tasks', 'task', 'projects']
    case 'message':
      return ['messages', 'system']
    case 'pr':
      return ['tasks', 'task', 'sessions']
    case 'orchestrator':
      return ['sessions']
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
  const { baseUrl } = useServers()
  const qc = useQueryClient()
  const [connected, setConnected] = useState(false)

  useEffect(() => {
    if (!baseUrl) return
    const conn = connectSse(`${baseUrl}/v1/events/stream`, {
      onOpen: () => setConnected(true),
      onError: () => setConnected(false),
      onEvent: (type) => {
        const segments = parseEventType(type)
        if (segments.length === 0) return
        qc.invalidateQueries({ predicate: (q) => keyMatches(q.queryKey, segments) })
      },
    })

    return () => {
      setConnected(false)
      conn.close()
    }
  }, [baseUrl, qc])

  return { connected }
}
