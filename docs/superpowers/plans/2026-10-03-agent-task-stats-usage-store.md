# Agent Task Stats Usage Store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist per-session token usage, collection state, model prices, and permanent session-to-task links for task #5138.

**Architecture:** Migration 0023 adds three tables and two nullable session keys. Store methods own transactional replacement, retry selection, historical task resolution, and flat read rows for the later collector and API work packages. Session creation stores task keys before launch so lifecycle failures still retain the link.

**Tech Stack:** Go, `database/sql`, `modernc.org/sqlite`, Go tests.

**Spec:** `docs/superpowers/specs/2026-10-03-agent-task-stats-design.md` in feature PR #128; parent plan `docs/superpowers/plans/2026-10-03-agent-task-stats.md`, W2.

## Global Constraints

- `billable = input + cache_write + output`; cache read stays separate.
- `reasoning` is a subset of output and has no separate price.
- Prices start empty and are keyed by exact transcript model ID.
- Only migration `internal/store/migrations/0023_session_usage.sql` changes schema.
- Zero-valued task IDs in Go map to nullable SQLite columns.

## Review Focus

- Migrating a database with existing sessions retains their rows and nullable task columns.
- A second usage collection removes prior model rows and replaces stats atomically.
- Historical worker links survive a respawn that overwrote `tasks.session_id`.
- Terminal `missing` and final `ok` sessions do not enter ordinary retry batches; errors retry after one hour at most three times.
- Date filtering uses `ended_at` for final rows and `collected_at` for live snapshots, including both endpoints.

---

### Task 1: Schema and session task keys

**Files:** migration 0023; `internal/store/sessions.go`; `internal/store/usage_test.go`.

**Interfaces:** `Session.TaskID, Session.SubtaskID int64`; `AddSession`, `GetSession`, `ListSessions`, and `UpdateSession` round-trip them.

- [ ] Write a test that migrates a populated version-22 database, retains its session, and observes all new tables and nullable keys; write a session CRUD round-trip test for nonzero task keys.
- [ ] Run focused tests and observe missing schema/fields.
- [ ] Add the exact §1 tables, indexes, ALTERs, and session SQL changes.
- [ ] Run focused tests; commit migration and session changes.

### Task 2: Collection state and historical resolution

**Files:** `internal/store/usage.go`, `usage_test.go`.

**Interfaces:** `SessionStats`, `UsageTokens`, `ModelUsage`, `ReplaceSessionUsage`, `GetSessionStats`, `SessionsNeedingUsage`, `ResolveSessionTask` as in parent plan W2.

- [ ] Write tests for replace-twice atomicity, retry selection, and resolution from direct key, task link, and spawn log.
- [ ] Run focused tests and observe missing methods.
- [ ] Implement transaction, getters, selector, and task resolution; persist resolved task keys for future reads.
- [ ] Run focused tests; commit collection methods.

### Task 3: Read rows and prices

**Files:** `internal/store/usage.go`, `usage_test.go`.

**Interfaces:** `UsageRow`, `ModelPrice`, `UsageRows`, `TaskUsageRows`, `CountPendingUsage`, `ListModelPrices`, `UpsertModelPrice`, `DeleteModelPrice`, `UsageModels` as in parent plan W2. `UsageRow` includes session metadata and task/subtask titles and status for W4.

- [ ] Write tests for date boundaries, snapshot date, project scope, task rows including running sessions, pending count, and nullable price CRUD.
- [ ] Run focused tests and observe missing methods.
- [ ] Implement the flat joins, pending count, and price methods.
- [ ] Run focused tests; commit read methods.

### Task 4: Launch wiring and documentation

**Files:** `internal/api/sessions.go`, `internal/session/manager.go`, existing API/session tests, `docs/05-state.md`.

**Interfaces:** `session.SpawnReq.TaskID int64`; API supplies root and subtask IDs; `SpawnOrchestrator` stores its root task ID.

- [ ] Extend API and manager tests to assert persisted task IDs on spawned workers and orchestrators; run and observe failure.
- [ ] Wire IDs through `SpawnReq` and `Session`; run focused tests.
- [ ] Document schema and collection semantics; run `go test ./...`, `go vet ./...`, and an end-to-end migration/CRUD exercise.
- [ ] Commit and create the task PR, naming the `UsageRow` contract in its description.
