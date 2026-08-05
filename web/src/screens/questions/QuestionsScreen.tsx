// Global Questions page (/questions) — the Decide/Browse screen from the
// approved v3 design.
//
// Decide is the default because the page exists for one job: get the threads
// that block agents off the human's plate, worst first, one at a time. Browse
// is the escape hatch for everything else — history, notes, threads waiting on
// somebody else.
//
// It reads GET /v1/threads rather than GET /v1/questions because the latter
// knows only about task threads: a role thread waiting on the human was
// invisible here, which is exactly how threads got left hanging.
//
// Every resolving action is DEFERRED (see deferred.ts): closing a thread is
// final on the server, so the human gets a five-second undo window before the
// call goes out.

import { useEffect, useMemo, useRef, useState } from 'react'
import { useAnswerThread, useAskThread, useReplyThread, useThreads, type ThreadRef } from '../../lib/queries'
import type { ThreadInboxEntry } from '../../lib/types'
import { AskComposer } from './AskComposer'
import { BrowseMode } from './BrowseMode'
import { createDeferredQueue } from './deferred'
import { FocusMode } from './FocusMode'
import { queueOf, type BrowseFilter } from './model'
import { ThreadCard } from './ThreadCard'
import { useThreadDetail } from './useThreadDetail'
import './questions.css'

/** How long the human has to undo before the API call goes out. */
const UNDO_MS = 5000

export interface QuestionsScreenProps {
  /**
   * The undo window, in ms. A seam for tests, which cannot use fake timers
   * here: faking them deadlocks msw's request handling.
   */
  undoMs?: number
}

type Mode = 'focus' | 'browse'

interface Toast {
  text: string
  undo?: () => void
}

/** The write endpoint behind an inbox row. */
function refOf(entry: ThreadInboxEntry): ThreadRef {
  return entry.kind === 'task'
    ? { id: entry.id, kind: 'task', taskId: entry.task_id ?? 0 }
    : { id: entry.id, kind: 'role', roleId: entry.role_id ?? '' }
}

const FOCUS_HINTS = [
  { key: '1–9', label: 'pick an option' },
  { key: 'J / K', label: 'next / previous' },
  { key: 'E', label: 'context' },
  { key: 'S', label: 'later' },
  { key: 'X', label: 'not relevant' },
  { key: 'B', label: 'browse' },
]
const BROWSE_HINTS = [
  { key: 'B', label: 'back to deciding' },
  { key: 'Click', label: 'a row opens it in Decide' },
]

