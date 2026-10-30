# Pi Usage and Chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add complete per-model Pi token/cache accounting and branch-aware chat with resumable cursor and transcript stat.

**Architecture:** Usage forward-scans all regular journals for the physical worktree and tracks each journal's governing model. Chat locates the exact session, reconstructs its active branch with Task 2 helpers, and resumes from an opaque journal-path/record-ID cursor.

**Tech Stack:** Go standard library, existing `internal/agent` and `internal/agent/pi` helpers, Go `testing`.

**Spec:** `docs/superpowers/specs/2026-10-30-pi-usage-chat-design.md`; feature design `orch/pi-and-deepseek2:docs/superpowers/specs/2026-10-29-pi-deepseek-agents-design.md`.

## Global Constraints

- Keep changes inside `internal/agent/pi/usage.go`, `usage_test.go`, `chat.go`, `chat_test.go`, and the Task 4 design/plan documents.
- Consume `findWorktreeSessions`, `findSession`, `readTail`, and `readActiveBranch` from merged Task 2; do not change their behavior.
- Count assistant, standalone `usage`, `compaction`, and `branch_summary` usage once each across active and abandoned branches.
- Model IDs are `provider/model`; compaction and branch summaries inherit the latest preceding `model_change` in their journal.
- Skip malformed and trailing partial JSONL records; support records larger than 64 KiB and propagate real read/cancellation errors.
- Chat reads only the active branch. Cursor resets to that branch when its saved record is no longer present.
- No new dependencies, subagents, unrelated refactors, or global Pi settings changes.

## Review Focus

- A paid assistant response on an abandoned branch still contributes usage; `TestUsageIncludesAllRecordTypesAndBranches` pins it.
- Compaction and branch summaries have no direct model fields; the same test pins governing-model attribution and all five token fields.
- An empty journal has no models but is still found; `TestUsageMissingEmptyAndSince` pins it.
- Switching branches invalidates a cursor on the old branch; `TestTranscriptTailResumesAndResetsAcrossBranches` pins reset behavior without replaying abandoned entries.
- A large malformed/partial suffix must not hide earlier chat or usage; `TestUsageHandlesLargeAndMalformedRecords` and `TestTranscriptTailSkipsMalformedAndPartialRecords` pin it.

---

### Task 1: UsageReader

**Files:**
- Create: `internal/agent/pi/usage.go`
- Create: `internal/agent/pi/usage_test.go`

**Interfaces:**
- Consumes: `piHome()`, `findWorktreeSessions(home, worktree string) ([]string, error)`, `agent.UsageReader`.
- Produces: `func (p *Pi) Usage(ctx context.Context, worktreePath string, since time.Time) (agent.Usage, error)`.

- [ ] **Step 1: Write failing usage tests**

Add `var _ agent.UsageReader = (*Pi)(nil)` and fixture-based tests:

- `TestUsageIncludesAllRecordTypesAndBranches`: two sessions for one worktree contain a governing `model_change`, two assistant models, standalone usage, compaction, branch summary, and an abandoned-branch assistant. Assert exact per-model `Input`, `CacheRead`, `CacheWrite`, `Output`, `Reasoning`, and `Messages`; assert every record is counted once and the abandoned assistant remains included.
- `TestUsageMissingEmptyAndSince`: no sessions returns `Found=false`; an empty regular journal returns `Found=true` with no models; records before `since` are excluded while a record exactly at `since` remains, with exact `FirstAt`/`LastAt`.
- `TestUsageMatchesPhysicalWorktree`: a symlinked worktree reads the same journal; another worktree's journal is excluded.
- `TestUsageHandlesLargeAndMalformedRecords`: a valid record larger than 64 KiB, malformed JSON, and a trailing partial line do not hide valid usage.
- `TestUsageCanceledContext`: an already-cancelled context returns `context.Canceled`.

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `go test ./internal/agent/pi -run '^TestUsage' -count=1`
Expected: FAIL because `Pi.Usage` is not implemented.

- [ ] **Step 3: Implement usage accounting**

Implement `Pi.Usage` using `findWorktreeSessions(piHome(), worktreePath)`. Mark `Found` for each existing regular journal. Forward-read complete lines with `bufio.Reader`; decode record type, timestamp, direct provider/model, nested assistant usage, and top-level usage. Track the latest `model_change` per journal. Add every usage-bearing supported record to its model and update `Messages`, `FirstAt`, and `LastAt`; never add a record twice. Check context during enumeration and reading and wrap non-cancellation I/O errors with the journal path.

