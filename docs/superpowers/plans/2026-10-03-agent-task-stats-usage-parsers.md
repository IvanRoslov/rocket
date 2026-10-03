# Agent Task Stats Usage Parsers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `agent.UsageReader` to Claude Code and Codex adapters so the collector can read per-model token usage from all transcripts of a Rocket session.

**Architecture:** The shared `agent` package defines token counts and the optional reader interface. Each adapter discovers its own transcript files, streams JSONL records, and returns per-model totals and transcript timestamps without changing the existing `Agent` interface.

**Tech Stack:** Go standard library, existing agent adapters, Go tests with temporary transcript fixtures.

**Spec:** `docs/superpowers/specs/2026-10-03-agent-task-stats-design.md` in PR #128 (`orch/agent-task-stats`); feature plan: `docs/superpowers/plans/2026-10-03-agent-task-stats.md` in the same PR. This branch implements W1 only.

## Global Constraints

- Actual model IDs come from `message.model` or `turn_context.model`, never the launch profile.
- Claude reads all top-level JSONL transcripts and their `*/subagents/*.jsonl` children, deduplicates by `message.id`, and excludes `<synthetic>`.
- Codex scans date shards from `since` through today, uses cumulative `info.total_token_usage` deltas, and splits input into uncached input and cached input.
- Invalid JSONL records are skipped; an unreadable matching file returns an error and no partial result.
- JSONL scanning supports records up to 16 MiB without loading the entire file.

## Review Focus

1. Claude streaming records with the same message ID and changing usage must contribute only the final observed usage. Test in Task 1.
2. A Claude subagent transcript without its parent session transcript must not be attributed to the worktree. Test in Task 1.
3. An unreadable matching transcript must return an error without partial usage. Test in each adapter task.
4. Codex repeated cumulative totals must not add tokens or inflate message count. Test in Task 2.
5. Codex totals that reset within a file must not produce negative deltas. Test in Task 2.

---

### Task 1: Shared contract and Claude Code reader

**Files:**
- Modify: `internal/agent/agent.go`
- Create: `internal/agent/claudecode/usage.go`
- Create: `internal/agent/claudecode/usage_test.go`
- Create: `internal/agent/claudecode/testdata/usage/*.jsonl`

**Interfaces:**
- Produces: `agent.Tokens{Input, CacheWrite, CacheRead, Output, Reasoning, Messages int64}`, `agent.Usage{Models map[string]Tokens, FirstAt, LastAt time.Time, Found bool}`, and `agent.UsageReader.Usage(context.Context, string, time.Time) (Usage, error)`.
- Implements: `(*claudecode.ClaudeCode).Usage(context.Context, string, time.Time) (agent.Usage, error)`.

- [ ] **Step 1: Write failing Claude tests.** Fixture records cover repeated message IDs with updated usage, two models, a child subagent, two top-level files, foreign cwd, `<synthetic>`, malformed JSON, `since`, transcript timestamps, and missing directory. A separate unreadable file test requires an error and zero result.
- [ ] **Step 2: Verify RED.** Run `go test ./internal/agent/claudecode -run '^TestUsage' -count=1`; expect build failure because `Usage` is not defined.
- [ ] **Step 3: Add the shared types and Claude implementation.** Reuse `transcriptDir`; determine eligible parent files by cwd, then stream their records and associated subagents. Keep a global map by message ID and sum its final usage per model. Use `bufio.Scanner` with a 16 MiB limit and check `ctx.Err()` in scan loops.
- [ ] **Step 4: Verify GREEN.** Run `go test ./internal/agent/claudecode -run '^TestUsage' -count=1`; expect all Usage tests to pass.
- [ ] **Step 5: Commit.** `git add internal/agent/agent.go internal/agent/claudecode/usage.go internal/agent/claudecode/usage_test.go internal/agent/claudecode/testdata/usage && git commit -m 'feat(usage): read Claude Code transcript tokens'`.

### Task 2: Codex reader and adapter documentation

**Files:**
- Create: `internal/agent/codex/usage.go`
- Create: `internal/agent/codex/usage_test.go`
- Create: `internal/agent/codex/testdata/usage/*.jsonl`
- Modify: `docs/10-agents.md`

**Interfaces:**
- Consumes: `agent.Usage`, `agent.Tokens`, and `agent.UsageReader` from Task 1.
- Implements: `(*codex.Codex).Usage(context.Context, string, time.Time) (agent.Usage, error)`.

- [ ] **Step 1: Write failing Codex tests.** Fixture records cover cumulative totals, cached input subtraction, model change, repeated total, two files with the same cwd, foreign cwd, date before `since`, malformed JSON, transcript timestamps, missing directory, and unreadable matching file. Include a nonnegative reset case.
- [ ] **Step 2: Verify RED.** Run `go test ./internal/agent/codex -run '^TestUsage' -count=1`; expect build failure because `Usage` is not defined.
- [ ] **Step 3: Implement Codex reader and document both readers.** Walk day shards from `since` through today, match `session_meta.payload.cwd`, stream each matching rollout, attribute each cumulative delta to its preceding `turn_context.model`, and add `UsageReader` behavior to `docs/10-agents.md`.
- [ ] **Step 4: Verify GREEN.** Run `go test ./internal/agent/codex -run '^TestUsage' -count=1`; expect all Usage tests to pass.
- [ ] **Step 5: Commit.** `git add internal/agent/codex/usage.go internal/agent/codex/usage_test.go internal/agent/codex/testdata/usage docs/10-agents.md && git commit -m 'feat(usage): read Codex rollout tokens'`.

### Task 3: Integration verification

**Files:** No new files.

**Interfaces:** Confirms both adapters satisfy `agent.UsageReader` and the full agent package suite remains green.

- [ ] **Step 1: Run `go test ./internal/agent/... -count=1`.** Expect zero failures.
- [ ] **Step 2: Run `go test ./... -count=1`.** Expect zero failures.
- [ ] **Step 3: Run `gofmt -l` on changed Go files and `git diff --check`.** Expect no output.
- [ ] **Step 4: Review the diff against W1 requirements and create the PR.** Reference feature `agent-task-stats` and task #5139.
