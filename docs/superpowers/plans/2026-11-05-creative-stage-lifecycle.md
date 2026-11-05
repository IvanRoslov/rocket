# Creative stage lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Notify the orchestrator of committed reviews and let only the current orchestrator withdraw an exact pending review.
**Architecture:** Keep transition locking in the store; integrate post-commit queue/FYI outcomes into existing action events. Preserve existing delivery and main Rocket behavior.
**Tech Stack:** Go, SQLite, HTTP, Cobra.
**Spec:** docs/superpowers/specs/2026-11-05-creative-stage-lifecycle-design.md

## Global Constraints

No live changes, replay, second transport, dashboard, or gate engine. Approval remains human-only. One PR for creative-work. Notifications are queued, never described as delivered.

## Review Focus

- Replacement orchestrator: authorization uses current binding under lock (task 1).
- Concurrent approve/withdraw: exactly one success (task 1).
- Queue insertion fails: decision remains committed; action event/log records error (task 2).
- FYI status/context: resolved informational discussion without permission request (task 2).
- Historical or unsuccessful actions: no replay or spurious notification (task 2).

### Task 1: Store withdrawal

**Files:** internal/store/creative_progress.go, creative_types.go, creative_progress_test.go.
**Interfaces:** WithdrawCreativeReview(runID, stageID, itemID int64, action CreativeAction, callerID string) (CreativeRun, error); CreativeAction.Reason string. Shared internal transition function preserves ApplyCreativeAction behavior.

- [x] Add tests for current bound orchestrator, denied human/worker/old orchestrator/cross-run, required reason, exact revision, resubmit increment and concurrent approve/withdraw.
- [x] Run go test ./internal/store -run CreativeWithdraw; expect missing method, then transition assertions until implemented.
- [x] Implement authorization within creativeWrite lock and awaiting_approval -> in_progress preserving snapshot.
- [x] Run go test ./internal/store; expect PASS. Commit.

### Task 2: API notification and contextual FYI

**Files:** internal/api/creative.go, creative_lifecycle.go, creative_lifecycle_test.go; internal/store/creative_sessions.go/tests.
**Interfaces:** API dispatches withdrawal to task 1 method using authenticated session ID; post-commit publisher receives full CreativeAction and committed CreativeRun. AddCreativeQuestion accepts existing FYI type as resolved notice, default decision unchanged.

- [x] Add failing tests for decision queue metadata/current recipient, failed transitions, stage/item notifications, withdrawal authorization/FYI/event and enqueue failure after commit.
- [x] Run go test ./internal/api -run CreativeLifecycle; expect assertions fail for missing notifications/withdrawal.
- [x] Implement existing message queue enqueue/wake, action event outcome metadata and slog failure, and contextual FYI notice. Use shared server timestamp and existing actor attribution. Include sibling approve reminder.
- [x] Run go test ./internal/api ./internal/store; expect PASS. Commit.

### Task 3: CLI, documentation, verification

**Files:** internal/cli/creative.go, creative_test.go; docs/creative-core-api.md.
**Interfaces:** action withdraw --review-revision N --reason TEXT serializes CreativeAction to existing endpoint.

- [x] Add failing CLI request and missing reason/revision tests; run go test ./internal/cli -run Creative; expect failure.
- [x] Implement flags/validation/help and document semantics/failure limits.
- [x] Run go test ./... and go test -race ./internal/store ./internal/api ./internal/cli; expect PASS; inspect failures before changes.
- [x] Commit; fresh branch review, address acceptance defects, push branch and open PR.


## Verification record

Implemented with store/API/CLI RED→GREEN tests and small commits. Rebased cleanly onto sibling worker-close PR #200 (`f61e676`). Independent fresh branch review found no acceptance defects or deferred findings.

- `make test` passed after integration: Go suite, 722 main web tests, 53 Creative web tests.
- `go test -race ./internal/store ./internal/api ./internal/cli` passed before the clean sibling rebase.
- `go test -race ./internal/store ./internal/api ./internal/cli -run 'Creative(Withdraw|Lifecycle|CLI|Close)' -count=1` passed after integration.
- `go vet ./...` and `git diff --check` passed after integration.
- Initial broad run hit the unchanged Claude hook fixture's three-second timeout under concurrent race load. Isolated fixture and subsequent full suites passed unchanged.

No live config/session changes, real-run evaluation, deployment/restart or paid calls. Human owns deployment and real evaluation. Existing post-commit failure limits are documented in the API reference.
