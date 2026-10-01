# Storm questions only in the Brainstorm tab — Implementation Plan

> **For agentic workers:** executed natively (superpowers:executing-plans), TDD per task.

**Goal:** storm threads (`type: 'brainstorm'`) leave the Questions tab and the inbox; the inbox shows one "Storm #N «title»: K questions waiting" row per task that links to the Brainstorm tab and counts as one badge item.

**Architecture:** client-only. A pure helper per client (`web/src/lib/storm.ts`, `mobile/src/lib/storm.ts`) groups open storm threads waiting on the human (`status==='open' && your_turn`) by `task_id`, and computes the inbox count (non-storm your-turn threads + storm groups). Screens filter storm threads out and render the groups.

**Tech Stack:** React + vitest/msw (web), Expo/React Native + jest (mobile).

**Spec:** task-4901 spec v2 §3.1 (brief: `.rocket/inbox/msg-63199.md`).

## Global Constraints
- UI labels English. Row text: `Storm #<id> «<title>»: <K> question(s) waiting`.
- No daemon API change; GET /v1/threads unchanged.
- Brainstorm tab must keep working (QuestionThread / QuestionCard storm code stays).

## Review Focus
- Storm thread waiting on an agent (your_turn=false) → no storm row, no badge.
- Resolved storm thread → never in inbox.
- Two tasks with storms → two rows, badge +2; one task with 3 → one row "3 questions", badge +1.
- Task banner whose first open question is a storm → jumps to Brainstorm, no option buttons.
- Questions-tab count/warn excludes storm threads.

### Task 1: web storm helper (`web/src/lib/storm.ts` + test)
`isStorm(t)`, `stormGroups(threads): StormGroup[]` ({taskId, projectId?, taskTitle?, count, stale, updatedAt}, stale first then oldest), `stormLabel(g)`, `stormHref(g)` (`/p/<project>/tasks/<id>?tab=brainstorm`), `inboxCount(threads)`.

### Task 2: web inbox + badge
QuestionsScreen drops storm rows from every view, renders storm rows in the Decide rail (and the Browse "Your turn" area); headline counts groups as one. Remove now-dead storm code from ThreadCard / QuestionsScreen.choose. AppShell badge uses `inboxCount`.

### Task 3: web task screen
QuestionsTab and its tab count exclude storm questions; QuestionBanner gets `onOpen` → Brainstorm for storm questions and hides its options.

### Task 4: mobile helper + inbox + tab badge
Same helper in mobile; `(tabs)/questions.tsx` filters storms and renders storm rows (router to `/task/<id>?tab=brainstorm`); `_layout.tsx` badge uses `inboxCount`; drop dead storm code in mobile ThreadCard.

### Task 5: mobile task screen
`task/[id].tsx` honours `?tab=`, Questions chip excludes storms, awaiting banner routes a storm to the Brainstorm chip.

### Task 6: verify
web `tsc -b` + vitest; mobile `npm ci`, typecheck, jest; `go test ./...`.
