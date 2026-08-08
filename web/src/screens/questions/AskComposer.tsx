// "Ask an agent": the one place a human starts a thread instead of answering
// one.
//
// The targets are real and deliberately narrow: a root task only appears while
// it has an orchestrator session bound to it (nobody is home on a backlog
// task), and every registered agent appears, running or not — an agent that is
// down still gets the question through its inbox.

import { useState } from 'react'
import { useAgents, useTasks, type AskTarget } from '../../lib/queries'
import type { ThreadType } from '../../lib/types'

export interface AskComposerProps {
  onSubmit: (payload: { target: AskTarget; body: string; title?: string; type: ThreadType }) => void
  onCancel: () => void
  /** Shown when the human submits nothing — the screen owns the toast. */
  onEmpty: () => void
}

interface TargetOption {
  key: string
  label: string
  target: AskTarget
}

export function AskComposer(props: AskComposerProps) {
  const { data: tasks } = useTasks()
  const { data: agents } = useAgents()
  const [kind, setKind] = useState<ThreadType>('decision')
  const [selected, setSelected] = useState<string>()
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')

  const targets: TargetOption[] = [
    ...(tasks ?? [])
      .filter((t) => t.session_id && t.status !== 'done' && t.status !== 'cancelled')
      .map((t) => ({
        key: `task:${t.id}`,
        label: `#${t.id} ${t.title}`,
        target: { kind: 'task', id: t.id } as AskTarget,
      })),
    ...(agents ?? []).map((a) => ({
      key: `role:${a.id}`,
      label: a.id,
      target: { kind: 'role', id: a.id } as AskTarget,
    })),
  ]
  const current = targets.find((t) => t.key === selected) ?? targets[0]

  function submit() {
    const text = body.trim()
    if (!text || !current) {
      props.onEmpty()
      return
    }
    // The heading is optional: the daemon derives one from the body when it
    // is left empty (task #1264).
    props.onSubmit({ target: current.target, body: text, title: title.trim() || undefined, type: kind })
    setTitle('')
    setBody('')
  }

  return (
    <div className="q__ask">
      <div className="q__ask-inner">
        <div className="q__ask-row">
          {(
            [
              ['decision', 'Question'],
              ['fyi', 'FYI note'],
            ] as [ThreadType, string][]
          ).map(([k, label]) => (
            <button
              key={k}
              type="button"
              aria-pressed={kind === k}
              className={kind === k ? 'q__pill q__pill--on' : 'q__pill'}
              onClick={() => setKind(k)}
            >
              {label}
            </button>
          ))}
          <span className="q__ask-divider" />
          {targets.map((t) => (
            <button
              key={t.key}
              type="button"
              aria-pressed={current?.key === t.key}
              className={current?.key === t.key ? 'q__target q__target--on' : 'q__target'}
              onClick={() => setSelected(t.key)}
            >
              {t.label}
            </button>
          ))}
          <div className="q__spacer" />
          <span className="q__hint">
            {kind === 'fyi' ? 'posted closed · no turn, no badge' : 'opens a decision thread'}
          </span>
        </div>
        <input
          type="text"
          className="q__ask-title"
          aria-label="Question heading (optional)"
          placeholder="Heading — one line, optional"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
        <textarea
          className="q__textarea"
          aria-label="What to ask"
          rows={2}
          value={body}
          placeholder={
            kind === 'fyi'
              ? 'What should they know? No answer expected.'
              : 'What do you need decided or clarified?'
          }
          onChange={(e) => setBody(e.target.value)}
        />
        <div style={{ display: 'flex', gap: 9 }}>
          <button type="button" className="q__btn-dark" onClick={submit}>
            {kind === 'fyi' ? 'Post note' : 'Open question'}
          </button>
          <button type="button" className="q__btn-ghost" onClick={props.onCancel}>
            Cancel
          </button>
        </div>
      </div>
    </div>
  )
}