- [ ] **Step 4: Run focused and package tests, then commit**

Run: `go test ./internal/agent/pi -run '^TestUsage' -count=1`
Expected: PASS.
Run: `go test ./internal/agent/pi -count=1`
Expected: PASS.
Commit: `feat(pi): add complete token usage accounting`.

### Task 2: TranscriptTail and TranscriptStat

**Files:**
- Modify: `internal/agent/pi/chat.go`
- Create: `internal/agent/pi/chat_test.go`

**Interfaces:**
- Consumes: `findSession(home, worktree, sessionID string)`, `readActiveBranch(path string)`, `sessionRecord`.
- Produces: `TranscriptTail(context.Context, agent.ActivityRef, string) ([]agent.ChatEntry, string, error)` and `TranscriptStat(context.Context, agent.ActivityRef) (int64, int64, error)`.

- [ ] **Step 1: Write failing chat tests**

- `TestTranscriptTailReadsActiveBranchEntries`: user/assistant text becomes `user`/`assistant`; assistant `toolCall` becomes one `tool` entry with name and a compact single-line digest of at most 120 runes; system, toolResult, usage, compaction, branch_summary, and abandoned sibling records emit nothing; record timestamps become Unix seconds.
- `TestTranscriptTailShowsProviderError`: an assistant `stopReason=error` with `errorMessage` emits an assistant warning even with no text content.
- `TestTranscriptTailResumesAndResetsAcrossBranches`: a repeated cursor returns no entries and an unchanged cursor; an appended active-branch message returns only that message; when the latest leaf moves to a sibling that lacks the saved ID, the result restarts on the new active branch and excludes the abandoned branch.
- `TestTranscriptTailSkipsMalformedAndPartialRecords`: malformed and trailing partial records are ignored without losing earlier valid entries or preventing later resume.
- `TestTranscriptStatReportsExactSession`: exact session ID wins over newer foreign sessions; returned size/mtime match `os.Stat`.
- `TestTranscriptNoJournalReturnsNoSignal`: both methods return `agent.ErrNoSignal`; already-cancelled contexts return `context.Canceled`.

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `go test ./internal/agent/pi -run '^TestTranscript' -count=1`
Expected: FAIL from the Task 2 no-signal stubs.

- [ ] **Step 3: Implement active-branch chat and stat**

Resolve `findSession(piHome(), ref.WorktreePath, ref.SessionID)`. For chat, parse cursor as path plus last record ID, reconstruct the active branch, skip through a matching saved ID, otherwise start at its beginning. Map user/assistant text, assistant tool calls, and provider errors in record/content order. Return `<path>:<last-processed-record-id>`; advance over valid records even when they emit no entry. For stat, return the exact journal's `ModTime().Unix()` and size; missing/non-regular files are `agent.ErrNoSignal`.

- [ ] **Step 4: Run focused and regression tests, then commit**

Run: `go test ./internal/agent/pi -run '^TestTranscript' -count=1`
Expected: PASS.
Run: `go test ./internal/agent/pi ./internal/agent -count=1`
Expected: PASS.
Commit: `feat(pi): add branch-aware chat transcript`.

### Task 3: Verification and Delivery

**Files:**
- Modify only if verification exposes a Task 4 defect: files above.

- [ ] **Step 1: Run formatting and focused/full checks**

Run: `gofmt -w internal/agent/pi/usage.go internal/agent/pi/usage_test.go internal/agent/pi/chat.go internal/agent/pi/chat_test.go`
Run: `go test ./internal/agent/pi ./internal/agent -count=1`
Run: `make test`
Run: `git diff --check`
Expected: all PASS. Inspect `git status` and remove only artifacts created by these checks.

- [ ] **Step 2: Self-review scope and behavior**

Compare the complete diff with every Task 4 checkbox. Confirm all four usage shapes and all branches, exact model attribution, no double counting, active-branch-only chat, safe resume/reset, no dependencies, and no unrelated changes. Use superpowers:systematic-debugging for any failure before changing code.

- [ ] **Step 3: Open one PR and report**

Push `feature/pi-and-deepseek2/pi-usage-chat` and open one PR referencing feature `pi-and-deepseek2` and subtask #5877; do not merge. Send the orchestrator the PR URL and concise verification summary, and follow CI/review feedback to green.
