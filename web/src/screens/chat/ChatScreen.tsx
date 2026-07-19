// Dedicated full-window chat page (`/chat/:sessionId`, docs/13-chat.md): a
// mirror of the session's native agent transcript (role user/assistant/tool)
// plus a composer that sends through the existing message queue
// (`POST /v1/messages`). Deliberately OUTSIDE AppShell, like /term — opened
// in its own tab from the «💬 chat» buttons.
//
// Unlike /term (dark, tmux-attach chrome) this page is in the LIGHT
// dashboard theme — it's a chat, not a terminal — reusing MessagesTab's
// bubble styling. The header's Dot(state/activity) comes from the chat
// response's own session card (`ChatSessionRef`), per spec, while the
// display name/tmux attach command come from `useSession` (the chat
// response doesn't carry `tmux_name`) — same split TermScreen uses.

import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Markdown } from '../../components/Markdown'
import { ApiError } from '../../lib/api'
import { classifyUserEntry, fromEntryParts, systemEntryTitle } from '../../lib/classifyUserEntry'
import { timeAgo } from '../../lib/format'
import { useMessages, useSendMessage, useSession } from '../../lib/queries'
import { useSessionChat } from '../../lib/useSessionChat'
import type { ChatEntry, ChatSessionRef, MessageStatus } from '../../lib/types'
import { Dot, type DotState } from '../../components/Dot'
import { TermChatSwitch } from '../../components/TermChatSwitch'
import './ChatScreen.css'

const COPY_FEEDBACK_MS = 2000
/** Bottom-proximity threshold (px) under which we still consider the reader "at the bottom". */
const AUTOSCROLL_THRESHOLD_PX = 48
/** How close (seconds) an own send and a `[large message]` pointer entry must land to be matched. */
const LARGE_MESSAGE_MATCH_WINDOW_S = 15

/** Builds the dashboard path for a session's dedicated chat page. */
export function chatPagePath(sessionId: string): string {
  return `/chat/${encodeURIComponent(sessionId)}`
}

function dotState(session: ChatSessionRef): DotState {
  if (session.activity) return session.activity
  switch (session.state) {
    case 'spawning':
      return 'spawning'
    case 'errored':
      return 'errored'
    case 'done':
    case 'killed':
      return 'exited'
    default:
      return 'ready'
  }
}

/** Human-readable reason the composer is hidden, per docs/13-chat.md's client conventions. */
function readonlyReason(session: ChatSessionRef | undefined): string | undefined {
  if (!session) return undefined
  if (session.kind !== 'orchestrator') return 'канал воркера — оркестратор'
  if (session.state !== 'spawning' && session.state !== 'running') return 'сессия завершена'
  return undefined
}

interface OptimisticMessage {
  localId: number
  body: string
  status: MessageStatus
  reason?: string
  serverId?: number
  createdAt: number
  /**
   * Set once this send's transcript echo turned out to be a
   * `[large message]` pointer rather than the original text (docs/13-chat.md
   * "Большие сообщения рендерятся в ленте иначе"). The optimistic bubble
   * stays, permanently, showing "delivered as file" instead of ever being
   * replaced by the pointer's raw text.
   */
  largeFile?: boolean
}

type FeedItem =
  | { kind: 'entry'; key: string; entry: ChatEntry }
  | { kind: 'optimistic'; key: string; msg: OptimisticMessage }

function buildFeed(entries: ChatEntry[], optimistic: OptimisticMessage[]): FeedItem[] {
  // Once the real send round-trips into the transcript as an ordinary user
  // entry (exact text match), drop the optimistic stand-in — it'd otherwise
  // render twice. Large-file collapses never get a matching entry (the
  // transcript only ever holds the pointer), so they stay forever.
  const pending = optimistic.filter(
    (o) => o.largeFile || !entries.some((e) => e.role === 'user' && e.text === o.body),
  )
  // The `[large message]` pointer entry that a largeFile-collapsed optimistic
  // bubble already stands in for would otherwise render a second time as its
  // own (system) row — hide that specific entry from the feed, but leave any
  // other `[large message]`/`[task #...]` entries (e.g. loaded from history
  // with no matching optimistic send) to render as collapsed system rows.
  const consumed = new Set<number>()
  for (const o of optimistic) {
    if (!o.largeFile) continue
    const createdS = o.createdAt / 1000
    const idx = entries.findIndex(
      (e, i) =>
        !consumed.has(i) &&
        e.role === 'user' &&
        e.text.startsWith('[large message]') &&
        e.ts >= createdS - LARGE_MESSAGE_MATCH_WINDOW_S &&
        e.ts <= createdS + LARGE_MESSAGE_MATCH_WINDOW_S,
    )
    if (idx !== -1) consumed.add(idx)
  }
  const items: FeedItem[] = entries
    .map((e, i) => ({ kind: 'entry' as const, key: `e-${i}-${e.ts}`, entry: e, idx: i }))
    .filter((item) => !consumed.has(item.idx))
    .map(({ idx, ...rest }) => {
      void idx
      return rest
    })
  for (const o of pending) items.push({ kind: 'optimistic', key: `o-${o.localId}`, msg: o })
  return items
}

