# Creative chat implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Make Conversation a stable messenger window with useful attachment cards.
**Architecture:** Keep API/cache/read contracts. Discussion owns bounded chronological scroll and draft; FeedEntry owns bubbles; AttachmentCards reuses Media and version/artifact queries.
**Tech Stack:** React, TanStack Query, TypeScript, Vitest/MSW, Playwright.
**Spec:** docs/superpowers/specs/2026-11-11-creative-chat.md

## Global Constraints
- Scope: Conversation only; no feed data, decisions, API or agent rule changes.
- Preserve draft target, auto-attached versions, read-through-highest, collapsed archive.
- English labels; light/dark and narrow screens.
- HTML opens isolated /view; other attachments open existing Media component.

## Review Focus
- Delayed attachment layout must not pull a reader away from history.
- Polling same IDs must preserve draft and scroll.
- Switching hidden chats must initialize scroll only when visible.
- Filter changes must reset the scroll scope without retargeting draft.
- Unavailable attachments must keep the message readable.

### Task 1: Messenger window
**Files:** Discussion.tsx, FeedEntry.tsx, App.tsx, style.css, Conversation.test.tsx (under creative-web/src).
**Interfaces:** Discussion keeps existing props plus optional stageName; FeedEntry gains optional contextLabel.
- [x] Write failing assertions for chronological order, composer below, collapsed context, keyboard submission, polling with reader scrolled up and new-message pill.
- [x] Run `npm test --prefix creative-web -- Conversation.test.tsx`; expect failures for missing new behavior.
- [x] Implement bounded scroll, bottom tracking, date separators, pending jump, compact context and styles.
- [x] Run Conversation, Discussion, FeedCards, FeedUnread tests; expect pass.
- [x] Commit messenger window.

### Task 2: Attachments and delivery
**Files:** creative-web/src/AttachmentCards.tsx and its tests; FeedEntry.tsx; docs/creative-dashboard.md; creative-web/browser/chat-window-check.mjs; docs/screenshots/creative-feed/.
**Interfaces:** AttachmentCards({ids, items}) renders metadata and reuses Media; human context expands BoundVersions.
- [x] Write failing tests for name/item/version, first three/+N, Media expansion and HTML isolated link, unavailable versions.
- [x] Run attachment tests; expect missing component failure.
- [x] Implement cards and delayed-size scroll handling.
- [x] Run full frontend suite, tsc, make creative-web-build and make test; expect pass.
- [x] Capture stage/item/run before and after in both themes and narrow width using mocked HTTP fixtures with 12 versions; inspect screenshots.
- [x] Update docs, commit, request independent review and fix material findings with regression tests.
- [ ] Push branch and open single PR to main referencing creative-messages with screenshot links.

## Verification record

- 90 creative-web tests; 722 shared web tests; Go suite: make test passed.
- make creative-web-build passed (includes tsc --noEmit).
- Playwright capture/acceptance check passed without console errors.
- Independent review: fixed initial unread readiness and asynchronous attachment anchoring with RED to GREEN regressions. A real browser resize check additionally caught a queued duplicate scroll event race; its regression also ran RED to GREEN.
- No API changes. No deferred material review findings.
