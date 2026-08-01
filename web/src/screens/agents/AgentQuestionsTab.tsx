// Role Q&A threads, both directions (docs/10-agents.md): threads the role
// opened to escalate to you, and threads you open to the role — the latter
// wake it with a `question` inbox event.

import { useMemo, useState } from 'react'
import { AgentQuestionThread } from '../../components/AgentQuestionThread'
import { Button } from '../../components/Button'
import { useAgentQuestions, useAskAgent } from '../../lib/queries'
import './agents.css'

export interface AgentQuestionsTabProps {
  roleId: string
}

export function AgentQuestionsTab({ roleId }: AgentQuestionsTabProps) {
  const { data: questions } = useAgentQuestions(roleId)
  const ask = useAskAgent(roleId)
  const [body, setBody] = useState('')
  const [showResolved, setShowResolved] = useState(false)

  const open = useMemo(
    () => (questions ?? []).filter((q) => q.status === 'open').sort((a, b) => a.ordinal - b.ordinal),
    [questions],
  )
  const resolved = useMemo(
    () => (questions ?? []).filter((q) => q.status !== 'open').sort((a, b) => b.ordinal - a.ordinal),
    [questions],
  )

  function submit() {
    if (!body.trim()) return
    ask.mutate({ body }, { onSuccess: () => setBody('') })
  }

  return (
    <div className="agent-questions">
      {open.length === 0 ? (
        <div className="agent-tab__empty">No open threads.</div>
      ) : (
        open.map((q) => <AgentQuestionThread key={q.id} roleId={roleId} question={q} />)
      )}

      {resolved.length > 0 && (
        <>
          <button
            type="button"
            className="agent-questions__section-label"
            onClick={() => setShowResolved((v) => !v)}
          >
            Resolved ({resolved.length}) {showResolved ? '▴' : '▾'}
          </button>
          {showResolved &&
            resolved.map((q) => <AgentQuestionThread key={q.id} roleId={roleId} question={q} />)}
        </>
      )}

      <div className="agent-questions__ask">
        <textarea
          aria-label="Ask the role"
          rows={3}
          placeholder={`Ask ${roleId} something…`}
          value={body}
          onChange={(e) => setBody(e.target.value)}
        />
        {ask.isError && <p className="agent-form__error">{ask.error.message}</p>}
        <div className="agent-questions__ask-actions">
          <Button variant="primary" size="sm" onClick={submit} disabled={!body.trim() || ask.isPending}>
            Ask
          </Button>
          <span className="agent-questions__ask-hint">
            Opens a thread and wakes the role.
          </span>
        </div>
      </div>
    </div>
  )
}
