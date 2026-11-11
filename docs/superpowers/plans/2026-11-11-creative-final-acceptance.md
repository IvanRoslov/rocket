# Creative Final Acceptance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Explicit human acceptance stops a Creative run and exposes its permanent readable archive.
**Architecture:** Store open/closing/accepted independently from stage status. Freeze before verified shutdown under the session manager lock, persist acceptance last, retain visible retry on failure. Guard writes at persistence and service boundaries.
**Tech Stack:** Go, SQLite, React, TanStack Query, Vitest.
**Spec:** docs/superpowers/specs/2026-11-11-creative-final-acceptance-design.md (accepted by orchestrator).

## Global Constraints

No global dashboard, archive reopening, workspace deletion, live deployment/restart, or changes to Development lifecycle. Existing stage-completed runs remain active. Keep one vertical deliverable and one PR for ui-improve-3.

## Review Focus

- Duplicate/concurrent acceptance: one durable decision and safe retries.
- Runtime Destroy errors and stale terminal session state: inventory must prove stopped.
- Stop/save failure: closing stays active and read-only with visible retry.
- Generic session, discussion and stale mutation paths: cannot resume/change archives.
- Accepted direct links: retain selected run, history, versions and downloads.

### Task 1: Durable lifecycle and archive write barriers

Files: internal/store/creative_acceptance.go, creative_acceptance_test.go, creative.go, creative_types.go, migrations/0034_creative_acceptance.sql; existing Creative artifact/feed/discussion writes.
Interfaces: BeginCreativeAcceptance(runID int64, human bool) (CreativeRun,error); FinishCreativeAcceptance(runID int64) error; RecordCreativeClosureError(runID int64,message string) error; CheckCreativeRunMutable(runID int64) error; CheckCreativeSessionMutable(id string) error.
- [ ] Write tests for eligibility, human-only decision, old completed compatibility, idempotency and frozen stage/feed/artifact/discussion/config writes.
- [ ] Run go test ./internal/store -run CreativeAcceptance and observe missing feature failure.
- [ ] Implement migration, durable fields and transactional write guards; keep reads intact.
- [ ] Run store suite; commit lifecycle and barriers.

### Task 2: Verified scoped stopping and service boundaries

Files: internal/session/creative.go and tests; internal/api/creative_acceptance.go and tests; creative.go, server.go and generic session/message boundaries.
Interfaces: Manager.AcceptCreativeRun(ctx context.Context,runID int64) (store.CreativeRun,error); POST /v1/creative/runs/{run}/accept returns run, requires caller nil (existing human convention).
- [ ] Write manager/API tests for stop errors, inventory errors, save failures, repeated calls, foreign-run isolation, retained files, restore/spawn rejection and stale API requests.
- [ ] Run focused tests and observe failure.
- [ ] Implement closure under manager lock; never use best-effort Kill as proof. Guard launch/restore and interactive/generic writes; publish changed event after lifecycle changes.
- [ ] Run store/session/API tests; commit service integration.

### Task 3: Existing Creative UI archive flow

Files: creative-web/src/App.tsx, types.ts, FinalAcceptance.tsx, tests; Discussion, FeedEntry, Artifacts, SessionRail.
Interfaces: Run.closure_state?: open|closing|accepted; accepted_at?: number; closure_error?: string. readOnly props to mutation-capable child views.
- [ ] Write failing React scenarios for final action on last stage, explanation, accepted selected success, retry error and project Completed works navigation.
- [ ] Run focused Vitest tests and observe failures.
- [ ] Implement final panel and project archive access, filter accepted active options/cards, suppress archived mutation controls while retaining reading.
- [ ] Run Creative suite/build; commit UI.

### Task 4: Full verification and PR

- [ ] Run go test ./..., Development and Creative npm test/build; investigate any failures before claiming completion.
- [ ] Exercise an isolated end-to-end scenario with test runtime/browser fixtures; record scenario evidence and limitations.
- [ ] Review diff for acceptance gaps, run git diff --check, commit evidence, push feature branch and open one PR referencing ui-improve-3 and task 6073.
