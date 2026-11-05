# Creative workspace tabs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Make questions/discussion the wide central default workspace using shared Rocket tabs.

**Architecture:** Recompose existing Workspace without changing backend or artifact contracts. Keep Discussion and Artifacts mounted across tabs; expose review readiness only with the artifact panel visible.

**Tech Stack:** React, TypeScript, shared Rocket UI, Vitest, Testing Library, MSW, Vite.

**Spec:** docs/superpowers/specs/2026-11-05-creative-workspace-tabs-design.md

## Global Constraints

- Implements approved creative-work spec v6 / plan v5 deliverable 1, gate 294.
- Preserve run/stage/item, version/draft context, review revisions, all existing actions and shared design components.
- No chat/control redesign, timelines, summaries, new agent behavior, delivery fixes, approval flows or mobile work.
- No live daemon restart, deployment, real creative runs or paid calls.
- Preserve Artifacts props/onShown; sibling owns artifact rendering/API.

## Review Focus

- Initial hidden artifact query success must not enable approval.
- Switching tabs must retain manually selected older versions and composer drafts.
- Scope navigation must keep a draft bound to its original scope and versions.
- Preview round trips must leave selection URL and approval payload intact.
- Show submitted versions must explicitly navigate to artifacts and restore submitted selection.

### Task 1: Compose central workspace and preserve context

**Files:** Modify creative-web/src/App.tsx, creative-web/src/style.css; Test creative-web/src/App.test.tsx.
**Interfaces:** Existing Tabs, Artifacts/onShown, Discussion context, ReviewActions props; no new public contract.

- [x] Add failing integration tests for default shared tablist, hidden artifacts/review gating, retained older version/draft across artifact/preview/discussion, scope navigation and exact message/action payloads. Mock fixture APIs only.
- [x] Run `cd creative-web && npm test -- src/App.test.tsx`; confirm failures reflect absent central discussion tab.
- [x] Add discussion default tab, mounted panels, central two-column CSS, and visible-artifacts readiness. Show submitted versions opens Artifacts and resets submitted selection explicitly. Preserve all other actions.
- [x] Run App, Discussion, Artifacts and ReviewActions tests; confirm pass.
- [x] Commit the central workspace and tests.

### Task 2: Compact metadata and verify

**Files:** Modify creative-web/src/RunWorkspace.tsx, creative-web/src/App.tsx, creative-web/src/style.css; Test creative-web/src/App.test.tsx.
**Interfaces:** Existing WorkDescription props and process/item navigation unchanged.

- [x] Add failing assertions that work details are collapsed, heading/progress stay visible and expected artifacts/description remain reachable; run App tests and confirm failure.
- [x] Collapse work description/expected artifacts/progress notes and run brief while retaining compact heading/progress; preserve item editor.
- [x] Run full creative tests and production build, plus shared UI/session tests and web build as available. Inspect composition output without real runs.
- [ ] Commit metadata and final verification notes, push feature branch and open one PR referencing creative-work with checks and visual limitation.
