# Creative chat polish implementation plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make long Conversation feeds readable and restore a spacious composer.
**Architecture:** Change only Conversation presentation and its visual acceptance fixture. Retain feed, draft, keyboard, and proposal logic.
**Tech Stack:** React, CSS tokens, Vitest, Playwright.
**Spec:** Task 6061 spec v9 brief supplied by creative-messages-orch (subtask 6072).

## Global constraints

Only creative-web Conversation styling/markup; no behavior/API changes. Light theme only. No visible Message label. About left; keyboard hint then Send rightmost. Four default rows with bounded growth. Header, adjacent messages, human/orchestrator colours, proposal and pending states distinct.

## Review focus

- Draft present: discard must not displace Send from the right edge.
- Narrow layout: context may wrap without pushing Send offscreen.
- Long draft: growth stops while history remains scrollable.
- Consecutive same-author messages: borders, shadows, spacing and metadata separate them.
- Proposal status: pending and historical cards remain distinct from messages.

### Task 1: Restore composer and separate conversation surfaces

**Files:** Discussion.tsx, style.css, Conversation.test.tsx under creative-web/src; creative-web/browser/chat-window-check.mjs; docs/screenshots/creative-feed/.
**Interfaces:** Existing Discussion props and HTTP contracts unchanged.

- [x] Update existing row assertion and assert hint precedes Send even with a draft; run focused tests RED.
- [x] Restore four rows/104px minimum and 192px maximum; move Send last. Tint header, strengthen metadata and bubble borders/shadows, preserve proposal distinction.
- [x] Extend browser fixture to 8+ mixed messages and assert geometry, colours, spacing and bounded growth at desktop/narrow widths.
- [x] Run focused tests, capture and inspect 1440x900 and narrow screenshots.
- [x] Run make test and make creative-web-build; review diff; commit and open PR to main.

## Verification record

- RED: Conversation regression expected rows=4, received rows=1.
- GREEN: focused Conversation tests 9/9; make test passed (Go packages, web 722 tests, creative-web 91 tests).
- make creative-web-build passed. Generated dist placeholder restored per repo convention.
- Playwright checks passed in desktop/narrow, both OS schemes and host attributes; 20-line draft capped with internal scrolling. Desktop, narrow and pending screenshots visually inspected.
- First make test run hit an unrelated three-second hook subprocess timeout; isolated test and full rerun passed.
