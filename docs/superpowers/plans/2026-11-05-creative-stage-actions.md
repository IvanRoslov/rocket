# Creative stage action safety Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement task-by-task with TDD.

**Goal:** Confirm the exact shown submission, block duplicate interactions, and record successful stage/item actions.
**Architecture:** Local snapshot confirmation and handler guards in ReviewActions; existing API event bus after committed mutation.
**Tech Stack:** React, TypeScript, Vitest/MSW, Go, SQLite, Rocket bus.
**Spec:** docs/superpowers/specs/2026-11-05-creative-stage-actions-design.md (accepted by orchestrator).

## Global Constraints

No live changes, new audit system, personal identity invention, historical cause assertions, generation gates, downstream resets, transport changes or real-run mutation. Preserve development behavior and Creative approval semantics. Intentional later same-version submission remains possible.

## Review Focus

- Historical version shown while current is newer: submit only confirmed IDs.
- Context changes during confirmation: invalidate acceptance.
- Second click lands on replacement action: ignore duplicate sequence.
- Failed actions: no successful transition event; existing HTTP failure remains failure.
- Header absent: record explicitly unattributed caller, never named human.

### Task 1: UI confirmation and click safety

**Files:** creative-web/src/ReviewActions.tsx; creative-web/src/ReviewActions.test.tsx
**Interfaces:** Existing ReviewActions props and action API payload remain unchanged.

- [x] Write tests for confirmation displaying scope/name/version/ID, cancellation, empty set, stale snapshot rejection (scope/status/revision/versions/readiness), repeated click and synchronous in-flight suppression, replacement button sequence, and intentional later same-version submission.
- [x] Run `npm --prefix creative-web test -- ReviewActions.test.tsx`; observe missing confirmation/guards failures.
- [x] Add local confirmation bound to snapshot identity and synchronous in-flight ref through mutateAsync plus invalidation; ignore click detail > 1. No global debounce or generation gate.
- [x] Run creative frontend suite and build; commit UI deliverable.

### Task 2: Existing action events

**Files:** internal/api/creative.go; internal/api/creative_test.go
**Interfaces:** API continues ApplyCreativeAction; existing d.Bus.Publish persists server TS and broadcasts.

- [x] Write API tests asserting stage/item action events, resolved session or explicitly unattributed caller, committed review/version/status context and no successful event after denied/invalid/conflicting mutation. Cover all five actions and retained versions on resubmission.
- [x] Run `go test ./internal/api -run Creative`; observe missing event failures.
- [x] Publish creative.stage_action / creative.item_action from returned committed target after success. Store only proven session ID and explicit attribution source; no body-supplied actor.
- [x] Run API/store suites; commit event deliverable.

### Task 3: Verify and deliver

- [x] Run Go suite, frontend suites/builds and git diff checks; review changes for scope/semantics.
- [x] Record evidence, limitations and decisions in Rocket task log; update checkboxes.
- [x] Push feature/creative-work/stage-actions and open one PR referencing creative-work and gate299; notify orchestrator with URL.
