# Questions v3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the web Questions screen with the designer-approved v3 UI — a Decide (focus) mode with a queue rail, one-tap options and hotkeys, a Browse mode with search/filters/grouped rows, an "Ask an agent" composer, and toast+undo — wired to the existing daemon API.

**Architecture:** One screen shell (`QuestionsScreen`) owns mode, selection, hotkeys, toast and the deferred-action queue; two presentational modes (`FocusMode`, `BrowseMode`) and a composer (`AskComposer`) hang off it. All derivations (queue order, status chip, browse grouping, search match) live in a pure `model.ts` so they are unit-testable without rendering. Undo is a client-side 5s deferred action (`deferred.ts`) because the server has no undo and a human cannot reopen a closed thread; the pending API call fires on timeout, on the next action, or is cancelled by Undo. Thread bodies come from `GET /v1/threads`; the current card's `context`/`messages` come from the per-subject questions endpoints, which already back the old screen.

**Tech Stack:** React 19 + TypeScript, @tanstack/react-query, react-router-dom, vitest + @testing-library/react + msw. Styling: one plain CSS file with the project's design tokens (`web/src/styles/tokens.css`), matching the existing screens' convention (no CSS-in-JS libraries, no Tailwind).

## Global Constraints

- Design source of truth: the v3 prototype (markup = exact visual spec, `Component` class = exact interaction/label/toast spec). Inline styles become a CSS file; the rendered result must match.
- Do NOT copy the prototype's mock data or fake targets (`orch-1023`, `cto` literals) into the app. Composer targets are real: root tasks with a live orchestrator session, plus registered agents.
- No Go changes. `GET /v1/threads` (`internal/api/thread_inbox.go`) carries no `context` and no `messages` — the focus card fetches those per subject.
- `choose` is a **1-based** index into `options` (see `useAnswerQuestion` in `web/src/lib/queries.ts`).
- Human participant ids are mid-migration (`""` vs `"human"`): always go through `isHuman()` / `participantLabel()` from `web/src/lib/participants.ts`.
- Route stays `/questions`. Mobile is out of scope.
- Hotkeys must be inert while focus is in a `TEXTAREA` or `INPUT`.
- Acceptance gate: `cd web && npx tsc -b && npx vitest run && npm run build` all green.

---

## File Structure

- `web/src/screens/questions/model.ts` — pure derivations: queue order, status chip, browse groups, search match, target list. No React.
- `web/src/screens/questions/model.test.ts` — unit tests for the above.
- `web/src/screens/questions/deferred.ts` — the 5s deferred-action queue used by toast+undo.
- `web/src/screens/questions/deferred.test.ts` — timer-driven unit tests.
- `web/src/screens/questions/QuestionsScreen.tsx` — shell: header strip, mode segmented control, composer slot, hotkeys, toast, hints bar.
- `web/src/screens/questions/FocusMode.tsx` — queue rail + `ThreadCard`, empty state.
- `web/src/screens/questions/ThreadCard.tsx` — one thread: chips, FYI banner, options, context, conversation, composer, closed view.
- `web/src/screens/questions/BrowseMode.tsx` — search input, filter chips, grouped rows, empty state.
- `web/src/screens/questions/AskComposer.tsx` — kind + target + textarea + submit.
- `web/src/screens/questions/questions.css` — all v3 styles (replaces `QuestionsScreen.css`).
- `web/src/screens/questions/Questions.test.tsx` — screen-level tests, rewritten for the new UI.
- Modify: `web/src/lib/queries.ts` — add `useAskThread()` (target-parameterised ask) and `useThreadActions()` (task/role dispatch for answer/reply).
- Modify: `web/src/components/AppShell.tsx` — amber nav badge per design.
- Modify: `web/src/styles/tokens.css` — the handful of neutrals/greens the v3 design uses that have no token yet.
- Delete: `web/src/screens/questions/QuestionsScreen.css`.

---

### Task 1: deferred-action queue

**Files:**
- Create: `web/src/screens/questions/deferred.ts`
- Test: `web/src/screens/questions/deferred.test.ts`

**Interfaces:**
- Produces: `createDeferredQueue(delayMs: number): DeferredQueue` with
  `schedule(run: () => void): void`, `cancel(): boolean`, `flush(): void`,
  `isPending(): boolean`, `dispose(): void`.

