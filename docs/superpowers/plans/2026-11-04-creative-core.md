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
- Agent identity must not bypass human approval or template editing (task 3).

### Task 1: Persistent identities and snapshot definitions

Files: new store/creative.go, store/creative_test.go, migrations/0028_creative_core.sql.
Interfaces: CreativePipeline/Run/Stage/Item/Context; GetCreativeRun/GetCreativeStage/GetCreativeItem(int64); ResolveCreativeContext(runID,stageID,itemID int64); SetCreativeRunOrchestrator(runID,sessionID).
- [ ] Write snapshot, ordering, context mismatch, project/session validation tests and observe missing-symbol failures.
- [ ] Implement schema, validated template CRUD, transactional run instantiation, read/context helpers.
- [ ] Run focused store tests; commit.

### Task 2: Guarded progression and repeated items

Files: new store/creative_progress.go, store/creative_progress_test.go.
Interfaces: ApplyCreativeAction(runID,stageID,itemID int64, action CreativeAction, human bool); UpdateCreativeProgress; Add/Update/DeleteCreativeItem.
- [ ] Test stage ordering, auto/stage/item approvals, pending-item editing, progress bounds, stale reviews, explicit human rework and concurrent transitions; observe red.
- [ ] Implement serialized run mutations, guarded state machine, review revision and immutable version-ID snapshots; reject nonempty versions unless assets schema validates them.
- [ ] Run focused store tests and commit.

### Task 3: Authenticated API and agent CLI

Files: new api/creative.go, api/creative_test.go, cli/creative.go, cli/creative_test.go; server.go and root.go registration.
Interfaces: documented /v1/creative routes; rocket creative pipeline/run/stage/item subcommands; existing client JSON transport.
- [ ] Test API snapshots, invalid context/body, agent approval prohibition, empty/missing identities, and CLI file/request routing; observe red.
- [ ] Implement routes using existing caller resolution, bounded strict JSON, store errors; commands use JSON files without shell interpolation.
- [ ] Run focused API/CLI tests; commit.

### Task 4: Verify and deliver

- [ ] Run go tests/build/vet appropriate to changed packages; record failures/limits honestly. Review whole diff against acceptance.
- [ ] Update contract docs with exact interfaces/examples and checks; commit, push task branch, create single PR referencing creative-work/5986/5987.
- [ ] Report PR URL and validation to orchestrator. React to review until acceptance.
