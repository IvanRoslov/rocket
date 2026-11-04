# Creative Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Deliver separate creative app/build/entry wired to the shared creative daemon APIs.
**Architecture:** React query-driven workspace, independent Vite build and Go embed. Existing API/auth primitives and sibling contracts supply authoritative state.
**Tech Stack:** Go net/http, React, TypeScript, Vite, TanStack Query, Vitest/testing-library.
**Spec:** docs/superpowers/specs/2026-11-04-creative-dashboard-design.md

## Global Constraints

Separate app/build/entry; shared daemon; no old dashboard creative mode; no mobile UI.
No e2e demo projects, paid calls, live daemon restart or CI infrastructure.
Approval and discussion bind displayed immutable versions. Approval copy uses artifact names/versions, never transport IDs.
Reopening preserves later results. Pipelines edits affect only new runs.

## Review Focus

- Deep creative URL after pairing must reload the separate application (task 1).
- Draft discussion must not silently follow a changed selection/version (task 4).
- New current artifact version during review must not change submitted approval (tasks 2/3).
- Missing preview setup must show backend actionable status (task 4).
- Project/run switch must not retain stale scope data or mutate wrong project (tasks 2/4).

### Task 1: Independent app delivery

Files: creative-web/{package.json,vite.config.ts,tsconfig.json,index.html,embed.go,dist/index.html,src/main.tsx,src/App.tsx,src/style.css}; internal/api/static.go,static_test.go; Makefile; web/src/screens/login/LoginScreen.tsx and test.
Interfaces: /creative/ serves separate assets and SPA deep routes; /login?next=/creative/... uses existing cookie pairing.
- [x] Add static tests distinguishing creative vs development index and missing assets/methods; add login return test.
- [x] Run focused Go/web tests and observe missing separate entry/return behavior.
- [x] Implement separate configuration, shell, embed and static route; browser navigation across app boundary on login.
- [x] Run focused tests and both production builds; commit independently.

### Task 2: Pipelines, runs and saved process

Files: creative-web/src/{types.ts,api.ts,Pipelines.tsx,RunWorkspace.tsx,App.tsx,workflow.ts,workflow.test.ts,Pipelines.test.tsx}.
Interfaces: core CreativePipeline/Input, CreativeRun/Input, CreativeStage/Item, action POST bodies; lists are arrays.
- [x] Test template add/reorder/approval editing without mutating original; test saved run stage selection/action eligibility and scope reset.
- [x] Observe failing tests, implement typed real endpoint client, project/run chooser, New work and pipeline editor.
- [x] Implement process rail, saved definitions, progress, concrete item creation/edit/delete, guarded start/submit/approve/reject/reopen actions.
- [x] Run tests/typecheck and commit.

### Task 3: Artifact inspection and review binding

Files: creative-web/src/{Artifacts.tsx,Artifacts.test.tsx,ReviewActions.tsx,ReviewActions.test.tsx,types.ts,api.ts}.
Interfaces: assets raw uploads and version/current/content URLs; core submit artifact_version_ids and approve/reject review_revision.
- [x] Test retained history/current labels, safe media rendering/downloads and source upload context; test submitted review stays fixed when current changes.
- [x] Observe failing tests, implement artifact list/detail/version selection, raw upload, current selection and text/image/audio/video/download presentations.
- [x] Show review names/version labels, submit shown results, approve/reject bound review and explain reopen preserving later results.
- [x] Run focused tests/typecheck and commit.

### Task 4: Contextual discussion and preview

Files: creative-web/src/{Discussion.tsx,Discussion.test.tsx,Preview.tsx,Preview.test.tsx,App.test.tsx,types.ts,api.ts}.
Interfaces: creative sessions/messages/questions; existing question reply/answer and session restore; preview status/access sibling API.
- [x] Test selected-context/version messages, draft scope stability, explicit Go question replies and session launch/restore errors.
- [x] Test available embedded/separate-tab preview and unavailable actionable message; project/run switch resets scope.
- [x] Observe failures, implement real APIs with loading/error/empty states and profile selection.
- [x] Run focused tests/typecheck, commit.

### Task 5: Verify and deliver

Files: docs and necessary integration corrections only.
- [x] Run complete Go and both web test suites, both web builds, go vet/build without starting daemon. Record any baseline failures explicitly.
- [x] Review diff against acceptance/contracts and inspect component-rendered UI without projects/generation (browser host unavailable); fix discovered regressions with failing tests first.
- [x] Commit final docs/corrections, push own feature branch, create one PR referencing creative-work 5986 / 5991, send orchestrator PR URL. Respond to checks/review until accepted.
