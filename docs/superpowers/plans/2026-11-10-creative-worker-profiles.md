# Creative Worker Profiles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Select worker profiles at launch and replace the list on an existing creative run.

**Architecture:** Share a checkbox group inside SessionRail. Pass saved run profiles from App; local drafts survive errors, and successful requests invalidate creative queries.

**Tech Stack:** React, TypeScript, React Query, Vitest, Testing Library, MSW.

**Spec:** docs/superpowers/specs/2026-11-10-creative-worker-profiles-design.md

## Global Constraints

- Only creative-web and dashboard documentation; no backend or CLI changes.
- Empty = “all profiles”; names and models visible; saved disabled entries removable.
- POST sessions accepts allowed_profiles; PUT models replaces allowed_profiles.
- Errors display and preserve checkbox drafts.

## Review Focus

- Registry loading/failure must not enable editing an unknown list.
- Saved disabled profiles must be removable without offering unrelated disabled entries.
- Saving empty must send [] and show “all profiles”.
- Refetches must not overwrite an unsaved draft.
- Switching runs must not carry drafts across run identities (existing keyed rail).

### Task 1: Launch and edit UI

**Files:** modify creative-web/src/{SessionRail.tsx,SessionRail.test.tsx,types.ts,App.tsx,style.css}; create creative-web/src/WorkerProfiles.tsx.

**Interfaces:** CreativeSessionRail accepts allowedProfiles?: string[] with [] fallback. WorkerProfiles receives profiles, picked, onChange, disabled. Run adds allowed_profiles?: string[] for compatibility with existing fixtures.

- [x] Add failing SessionRail API-boundary tests: selected launch sends profile and allowed_profiles; launch failure keeps choices; edit/save replaces full list and reflects server state; failure/refetch keeps draft; disabled saved profile removable; clear sends []; registry pending/error blocks edit.
- [x] Run npm test --prefix creative-web -- src/SessionRail.test.tsx; confirm failures reflect missing UI.
- [x] Add shared checkbox group, launch draft and existing-run summary/editor, API calls using supplied contract, run prop wiring and scoped styles.
- [x] Run focused tests, then npm test --prefix creative-web and npm run build --prefix creative-web; expect all pass.
- [x] Commit implementation and tests.

### Task 2: Documentation and final verification

**Files:** modify docs/creative-dashboard.md.

- [x] Document launch selection, empty semantics, editing, disabled saved entries and future-spawn effect.
- [ ] Rebase after backend merges, inspect real handlers and exercise API contract tests; run project checks appropriate to frontend changes and git diff --check.
- [ ] Review whole diff, commit docs, push branch and open one PR referencing creative-improvement. Report URL and integration dependency to orchestrator.
