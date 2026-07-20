// SSE hook for the daemon's live event stream (`GET /v1/events/stream`).
//
// The daemon writes one `event: <type>` per message (docs/03-daemon-api.md
// «События»), so a single generic `EventSource.onmessage` handler can't see
// them — EventSource only delivers named event types to listeners added via
// `addEventListener(type, handler)`. We subscribe to every known type in
// EVENT_TYPES individually. On error we close and reopen after 2s; the
// browser resends `Last-Event-ID` automatically so the daemon can resume
// from where we left off.
//
// The underlying EventSource is a module-level SINGLETON shared by every
// useEventStream() subscriber in the tab. Browsers cap cleartext HTTP/1.1
// at ~6 connections per host ACROSS ALL TABS; when each mounted component
// opened its own stream, a couple of dashboard tabs exhausted the pool and
// fresh page loads hung waiting for a free slot (observed live: alternating
// instant/stuck reloads). One connection per tab keeps the pool breathing
// even without the HTTP/2 (tls_port) listener.

import { useEffect, useRef } from 'react'
import type { RocketEvent } from './types'

export const EVENT_TYPES = [
  'session.spawned',
  'session.state_changed',
  'session.activity_changed',
  'session.killed',
  'session.restored',
  // Pure ping — carries no `data` (docs/13-chat.md); consumers must re-fetch
  // the chat feed by cursor rather than expect a payload here.
  'session.chat_updated',
  // Quiz (AskUserQuestion) lifecycle pings — also content-less, no `data`
  // (docs/13-chat.md «Квизы»): re-fetch the session/chat to see
  // `pending_quiz` appear/disappear. `quiz_answer_unconfirmed` doesn't
  // clear `pending_quiz` — it just means the 60s injection ack timed out.
  'session.quiz_asked',
  'session.quiz_resolved',
  'session.quiz_answer_unconfirmed',
  'message.queued',
  'message.delivered',
  'message.failed',
  'workspace.branch_collision',
  'workspace.cleanup',
  'reconcile.orphan_tmux',
  'task.created',
  'task.status_changed',
  'task.worker_spawned',
  'task.question_asked',
  'task.question_replied',
  'task.question_resolved',
  // Orchestrator disputed a final answer — the thread is open again.
  'task.question_reopened',
  'orchestrator.heartbeat_sent',
  'pr.opened',
  'pr.ci_changed',
  'pr.merged',
  'repo.clone_started',
  'repo.clone_done',
  'repo.clone_failed',
] as const

const RECONNECT_DELAY_MS = 2000

type Subscriber = (e: RocketEvent) => void

const subscribers = new Set<Subscriber>()
let sharedSource: EventSource | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null

function dispatch(ev: MessageEvent): void {
  let parsed: RocketEvent
  try {
    parsed = JSON.parse(ev.data) as RocketEvent
  } catch {
    // Malformed event payload; ignore rather than crash the stream.
    return
  }
  for (const fn of subscribers) fn(parsed)
}

function connectShared(): void {
  const source = new EventSource('/v1/events/stream')
  sharedSource = source

  for (const type of EVENT_TYPES) {
    source.addEventListener(type, dispatch)
  }

  source.onerror = () => {
    source.close()
    sharedSource = null
    // Reconnect only while someone is still listening; the last
    // unsubscribe cancels the timer.
    if (subscribers.size > 0 && reconnectTimer === null) {
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null
        if (subscribers.size > 0) connectShared()
      }, RECONNECT_DELAY_MS)
    }
  }
}

function subscribe(fn: Subscriber): () => void {
  subscribers.add(fn)
  if (sharedSource === null && reconnectTimer === null) {
    connectShared()
  }
  return () => {
    subscribers.delete(fn)
    if (subscribers.size === 0) {
      if (reconnectTimer !== null) {
        clearTimeout(reconnectTimer)
        reconnectTimer = null
      }
      sharedSource?.close()
      sharedSource = null
    }
  }
}

export function useEventStream(onEvent: (e: RocketEvent) => void): void {
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  useEffect(() => {
    const handler: Subscriber = (e) => onEventRef.current(e)
    return subscribe(handler)
  }, [])
}
