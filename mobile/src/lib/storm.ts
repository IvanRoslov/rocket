// Storm questions are answered only in the Storms tab and the task's Brainstorm
// tab (the same rules as the web dashboard, web/src/lib/storm.ts): the
// Questions tab lists no storm thread and its badge counts none.
import type { ThreadInboxEntry } from '../api/types'
import { isBrainstorm } from './brainstorm'

function waitingOnYou(t: ThreadInboxEntry): boolean {
  return t.status === 'open' && t.your_turn
}

/** The Questions tab badge: ordinary threads on your turn (storms have their own tab). */
export function inboxCount(threads: ThreadInboxEntry[]): number {
  return threads.filter((t) => waitingOnYou(t) && !isBrainstorm(t)).length
}
