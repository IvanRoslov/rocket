// One open-question thread card of a TASK: the shared QuestionThreadView with
// the task question mutations wired in. Role threads use the same view via
// AgentQuestionThread.

import { QuestionThreadView } from './QuestionThreadView'
import { useAnswerQuestion, useReplyQuestion } from '../lib/queries'
import type { Question } from '../lib/types'

function whoseTurnLabel(question: Question): string {
  if (question.whose_turn === 'user') return 'awaiting you'
  if (question.whose_turn === 'orchestrator') return 'awaiting orchestrator'
  return ''
}

export function authorLabel(author: string | undefined, orchestratorName?: string): string {
  if (!author) return 'you'
  return orchestratorName ?? author
}

/**
 * `asked_by === ""` means the human opened this thread TO the orchestrator
 * (docs/12-tasks.md); anything else is the existing orchestrator-opened
 * direction. The asker slot must never show `orchestratorName` for a
 * user-opened thread — that would misattribute the question.
 */
function askerLabel(question: Question, orchestratorName?: string): string {
  if (question.asked_by === '') return 'you asked the orchestrator'
  return `${orchestratorName ?? question.asked_by} asked`
}

export interface QuestionThreadProps {
  taskId: number
  question: Question
  orchestratorName?: string
}

export function QuestionThread({ taskId, question, orchestratorName }: QuestionThreadProps) {
  const reply = useReplyQuestion()
  const answer = useAnswerQuestion()

  return (
    <QuestionThreadView
      ordinal={question.ordinal}
      body={question.body}
      context={question.context}
      messages={question.messages}
      turnLabel={whoseTurnLabel(question)}
      turnWarn={question.whose_turn === 'user'}
      askerLabel={askerLabel(question, orchestratorName)}
      agentName={orchestratorName}
      agentInitial="O"
      placeholder="Write a reply, ask the orchestrator to rephrase, or give your final answer…"
      busy={reply.isPending || answer.isPending}
      onClarify={(body) => reply.mutate({ id: question.id, body, taskId })}
      onAnswer={(body) => answer.mutate({ id: question.id, body, taskId })}
      onDismiss={() => answer.mutate({ id: question.id, dismiss: true, taskId })}
    />
  )
}
