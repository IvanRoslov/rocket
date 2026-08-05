// One Q&A thread of a ROLE (docs/10-agents.md «Q&A-треды роли») — the shared
// QuestionThreadView with the role question mutations wired in.

import { QuestionThreadView } from './QuestionThreadView'
import { isHuman, threadParticipantLabel } from '../lib/participants'
import { useAnswerAgentQuestion, useReplyAgentQuestion } from '../lib/queries'
import type { AgentQuestion } from '../lib/types'

/**
 * Driven by `your_turn`, the caller-relative boolean — see the same note in
 * QuestionThread.tsx. `whose_turn` is only the pre-participants fallback.
 */
function whoseTurnLabel(question: AgentQuestion, roleId: string): string {
  if (question.your_turn) return 'awaiting you'
  const waiting = (question.waiting_on ?? []).filter((p) => !isHuman(p))
  if (waiting.length > 0) {
    return `awaiting ${waiting
      .map((id) => threadParticipantLabel(id, roleId, question.participants))
      .join(', ')}`
  }
  if (question.waiting_on) return ''
  if (question.whose_turn === 'role') return `awaiting ${roleId}`
  return ''
}

/**
 * A human `asked_by` means you opened this thread TO the role; anything else
 * is the role escalating TO you. The asker slot must never show the role id
 * for a user-opened thread — that would misattribute the question. `asked_by`
 * is `""` on the wire today and `"human"` after subtask #736.
 */
function askerLabel(question: AgentQuestion, roleId: string): string {
  if (isHuman(question.asked_by)) return `you asked ${roleId}`
  return `${roleId} asked`
}

export interface AgentQuestionThreadProps {
  roleId: string
  question: AgentQuestion
}

export function AgentQuestionThread({ roleId, question }: AgentQuestionThreadProps) {
  const reply = useReplyAgentQuestion()
  const answer = useAnswerAgentQuestion()

  return (
    <QuestionThreadView
      ordinal={question.ordinal}
      localRef={question.local_ref}
      body={question.body}
      context={question.context}
      messages={question.messages}
      turnLabel={whoseTurnLabel(question, roleId)}
      turnWarn={!!question.your_turn}
      askerLabel={askerLabel(question, roleId)}
      participants={question.participants}
      agentName={roleId}
      agentInitial="A"
      options={question.options}
      stale={question.stale}
      placeholder={`Write a reply, ask ${roleId} to rephrase, or give your final answer…`}
      busy={reply.isPending || answer.isPending}
      onClarify={(body, to) => reply.mutate({ id: question.id, body, to, roleId })}
      onAnswer={(body, to) => answer.mutate({ id: question.id, body, to, roleId })}
      onDismiss={() => answer.mutate({ id: question.id, dismiss: true, roleId })}
      onChoose={(choose) => answer.mutate({ id: question.id, choose, roleId })}
    />
  )
}
