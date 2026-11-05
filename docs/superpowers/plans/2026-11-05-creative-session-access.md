# Creative Session Access Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Familiar session access for the current Creative run.
**Architecture:** Share presentation-only SessionCard with development-only slots. Share root route builders without importing entire screens. Creative uses its existing run sessions query and shared Session type.
**Tech Stack:** React, TypeScript, Vitest, Testing Library, existing Rocket CSS.
**Spec:** docs/superpowers/specs/2026-11-05-creative-session-access-design.md

## Global Constraints

No backend/lifecycle/global pipeline changes, new terminal/chat stack, kill feature, demos, paid calls or restarts. Existing development actions and Creative context/launch/restore remain. Focused checks and both builds suffice, per orchestrator acceptance.

## Review Focus

Encoded session IDs; tmux name differing from ID; failed clipboard after successful copy; distinct worker model/activity; sessions scoped to the selected run. All checked in the focused integration tests.

### Task 1: Shared session card and Creative integration

**Files:** Create web/src/components/SessionCard.tsx and SessionCard.css; create web/src/lib/sessionPaths.ts; modify SessionRail.tsx/CSS, TermScreen.tsx, ChatScreen.tsx; modify creative-web/src/Discussion.tsx, types.ts, style.css and Discussion.test.tsx.
**Interfaces:** SessionCard takes presentation fields from Session, orchestrator/worker role, optional repository, status and action slots. sessionPaths exports termPagePath/chatPagePath using encodeURIComponent.

- [x] Add Discussion tests for orchestrator/worker identity, model/activity and encoded root links with safe target attributes; copy correct tmux command, pending/success/failure feedback; run switch removes old sessions; no kill/PR actions.
- [x] Run tests, expect new card assertions to fail because access is absent.
- [x] Extract SessionCard preserving existing class names/styles and dev behavior through optional slots. Keep copy confirmation truthful and reset it on every attempt. Move only card CSS into shared file.
- [x] Extract and re-export root path helpers from screens for existing consumers. CreativeSession extends shared Session; render current orchestrator and worker cards above contextual discussion.
- [ ] Run Discussion and SessionRail focused tests and both app builds; expect all pass. Inspect diff against brief.
- [ ] Commit coherent changes, push feature branch and create one PR referencing creative-work 5986 / task 5995. Report URL to orchestrator.
