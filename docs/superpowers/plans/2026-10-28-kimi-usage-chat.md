# Kimi usage and chat implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Read Kimi Code token usage and chat entries from native wire journals for Rocket's usage and chat views.

**Architecture:** Usage scans every native session whose state cwd matches the physical worktree, then streams each agent journal and sums only `usage.record` rows at or after `since`. Chat selects the latest current session with `findSession`, reads complete lines of its main journal from a byte offset, and returns message text and tool call digests. The cursor identifies that journal and offset so restore or truncation restarts cleanly.

**Tech Stack:** Go standard library, existing `agent` interfaces and `kimicode` wire helpers.

**Spec:** `docs/superpowers/specs/2026-10-28-kimi-code-agent-design.md` (v2); task and interfaces in `docs/superpowers/plans/2026-10-28-kimi-code-agent.md`.

## Global Constraints

- Keep changes inside `internal/agent/kimicode/usage.go`, `usage_test.go`, `chat.go`, `chat_test.go` and this task plan.
- Kimi home is `$KIMI_CODE_HOME`, default `~/.kimi-code`; tests use temporary homes and never write to the human's global Kimi files.
- Sum `usage.record` from all `agents/*/wire.jsonl` across matching sessions; exclude records before `since` and never add `subagent.completed.usage`.
- Read chat from `agents/main/wire.jsonl`; user and assistant text comes from `agent.message.appended`, tool calls get single-line digests of at most 120 runes, and the cursor is a file offset.
- Ignore malformed JSONL lines and handle lines larger than 64 KiB.

## Review Focus

- A restored process has an older journal in the same worktree: chat honors `ActivityRef.Since` and resets a stale cursor to the current session.
- The journal ends in a partial line: do not emit that line or advance past it until complete.
- A tool call appears in both loop events and appended assistant messages: emit one tool entry.
- A session may have a journal but no usage records: `Found` still reflects the current journal.
- Worktree paths may be symlinks: compare physical cwd.

---

### Task 1: UsageReader

**Files:** Create `internal/agent/kimicode/usage.go`, `usage_test.go`.

**Interfaces:** Produce `func (k *KimiCode) Usage(context.Context, string, time.Time) (agent.Usage, error)`; consume `kimiHome`, `agentWires`, native session `state.json`.

- [ ] Write fixture-based tests for per-model fields, message count, `since` filtering, all matching sessions, child journal inclusion without duplicate completion usage, empty and missing journals, and symlinked worktree.
- [ ] Run `go test ./internal/agent/kimicode -run '^TestUsage' -count=1`; observe the expected missing-interface failures.
- [ ] Implement session enumeration and streaming JSONL accounting; check context cancellation and return read errors for matching journals.
- [ ] Run the focused usage tests and inspect results.
- [ ] Commit usage code and tests.

### Task 2: TranscriptTail and TranscriptStat

**Files:** Create `internal/agent/kimicode/chat.go`, `chat_test.go`; remove the two stubs from `kimicode.go`.

**Interfaces:** Produce `TranscriptTail(context.Context, agent.ActivityRef, string) ([]agent.ChatEntry, string, error)` and `TranscriptStat(context.Context, agent.ActivityRef) (int64, int64, error)`.

- [ ] Write tests using `completed-tool-turn.wire.jsonl` for user and assistant text, one Bash digest, record timestamp, cursor resume, and stat size/mtime; add partial line, malformed line, restore cursor and no-journal tests.
- [ ] Run `go test ./internal/agent/kimicode -run '^TestTranscript' -count=1`; observe failures from the stubs.
- [ ] Implement forward complete-line reading, chat record mapping, cursor resolution, and stat.
- [ ] Run focused transcript tests and inspect results.
- [ ] Run `make test`, inspect its tail, and commit chat code, tests, and plan.

### Final review

- [ ] Request an independent code review against this plan, the v2 spec, and the task brief; resolve important findings with a failing test first.
- [ ] Run `make test` and check the exit code, `git diff --check`, and branch diff.
- [ ] Open a PR referencing feature `kimi-code-support`, task #5757 and subtask #5766, then send its URL to the orchestrator.
