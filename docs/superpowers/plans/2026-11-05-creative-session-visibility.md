# Creative Session Visibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Remove closed Creative workers from the active rail while preserving history and recovery.

**Architecture:** Derive a local worker list from the existing full session query, excluding killed/done only. Render cards and empty state from that list; leave orchestrator behavior and polling intact.

**Tech Stack:** React, TypeScript, TanStack Query, Vitest, Testing Library, MSW.

**Spec:** docs/superpowers/specs/2026-11-05-creative-session-visibility-design.md

## Global Constraints

- Only Creative panel filtering changes; no backend, shared SessionCard, main-dashboard or live-session changes.
- Preserve errored workers, orchestrator restore, full query/cache/history, and existing polling.

## Review Focus

- Stale activity must not keep killed/done workers visible.
- Spawning/running/errored workers retain their links.
- Refetched closure removes a previously visible worker without purging cached history.
- All-closed and empty responses show the existing empty message.
- Offline orchestrator retains its restore action; existing run/context changes still work.

### Task 1: Filter closed Creative workers

**Files:** Modify creative-web/src/SessionRail.tsx; create creative-web/src/SessionRail.test.tsx.

**Interfaces:** Existing CreativeSessionRail({runID: number, orchestrator: string}) and full CreativeSession[] run sessions query; no new public interface.

- [x] Add real rendered-rail tests with MSW responses for all review-focus cases. Reuse existing test setup conventions; assert visible cards/links, empty message and restore POST behavior.
- [x] Run `npm test -- src/SessionRail.test.tsx` in creative-web. Confirm killed/done and refreshed-close cases fail because closed cards remain visible.
- [x] Derive workers by kind and state != killed/done in SessionRail.tsx; use the list for both cards and empty state.
- [x] Run Creative full tests/build, plus main SessionRail and context regression tests. Check git diff for scope and whitespace.
- [ ] Commit the focused fix with tests and design/plan; push this feature branch and create one PR referencing creative-work and #6008.
