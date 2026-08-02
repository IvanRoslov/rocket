// One open-question thread card of a TASK: the shared QuestionThreadView with
// the task question mutations wired in. Role threads use the same view via
// AgentQuestionThread.

import { QuestionThreadView } from './QuestionThreadView'
import { isHuman, participantLabel, threadParticipantLabel } from '../lib/participants'
import { useAnswerQuestion, useReplyQuestion } from '../lib/queries'
import type { Question } from '../lib/types'

/**
 * The chip is driven by `your_turn` — the caller-relative boolean — never by
 * the legacy two-party `whose_turn`, which cannot express "waiting on the cto
 * agent, not on you". `whose_turn` survives only as the fallback for a
 * pre-participants daemon that sends no `waiting_on` at all.
 */
function whoseTurnLabel(question: Question, orchestratorName?: string): string {
  if (question.your_turn) return 'awaiting you'
  const waiting = (question.waiting_on ?? []).filter((p) => !isHuman(p))
  if (waiting.length > 0) {
    return `awaiting ${waiting
      .map((id) => threadParticipantLabel(id, orchestratorName, question.participants))
      .join(', ')}`
  }
  if (question.waiting_on) return ''
  if (question.whose_turn === 'orchestrator') return 'awaiting orchestrator'
  return ''
}

export function authorLabel(author: string | undefined, orchestratorName?: string): string {
  return participantLabel(author, orchestratorName)
}

/**
 * A human `asked_by` means the human opened this thread TO the orchestrator
 * (docs/12-tasks.md); anything else is the existing orchestrator-opened
 * direction. The asker slot must never show `orchestratorName` for a
 * user-opened thread — that would misattribute the question. `asked_by` is
 * `""` on the wire today and `"human"` after subtask #736, so it is only ever
 * read through `isHuman()`.
 */
function askerLabel(question: Question, orchestratorName?: string): string {
  if (isHuman(question.asked_by)) return 'you asked the orchestrator'
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
      turnLabel={whoseTurnLabel(question, orchestratorName)}
      turnWarn={!!question.your_turn}
      askerLabel={askerLabel(question, orchestratorName)}
      participants={question.participants}
      agentName={orchestratorName}
      agentInitial="O"
      placeholder="Write a reply, ask the orchestrator to rephrase, or give your final answer…"
      busy={reply.isPending || answer.isPending}
      onClarify={(body, to) => reply.mutate({ id: question.id, body, to, taskId })}
      onAnswer={(body, to) => answer.mutate({ id: question.id, body, to, taskId })}
      onDismiss={() => answer.mutate({ id: question.id, dismiss: true, taskId })}
    />
  )
}