const STATUS_LABEL: Record<MessageStatus, string> = {
  delivered: 'delivered',
  queued: 'queued',
  failed: 'failed',
}

function ToolRow({ entry }: { entry: ChatEntry }) {
  return (
    <div className="chat-screen__tool-row">
      <span className="chat-screen__tool-badge">{entry.tool_name}</span>
      <span className="chat-screen__tool-text">{entry.text}</span>
    </div>
  )
}

function EntryBubble({ entry }: { entry: ChatEntry }) {
  const own = entry.role === 'user'
  return (
    <div className={own ? 'chat-screen__row chat-screen__row--own' : 'chat-screen__row'}>
      <div className={own ? 'chat-screen__bubble chat-screen__bubble--own' : 'chat-screen__bubble'}>
        <div className="chat-screen__body">
          <Markdown compact>{entry.text}</Markdown>
        </div>
        {entry.ts > 0 && <div className="chat-screen__when">{timeAgo(entry.ts)}</div>}
      </div>
    </div>
  )
}

/**
 * Collapsed single-line plaque for a `role: "user"` entry that's actually a
 * service inject (task-notification hooks, `<system-...>` reminders,
 * `[large message]`/`[task #...]` pointers) rather than something the human
 * typed. Collapsed by default; click reveals the raw text verbatim (no
 * markdown — this is machine-formatted, not prose).
 */
function SystemRow({ entry }: { entry: ChatEntry }) {
  const [expanded, setExpanded] = useState(false)
  const title = systemEntryTitle(entry.text)
  return (
    <div className="chat-screen__system-row">
      <button
        type="button"
        className="chat-screen__system-toggle"
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
      >
        <span className="chat-screen__system-icon" aria-hidden="true">
          ⚙
        </span>
        <span className="chat-screen__system-title">{title || '(пусто)'}</span>
      </button>
      {expanded && <pre className="chat-screen__system-body">{entry.text}</pre>}
    </div>
  )
}

/** Left-aligned, neutrally-styled bubble for a `[from <session>]` queue inject from another agent. */
function FromBubble({ entry }: { entry: ChatEntry }) {
  const parts = fromEntryParts(entry.text)
  const session = parts?.session ?? ''
  const body = parts?.body ?? entry.text
  return (
    <div className="chat-screen__row">
      <div className="chat-screen__bubble chat-screen__bubble--from">
        <div className="chat-screen__from-badge">from {session}</div>
        <div className="chat-screen__body">
          <Markdown compact>{body}</Markdown>
        </div>
        {entry.ts > 0 && <div className="chat-screen__when">{timeAgo(entry.ts)}</div>}
      </div>
    </div>
  )
}

function OptimisticBubble({ msg }: { msg: OptimisticMessage }) {
  return (
    <div className="chat-screen__row chat-screen__row--own">
      <div className="chat-screen__bubble chat-screen__bubble--own">
        <div className="chat-screen__body">
          <Markdown compact>{msg.body}</Markdown>
        </div>
        <div className={`chat-screen__status chat-screen__status--${msg.status}`}>
          {msg.largeFile
            ? 'доставлено файлом'
            : msg.status === 'failed' && msg.reason
              ? `✗ ${msg.reason}`
              : msg.status === 'delivered'
                ? '✓ delivered'
                : STATUS_LABEL[msg.status]}
        </div>
      </div>
    </div>
  )
}

