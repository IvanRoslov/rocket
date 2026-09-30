// Pure derivations behind the Questions tab — a function of GET /v1/threads,
// no React, so ordering can be tested directly. Same rules as the web
// dashboard's Decide queue (web/src/screens/questions/model.ts).
import type { ThreadInboxEntry } from '../api/types'

const TITLE_MAX = 80

/** Open decision threads waiting on you: stale first, then longest without movement first. */
export function waitingOnYou(threads: ThreadInboxEntry[]): ThreadInboxEntry[] {
  return threads
    .filter((t) => t.status === 'open' && t.your_turn)
    .sort((a, b) => Number(b.stale ?? false) - Number(a.stale ?? false) || a.updated_at - b.updated_at)
}

/** Other open decision threads — waiting on someone else; newest movement first. */
export function otherOpen(threads: ThreadInboxEntry[]): ThreadInboxEntry[] {
  return threads
    .filter((t) => t.status === 'open' && !t.your_turn && t.type !== 'fyi')
    .sort((a, b) => b.updated_at - a.updated_at)
}

/** Where a thread lives: the line above the card and the screen it opens. */
export function threadSource(t: ThreadInboxEntry): { label: string; href: string } {
  if (t.kind === 'role' && t.role_id) return { label: `agent ${t.role_id}`, href: `/agent/${t.role_id}` }
  const title = t.task_title ? ` · ${t.task_title}` : ''
  return { label: `#${t.task_id}${title}`, href: `/task/${t.task_id}` }
}

/** The heading: `title`, else the first non-empty body line cut on a word at 80 chars. */
export function questionTitle(q: { title?: string; body: string }): string {
  const title = q.title?.trim()
  if (title) return title
  const line = q.body.split('\n').find((l) => l.trim() !== '')?.trim() ?? ''
  if (line.length <= TITLE_MAX) return line
  const cut = line.slice(0, TITLE_MAX)
  const space = cut.lastIndexOf(' ')
  return `${(space > 0 ? cut.slice(0, space) : cut).trimEnd()}…`
}

/** The agent's plain-language brief, trimmed; `''` when there is none (old daemon, old or human-opened thread). */
export function questionBrief(q: { brief?: string }): string {
  return q.brief?.trim() ?? ''
}

/**
 * One-line list preview: the brief flattened to plain text when there is one
 * — it is the part written to be read first — else the body, as before.
 */
export function questionPreview(q: { brief?: string; body: string }): string {
  const brief = questionBrief(q)
  if (!brief) return q.body
  return brief
    .replace(/^\s{0,3}(#{1,6}\s+|>\s?|[-*+]\s+|\d+[.)]\s+)/gm, '')
    .replace(/\*\*|__|`/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}
