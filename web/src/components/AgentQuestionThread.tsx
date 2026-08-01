// One Q&A thread of a ROLE (docs/10-agents.md «Q&A-треды роли») — the shared
// QuestionThreadView with the role question mutations wired in.

import { QuestionThreadView } from './QuestionThreadView'
import { useAnswerAgentQuestion, useReplyAgentQuestion } from '../lib/queries'
import type { AgentQuestion } from '../lib/types'

function whoseTurnLabel(question: AgentQuestion, roleId: string): string {
  if (question.whose_turn === 'user') return 'awaiting you'
  if (question.whose_turn === 'role') return `awaiting ${roleId}`
  return ''
}

/**
 * `asked_by === ""` means you opened this thread TO the role; anything else is
 * the role escalating TO you. The asker slot must never show the role id for a
 * user-opened thread — that would misattribute the question.
 */
function askerLabel(question: AgentQuestion, roleId: string): string {
  if (question.asked_by === '') return `you asked ${roleId}`
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
      body={question.body}
      context={question.context}
      messages={question.messages}
      turnLabel={whoseTurnLabel(question, roleId)}
      turnWarn={question.whose_turn === 'user'}
      askerLabel={askerLabel(question, roleId)}
      agentName={roleId}
      agentInitial="A"
      placeholder={`Write a reply, ask ${roleId} to rephrase, or give your final answer…`}
      busy={reply.isPending || answer.isPending}
      onClarify={(body) => reply.mutate({ id: question.id, body, roleId })}
      onAnswer={(body) => answer.mutate({ id: question.id, body, roleId })}
      onDismiss={() => answer.mutate({ id: question.id, dismiss: true, roleId })}
    />
  )
}
