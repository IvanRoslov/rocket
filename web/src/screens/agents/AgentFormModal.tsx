// Create/edit a role: id, role prompt (markdown, with preview), GitHub
// subscriptions, cron and the underlying agent. The prompt is the role's
// triage policy in free text (docs/10-agents.md) — editing it takes effect on
// the role's next wake, no restart involved.

import { useState, type FormEvent } from 'react'
import { Button } from '../../components/Button'
import { Markdown } from '../../components/Markdown'
import { Modal } from '../../components/Modal'
import { useCreateAgent, useUpdateAgent } from '../../lib/queries'
import type { Agent, AgentSubscription } from '../../lib/types'
import './agents.css'

const ID_PATTERN = /^[a-z0-9-]+$/

const AGENT_OPTIONS = ['claude', 'claude-code', 'codex']

/**
 * Subscriptions are edited as one repo per line — the daemon's structural
 * filters (docs/10-agents.md): `owner/repo [label=a,b] [mention-only]`.
 * Unrecognized trailing words are ignored rather than rejected, so a typo in
 * a modifier never silently drops the whole repo from the subscription list.
 */
export function parseSubscriptions(text: string): AgentSubscription[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [repo, ...rest] = line.split(/\s+/)
      const labelPart = rest.find((w) => w.startsWith('label='))
      return {
        repo,
        labels: labelPart ? labelPart.slice('label='.length).split(',').filter(Boolean) : [],
        mention_only: rest.includes('mention-only'),
      }
    })
}

export function formatSubscriptions(subs: AgentSubscription[]): string {
  return subs
    .map((s) => {
      const parts = [s.repo]
      if (s.labels && s.labels.length > 0) parts.push(`label=${s.labels.join(',')}`)
      if (s.mention_only) parts.push('mention-only')
      return parts.join(' ')
    })
    .join('\n')
}

export interface AgentFormModalProps {
  projectId: string
  /** Existing role to edit; omitted when creating. */
  agent?: Agent
  onClose: () => void
  /** Called with the role id after a successful create. */
  onCreated?: (id: string) => void
}

export function AgentFormModal({ projectId, agent, onClose, onCreated }: AgentFormModalProps) {
  const editing = agent !== undefined

  const [id, setId] = useState(agent?.id ?? '')
  const [prompt, setPrompt] = useState(agent?.prompt ?? '')
  const [subsText, setSubsText] = useState(formatSubscriptions(agent?.subscriptions ?? []))
  const [cron, setCron] = useState(agent?.cron ?? '')
  const [underlying, setUnderlying] = useState(agent?.agent ?? '')
  const [preview, setPreview] = useState(false)

  const create = useCreateAgent()
  const update = useUpdateAgent()

  const idValid = ID_PATTERN.test(id)
  const busy = create.isPending || update.isPending
  const error = create.error ?? update.error

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!idValid || busy) return

    const subscriptions = parseSubscriptions(subsText)
    if (editing) {
      update.mutate(
        { id: agent.id, prompt, subscriptions, cron, agent: underlying },
        { onSuccess: onClose },
      )
      return
    }
    create.mutate(
      { id, project: projectId, prompt, subscriptions, cron, agent: underlying },
      {
        onSuccess: () => {
          onCreated?.(id)
          onClose()
        },
      },
    )
  }

  return (
    <Modal title={editing ? `Edit role ${agent.id}` : 'New role'} onClose={onClose}>
      <form className="agent-form" onSubmit={handleSubmit}>
        <label className="agent-form__label" htmlFor="agent-form-id">
          Role id
        </label>
        <input
          id="agent-form-id"
          className="agent-form__input agent-form__input--mono"
          value={id}
          onChange={(e) => setId(e.target.value)}
          placeholder="sre"
          disabled={editing}
          autoFocus={!editing}
        />
        {id !== '' && !idValid && (
          <p className="agent-form__error">Use lowercase letters, digits and dashes</p>
        )}
        <p className="agent-form__hint">
          The id is also the role's address in the message queue: <code>rocket send {id || 'sre'}</code>.
        </p>

        <div className="agent-form__label-row">
          <label className="agent-form__label" htmlFor="agent-form-prompt">
            Role prompt
          </label>
          <button
            type="button"
            className="agent-form__toggle"
            onClick={() => setPreview((v) => !v)}
          >
            {preview ? 'Edit' : 'Preview'}
          </button>
        </div>
        {preview ? (
          <div className="agent-form__preview">
            <Markdown>{prompt || '_empty prompt_'}</Markdown>
          </div>
        ) : (
          <textarea
            id="agent-form-prompt"
            className="agent-form__textarea agent-form__textarea--mono"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder={'# SRE\n\nWhat this role owns and how it triages: what to take, what to label, what to escalate.'}
            rows={14}
          />
        )}
        <p className="agent-form__hint">
          Markdown. Read on every wake — edits need no restart.
        </p>

        <label className="agent-form__label" htmlFor="agent-form-subs">
          GitHub subscriptions
        </label>
        <textarea
          id="agent-form-subs"
          className="agent-form__textarea agent-form__textarea--mono"
          value={subsText}
          onChange={(e) => setSubsText(e.target.value)}
          placeholder={'acme/platform label=bug mention-only\nacme/web'}
          rows={4}
        />
        <p className="agent-form__hint">
          One repo per line: <code>owner/repo [label=a,b] [mention-only]</code>. These decide what
          reaches the inbox; the prompt decides what to do with it.
        </p>

        <label className="agent-form__label" htmlFor="agent-form-cron">
          Cron
        </label>
        <input
          id="agent-form-cron"
          className="agent-form__input agent-form__input--mono"
          value={cron}
          onChange={(e) => setCron(e.target.value)}
          placeholder="0 * * * *"
        />
        <p className="agent-form__hint">Five fields; leave empty for no schedule.</p>

        <label className="agent-form__label" htmlFor="agent-form-agent">
          Underlying agent
        </label>
        <select
          id="agent-form-agent"
          className="agent-form__input"
          value={underlying}
          onChange={(e) => setUnderlying(e.target.value)}
        >
          <option value="">daemon default</option>
          {AGENT_OPTIONS.map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>

        {error && <p className="agent-form__error">{error.message}</p>}

        <div className="agent-form__actions">
          <Button variant="secondary" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={!idValid || busy}>
            {editing ? 'Save' : 'Create role'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
