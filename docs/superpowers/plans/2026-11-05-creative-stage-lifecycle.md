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

- [ ] Add tests for current bound orchestrator, denied human/worker/old orchestrator/cross-run, required reason, exact revision, resubmit increment and concurrent approve/withdraw.
- [ ] Run go test ./internal/store -run CreativeWithdraw; expect missing method, then transition assertions until implemented.
- [ ] Implement authorization within creativeWrite lock and awaiting_approval -> in_progress preserving snapshot.
- [ ] Run go test ./internal/store; expect PASS. Commit.

### Task 2: API notification and contextual FYI

**Files:** internal/api/creative.go, creative_lifecycle.go, creative_lifecycle_test.go; internal/store/creative_sessions.go/tests.
**Interfaces:** API dispatches withdrawal to task 1 method using authenticated session ID; post-commit publisher receives full CreativeAction and committed CreativeRun. AddCreativeQuestion accepts existing FYI type as resolved notice, default decision unchanged.

- [ ] Add failing tests for decision queue metadata/current recipient, failed transitions, stage/item notifications, withdrawal authorization/FYI/event and enqueue failure after commit.
- [ ] Run go test ./internal/api -run CreativeLifecycle; expect assertions fail for missing notifications/withdrawal.
- [ ] Implement existing message queue enqueue/wake, action event outcome metadata and slog failure, and contextual FYI notice. Use shared server timestamp and existing actor attribution. Include sibling approve reminder.
- [ ] Run go test ./internal/api ./internal/store; expect PASS. Commit.

### Task 3: CLI, documentation, verification

**Files:** internal/cli/creative.go, creative_test.go; docs/creative-core-api.md.
**Interfaces:** action withdraw --review-revision N --reason TEXT serializes CreativeAction to existing endpoint.

- [ ] Add failing CLI request and missing reason/revision tests; run go test ./internal/cli -run Creative; expect failure.
- [ ] Implement flags/validation/help and document semantics/failure limits.
- [ ] Run go test ./... and go test -race ./internal/store ./internal/api ./internal/cli; expect PASS; inspect failures before changes.
- [ ] Commit; fresh branch review, address acceptance defects, push branch and open PR.
