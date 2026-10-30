# Pi usage and chat design

Task: #5877, feature `pi-and-deepseek2` (#5867).

## Intent

Read actual Pi token/cache usage and the current conversation from native session journals. Usage is accounting for work already paid for; chat is a view of the branch currently in context. These different goals require different record selection.

## Goals

- Sum assistant-message, standalone `usage`, `compaction`, and `branch_summary` usage once per journal record, per actual model, across every session for the physical worktree and across active and abandoned branches.
- Map `input`, `cacheRead`, `cacheWrite`, `output`, and `reasoning` to the corresponding `agent.Tokens` fields and count each usage-bearing record once in `Messages`.
- Attribute assistant and standalone records from their direct provider/model fields; attribute compaction and branch summaries to the governing `model_change`.
- Exclude records before `since`, preserve first/last record timestamps, report empty existing journals as found, skip malformed/partial lines, and return real I/O or cancellation errors.
- Read user text, assistant text, assistant tool calls, and provider errors from the active branch only. Resume after a cursor without duplicate entries and report the exact session journal's size and mtime.

## Non-goals

- No activity, delivery, catalog, launch, UI, database, or price changes.
- No deduplication of distinct paid operations and no removal of abandoned-branch usage.
- No new dependencies or global Pi configuration changes.

## Approaches

1. **Full usage scan plus active-branch record-ID cursor (chosen).** Usage streams every matching journal from the start. Chat uses Task 2's `readActiveBranch`; the cursor contains the journal path and last processed record ID. This handles branch switches and avoids counting records twice.
2. **Active-branch-only usage plus byte-offset chat.** Rejected: it loses paid work on abandoned branches, and a byte offset cannot represent a changed branch cleanly.
3. **Whole-file chat replay on every poll.** Rejected: it duplicates entries and is unnecessarily expensive.

## Cursor behavior

The cursor is `<journal-path>:<record-id>`. If its path and ID occur in the current active branch, only later records are emitted. Otherwise chat restarts from the beginning of the current active branch. The cursor advances over every valid branch record, including records that produce no chat entry, so repeated polls stay empty until new work appears. A missing journal returns `agent.ErrNoSignal`.

## Builder calls

- Tool results are not rendered as separate entries; assistant `toolCall` parts produce one bounded single-line digest each.
- Chat timestamps come from the journal record's ISO timestamp.
- Compaction and branch-summary records do not themselves become chat messages.
