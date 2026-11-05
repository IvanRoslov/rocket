# Creative Worker Close Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Explicitly close own-run Creative workers without deleting their results.

**Architecture:** A run-scoped endpoint authorizes current orchestrator and worker
bindings, then calls existing Manager.Kill with cleanup false. A dedicated CLI
command and Creative-only guidance expose the action.

**Tech Stack:** Go, Cobra, net/http, SQLite, existing session manager.

**Spec:** docs/superpowers/specs/2026-11-05-creative-worker-close-design.md

## Global Constraints

- No live session actions, daemon/config changes or paid calls.
- No cleanup, cascade, automatic closure or publication-existence guarantee.
- Keep ordinary development prompt/rendering and lifecycle unchanged.
- Current orchestrator owns workers by run/project, including predecessor workers.

## Review Focus

- Stale orchestrator cannot act after replacement.
- Foreign-run worker cannot pass because its ParentID matches caller.
- Project mismatch and development-task contamination must be rejected.
- Terminal close preserves state and produces no second runtime stop.
- Cleanup/cascade parameters cannot expand close to workspace deletion or fleet stop.

### Task 1: Scoped close endpoint and preservation

**Files:** Create internal/api/creative_close.go and creative_close_test.go;
modify internal/api/creative_agents.go for route registration.

**Interfaces:** Consumes Store session/run bindings and Manager.Kill(ctx,id,false).
Produces POST /v1/creative/runs/{run}/workers/{session}/close with killed status.

- [x] Write table tests for accepted current/replacement orchestrators and rejected
  human, worker, stale/unbound caller, foreign run/project, unbound/nonworker target,
  task contamination and missing target. Add preservation/idempotency test using
  recorded runtime stops and workspace deletion detection, files and publications.
- [x] Run `go test ./internal/api -run TestCreativeClose -count=1`.
  Expected: FAIL because endpoint does not exist.
- [x] Implement ownership validation and delegate to Kill with cleanup=false.
- [x] Run the same tests plus ordinary kill tests. Expected: PASS.
- [x] Commit endpoint and tests.

### Task 2: CLI and guidance

**Files:** Create internal/cli/creative_close.go and creative_close_test.go;
modify internal/cli/creative_agents.go, internal/prompts/templates/creative-rules.md,
internal/prompts/templates/creative-orchestrator.md and docs/creative-agents.md.

**Interfaces:** Consumes Task 1 endpoint; produces close RUN WORKER_SESSION CLI.

- [x] Write CLI request/identity/output and invalid argument tests.
- [x] Run `go test ./internal/cli -run TestCreativeClose -count=1`.
  Expected: FAIL because close command does not exist.
- [x] Implement CLI command with positive run ID and explicit session argument.
  Add semantic publication verification and post-approve explicit close guidance.
- [x] Run `go test ./internal/cli ./internal/prompts -count=1`. Expected: PASS.
- [x] Commit command and guidance.

### Final verification

- [x] Run `make test` and `go vet ./...`; report any named failures.
- [x] Review full diff and preservation/authorization edge cases.
- [ ] Push only feature branch and create one PR referencing creative-work.

## Verification record

Both implementation tasks completed RED-to-GREEN. `make test` passed all Go
packages, 722 web tests and 53 Creative web tests. `go vet ./...` and
`git diff --check` passed. Fresh-context read-only review found no
acceptance-breaking issue or spec contradiction; its targeted regression run
also passed. No live closure/restart, configuration change, real-run action or
paid call was performed.

Review scope decisions:

- Ownership is checked when authorizing the request, as in existing Creative
  endpoints. Serializing replacement against an already-authorized in-flight
  close could also be handled, but is outside this bounded change; a request
  accepted just before replacement can finish afterward.
- Ordinary session-kill permissions retain their existing contract; this
  deliverable adds a strictly scoped Creative close endpoint.
- Runtime destruction retains the existing best-effort Manager.Kill contract;
  close does not add a stronger guarantee that a failed runtime stop succeeded.
- Approval notifications and withdrawal remain owned by the stage-lifecycle
  sibling, using the agreed publication-verification/explicit-close reminder.
- Publication completeness remains semantic agent verification. Automating it
  would require a different contract and must not be inferred from file presence.