- [ ] **Step 1: Write the failing test** — `deferred.test.ts`: fires after the delay; `cancel()` prevents it and returns true; `flush()` runs it immediately and only once; scheduling a second action flushes the first; `dispose()` cancels without running.
- [ ] **Step 2: Run** `npx vitest run src/screens/questions/deferred.test.ts` — expect FAIL (module missing).
- [ ] **Step 3: Implement** `deferred.ts` — a closure holding `timer` and `pendingRun`; `schedule` flushes any prior pending action first, then arms `setTimeout(flush, delayMs)`.
- [ ] **Step 4: Run** the same command — expect PASS.
- [ ] **Step 5: Commit** `feat(web): deferred-action queue for questions undo (#1023)`.

### Task 2: pure derivations (`model.ts`)

**Files:**
- Create: `web/src/screens/questions/model.ts`
- Test: `web/src/screens/questions/model.test.ts`

**Interfaces:**
- Produces:
  - `queueOf(threads: ThreadInboxEntry[], later: ReadonlySet<number>): ThreadInboxEntry[]` — open + `your_turn`, stale first then oldest `updated_at` first; entries in `later` sink to the end.
  - `statusChip(entry): { label: string; tone: 'turn' | 'waiting' | 'closed' | 'note' }`.
  - `matchesQuery(entry, query: string): boolean` over `local_ref`, `subject`, `task_title`, `body`.
  - `browseGroups(threads, filter: BrowseFilter, query): BrowseGroup[]` with `BrowseFilter = 'mine' | 'open' | 'closed' | 'all'` and groups `Your turn` / `Waiting on agents` / `Closed & notes`.
- [ ] Steps 1–5 as in Task 1 (test first, run red, implement, run green, commit).

### Task 3: API wiring hooks

**Files:**
- Modify: `web/src/lib/queries.ts`
- Test: `web/src/lib/queries.test.tsx`

**Interfaces:**
- Produces:
  - `useAskThread()` — mutation `{ target: { kind: 'task'; id: number } | { kind: 'role'; id: string }; body: string; type?: 'decision' | 'fyi' }` → POST `/v1/tasks/{id}/questions` or `/v1/agents/{id}/questions`; invalidates `['threads']`.
  - `useThreadActions()` — `{ answer(entry, payload), reply(entry, payload) }` dispatching to the task or agent-question endpoint by `entry.kind`.
- [ ] Test-first against the msw handlers, then implement, then commit.

### Task 4: Focus mode

**Files:** create `FocusMode.tsx`, `ThreadCard.tsx`, `questions.css`; rewrite `QuestionsScreen.tsx`; delete `QuestionsScreen.css`.

Covers: queue rail (ref, STALE, age, 2-line body, options hint, active rule amber/red), thread card (ref chip, subject link, status chip, age, FYI banner, question, "asked by", numbered option buttons with "closes thread", Show context (E), Conversation toggle, composer with Answer & close (⌘↩) / Ask back — keep open / Later (S) / Not relevant (X), "Turn passes to" chips, closed view with the green resolution box), Queue-clear empty state, header strip (`N decisions on you`, stale/cleared subline, progress bar), hints bar, toast+undo, hotkeys 1–9/J/K/E/S/X/B/Z disabled while typing.

- [ ] Test-first in `Questions.test.tsx` (msw fixtures), implement, run green, commit.

### Task 5: Browse mode

**Files:** create `BrowseMode.tsx`; wire into `QuestionsScreen.tsx`.

Covers: search input, four filter chips with counts, grouped row lists, row CTA `Decide`/`Open` jumping into focus mode on that thread, `Nothing matches` empty state, `N shown` counter.

- [ ] Test-first, implement, run green, commit.

### Task 6: Ask-an-agent composer

**Files:** create `AskComposer.tsx`.

Covers: `Ask an agent` toggle button in the header strip, kind pills (Question / FYI note), real target pills (root tasks with a live orchestrator via `useTasks`, agents via `useAgents`), hint line, textarea with kind-specific placeholder, `Open question` / `Post note` submit, toast on success. FYI posts with `type: 'fyi'` (born resolved).

- [ ] Test-first, implement, run green, commit.

### Task 7: live updates + nav badge

**Files:** modify `QuestionsScreen.tsx`, `web/src/components/AppShell.tsx`, `web/src/styles/tokens.css`.

Covers: a newly-arrived `your_turn` thread (seen after an SSE-driven refetch) raises the `"<ref> arrived — added to your queue"` toast; nav badge amber-filled per design.

- [ ] Test-first, implement, run green, commit.

### Task 8: verification

- [ ] `cd web && npx tsc -b && npx vitest run && npm run build` — all green, output pasted into the PR body.
- [ ] Side-by-side pass against the prototype for both modes.
- [ ] Open the PR referencing feature task-1023, subtask #1049.
