# Mobile permission card Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The mobile chat shows a pending Claude Code permission dialog as a one-tap card and shows resolved permission prompts as compact entries.

**Architecture:** The daemon publishes the dialog as `session.pending_quiz` with `source:"permission"` and logs resolved ones as `role:"permission"` chat entries. The client stays thin: types, pure helpers in `src/lib/permission.ts`, a `PermissionCard.tsx` component (pending and resolved), a branch inside `PendingQuizCard`, a small event fan-out in `src/api/events.ts` for `session.quiz_answer_unconfirmed`, and one extra branch in `EntryBubble`.

**Tech Stack:** Expo 57 / React Native 0.86, @tanstack/react-query, jest-expo + @testing-library/react-native, TypeScript.

**Spec:** task #4881 design «Разрешения в режиме чата» (orchestrator message msg-62375) + brief msg-62374.

## Global Constraints

- The API contract is fixed; without the backend (no `source`, no `role:"permission"`) behaviour must be unchanged.
- Copy: badge «Разрешение»; resolved «Разрешение: <title> → <answer_label>» / «Разрешение: <title> → отвечено в терминале»; errors «Диалог изменился, обновляю…» (409 `prompt_changed`) and «Ответ не подтвердился» (SSE `session.quiz_answer_unconfirmed`).
- Answer body: `{"answers":[{"question_index":0,"option_indices":[i]}]}`; Esc = `option_indices:[-1]`. Never send `text`.
- One tap = answer: no confirmation step, no «Other» row, no special colouring per option.
- No tests inside `mobile/app/` (bundling breaks) — screen tests live in `mobile/__tests__/`.
- Regular quizzes unchanged.

## Review Focus

- Dialog changes between taps (new `asked_at`/question): card must reset its in-flight state, not stay disabled forever → parent keys the card by `asked_at` + question; test.
- 409 `prompt_changed`: buttons re-enable and the error copy shows; test.
- Unconfirmed event for ANOTHER session must not affect this card; test.
- `raw` is long (20 lines) with empty options: shown monospace in a scrollable block, only «Esc»; test.
- Worker (read-only) session with a pending permission: card visible, composer and read-only bar hidden; test.

---

### Task 1: Types and pure helpers

**Files:**
- Modify: `mobile/src/api/types.ts` (ChatRole, ChatEntry, PendingQuiz)
- Create: `mobile/src/lib/permission.ts`, `mobile/src/lib/permission.test.ts`

**Interfaces — Produces:**
```ts
// types.ts
export type ChatRole = 'user' | 'assistant' | 'tool' | 'quiz_answer' | 'permission'
export interface PermissionEcho { title: string; context?: string; answer_label?: string; answered_via: 'chat' | 'terminal' }
// ChatEntry.permission?: PermissionEcho
// PendingQuiz.source?: string; PendingQuiz.raw?: string
// permission.ts
export const ESC_INDEX = -1
export function isPermissionQuiz(q: PendingQuiz | undefined): boolean
export function splitPermissionQuestion(question: string): { title: string; context: string }
export function resolvedPermissionLine(p: PermissionEcho): string
export function permissionErrorText(e: unknown): string
export const UNCONFIRMED_TEXT = 'Ответ не подтвердился'
```

- [ ] Step 1: tests — `splitPermissionQuestion('T?\n\nctx\nline')` → `{title:'T?', context:'ctx\nline'}`; no `\n\n` → whole text is the title; `resolvedPermissionLine` chat/terminal/missing label (chat without label → «отвечено»); `permissionErrorText(new ApiError('prompt_changed', 'x', 409))` → «Диалог изменился, обновляю…», other errors → their message; `isPermissionQuiz` for undefined / plain / permission.
- [ ] Step 2: run `npx jest src/lib/permission.test.ts` → FAIL (module missing).
- [ ] Step 3: implement.
- [ ] Step 4: run → PASS; `npx tsc --noEmit`.
- [ ] Step 5: commit.

### Task 2: Daemon event fan-out

**Files:**
- Modify: `mobile/src/api/events.ts`
- Test: `mobile/src/api/events.test.ts`

**Interfaces — Produces:**
```ts
export function subscribeDaemonEvents(cb: (type: string, data: string) => void): () => void
export function emitDaemonEvent(type: string, data: string): void   // used by useEventStream.onEvent
export function useSessionEvent(sessionId: string, type: string, handler: () => void): void
```
`data` is the SSE JSON `{id, ts, type, session_id, data}`; `useSessionEvent` fires only when `type` matches and `session_id === sessionId` (malformed JSON ignored).

