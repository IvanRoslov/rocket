// The full conversation behind one inbox row.
//
// `GET /v1/threads` deliberately carries the question only — no messages
// (internal/api/thread_inbox.go), so the inbox stays cheap however long the
// conversations get. The focus card needs the thread too, and it comes from
// the same per-subject endpoint the task and agent pages already read, so the
// two views can never disagree about a thread.

import { useAgentQuestions, useTaskQuestions } from '../../lib/queries'
import type { QuestionMessage, ThreadInboxEntry } from '../../lib/types'

export interface ThreadDetail {
  messages: QuestionMessage[]
  /**
   * The words the thread was closed with — the last `answer` message. The
   * inbox's own `resolution` is only an enum, so this is the one source for
   * the resolution the design shows.
   */
  resolutionText?: string
  /** Who wrote that answer; absent while the thread is open. */
  closedBy?: string
  isLoading: boolean
}

const EMPTY: QuestionMessage[] = []

export function useThreadDetail(entry?: ThreadInboxEntry): ThreadDetail {
  // Both hooks are always called — `enabled` is what makes the irrelevant one
  // a no-op. Calling them conditionally would break the rules of hooks the
  // moment the human moves from a task thread to a role thread.
  const task = useTaskQuestions(entry?.kind === 'task' ? entry.task_id : undefined)
  const role = useAgentQuestions(entry?.kind === 'role' ? entry.role_id : undefined)

  if (!entry) return { messages: EMPTY, isLoading: false }

  const source = entry.kind === 'task' ? task : role
  const thread = (source.data ?? []).find((q) => q.id === entry.id)
  const messages = thread?.messages ?? EMPTY
  const answer = [...messages].reverse().find((m) => m.kind === 'answer')

  return {
    messages,
    resolutionText: answer?.body,
    closedBy: answer?.author,
    isLoading: source.isLoading,
  }
}
