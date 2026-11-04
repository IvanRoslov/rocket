# Creative artifacts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Durable creative source/result publication with retained immutable versions and browser content access.
**Architecture:** SQLite named artifact and version rows; streamed private files beneath Rocket home. Core owns context and review transitions, assets validates immutable version ownership.
**Tech Stack:** Go, SQLite, net/http, Cobra.
**Spec:** docs/superpowers/specs/2026-11-04-creative-artifacts-design.md

## Global Constraints
- Upload limit: 2 GiB file content; streaming, never whole-video allocation.
- Migration 0029, dependency on core 0028; preserve existing attachments.
- No paid calls, demo projects, live daemon restart, or modifications outside this worktree.
- Immutable version IDs and exact-context validation, explicit current selection; no unchecked review fallback.

## Review Focus
- Concurrent publication must preserve all versions and unique sequence numbers (Task 1).
- Mid-transfer failures and storage failure must publish no rows or current pointer (Task 2).
- Arbitrary filenames/active content must not escape storage or execute as dashboard content (Task 2).
- Cross-project sessions and invalid ancestry/version ownership must fail (Tasks 1 and 2).
- Large streams and server errors must propagate without false success (Tasks 2 and 3).

### Task 1: Metadata and immutable version validation
**Files:** Create internal/store/creative_artifacts.go, internal/store/creative_artifacts_test.go, internal/store/migrations/0029_creative_artifacts.sql.
**Interfaces:** Consumes core ResolveCreativeContext(runID,stageID,itemID int64) (CreativeContext,error); produces PublishCreativeArtifact(input CreativeArtifactPublication) (CreativeArtifact,CreativeArtifactVersion,error), GetCreativeArtifact, ListCreativeArtifacts, ListCreativeArtifactVersions, GetCreativeArtifactVersion, SetCreativeArtifactCurrent, ValidateCreativeArtifactVersions.
- [x] Write failing tests for retained history/current, wrong ancestry and versions, and concurrent publication using real SQLite.
- [x] Run go test ./internal/store -run CreativeArtifact; expect missing APIs first.
- [x] Implement tables and transactional publish/current changes, immutable joined version reads and exact-context validation.
- [x] Run focused store tests; expect PASS. Commit store implementation and tests.

### Task 2: Stream upload and authenticated content API
**Files:** Create internal/api/creative_artifacts.go and internal/api/creative_artifacts_test.go; modify internal/api/server.go registration only.
**Interfaces:** Consumes Task 1 store APIs; produces registerCreativeArtifactRoutes and documented /v1/creative artifact HTTP endpoints.
- [x] Write failing HTTP tests for publication/playback Range/download/history, wrong project/context, aborted/oversize/empty uploads, blocked storage and active content disposition.
- [x] Run go test ./internal/api -run CreativeArtifact; expect unregistered routes.
- [x] Implement bounded streaming to synced durable blob before metadata, error cleanup, safe content headers and URLs, existing caller authentication.
- [x] Run focused API and attachment compatibility tests; expect PASS. Commit API implementation and tests.

### Task 3: Agent CLI and stream client
**Files:** Create internal/client/creative_artifacts.go and tests; internal/cli/creative_artifacts.go and tests; modify core creative CLI to add newCreativeArtifactCmd only.
**Interfaces:** Consumes Task 2 routes; produces Client.UploadCreativeArtifact(ctx,path,filename,mime,reader,out) error, creative artifact publish/ls/show/versions/current commands.
- [x] Write failing client stream tests for byte/header propagation, server rejection and read errors; CLI parse/required-context tests without live daemon.
- [x] Run go test ./internal/client ./internal/cli -run CreativeArtifact; expect missing command/client.
- [x] Implement raw streaming with bounded transfer timeout and JSON result/error, file-derived MIME and validation before connection.
- [x] Run focused client/CLI checks; expect PASS. Commit agent interface and docs.

### Task 4: Integration and final verification
**Files:** Update design/plan status and any integration tests required by core review contract.
- [x] Verify core review snapshot validation against real published versions, wrong/missing versions and current pointer changes.
- [ ] Run go test ./..., go vet ./..., go build ./cmd/rocket and web npm test -- --run; inspect failures and record any baseline issues.
- [ ] Obtain independent whole-branch review and address acceptance-breaking findings with regression tests.
- [ ] Open focused PR referencing creative-work/5986 and 5988; send URL to orchestrator and react to CI/review.
