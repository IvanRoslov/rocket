// Shared question-thread display rules (task #1264), so the global Questions
// screen and the task screen can never disagree about what a thread looks
// like: the same heading and the same number of visible replies.

/** How many trailing replies a thread shows before "show earlier". */
export const VISIBLE_REPLIES = 3

/** Longest heading we derive locally — matches store.DeriveTitle's limit. */
const TITLE_MAX = 80

/**
 * The one-line heading of a question. The daemon derives it on write, so this
 * only has to cover the rows written before task #1264 landed: for those it
 * falls back to the opening line of the body rather than an empty row.
 */
export function questionTitle(question: { title?: string; body: string }): string {
  const title = question.title?.trim()
  if (title) return title

  const line = question.body.split('\n').find((l) => l.trim() !== '')?.trim() ?? ''
  if (line.length <= TITLE_MAX) return line

  const cut = line.slice(0, TITLE_MAX)
  const space = cut.lastIndexOf(' ')
  return `${(space > 0 ? cut.slice(0, space) : cut).trimEnd()}…`
}

/**
 * The tail of a thread the card shows, plus how many older replies it hid.
 * The tail is what matters — a thread is read from its latest turn — but the
 * history stays one click away.
 */
export function splitReplies<T>(
  messages: readonly T[],
  expanded: boolean,
): { hidden: number; shown: T[] } {
  const hidden = expanded ? 0 : Math.max(0, messages.length - VISIBLE_REPLIES)
  return { hidden, shown: messages.slice(hidden) }
}
