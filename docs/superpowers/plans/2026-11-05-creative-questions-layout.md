# Creative Questions Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Deliver gate297 specv9/planv8 UI correction with permanent sessions and central threaded questions.

**Architecture:** Extract Creative session controls/cards into a permanent right rail outside tab panels. Adapt shared QuestionThreadView presentation for Creative history and externally retained drafts, preserving default main Rocket behavior and current endpoints.

**Tech Stack:** React, TypeScript, TanStack Query, Vitest, Testing Library, MSW.

**Spec:** docs/superpowers/specs/2026-11-05-creative-questions-layout-design.md

## Global Constraints

- No deeper conversational/control redesign, development kill/PR controls, live changes or paid calls.
- Preserve run/stage/item/version context, permissions, delivery, review payloads and artifact renderer contracts.
- Reuse existing reply/answer endpoints; clarification keeps open, explicit answer resolves.
- Existing main Rocket behavior must remain compatible.

## Review Focus

- Sessions remain visible through every central tab and scoped selection.
- New arrivals retain earlier open questions and typed replies.
- Failed submissions retain draft text and original context.
- Resolved history expands completely with no reply/answer controls.
- Artifact version selection and approval payloads survive navigation.

### Task 1: Permanent Creative session rail

**Files:** Create creative-web/src/SessionRail.tsx; modify creative-web/src/Discussion.tsx, App.tsx, style.css, Discussion.test.tsx, App.test.tsx.

**Interfaces:** CreativeSessionRail({runID: number, orchestrator: string}) owns existing session queries/controls. Discussion keeps message/question queries and composer.

- [x] Add failing App integration test asserting session rail visibility and access links on discussion/artifacts/preview and outside main content.
- [x] Run Creative App tests; confirm regression fails because sessions are hidden in Discussion.
- [x] Extract unchanged Creative controls/cards to permanent sibling rail; use flexible center and desktop minimum width preserving readable questions.
- [x] Update session unit tests to mount rail directly; run Creative suite and commit.

### Task 2: Shared central question threads

**Files:** Modify web/src/components/QuestionThreadView.tsx and its tests; creative-web/src/Discussion.tsx, types.ts, style.css and Discussion.test.tsx.

**Interfaces:** QuestionThreadView gains optional readOnly, children context slot, and controlled draft hooks; defaults unchanged. Creative callbacks post existing /reply or /answer contracts and clear retained text only on success.

- [x] Add failing regression tests: newest-first open questions, individually collapsed resolved history with all replies, clarification vs answer payloads, earlier threads surviving new arrival, drafts across navigation and failure.
- [x] Add shared view tests for read-only full history and controlled drafts without changing default behavior; run tests and confirm intended failures.
- [x] Reuse shared QuestionThreadView/Markdown for Creative question and message rendering; retain drafts keyed by question identity; use shared form/action styling for discussion composer.
- [x] Run focused regressions, full Creative and shared web suites and both builds; inspect diff for context/review/renderer preservation.
- [x] Commit, fresh branch review, address acceptance failures, push branch and open one PR referencing creative-work.

## Verification and review

- Full Creative suite: 35/35 passed; full main web suite: 722/722 passed.
- Both production builds pass; whole-branch diff whitespace check passes. Build-generated index files excluded, matching existing Creative PR convention.
- Independent review found one acceptance failure: per-call mutation callbacks skip draft clearing after thread unmount. Two added RED→GREEN regressions cover successful delivery while navigating and retention of newer unsent text; completion now clears only the submitted draft.
- One pre-existing main Task.test attach selector was corrected to the accessible name already supplied by SessionCard; no main-dashboard behavior change.
- Existing environment warnings: Node experimental localStorage and main production bundle size. Real visual cases remain the designated human check; no live runs, restart/config changes or paid calls.