export function QuestionsScreen({ undoMs = UNDO_MS }: QuestionsScreenProps = {}) {
  // `all: true` — Browse mode is history too, and the queue filters itself.
  const { data: threads } = useThreads({ all: true })
  const answer = useAnswerThread()
  const reply = useReplyThread()
  const ask = useAskThread()

  const [mode, setMode] = useState<Mode>('focus')
  const [currentId, setCurrentId] = useState<number>()
  const [later, setLater] = useState<ReadonlySet<number>>(new Set())
  // Threads the human has acted on whose API call has not fired yet: the
  // screen must show them closed at once, or the undo window would look like
  // a screen that ignored the click.
  const [pending, setPending] = useState<Record<number, string>>({})
  const [cleared, setCleared] = useState(0)
  const [drafts, setDrafts] = useState<Record<number, string>>({})
  const [picks, setPicks] = useState<Record<number, string[]>>({})
  const [contextOpen, setContextOpen] = useState(false)
  const [conversationOpen, setConversationOpen] = useState<Record<number, boolean>>({})
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<BrowseFilter>('open')
  const [askOpen, setAskOpen] = useState(false)
  const [toast, setToast] = useState<Toast>()

  const deferred = useRef(createDeferredQueue(undoMs))
  const toastTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  useEffect(() => {
    const queue = deferred.current
    return () => {
      queue.dispose()
      clearTimeout(toastTimer.current)
    }
  }, [])

  function flash(text: string, undo?: () => void) {
    setToast({ text, undo })
    clearTimeout(toastTimer.current)
    toastTimer.current = setTimeout(() => setToast(undefined), undoMs)
  }

  function runUndo() {
    const undo = toast?.undo
    clearTimeout(toastTimer.current)
    setToast(undefined)
    undo?.()
  }

  // The list every view works off: the server's threads with the not-yet-sent
  // local closes folded in.
  const rows: ThreadInboxEntry[] = useMemo(
    () =>
      (threads ?? []).map((t) =>
        pending[t.id] !== undefined
          ? { ...t, status: 'resolved' as const, resolution: 'answered' as const }
          : t,
      ),
    [threads, pending],
  )

  const queue = queueOf(rows, later)
  const mine = rows.filter((t) => t.status === 'open' && t.your_turn)
  const waitingOnAgents = rows.filter((t) => t.status === 'open' && !t.your_turn).length
  const notes = rows.filter((t) => t.type === 'fyi').length
  const staleCount = mine.filter((t) => t.stale).length
  const current = rows.find((t) => t.id === currentId) ?? queue[0]
  const detail = useThreadDetail(current)

  // A thread that arrives on your turn while you are looking at the page is
  // the one live event this screen must not swallow — it is why the page is
  // open at all. `seen` starts unset so the first load never toasts.
  const seen = useRef<Set<number>>(undefined)
  useEffect(() => {
    if (!threads) return
    const now = new Set(threads.filter((t) => t.status === 'open' && t.your_turn).map((t) => t.id))
    const before = seen.current
    seen.current = now
    if (!before) return
    const fresh = threads.find((t) => now.has(t.id) && !before.has(t.id))
    if (fresh) flash(`${fresh.local_ref} arrived — added to your queue`)
    // `flash` is stable enough for this effect: it only ever sets state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [threads])

  function advance(fromId: number) {
    const i = queue.findIndex((t) => t.id === fromId)
    const next = queue[i + 1] ?? queue.find((t) => t.id !== fromId)
    setCurrentId(next?.id)
    setContextOpen(false)
  }

  function step(delta: number) {
    if (!current) return
    const i = queue.findIndex((t) => t.id === current.id)
    const next = queue[Math.min(queue.length - 1, Math.max(0, i + delta))]
    if (next) {
      setCurrentId(next.id)
      setContextOpen(false)
    }
  }

  function select(id: number) {
    setCurrentId(id)
    setContextOpen(false)
    setMode('focus')
  }

  /** Closes a thread locally and schedules the call that makes it real. */
  function closeWith(
    entry: ThreadInboxEntry,
    resolution: string,
    toastText: string,
    run: () => void,
  ) {
    setPending((p) => ({ ...p, [entry.id]: resolution }))
    setCleared((n) => n + 1)
    advance(entry.id)
    deferred.current.schedule(run)
    flash(toastText, () => {
      deferred.current.cancel()
      setPending(({ [entry.id]: _dropped, ...rest }) => rest)
      setCleared((n) => Math.max(0, n - 1))
      setCurrentId(entry.id)
      setMode('focus')
    })
  }

  function choose(entry: ThreadInboxEntry, index: number) {
    const option = entry.options?.[index]
    if (!option) return
    closeWith(entry, option, `${entry.local_ref} closed · ${index + 1} ${option}`, () =>
      // 1-based: the daemon substitutes the option's own text.
      answer.mutate({ ref: refOf(entry), choose: index + 1 }),
    )
  }

  function answerClose(entry: ThreadInboxEntry) {
    const text = (drafts[entry.id] ?? '').trim()
    if (!text) {
      flash('Pick an option or write the resolution first')
      return
    }
    const to = picks[entry.id] ?? []
    setDrafts((d) => ({ ...d, [entry.id]: '' }))
    closeWith(entry, text, `${entry.local_ref} closed with your answer`, () =>
      answer.mutate({ ref: refOf(entry), body: text, to }),
    )
  }

  function dismiss(entry: ThreadInboxEntry) {
    closeWith(entry, 'Not relevant.', `${entry.local_ref} closed as not relevant`, () =>
      answer.mutate({ ref: refOf(entry), dismiss: true }),
    )
  }

  // Later is purely local: the daemon has no "snooze", and inventing one on
  // the server would hide a thread from everybody, not just from this queue.
  function skip(entry: ThreadInboxEntry) {
    setLater((s) => new Set(s).add(entry.id))
    advance(entry.id)
    flash(`${entry.local_ref} moved to the end of the queue`, () => {
      setLater((s) => {
        const next = new Set(s)
        next.delete(entry.id)
        return next
      })
      setCurrentId(entry.id)
    })
  }

  function askBack(entry: ThreadInboxEntry) {
    const text = (drafts[entry.id] ?? '').trim()
    if (!text) {
      flash('Nothing to send yet')
      return
    }
    const to = picks[entry.id] ?? []
    const others = (entry.participants ?? []).filter((p) => p !== 'human' && p !== '')
    const names = (to.length > 0 ? to : others).join(', ')
    setDrafts((d) => ({ ...d, [entry.id]: '' }))
    setLater((s) => new Set(s).add(entry.id))
    advance(entry.id)
    deferred.current.schedule(() => reply.mutate({ ref: refOf(entry), body: text, to }))
    flash(`Sent · turn passed to ${names}`, () => {
      deferred.current.cancel()
      setDrafts((d) => ({ ...d, [entry.id]: text }))
      setLater((s) => {
        const next = new Set(s)
        next.delete(entry.id)
        return next
      })
      setCurrentId(entry.id)
    })
  }

  // Hotkeys. Inert while the human is typing — otherwise "s" in a sentence
  // would push the thread you are answering to the back of the queue.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const tag = (e.target as HTMLElement | null)?.tagName ?? ''
      if (tag === 'TEXTAREA' || tag === 'INPUT') return
      if (e.metaKey || e.ctrlKey || e.altKey) return
      const k = e.key.toLowerCase()
      if (k === 'b') {
        e.preventDefault()
        setMode((m) => (m === 'focus' ? 'browse' : 'focus'))
        return
      }
      if (k === 'z' && toast?.undo) {
        e.preventDefault()
        runUndo()
        return
      }
      if (mode !== 'focus' || !current) return
      if (k === 'j') {
        e.preventDefault()
        step(1)
        return
      }
      if (k === 'k') {
        e.preventDefault()
        step(-1)
        return
      }
      if (k === 'e') {
        e.preventDefault()
        setContextOpen((v) => !v)
        return
      }
      if (current.status !== 'open') return
      if (/^[1-9]$/.test(k)) {
        e.preventDefault()
        choose(current, Number(k) - 1)
        return
      }
      if (k === 's') {
        e.preventDefault()
        skip(current)
      } else if (k === 'x') {
        e.preventDefault()
        dismiss(current)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  function goNotes() {
    const note = rows.find((t) => t.type === 'fyi')
    if (note) select(note.id)
    else {
      setMode('browse')
      setFilter('closed')
    }
  }

  const headline = mine.length > 0 ? `${mine.length} decisions on you` : 'Nothing on you'
  const subline =
    staleCount > 0 ? `${staleCount} stale · ${cleared} cleared today` : `${cleared} cleared today`
  const total = mine.length + cleared
  const progress = total > 0 ? Math.round((cleared / total) * 100) : 100

  const card = current ? (
    <ThreadCard
      key={current.id}
      entry={current}
      detail={detail}
      pendingResolution={pending[current.id]}
      draft={drafts[current.id] ?? ''}
      onDraft={(v) => setDrafts((d) => ({ ...d, [current.id]: v }))}
      picks={picks[current.id] ?? []}
      onTogglePick={(p) =>
        setPicks((all) => {
          const has = all[current.id] ?? []
          return {
            ...all,
            [current.id]: has.includes(p) ? has.filter((x) => x !== p) : [...has, p],
          }
        })
      }
      contextOpen={contextOpen}
      onToggleContext={() => setContextOpen((v) => !v)}
      conversationOpen={conversationOpen[current.id] ?? false}
      onToggleConversation={() =>
        setConversationOpen((c) => ({ ...c, [current.id]: !(c[current.id] ?? false) }))
      }
      onChoose={(i) => choose(current, i)}
      onAnswerClose={() => answerClose(current)}
      onReply={() => askBack(current)}
      onSkip={() => skip(current)}
      onDismiss={() => dismiss(current)}
      onBackToQueue={() => {
        setCurrentId(queue[0]?.id)
        setContextOpen(false)
        setMode('focus')
      }}
      onBrowse={() => setMode('browse')}
    />
  ) : null

  return (
    <div className="q">
      <div className="q__strip">
        <span className="q__headline">{headline}</span>
        <span className="q__subline">{subline}</span>
        <div className="q__progress">
          <div className="q__progress-fill" style={{ width: `${progress}%` }} />
        </div>
        <div className="q__spacer" />
        <div className="q__modes">
          {(
            [
              ['focus', 'Decide', 'F'],
              ['browse', 'Browse', 'B'],
            ] as [Mode, string, string][]
          ).map(([m, label, key]) => (
            <button
              key={m}
              type="button"
              aria-pressed={mode === m}
              className={mode === m ? 'q__mode q__mode--on' : 'q__mode'}
              onClick={() => setMode(m)}
            >
              {label}
              <span className="q__mode-key">{key}</span>
            </button>
          ))}
        </div>
        <button type="button" className="q__ask-toggle" onClick={() => setAskOpen((v) => !v)}>
          {askOpen ? 'Close' : 'Ask an agent'}
        </button>
      </div>

      {askOpen && (
        <AskComposer
          onCancel={() => setAskOpen(false)}
          onEmpty={() => flash('Write the question first')}
          onSubmit={({ target, body, type }) => {
            ask.mutate({ target, body, ...(type === 'fyi' ? { type } : {}) })
            setAskOpen(false)
            const name = target.kind === 'task' ? `#${target.id}` : target.id
            flash(type === 'fyi' ? `Note posted → ${name}` : `Question opened → ${name}`)
          }}
        />
      )}

      {mode === 'focus' ? (
        <FocusMode
          queue={queue}
          currentId={current?.id}
          onSelect={select}
          waitingOnAgents={waitingOnAgents}
          notes={notes}
          onBrowse={() => setMode('browse')}
          onNotes={goNotes}
          clearedToday={cleared}
          card={card}
        />
      ) : (
        <BrowseMode
          threads={rows}
          query={query}
          onQuery={setQuery}
          filter={filter}
          onFilter={setFilter}
          onOpen={select}
        />
      )}

      <div className="q__hints">
        {(mode === 'focus' ? FOCUS_HINTS : BROWSE_HINTS).map((h) => (
          <span key={h.key} className="q__hints-item">
            <span className="q__hints-key">{h.key}</span>
            {h.label}
          </span>
        ))}
        <div className="q__spacer" />
        <span className="q__live">
          <span className="q__live-dot" aria-hidden="true" />
          live
        </span>
      </div>

      {toast && (
        <div className="q__toast" role="status">
          <span className="q__toast-tick" aria-hidden="true">
            ✓
          </span>
          <span className="q__toast-text">{toast.text}</span>
          {toast.undo && (
            <button type="button" className="q__toast-undo" onClick={runUndo}>
              Undo<span className="q__toast-key">Z</span>
            </button>
          )}
        </div>
      )}
    </div>
  )
}
