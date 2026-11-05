# Materials cleanup implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the central general-message surface and make retained Markdown readable safely.
**Architecture:** Keep existing question and artifact APIs; change only Creative UI. Render Markdown using the existing GFM dependencies with a document-specific safe URL/image policy.
**Tech Stack:** React, TanStack Query, react-markdown, remark-gfm, Vitest/MSW, Go API.
**Spec:** docs/superpowers/specs/2026-11-05-materials-cleanup-design.md (accepted by creative-work-orch).

## Global Constraints

- Rocket worktree only; one PR; no live mutations/configuration/restarts or paid calls.
- Preserve questions, contextual drafts, selected-version identity, session access and backend communication.
- Do not rewrite published names/content or infer spending permission.
- Retain 1 MiB text preview cap, authenticated immutable content URLs and download.

## Review Focus

- Generic MIME Markdown filename fallback must not reinterpret HTML/media.
- Version changes must not show stale content or the wrong download.
- Markdown must not execute HTML or load embedded remote images or relative API links.
- Question creation must retain original draft context after navigation.
- Load errors and oversized documents must explain download fallback.

### Task 1: Question-only central discussion

**Files:** creative-web/src/Discussion.tsx, Discussion.test.tsx, App.test.tsx.
**Interfaces:** Discussion keeps context/orchestrator/ready props; consumes existing questions/reply/answer APIs. No API changes.
- [ ] Update draft tests to open questions, add regression that messages are neither fetched nor shown and message composer/mode is absent; retain thread and session access tests.
- [ ] Run `npm --prefix creative-web test -- src/Discussion.test.tsx src/App.test.tsx`; expect new regression to fail before implementation.
- [ ] Remove Messages query/feed and message mode; keep question form with original contextual draft binding and readiness behavior.
- [ ] Run same tests; expect pass. Commit coherent UI change.

### Task 2: Safe selected Markdown viewing

**Files:** creative-web/src/Artifacts.tsx, Artifacts.test.tsx, style.css; add ArtifactMarkdown.tsx if separation improves safety policy clarity.
**Interfaces:** Media consumes ArtifactVersion with immutable content/download URLs; artifact card retains name and exposes filename/version.
- [ ] Add tests for actual generic-MIME .md case, formatted GFM, raw HTML/scripts/unsafe links/remote images, retained version switching, filename identity, load failure and oversized documents; preserve existing media tests.
- [ ] Run `npm --prefix creative-web test -- src/Artifacts.test.tsx`; expect formatted heading and generic-MIME preview regressions to fail.
- [ ] Implement document-specific GFM renderer, bounded MIME/extension detection and explicit preview states; show filename/version alongside stored name. No raw HTML plugin or automatic images; allow only explicit absolute http(s)/mailto links.
- [ ] Run artifact tests and full Creative suite; expect pass. Commit fix.

### Task 3: Verification and delivery

- [ ] Run `npm --prefix creative-web test`, `npm --prefix creative-web run build`, `npm --prefix web run build`, and relevant Go API artifact/questions/message tests.
- [ ] Review diff for context/version safety, access boundary changes, and scope creep; record evidence and limits. Open one PR referencing creative-work and send URL to orchestrator.
