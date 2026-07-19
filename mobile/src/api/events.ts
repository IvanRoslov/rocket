import { useQueryClient } from '@tanstack/react-query'
import { createContext, useContext, useEffect, useState } from 'react'
import EventSource from 'react-native-sse'
import { useServers } from '../servers/ServerContext'

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
    const es = new EventSource(`${baseUrl}/v1/events/stream`, {
      pollingInterval: 4000, // reconnect delay after drop
    })

    es.addEventListener('open', () => setConnected(true))
    es.addEventListener('error', () => setConnected(false))
    es.addEventListener('message', (e) => {
      if (!e.data) return
      try {
        const ev = JSON.parse(e.data) as { type?: string }
        const segments = parseEventType(ev.type ?? '')
        if (segments.length === 0) return
        qc.invalidateQueries({ predicate: (q) => keyMatches(q.queryKey, segments) })
      } catch {
        // malformed event payload — ignore
      }
    })

    return () => {
      setConnected(false)
      es.removeAllEventListeners()
      es.close()
    }
  }, [baseUrl, qc])

  return { connected }
}
