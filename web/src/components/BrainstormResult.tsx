// An answered storm question (task #4901 spec §3.1): the chosen option (or
// "own answer"), the human's words, the outcome — which the human may correct
// — whether the orchestrator recorded the answer from its terminal, and which
// agent answered when it was not the human (task #5019).

import { isHuman } from '../lib/participants'
import type { BrainstormFields, BrainstormOutcome } from '../lib/types'
import './brainstorm.css'

const OUTCOMES: { value: BrainstormOutcome; label: string }[] = [
  { value: 'accepted', label: 'Accepted' },
  { value: 'corrected', label: 'Corrected' },
  { value: 'wrong_turn', label: 'Wrong turn' },
]

/** The outcome as the human reads it; an accepted answer with words is "with a comment". */
export function outcomeLabel(outcome: BrainstormOutcome, comment: string): string {
  if (outcome === 'accepted' && comment.trim()) return 'Accepted with comment'
  return OUTCOMES.find((o) => o.value === outcome)?.label ?? outcome
}

export interface BrainstormResultProps {
  question: BrainstormFields & { options?: string[] }
  /** Present only where the human may correct the outcome. */
  onOverride?: (outcome: BrainstormOutcome) => void
  busy?: boolean
  /** Why the last outcome change was refused (403 agent, 409 not answered). */
  error?: string
}

export function BrainstormResult({ question, onOverride, busy, error }: BrainstormResultProps) {
  const outcome = question.outcome
  // Open, dismissed or pre-storm threads have nothing to grade.
  if (!outcome) return null

  const chosen = question.chosen_option ?? null
  const comment = question.answer_comment ?? ''
  const optionText = chosen !== null ? question.options?.[chosen - 1] : undefined

  return (
    <div className="brainstorm-result">
      <div className="brainstorm-result__row">
        <span className="brainstorm-result__choice">
          {chosen !== null ? (
            <>
              Option {chosen}
              {optionText ? `: ${optionText}` : ''}
              {chosen === question.recommended_option && (
                <span className="brainstorm-options__star" title="Recommended by the agent">
                  {' '}★
                </span>
              )}
            </>
          ) : (
            'Own answer'
          )}
        </span>
        <span className={`brainstorm-result__outcome brainstorm-result__outcome--${outcome}`}>
          {outcomeLabel(outcome, comment)}
        </span>
        {question.outcome_overridden && <span className="brainstorm-result__note">changed by hand</span>}
        {!isHuman(question.answered_by) && (
          <span className="brainstorm-result__source">answered by {question.answered_by}</span>
        )}
        {question.answer_source === 'terminal' && (
          <span className="brainstorm-result__source">From terminal</span>
        )}
        {onOverride && (
          <select
            className="brainstorm-result__override"
            aria-label="Outcome"
            value={outcome}
            disabled={busy}
            onChange={(e) => onOverride(e.target.value as BrainstormOutcome)}
          >
            {OUTCOMES.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        )}
      </div>
      {comment && <div className="brainstorm-result__comment">{comment}</div>}
      {error && (
        <div className="brainstorm-result__error" role="alert">
          Could not change the outcome: {error}
        </div>
      )}
    </div>
  )
}
