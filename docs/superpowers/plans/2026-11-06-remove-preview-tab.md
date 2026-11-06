# Remove Creative Landing Preview Tab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove only Creative's unused Landing preview tab.

**Architecture:** Delete the reachable preview tab/state/render branch in App.tsx. Keep discussion/artifact panels mounted and all artifact and server preview support unchanged.

**Tech Stack:** React, TypeScript, Vitest, Testing Library, Vite.

**Spec:** docs/superpowers/specs/2026-11-06-remove-preview-tab-design.md

## Global Constraints

- Rocket only, one independent PR for creative-work deliverable1.
- Preserve artifact HTML opening links, media previews, access isolation, preview listener/config and published versions/history.
- Keep questions/artifacts tabs and current session/stage navigation; ordinary Rocket unchanged.
- No live run/session/config changes, restarts or paid calls.

## Review Focus

- Older selected version survives tab switches: existing App version/draft regression.
- Draft remains bound to its original item on stage navigation: existing App scope regression.
- Session rail remains outside the panels and visible: existing App session regression.
- HTML still opens an isolated immutable version: existing Artifacts HTML regression.
- Image/video/audio previews and downloads/history survive: existing Artifacts media/history regressions.

### Task 1: Remove reachable preview tab and retain workspace behavior

**Files:** Modify creative-web/src/App.tsx and creative-web/src/App.test.tsx.

**Interfaces:** Consumes existing Workspace, Tabs and artifact/discussion callbacks. Produces a two-tab workspace with unchanged scope/context behavior; no new interface.

- [x] Run baseline Creative tests.
- [x] In App.test.tsx assert exactly two workspace tabs and absence of Landing preview/interactive preview, replace preview detours with discussion/artifact switching, and keep scope/version/draft/session assertions. Remove obsolete preview API fixture handlers.
- [x] Run App tests and observe the two-tab assertion fail against current three-tab production code.
- [x] Remove Preview import, preview state union member, tab item, and conditional Preview render in App.tsx only.
- [x] Run Creative full suite and build; verify Artifacts HTML/media/history tests pass unchanged.
- [ ] Review diff for scope, commit code/tests/docs, push feature branch and open PR referencing creative-work/task 6012. Review CI and coordinate sibling rebase.

Expected commands: npm test --prefix creative-web (all pass); npm run build --prefix creative-web (TypeScript and Vite pass). No live service exercised.
