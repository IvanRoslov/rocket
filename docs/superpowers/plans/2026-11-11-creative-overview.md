# Global Creative overview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make global Creative Active/Completed the default navigation without top project/work selectors.

**Architecture:** Reuse global runs API and per-run unread caches. URL-backed views coexist with existing run/stage/item links. Overview cards own unread polling only while visible.

**Tech Stack:** React, TanStack Query, TypeScript, Vitest/MSW, isolated Playwright browser checks.

**Spec:** docs/superpowers/specs/2026-11-11-creative-overview-design.md (accepted by orchestrator).

## Global Constraints

- Creative only; no quick switcher/search/new notification semantics.
- Preserve release 1 acceptance and archive contract.
- No live deployment/restart/real-work acceptance.
- Overview New work project starts blank; run context may initialize it.

## Review Focus

- Same-named work across projects: labeled cards and authoritative project context.
- Reload/back/forward: URL reconstructs global views and deep scopes.
- Unread failure/stale cache: unavailable state never masquerades as zero; card remains openable.
- Closing or derived completed: Active until accepted, archive retains read-only behavior.
- Empty/loading/list failure: distinct visible states, New work and Completed remain reachable.

### Task 1: Global overview and navigation

**Files:** App.tsx, App.test.tsx, new Overview.tsx and Overview.test.tsx under creative-web/src; style.css.
**Interfaces:** Overview receives runs query state, project catalog and run-opening callback; existing useUnread(runID) supplies per-card total/error. App owns Selection including URL-backed view.

- [ ] Write integration tests for global list without project filter, project-labeled duplicates, current state, unread refresh/error/read invalidation, empty/loading/error distinctions, deep links, history and Active/Completed membership. Update obsolete selector tests to card navigation.
- [ ] Run `npm test --prefix creative-web -- src/App.test.tsx src/Overview.test.tsx`; confirm new expectations fail on missing global overview.
- [ ] Implement global overview, header context/All works, URL-backed views and visible-only two-second queries; preserve Workspace internals.
- [ ] Run targeted tests and Creative suite/build; expected green.
- [ ] Commit global overview and tests.

### Task 2: Explicit creation context

**Files:** creative-web/src/App.tsx, App.test.tsx.
**Interfaces:** NewWork receives projects and initial project; created Run.project_id drives navigation.

- [ ] Test blank overview project/disabled submission, selecting the other project in POST, and run-context default (including reload of New work URL).
- [ ] Run App tests; confirm creation expectations fail before implementation.
- [ ] Add required project field, explicit enabled-state validation and returned-project navigation.
- [ ] Run Creative suite/build; expected green. Commit creation behavior.

### Task 3: Browser acceptance and final verification

**Files:** creative-web/browser/overview-check.mjs, docs/validation/2026-11-11-creative-overview.md, docs/screenshots/creative-overview-*.png.
**Interfaces:** Export runOverviewCheck(page) for browser harness. Intercept every /v1 request using isolated fixture state.

- [ ] Exercise duplicate projects, All works, back/forward, reload, item/stage links, unseen-stage unread polling/read/error/recovery, blank/explicit creation, final acceptance transition, read-only archives and mobile overflow.
- [ ] Capture desktop/mobile screenshots and inspect them.
- [ ] Run `npm test --prefix creative-web`, `npm run build --prefix creative-web`, `npm test --prefix web -- --run`, `npm run build --prefix web`, `go test -p 2 ./...`, `git diff --check`; record exact results and limitations.
- [ ] Review complete diff against acceptance, fix concrete failures with reproducing tests, commit evidence, push branch, open one PR referencing ui-improve-3 and notify orchestrator.