export function ChatScreen() {
  const { sessionId } = useParams<{ sessionId: string }>()
  const navigate = useNavigate()
  const { data: fullSession } = useSession(sessionId)
  const { entries, session, loading } = useSessionChat(sessionId)
  const { data: messages } = useMessages(session?.id)
  const send = useSendMessage()

  const [body, setBody] = useState('')
  const [copied, setCopied] = useState(false)
  const [optimistic, setOptimistic] = useState<OptimisticMessage[]>([])
  const lastSentIdRef = useRef<number | null>(null)

  const feedRef = useRef<HTMLDivElement>(null)
  const atBottomRef = useRef(true)

  const name = fullSession?.tmux_name ?? sessionId ?? ''

  useEffect(() => {
    const previous = document.title
    document.title = name ? `${name} — rocket chat` : 'rocket chat'
    return () => {
      document.title = previous
    }
  }, [name])

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), COPY_FEEDBACK_MS)
    return () => clearTimeout(timer)
  }, [copied])

  // Collapse the latest own send into a delivery status once its transcript
  // echo shows up as a `[large message]` pointer instead of the raw text.
  useEffect(() => {
    setOptimistic((prev) => {
      let changed = false
      const next = prev.map((o) => {
        if (o.largeFile || o.localId !== lastSentIdRef.current) return o
        const createdS = o.createdAt / 1000
        const pointer = entries.find(
          (e) =>
            e.role === 'user' &&
            e.text.startsWith('[large message]') &&
            e.ts >= createdS - LARGE_MESSAGE_MATCH_WINDOW_S &&
            e.ts <= createdS + LARGE_MESSAGE_MATCH_WINDOW_S,
        )
        if (!pointer) return o
        changed = true
        return { ...o, largeFile: true, status: 'delivered' as MessageStatus }
      })
      return changed ? next : prev
    })
  }, [entries])

  // Track delivery status from GET /v1/messages?session= (kept fresh by the
  // global SSE -> react-query invalidation bridge on message.* events).
  useEffect(() => {
    if (!messages) return
    setOptimistic((prev) =>
      prev.map((o) => {
        if (o.serverId === undefined) return o
        const m = messages.find((mm) => mm.id === o.serverId)
        if (!m || (m.status === o.status && m.reason === o.reason)) return o
        return { ...o, status: m.status, reason: m.reason }
      }),
    )
  }, [messages])

  const feed = buildFeed(entries, optimistic)

  // Autoscroll only when the reader is already at (or near) the bottom, so
  // scrolling back through history isn't yanked away by new entries.
  useEffect(() => {
    const el = feedRef.current
    if (!el || !atBottomRef.current) return
    el.scrollTop = el.scrollHeight
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [feed.length])

  function handleScroll() {
    const el = feedRef.current
    if (!el) return
    atBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < AUTOSCROLL_THRESHOLD_PX
  }

  function handleBack() {
    if (window.history.length > 1) {
      navigate(-1)
    } else {
      navigate('/')
    }
  }

  async function handleCopyAttach() {
    if (!fullSession) return
    try {
      await navigator.clipboard.writeText(`rocket attach ${fullSession.tmux_name}`)
      setCopied(true)
    } catch {
      // Clipboard access can fail (permissions, non-secure context); the
      // button simply won't show the "copied" confirmation.
    }
  }

  const readonly = readonlyReason(session)
  const canCompose = session !== undefined && readonly === undefined

  function handleSend() {
    if (!session || !canCompose) return
    const text = body.trim()
    if (!text) return
    setBody('')
    const localId = Date.now()
    lastSentIdRef.current = localId
    atBottomRef.current = true
    setOptimistic((prev) => [
      ...prev,
      { localId, body: text, status: 'queued', createdAt: Date.now() },
    ])
    send.mutate(
      { to: session.id, body: text },
      {
        onSuccess: (res) => {
          setOptimistic((prev) =>
            prev.map((o) => (o.localId === localId ? { ...o, serverId: res.id } : o)),
          )
        },
        onError: (err) => {
          const reason =
            err instanceof ApiError && err.code === 'recipient_terminal'
              ? 'получатель уже завершён'
              : err.message
          setOptimistic((prev) =>
            prev.map((o) => (o.localId === localId ? { ...o, status: 'failed', reason } : o)),
          )
        },
      },
    )
  }

  if (!sessionId) return null

  return (
    <div className="chat-screen">
      <div className="chat-screen__header">
        <button type="button" className="chat-screen__back" onClick={handleBack}>
          ← back
        </button>
        {session && <Dot state={dotState(session)} />}
        <span className="chat-screen__name">{name}</span>
        <div className="chat-screen__spacer" />
        <TermChatSwitch sessionId={sessionId} active="chat" tone="light" />
        <button type="button" className="chat-screen__attach" onClick={handleCopyAttach}>
          attach ⧉
        </button>
      </div>
      {copied && fullSession && (
        <div className="chat-screen__copied">copied: rocket attach {fullSession.tmux_name}</div>
      )}

      <div className="chat-screen__feed" ref={feedRef} onScroll={handleScroll}>
        {loading && feed.length === 0 && <p className="chat-screen__empty">Loading…</p>}
        {!loading && feed.length === 0 && <p className="chat-screen__empty">No messages yet.</p>}
        {feed.map((item) => {
          if (item.kind === 'optimistic') {
            return <OptimisticBubble key={item.key} msg={item.msg} />
          }
          const { entry } = item
          if (entry.role === 'tool') {
            return <ToolRow key={item.key} entry={entry} />
          }
          if (entry.role === 'user') {
            const kind = classifyUserEntry(entry.text)
            if (kind === 'system') return <SystemRow key={item.key} entry={entry} />
            if (kind === 'from') return <FromBubble key={item.key} entry={entry} />
          }
          return <EntryBubble key={item.key} entry={entry} />
        })}
      </div>

      <div className="chat-screen__composer">
        {canCompose ? (
          <>
            <textarea
              aria-label="Message the orchestrator"
              placeholder="Message the orchestrator…"
              rows={1}
              value={body}
              onChange={(e) => setBody(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  handleSend()
                }
              }}
            />
            <button type="button" onClick={handleSend} disabled={send.isPending || !body.trim()}>
              Send
            </button>
          </>
        ) : (
          <div className="chat-screen__readonly">{readonly ?? 'read-only'}</div>
        )}
      </div>
    </div>
  )
}
