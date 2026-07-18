// SSE hook for the daemon's live event stream (`GET /v1/events/stream`).
//
// The daemon writes one `event: <type>` per message (docs/03-daemon-api.md
// «События»), so a single generic `EventSource.onmessage` handler can't see
// them — EventSource only delivers named event types to listeners added via
// `addEventListener(type, handler)`. We subscribe to every known type in
// EVENT_TYPES individually. On error we close and reopen after 2s; the
// browser resends `Last-Event-ID` automatically so the daemon can resume
// from where we left off.

import { useEffect, useRef } from 'react'
import type { RocketEvent } from './types'

export const EVENT_TYPES = [
  'session.spawned',
  'session.state_changed',
  'session.activity_changed',
  'session.killed',
  'session.restored',
  'message.queued',
  'message.delivered',
  'message.failed',
  'workspace.branch_collision',
  'workspace.cleanup',
  'reconcile.orphan_tmux',
  'task.question_asked',
  'task.question_replied',
  'task.question_resolved',
  'pr.opened',
  'pr.ci_changed',
  'pr.merged',
  'repo.clone_started',
  'repo.clone_done',
  'repo.clone_failed',
] as const

const RECONNECT_DELAY_MS = 2000

export function useEventStream(onEvent: (e: RocketEvent) => void): void {
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  useEffect(() => {
    let es: EventSource | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null
    let cancelled = false

    function connect() {
      const source = new EventSource('/v1/events/stream')
      es = source

      for (const type of EVENT_TYPES) {
        source.addEventListener(type, (ev: MessageEvent) => {
          try {
            const parsed = JSON.parse(ev.data) as RocketEvent
            onEventRef.current(parsed)
          } catch {
            // Malformed event payload; ignore rather than crash the stream.
          }
        })
      }

      source.onerror = () => {
        source.close()
        if (!cancelled) {
          reconnectTimer = setTimeout(connect, RECONNECT_DELAY_MS)
        }
      }
    }

    connect()

    return () => {
      cancelled = true
      if (reconnectTimer !== null) clearTimeout(reconnectTimer)
      es?.close()
    }
  }, [])
}
