# Creative Core Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement task-by-task with test-driven-development.

**Goal:** Separate creative workflow persistence and API/CLI contracts for feature 5986, task 5987.
**Architecture:** Project-owned templates instantiate independent ordered stage/item state machines. Guarded store transactions enforce progression; API authenticates human versus agent actions.
**Tech Stack:** Go, SQLite, net/http, Cobra.
**Spec:** docs/superpowers/specs/2026-11-04-creative-core-design.md

## Global Constraints

No development lifecycle changes, paid generation, daemon restart, new CI, demo projects or simulated generation suites. Artifacts, launch, contextual discussion, preview and UI belong to siblings.

## Review Focus

- Concurrent submits/approvals must not allow stale decisions (task 2).
- Cross-run stage/item IDs must fail context resolution (task 1).
- Pipeline edits must not mutate active run definitions (task 1).
- Rework preserves downstream results but renews target approval (task 2).
- Agent identity must not bypass human approval  (task 3).

### Task 1: Persistent identities and snapshot definitions

Files: new store/creative.go, store/creative_test.go, migrations/0028_creative_core.sql.
Interfaces: CreativePipeline/Run/Stage/Item/Context; GetCreativeRun/GetCreativeStage/GetCreativeItem(int64); ResolveCreativeContext(runID,stageID,itemID int64); SetCreativeRunOrchestrator(runID,sessionID).
- [x] Write snapshot, ordering, context mismatch, project/session validation tests and observe missing-symbol failures.
- [x] Implement schema, validated template CRUD, transactional run instantiation, read/context helpers.
- [x] Run focused store tests; commit.

### Task 2: Guarded progression and repeated items

Files: new store/creative_progress.go, store/creative_progress_test.go.
Interfaces: ApplyCreativeAction(runID,stageID,itemID int64, action CreativeAction, human bool); UpdateCreativeProgress; Add/Update/DeleteCreativeItem.
- [x] Test stage ordering, auto/stage/item approvals, pending-item editing, progress bounds, stale reviews, explicit human rework and concurrent transitions; observe red.
- [x] Implement serialized run mutations, guarded state machine, review revision and immutable version-ID snapshots; reject nonempty versions unless assets schema validates them.
- [x] Run focused store tests and commit.

### Task 3: Authenticated API and agent CLI

Files: new api/creative.go, api/creative_test.go, cli/creative.go, cli/creative_test.go; server.go and root.go registration.
Interfaces: documented /v1/creative routes; rocket creative pipeline/run/stage/item subcommands; existing client JSON transport.
- [x] Test API snapshots, invalid context/body, agent approval prohibition, empty/missing identities, and CLI file/request routing; observe red.
- [x] Implement routes using existing caller resolution, bounded strict JSON, store errors; commands use JSON files without shell interpolation.
- [x] Run focused API/CLI tests; commit.

### Task 4: Verify and deliver

- [x] Run go tests/build/vet appropriate to changed packages; record failures/limits honestly. Review whole diff against acceptance.
- [ ] Update contract docs with exact interfaces/examples and checks; commit, push task branch, create single PR referencing creative-work/5986/5987.
- [ ] Report PR URL and validation to orchestrator. React to review until acceptance.

## Execution record

- Published design/types in 0768ec0; store implementation b40d6cc; API/CLI 62366fc.
- Orchestrator accepted explicit human-only rework and version-bound review; later clarified whole-stage review/discussion includes same-stage item versions (Q6/Q16), while item/run context remains exact.
- Independent review found later-item progression after predecessor reopen and pending status between stages. Both reproduced with failing tests and corrected.
- Focused store/API/CLI tests and race checks passed. Full concurrent Go suite had one existing subprocess deadline failure in TestHookExtractsOnlyBoundedMetadata/valid; isolated retry passed, and `go test -p 1 ./...` passed. Latest stage-scope predicate checked with focused tests; build/vet passed. No production daemon touched.
- Contract and command examples: docs/creative-core-api.md. Assets owns migration 0029 and immutable version tables; core rejects nonempty unknown version snapshots until that migration lands.