- [ ] Step 1: tests — subscriber gets emitted events; unsubscribe stops it; `useSessionEvent` (renderHook) fires for matching session, not for another session, not for another type, not on bad JSON.
- [ ] Step 2: FAIL. Step 3: implement (module-level `Set` of listeners; `useEventStream` calls `emitDaemonEvent(type, data)` before invalidation; handler kept in a ref). Step 4: PASS. Step 5: commit.

### Task 3: Permission cards

**Files:**
- Create: `mobile/src/components/PermissionCard.tsx`
- Modify: `mobile/src/components/QuizCard.tsx` (branch at top of `PendingQuizCard`)
- Test: `mobile/__tests__/chat/permission-card.test.tsx`

**Interfaces — Produces:**
```ts
export function PendingPermissionCard(props: { sessionId: string; quiz: PendingQuiz }): JSX.Element
export function ResolvedPermissionCard(props: { permission?: PermissionEcho; fallback: string }): JSX.Element
```
Behaviour: badge «Разрешение», title (bold), context in `MonoText` block if non-empty; one full-width `Pressable` (minHeight 48, `accessibilityRole="button"`) per option, in order; tap → `useQuizAnswer().mutate({sessionId, answers:[{question_index:0, option_indices:[i]}]})`. Empty options → `raw` in a monospace `ScrollView` (maxHeight ~220) + single «Esc» button sending `[-1]`. State: `sent` (true after 202 until unmount / reset), inline `error` text. While `isPending || sent` all buttons disabled and a «Отправляю ответ…» hint shows. onError → `error = permissionErrorText(e)`, `sent=false`. `useSessionEvent(sessionId, 'session.quiz_answer_unconfirmed', …)` → `error = UNCONFIRMED_TEXT`, `sent=false`. `PendingQuizCard` returns `<PendingPermissionCard/>` when `isPermissionQuiz(quiz)`, before any hook usage differences (branch by rendering a separate component to keep hook order valid).

- [ ] Step 1: tests (render inside QueryClientProvider + ServerProvider + ToastProvider, mocked fetch):
  - options rendered, title/context split, no «Other»/«Answer»; tapping «Yes, and don't ask again» posts `option_indices:[1]` with no `text`; buttons disabled afterwards.
  - empty options → raw text visible, «Esc» posts `[-1]`.
  - 409 `prompt_changed` → «Диалог изменился, обновляю…», buttons enabled again.
  - `emitDaemonEvent('session.quiz_answer_unconfirmed', JSON with session_id)` after a successful tap → «Ответ не подтвердился»; for another session → nothing.
  - `ResolvedPermissionCard` chat/terminal copy.
  - Plain quiz through `PendingQuizCard` still shows «Answer» and «Other — type your own».
- [ ] Step 2: FAIL. Step 3: implement. Step 4: PASS + tsc. Step 5: commit.

### Task 4: Chat screen wiring

**Files:**
- Modify: `mobile/app/chat/[id].tsx` (EntryBubble branch for `role === 'permission'`; key the pending card by `asked_at` + first question so a new dialog resets state)
- Test: `mobile/__tests__/chat/chat-permission.test.tsx`

- [ ] Step 1: tests (pattern from `__tests__/chat/chat-agent.test.tsx`, session kind `worker`, `agent` param undefined):
  - worker session with permission pending_quiz → card and option buttons visible, no read-only bar, no composer.
  - resolved entries → «Разрешение: Do you want…? → Yes» and «… → отвечено в терминале» rendered, not hidden as noise.
  - no `source` / no permission entries → existing behaviour (read-only bar for worker).
- [ ] Step 2: FAIL. Step 3: implement. Step 4: full `npx jest` + `npx tsc --noEmit`. Step 5: commit.

### Task 5: Verify, screenshots, PR

- [ ] Full `npx jest`, `npx tsc --noEmit` green.
- [ ] Screenshots of the pending (options + raw fallback) and resolved cards (iOS simulator with a mock daemon, or Expo web if available); attach to PR.
- [ ] `docs/13-chat.md` mobile note only if the backend docs task does not cover clients (check with orchestrator; default: no docs change here).
- [ ] `gh pr create` referencing feature task-4881; report URL to orchestrator.
