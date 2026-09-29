// The agent transcript stores every injected message with role="user":
// real human replies, inter-agent mail ("[from worker] …"), Q&A thread
// deliveries ("[#1023/Q2 reply from cto] …"), daemon notices ("[rocket …]")
// and harness noise (<task-notification>, <system-reminder>…). The daemon
// already unwraps the socket envelope (docs/13-chat.md), so the prefix is
// always at the start. This classifier decides how the chat renders each.

export type UserDisplay =
  | { kind: 'human' }
  /** Message injected from another session (worker/daemon) — shown as a left bubble. */
  | { kind: 'agent'; from: string; body: string }
  /** A Q&A thread entry delivered into the session — a visible compact row. */
  | { kind: 'thread'; ref: string; label: string; body: string }
  /** Harness/system injection — collapsed into a dim expandable row, hidden as noise. */
  | { kind: 'system'; label: string; body: string }

/** internal/api/threads.go threadPrefix: `[#1023/Q2 reply from cto]`, `[cto/Q1 answer from human]`. */
const THREAD_FRAME_RE = /^\[(#?[\w.-]+\/Q\d+) ([a-z-]+ from [^\]]+)\]\s*([\s\S]*)$/

export function classifyUserEntry(text: string): UserDisplay {
  const t = text.trimStart()

  const from = t.match(/^\[from ([^\]]+)\]\s*([\s\S]*)$/)
  if (from) return { kind: 'agent', from: from[1], body: from[2] }

  const frame = t.match(THREAD_FRAME_RE)
  if (frame) return { kind: 'thread', ref: frame[1], label: frame[2], body: frame[3] }

  const qm = t.match(/^\[task #(\d+) QM (reply|answer)\]\s*([\s\S]*)$/)
  if (qm) return { kind: 'system', label: `Q&A · task #${qm[1]} ${qm[2]}`, body: qm[3] }

  const rocket = t.match(/^\[(rocket[^\]]*)\]\s*([\s\S]*)$/)
  if (rocket) return { kind: 'system', label: rocket[1], body: rocket[2] }

  if (t.includes('[SYSTEM NOTIFICATION - NOT USER INPUT]')) {
    return { kind: 'system', label: 'system notification', body: t }
  }
  if (t.startsWith('[heartbeat')) return { kind: 'system', label: 'heartbeat', body: t }
  if (t.startsWith('[large message]')) return { kind: 'system', label: 'large message pointer', body: t }

  // XML-ish harness wrappers: <task-notification>, <system-reminder>,
  // <command-name>, <local-command-stdout>, …
  const tag = t.match(/^<([a-z][a-z0-9_-]*)[\s>]/i)
  if (tag) return { kind: 'system', label: tag[1], body: t }

  if (t.startsWith('Caveat:')) return { kind: 'system', label: 'caveat', body: t }

  return { kind: 'human' }
}
