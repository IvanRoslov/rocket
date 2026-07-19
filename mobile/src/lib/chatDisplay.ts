// The agent transcript stores every injected message with role="user":
// real human replies, inter-agent mail ("[from worker] …"), question-funnel
// deliveries, and harness noise (<task-notification>, <system-reminder>…).
// This classifier decides how the chat screen renders each of them.

export type UserDisplay =
  | { kind: 'human' }
  /** Message injected from another session (worker/daemon) — shown as a left bubble. */
  | { kind: 'agent'; from: string; body: string }
  /** Harness/system injection — collapsed into a dim expandable row. */
  | { kind: 'system'; label: string; body: string }

export function classifyUserEntry(text: string): UserDisplay {
  const t = text.trimStart()

  const from = t.match(/^\[from ([^\]]+)\]\s*([\s\S]*)$/)
  if (from) return { kind: 'agent', from: from[1], body: from[2] }

  const qm = t.match(/^\[task #(\d+) QM (reply|answer)\]\s*([\s\S]*)$/)
  if (qm) return { kind: 'system', label: `Q&A · task #${qm[1]} ${qm[2]}`, body: qm[3] }

  if (t.startsWith('[heartbeat')) return { kind: 'system', label: 'heartbeat', body: t }
  if (t.startsWith('[large message]')) return { kind: 'system', label: 'large message pointer', body: t }

  // XML-ish harness wrappers: <task-notification>, <system-reminder>,
  // <command-name>, <local-command-stdout>, …
  const tag = t.match(/^<([a-z][a-z0-9_-]*)[\s>]/i)
  if (tag) return { kind: 'system', label: tag[1], body: t }

  if (t.startsWith('Caveat:')) return { kind: 'system', label: 'caveat', body: t }

  return { kind: 'human' }
}
