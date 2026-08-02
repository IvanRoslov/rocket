// Presentation rules for multi-participant Q&A threads (docs/12-tasks.md).
// Kept pure so both thread screens share one set of rules and they are
// unit-tested without rendering.

/** The canonical participant id of the person using the app. */
export const HUMAN = 'human'

/**
 * True for the human. The author wire is deliberately legacy: `asked_by` and
 * `messages[].author` are "" today and become "human" with subtask #736,
 * while `participants`/`waiting_on`/`addressed_to` already say "human". Both
 * forms mean us, so never compare against just one.
 */
export function isHuman(id: string | undefined): boolean {
  return !id || id === HUMAN
}

/** Display name of a participant: ourselves as "you", anyone else by id. */
export function participantLabel(id: string | undefined): string {
  return isHuman(id) ? 'you' : id!
}

/** Single-letter avatar glyph matching `participantLabel`. */
export function participantInitial(id: string | undefined): string {
  return isHuman(id) ? 'Y' : id!.slice(0, 1).toUpperCase()
}

/** "→ cto, you" for a message's addressees; empty when it addressed everyone. */
export function addresseeLabel(to: string[] | undefined): string {
  if (!to || to.length === 0) return ''
  return `→ ${to.map(participantLabel).join(', ')}`
}

/** The addressees we may pick: every participant but ourselves. */
export function answerableBy(participants: string[]): string[] {
  return participants.filter((p) => !isHuman(p))
}

/** Flip one addressee in the picker's selection, preserving order. */
export function toggleAddressee(sel: string[], id: string): string[] {
  return sel.includes(id) ? sel.filter((x) => x !== id) : [...sel, id]
}

/**
 * Body fragment carrying the picked addressees. Picking nobody must omit the
 * key entirely rather than send `to: []`: an absent `to` means "everyone
 * except the author", which is the daemon's default and not the same thing.
 */
export function addresseePayload(sel: string[]): { to?: string[] } {
  return sel.length > 0 ? { to: sel } : {}
}

/**
 * Open threads whose next word is ours. Driven by `your_turn`, the
 * participant-aware field — never by the compat `whose_turn`.
 */
export function countYourTurn(threads: { status: string; your_turn?: boolean }[]): number {
  return threads.filter((t) => t.status === 'open' && t.your_turn === true).length
}
