// Helpers for the chat screen's tool-call rows (docs/13-chat.md `role:
// "tool"` entries): turning a raw JSON-digest `text` (≤120 runes, possibly
// truncated with a trailing "…") into a human-readable one-liner, and
// collapsing runs of consecutive tool entries in the feed into a single
// expandable group. Both are pure — no React, no DOM — so they're covered by
// plain unit tests; ChatScreen.tsx wires them into components.

/** Human-readable pieces pulled out of a tool entry's raw JSON-ish text. */
export interface ToolDigest {
  /** `command` field, unescaped and with newlines rendered as `⏎` — shown monospace. */
  command?: string
  /** `basename(file_path)` — the full path stays available via `filePath` for a title attribute. */
  fileName?: string
  filePath?: string
  /** `description` field, shown muted alongside `command`/`fileName` when present. */
  description?: string
  /** Fallback: neither `command` nor `file_path` was found — show the raw text as-is. */
  raw?: string
}

function unescapeJsonString(value: string): string {
  return value.replace(/\\(.)/g, (_, ch: string) => {
    switch (ch) {
      case 'n':
        return '\n'
      case 't':
        return '\t'
      case '"':
        return '"'
      case '\\':
        return '\\'
      default:
        return ch
    }
  })
}

/** Renders control characters visibly for a single display line. */
function toDisplayLine(value: string): string {
  return value.replace(/\n/g, '⏎').replace(/\t/g, '  ')
}

function basename(path: string): string {
  const parts = path.split('/')
  return parts[parts.length - 1] || path
}

// Tolerant of a missing closing quote (truncated mid-value): the trailing
// `"` is optional, so a cut-off value is captured up to the end of `text`
// rather than failing to match at all.
const COMMAND_RE = /"command"\s*:\s*"((?:\\.|[^"\\])*)"?/
const FILE_PATH_RE = /"file_path"\s*:\s*"((?:\\.|[^"\\])*)"?/
const DESCRIPTION_RE = /"description"\s*:\s*"((?:\\.|[^"\\])*)"?/

function extract(re: RegExp, text: string): string | undefined {
  const match = re.exec(text)
  return match ? unescapeJsonString(match[1]) : undefined
}

/**
 * Extracts a human digest from a tool entry's `text`. `text` is often a
 * *truncated* JSON blob (server caps it at ~120 runes and appends "…"), so a
 * full `JSON.parse` is tried first and, on failure, the same fields are
 * pulled out with tolerant regexes instead.
 */
export function summarizeToolEntry(toolName: string, text: string): ToolDigest {
  void toolName
  const value = text ?? ''
  if (!value) return {}

  let parsed: Record<string, unknown> | undefined
  try {
    const obj: unknown = JSON.parse(value)
    if (obj && typeof obj === 'object' && !Array.isArray(obj)) parsed = obj as Record<string, unknown>
  } catch {
    parsed = undefined
  }

  const command = typeof parsed?.command === 'string' ? parsed.command : extract(COMMAND_RE, value)
  const filePath = typeof parsed?.file_path === 'string' ? parsed.file_path : extract(FILE_PATH_RE, value)
  const description =
    typeof parsed?.description === 'string' ? parsed.description : extract(DESCRIPTION_RE, value)

  if (command !== undefined) {
    return { command: toDisplayLine(command), description: description ? toDisplayLine(description) : undefined }
  }
  if (filePath !== undefined) {
    return {
      fileName: basename(filePath),
      filePath,
      description: description ? toDisplayLine(description) : undefined,
    }
  }
  return { raw: value }
}

// ---------------------------------------------------------------------------
// Grouping consecutive tool entries in the feed
// ---------------------------------------------------------------------------

/** One feed position, generic over whatever item type the caller renders. */
export interface GroupableEntry<T> {
  kind: 'item'
  key: string
  isTool: boolean
  value: T
}

export interface ToolGroup<T> {
  kind: 'tool-group'
  key: string
  items: GroupableEntry<T>[]
}

export type GroupedFeedItem<T> = GroupableEntry<T> | ToolGroup<T>

/**
 * Collapses runs of *adjacent* `isTool` items into a single `ToolGroup`.
 * Anything non-tool (optimistic bubbles, user/assistant/system rows) breaks
 * a run by its position in `items` — the caller is expected to have already
 * interleaved optimistic messages into the feed before calling this. A run
 * of exactly one tool item is left ungrouped (rendered inline, no wrapper),
 * per spec.
 */
export function groupChatEntries<T>(items: GroupableEntry<T>[]): GroupedFeedItem<T>[] {
  const result: GroupedFeedItem<T>[] = []
  let run: GroupableEntry<T>[] = []

  function flush() {
    if (run.length === 0) return
    if (run.length === 1) {
      result.push(run[0])
    } else {
      result.push({ kind: 'tool-group', key: `group-${run[0].key}`, items: run })
    }
    run = []
  }

  for (const item of items) {
    if (item.isTool) {
      run.push(item)
    } else {
      flush()
      result.push(item)
    }
  }
  flush()
  return result
}
