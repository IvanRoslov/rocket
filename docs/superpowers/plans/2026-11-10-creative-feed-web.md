# Creative Feed Web Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver creative-messages task 6063: conversation feeds, proposal decisions, unread counts and old question archive.

**Architecture:** Keep existing workspace/version state. Add feed-specific query/HTTP helpers, feed entry rendering and a separate archive while Discussion owns frozen message drafts and item-filter/jump state. ProcessRail shares the unread query.

**Tech Stack:** React 19, TypeScript, TanStack Query 5, existing shared Markdown/Button components, Vitest/Testing Library/MSW, browser contract mocks.

**Spec:** docs/superpowers/specs/2026-11-10-creative-feed-web-design.md; docs/creative-agents.md Conversation feed HTTP contract.

## Global Constraints
- One feed per run and stage; item is a tag/read filter, never its own feed.
- Poll feed and unread every 2000 ms while a run is open.
- Preserve original draft context and displayed immutable versions on navigation and failure.
- Existing review, session rail, artifacts and API remain unchanged.
- No live daemon restart, binary install or paid calls. PR merges after core.

## Review Focus
- Response arriving after scope navigation must not mark the new scope read with an old ID.
- Hidden discussion must not clear unread badges while browsing artifacts.
- Replacements may cross feeds or item filters; resolve successor scope before navigation.
- A 409 response's entry must update immediately even when subsequent polling fails.
- Read failures must surface and retry without an immediate request loop.

### Task 1: Conversation and archive

**Files:** create src/QuestionArchive.tsx, src/feed.ts (under creative-web); modify Discussion.tsx, types.ts, Discussion.test.tsx, App.test.tsx.
**Interfaces:** Discussion consumes Context, optional stage item names, active visibility and navigation callback; feedKey(scope) identifies run/stage/optional item filter; useFeed(scope) returns polled FeedEntry[]; archive consumes exact Context.

- [ ] Add failing component tests for composer-before-feed, descending IDs, metadata/versions, run/stage/item-filter scope, draft freeze/discard/failure, archived question replies/answers and no Open question. Update existing integration expectations to messages.
- [ ] Run focused tests and confirm missing feed behavior fails.
- [ ] Implement entry types, polled feed query, composer and exact-scope collapsed archive; retain existing reply implementation.
- [ ] Run focused tests until green; commit independently.

### Task 2: Proposal cards

**Files:** create creative-web/src/FeedEntry.tsx; modify feed.ts, Discussion.tsx, Discussion.test.tsx.
**Interfaces:** decideCard(runID, entryID, decision) returns entry; conflict error carries authoritative entry. FeedEntryView accepts entry, item display names and successor navigation callback. Update cached feed variants by entry ID.

- [ ] Add failing tests for Go/Revise/Deny, pending highlight, decided time/no actions, replaced navigation across feeds/filter, immediate 409 entry update while refetch fails, and local errors.
- [ ] Confirm RED, implement cards and local decision HTTP envelope handling with shared authentication behavior.
- [ ] Run focused tests GREEN; commit.

### Task 3: Unread and integration

**Files:** modify feed.ts, Discussion.tsx, RunWorkspace.tsx, App.tsx, style.css; add creative-web/src/FeedReadMarker.tsx and unread tests.
**Interfaces:** useUnread(runID) returns run total/stage counts; FeedReadMarker consumes visible, loaded feed scope, highest displayed ID and data update timestamp. Only unread invalidates after read, avoiding feed refetch loops.

- [ ] Write failing tests for badge appearance/clearing, stage/run isolation, filtered read ID, empty feed zero, hidden discussion, navigation during pending fetch, poll refresh and error recovery.
- [ ] Confirm RED; implement marker lifecycle, rail badges, active visibility and workspace item names/navigation.
- [ ] Run complete creative suite and TypeScript build GREEN; commit.

### Task 4: Browser, documentation and delivery

**Files:** docs/creative-dashboard.md; browser fixture/check and screenshots under creative-web browser validation directory.

- [ ] Exercise exact-contract browser mocks: stage conversation, pending card, Go result, replaced successor, badge appearing/clearing, archive; inspect screenshots and attach to PR.
- [ ] Update dashboard documentation and plan/ledger outcomes.
- [ ] Run make test and make build; inspect results and git diff --check.
- [ ] Whole-branch review, fix acceptance defects with regression tests, push feature branch and open PR referencing creative-messages/task 6063, core dependency and verification evidence.
