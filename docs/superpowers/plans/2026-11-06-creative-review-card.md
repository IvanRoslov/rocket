# Creative Review Card Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** One formal Creative review card with exact artifacts and required delivered return feedback.

**Architecture:** Reuse revision/state transitions and reason, existing event persistence and notification queue. Resolve the review version set independently of artifact browsing; retain existing Media viewers and shared controls.

**Tech Stack:** Go/SQLite, React/TypeScript, React Query, Vitest/MSW.

**Spec:** docs/superpowers/specs/2026-11-06-creative-review-card-design.md (accepted by creative-work-orch).

## Global Constraints

- No live decisions, configuration, restarts, resends or paid calls.
- Main Rocket, auto stages, stage/item modes, access and existing transport remain unchanged.
- Integrate App after task 6012; preserve isolated HTML viewing.

## Review Focus

- Whitespace-only feedback must leave review pending (task 1).
- Existing pending reviews and multiple versions of one artifact resolve exact IDs (task 2).
- Failed returns retain text; scope/revision changes cannot reuse it (task 2).
- Successful requests suppress obsolete actions even before polling refresh (task 2).
- Ordinary discussion and artifact history browsing never become decisions (tasks 2/3).

### Task 1: Required persisted feedback

Files: internal/store/creative_progress.go, creative_types.go; internal/api/creative.go and creative_lifecycle_test.go; internal/cli/creative.go and creative_test.go; existing store/API regression fixtures.

Interfaces: CreativeAction.Reason carries trimmed return feedback; action events and queued notifications include reason alongside review_revision and artifact_version_ids.

- [x] Add store/API/CLI tests rejecting empty/whitespace reject reasons without transition; assert accepted feedback in event and queued payload bound to exact versions/revision.
- [x] Run targeted Go tests and observe the new missing validation/payload assertions fail.
- [x] Validate reject reason in store and CLI, normalize in API, include reason in existing event before notification. Update old successful reject fixtures with reasons.
- [x] Run targeted store/API/CLI tests; commit.

### Task 2: Independent review card

Files: creative-web/src/ReviewActions.tsx and tests, App.tsx and tests, workflow.ts and tests, style.css.

Interfaces: ReviewActions takes scope, work, canProgress and itemContexts; internally reads existing artifact-versions/:id and artifacts/:id endpoints. Uses existing Media for clicked exact version; no coupling to shown artifact history.

- [x] Replace obsolete submit tests with card behavior: pending snapshot with newer current version, descendant names, exact viewing, approve, required return, failed draft, duplicates, revision/scope reset, withdrawal/new submit, empty review, start/reopen and no human submit.
- [x] Run Vitest card tests and observe failures against old UI contract.
- [x] Implement card, bound draft and successful outcome guards; remove submit from UI actions. Integrate above tabs after 6012; remove old Artifacts decision block and obsolete reset callback.
- [x] Update integration tests: card visible before tabs, approval works from discussion, history browsing does not change exact review. Run frontend tests/build; commit.

### Task 3: Agent contract and final verification

Files: internal/prompts/templates/creative-rules.md, creative-orchestrator.md, internal/prompts/creative_test.go; docs/creative-agents.md, creative-core-api.md and creative-dashboard.md as relevant.

- [x] Add prompt regression requiring exact-version submission and no separate stage-approval question; run and observe missing instruction failure.
- [x] Update Creative-only instructions and API/UI docs; run prompt tests.
- [x] Run relevant Go suite/build, frontend full tests/build, inspect diff for scope and check git diff --check.
- [ ] Commit; push branch and open one PR referencing creative-work and task 6013. Notify orchestrator with URL and validation. Respond to CI/review until merged.
