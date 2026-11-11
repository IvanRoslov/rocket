# Creative chat space implementation plan

> **For agentic workers:** Use superpowers:executing-plans inline with TDD and independent final review.

**Goal:** Give conversation the main viewport space and stop automatically attaching artifact versions to human messages.

**Architecture:** Preserve existing discussion, review and archive logic. Put the review instance above Sessions in a responsive sidebar; let viewport flex/grid allocate chat height. Show the archive within chat via header navigation. Keep historical human context rendering unchanged.

**Tech Stack:** React, CSS, Vitest/MSW, mocked Playwright acceptance.

**Spec:** Orchestrator spec v7+v8 in .rocket/inbox/msg-82780.md; layout summary accepted via rocket.

## Global constraints

- Human sends omit artifact_version_ids; stage/item target stays frozen for drafts.
- Historical messages retain their collapsed versions.
- Formal decisions remain exclusively in ReviewActions.
- Desktop 1440x900 page does not scroll during conversation; only history scrolls.
- Narrow pending review appears above chat; light screenshots only.

## Review focus

- Selection changes, failed sends and keyboard sends keep original stage/item without versions.
- Historical version links remain accessible.
- Long code/unbroken text wraps without horizontal scrolling.
- Review actions and pending state remain visible with many submitted versions.
- Artifacts, old question actions and drafts keep working across tabs/panels.

### Task 1: Human message target

Files: Discussion.tsx, Conversation.test.tsx, Discussion.test.tsx, browser/chat-window-check.mjs.

- [x] Change payload expectations to omit versions; add stage-send and composer-context assertions; run focused tests and observe failure.
- [x] Remove automatic version IDs from POST and composer context, keeping frozen scope and keyboard sending.
- [x] Run focused and creative suites; commit alongside the shared composer/layout change.

### Task 2: Conversation layout

Files: App.tsx, Discussion.tsx, QuestionArchive.tsx, ReviewActions.tsx, style.css; component/browser tests; docs/creative-dashboard.md; screenshots.

- [x] Add integration tests for single sidebar review and tabs, archive panel and compact accessible composer; observe failures.
- [x] Add 1440x900/narrow browser assertions and pending/idle fixtures; observe layout failure before CSS changes.
- [x] Implement responsive sidebar and flex viewport layout; remove estimated chat height. Keep archive and history mounted while switching their visibility.
- [x] Grow one-row textarea to a few lines; widen bubbles and wrap code. Highlight pending review, bound results list and compact idle state.
- [x] Run component/browser checks, capture pending/idle desktop/narrow screenshots, update dashboard and screenshot docs.
- [x] Run make test, make creative-web-build and diff check; independent review; commit and publish second PR on authorized force-with-lease branch.

Review correction: Growing the composer hid 66px of newest content. Observe history viewport as well as content; browser coverage checks bottom following and older-anchor preservation during growth.
