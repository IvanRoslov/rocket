// Pure derivations behind the Questions v3 screen.
//
// Everything here is a function of `GET /v1/threads` plus local session state,
// with no React and no fetching, so the ordering and grouping rules that make
// or break the screen can be tested directly. The daemon deliberately does not
// sort or group the inbox: what the human should look at next is a dashboard
// decision, not a storage one.

import { isHuman, participantLabel } from '../../lib/participants'
import type { ThreadInboxEntry } from '../../lib/types'

/** How much of a resolution fits in the closed chip before it is elided. */
const CHIP_RESOLUTION_MAX = 30

/**
 * The Decide queue: open threads waiting on you, worst first.
 *
 * Order is stale-first (the daemon derives `stale` from the configurable
 * `question_stale_after`, so never recompute it here), then longest without
 * movement first — the queue is a work order, not a feed.
 *
 * `later` holds the ids the human pushed back with S. They drop out of the
 * queue, but if that would empty it they all come back: an empty rail would
 * read as "Queue clear" when in fact nothing has been decided.
 */
export function queueOf(
  threads: ThreadInboxEntry[],
  later: ReadonlySet<number>,
): ThreadInboxEntry[] {
  const mine = threads
    .filter((t) => t.status === 'open' && t.your_turn)
    .sort((a, b) => Number(b.stale ?? false) - Number(a.stale ?? false) || a.updated_at - b.updated_at)
  const active = mine.filter((t) => !later.has(t.id))
  return active.length > 0 ? active : mine
}

export type ChipTone = 'turn' | 'waiting' | 'closed' | 'note'

export interface StatusChip {
  label: string
  tone: ChipTone
}

/** The one-line status of a thread: whose turn it is, or how it ended. */
export function statusChip(entry: ThreadInboxEntry): StatusChip {
  if (entry.status !== 'open') {
    if (entry.type === 'fyi') return { label: 'note', tone: 'note' }
    const resolution = entry.resolution ?? ''
    const short =
      resolution.length > CHIP_RESOLUTION_MAX
        ? `${resolution.slice(0, CHIP_RESOLUTION_MAX)}…`
        : resolution
    return { label: `closed: ${short}`, tone: 'closed' }
  }
  if (entry.your_turn) return { label: 'your turn', tone: 'turn' }
  // `?? []`, not a bare `.find`: an older daemon serialises an empty attention
  // set as `null`, and that once crashed the whole route (commit f2f073e).
  const other = (entry.attention ?? []).find((p) => !isHuman(p))
  return { label: `waiting on ${other ? participantLabel(other) : 'nobody'}`, tone: 'waiting' }
}

/** Free-text search over everything a human would type: ref, subject, body. */
export function matchesQuery(entry: ThreadInboxEntry, query: string): boolean {
  const q = query.trim().toLowerCase()
  if (!q) return true
  const haystack = [entry.local_ref, entry.subject, entry.task_title ?? '', entry.body]
    .join(' ')
    .toLowerCase()
  return haystack.includes(q)
}

export type BrowseFilter = 'mine' | 'open' | 'closed' | 'all'

export interface BrowseGroup {
  label: string
  /** The line under the label explaining what the group means. */
  sub: string
  tone: 'turn' | 'waiting' | 'closed'
  rows: ThreadInboxEntry[]
}

/**
 * Browse mode's three groups, in reading order: what blocks you, what blocks
 * an agent, and history. A group with no rows is dropped rather than rendered
 * empty — an empty "Your turn" heading reads as an unanswered question.
 */
export function browseGroups(
  threads: ThreadInboxEntry[],
  filter: BrowseFilter,
  query: string,
): BrowseGroup[] {
  const shown = threads.filter((t) => matchesQuery(t, query))
  const mine = queueOf(shown, new Set())
  const others = shown.filter((t) => t.status === 'open' && !t.your_turn)
  const closed = shown.filter((t) => t.status !== 'open')

  const groups: BrowseGroup[] = []
  if (filter !== 'closed' && mine.length > 0) {
    groups.push({
      label: 'Your turn',
      sub: 'nothing moves until you answer',
      tone: 'turn',
      rows: mine,
    })
  }
  if (filter !== 'closed' && filter !== 'mine' && others.length > 0) {
    groups.push({
      label: 'Waiting on agents',
      sub: 'an agent has the turn',
      tone: 'waiting',
      rows: others,
    })
  }
  if ((filter === 'closed' || filter === 'all') && closed.length > 0) {
    groups.push({
      label: 'Closed & notes',
      sub: 'history, including fyi',
      tone: 'closed',
      rows: closed,
    })
  }
  return groups
}

/** The counts behind the four browse filter chips. */
export function browseCounts(
  threads: ThreadInboxEntry[],
  query: string,
): Record<BrowseFilter, number> {
  const shown = threads.filter((t) => matchesQuery(t, query))
  const mine = shown.filter((t) => t.status === 'open' && t.your_turn).length
  const others = shown.filter((t) => t.status === 'open' && !t.your_turn).length
  const closed = shown.filter((t) => t.status !== 'open').length
  return { mine, open: mine + others, closed, all: mine + others + closed }
}
