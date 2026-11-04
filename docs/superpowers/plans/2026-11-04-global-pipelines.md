# Global Pipeline Catalog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Make existing creative templates reusable globally while retaining project-owned run snapshots.

**Architecture:** Drop only template project ownership in migration 0031. Global store contracts and API-only ignored legacy hints feed a projectless UI and compatible CLI.

**Tech Stack:** Go, embedded SQLite migrations, Cobra, React, TanStack Query, Vitest/MSW.

**Spec:** docs/superpowers/specs/2026-11-04-global-pipelines-design.md (accepted by orchestrator).

## Global Constraints

No live DB changes/restart, paid calls, demo projects, new permission framework, template cloning or unrelated refactor. Preserve IDs, equal names, run graph and shared UI styling. Targeted verification only; one PR for creative-work 5986/subtask 5994.

## Review Focus

- Duplicate names from different projects stay distinct (task 1 upgrade fixture).
- Deleted high template IDs must not be reused (task 1 sequence assertion).
- Legacy project hints never scope listing or edits (task 2 contract checks).
- No projects must not hide the editor (task 3 App test).
- Switching projects keeps editor drafts but clears run selection (task 3 App tests).

### Task 1: Storage and upgrade preservation

Files: internal/store/migrations/0031_global_creative_pipelines.sql, creative.go, creative_types.go, creative_test.go, migrate_creative_global_test.go; dependent Go test fixtures.
Interfaces: CreativePipelineInput contains name/stages only; ListCreativePipelines() returns all templates; CreateCreativeRun(CreativeRunInput) validates project independently.

- [ ] Write TestCreativeGlobalCatalog for projectless CRUD, two project runs, unchanged old snapshots and invalid project rejection; write TestMigrateCreativeGlobalCatalog using schema versions 1–30 and a populated active graph, duplicate names, deleted high ID, equality checks on all run tables, FK check and reopen.
- [ ] Run targeted store tests; expect projectless validation/ownership and missing migration failures.
- [ ] Add DROP COLUMN migration; remove template ownership validation/SQL/filtering and equality restriction; keep run project validation. Adapt dependent fixtures to global input type.
- [ ] Run go test ./internal/store -run 'Creative|MigrateCreative'; expect PASS.
- [ ] Commit storage, migration and fixture adaptation.

### Task 2: API/CLI global contracts

Files: internal/api/creative.go, creative_test.go; internal/cli/creative.go, creative_test.go; docs/creative-core-api.md.
Interfaces: API-only legacy project_id input accepted/ignored; GET template query hints ignored; authenticated/trusted callers can manage shared catalog; run operations retain project checks. CLI pipeline ls --project deprecated/ignored, runs keep filtering.

- [ ] Write API tests for projectless CRUD, old hints, absent project_id output, two project runs and cross-project run mutation denial. Add CLI request tests for pipeline ls with/without legacy flag and projectless JSON create/update.
- [ ] Run targeted API/CLI tests; expect failure at projectless request or filtered path.
- [ ] Implement API wrapper and global route calls; remove project write checks only for templates; deprecate ignored pipeline listing flag. Update contract docs.
- [ ] Run go test ./internal/api ./internal/cli -run Creative; expect PASS.
- [ ] Commit API/CLI contract correction.

### Task 3: Global projectless UI and verification

Files: creative-web/src/App.tsx, Pipelines.tsx, types.ts, App.test.tsx, Pipelines.test.tsx; docs/creative-dashboard.md and superseded core design note.
Interfaces: one global template query key and URL; editor props pipelines/onClose; save body name/stages.

- [ ] Update ordered-stage save assertion to omit ownership; add no-project creation/edit tests, catalog persistence/draft preservation on project change, and selection of same template for another project's new run.
- [ ] Run npm test in creative-web; expect projectless/editor/catalog failures.
- [ ] Remove pipeline project prop/type field, query scope and disabled guard. Render editor ahead of project empty state; retain pipeline view on project switch while clearing run selection. Update user docs.
- [ ] Run creative-web npm test and npm run build; expect PASS. Run targeted Go packages including session and ghpoller Creative checks, go vet of changed packages and go build ./cmd/rocket; expect PASS.
- [ ] Review diff against accepted design; commit UI/docs and verification evidence, then create focused PR and report URL.
