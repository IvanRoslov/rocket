// Messages tab (docs/design/Task.dc.html "MESSAGES"): a chat with the
// task's orchestrator session. A message is "own" (from the human user) iff
// it has no `from` — the daemon never populates `from` for user-sent
// messages (internal/api/messages.go), so that's the reliable check, not
// `to === session.id` (which would also match e.g. a message from a
// *different* session to this orchestrator). Orchestrator messages carry
// `from === session.id` and render on the left in white.

import { useState } from 'react'
import { Markdown } from '../../components/Markdown'
import { timeAgo } from '../../lib/format'
import { useSendMessage } from '../../lib/queries'
import type { Message } from '../../lib/types'
import './MessagesTab.css'

export interface MessagesTabProps {
  session?: { id: string; tmux_name: string }
  messages: Message[]
}

const STATUS_LABEL: Record<Message['status'], string> = {
  delivered: 'delivered',
  queued: 'queued',
  failed: 'failed',
}

export function MessagesTab({ session, messages }: MessagesTabProps) {
  const [body, setBody] = useState('')
  const send = useSendMessage()

  if (!session) {
    return <p className="messages-tab__empty">No orchestrator session for this task yet.</p>
  }

  const ordered = [...messages].sort((a, b) => a.created_at - b.created_at)

  function handleSend() {
    if (!session || !body.trim()) return
    send.mutate({ to: session.id, body }, { onSuccess: () => setBody('') })
  }

  return (
    <div className="messages-tab">
      <div className="messages-tab__list">
        {ordered.map((m) => {
          const own = !m.from
          return (
            <div key={m.id} className={own ? 'messages-tab__row messages-tab__row--own' : 'messages-tab__row'}>
              <div className={own ? 'messages-tab__bubble messages-tab__bubble--own' : 'messages-tab__bubble'}>
                <div className="messages-tab__bubble-head">
                  <span className="messages-tab__author">{own ? 'you' : session.tmux_name}</span>
                  <span className="messages-tab__when">{timeAgo(m.created_at)}</span>
                </div>
                <div className="messages-tab__body">
                  <Markdown compact>{m.body}</Markdown>
                </div>
                {own && (
                  <div className={`messages-tab__status messages-tab__status--${m.status}`}>
                    {m.status === 'delivered' ? '✓ ' : ''}
                    {STATUS_LABEL[m.status]}
                  </div>
                )}
              </div>
            </div>
          )
        })}
        {ordered.length === 0 && <p className="messages-tab__empty">No messages yet.</p>}
      </div>

      <div className="messages-tab__compose">
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
      </div>
    </div>
  )
}
