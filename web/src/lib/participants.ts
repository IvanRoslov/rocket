// Participant identity for question threads (docs/12-tasks.md, docs/10-agents.md).
//
// A thread is a conversation between several participants: the human,
// persistent agent ids (`cto`) and session ids (`reply-answer-orch`). The wire
// is mid-migration for the human's id: `internal/api/questions.go`'s
// wireAuthor() still emits `""` in `messages[].author` and `asked_by`, while
// `participants`, `waiting_on` and `addressed_to` already carry the canonical
// `"human"`. Subtask #736 flips the remaining two. Nothing in the dashboard may
// compare against one spelling — everything goes through isHuman().

export const HUMAN = 'human'

/** True for both the legacy `""`/absent author and the canonical `"human"`. */
export function isHuman(id?: string): boolean {
  return !id || id === HUMAN
}

/**
 * Display name for a participant id. `agentName` is the caller's display name
 * for its single known counterpart (an orchestrator name or a role id); the
 * raw participant id is the honest fallback for everyone else.
 */
export function participantLabel(id: string | undefined, agentName?: string): string {
  if (isHuman(id)) return 'you'
  return agentName ?? id!
}

/**
 * Display name for a message's author.
 *
 * `agentName` names ONE counterpart. In a thread with several agent
 * participants it would stamp that one name onto every agent-authored message,
 * so it only applies while the thread has at most one non-human participant.
 * Beyond that everyone shows under their own participant id.
 */
export function messageAuthorLabel(
  author: string | undefined,
  agentName: string | undefined,
  participants: string[] | undefined,
): string {
  if (isHuman(author)) return 'you'
  if (!participants) return agentName ?? author!
  const agents = participants.filter((p) => !isHuman(p))
  return agents.length <= 1 ? (agentName ?? author!) : author!
}
