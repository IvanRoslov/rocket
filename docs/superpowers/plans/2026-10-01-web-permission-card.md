# Web permission card Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The web chat (`/chat/:sessionId`) renders a Claude Code permission prompt (`pending_quiz.source === "permission"`) as a one-tap answer card, and resolved prompts (`role:"permission"` entries) as compact feed cards.

**Architecture:** Purely client-side, against the fixed API contract of task #4881. `ChatScreen.tsx` branches on `pendingQuiz.source`: permission → new `PermissionCard`, otherwise the untouched `LiveQuizBubble`. The feed loop gets a `role:"permission"` branch rendering `PermissionEntryRow`. Without the backend nothing changes (no `source`, no permission entries).

**Tech Stack:** React 19, TypeScript, vitest + Testing Library + msw (mocks in `web/src/mocks`).

**Spec:** orchestrator messages `.rocket/inbox/msg-62371.md` (brief + API contract) and `msg-62372.md` (design, §3 and §5).

## Global Constraints

- Copy: badge «Разрешение»; resolved «Разрешение: <title> → <answer_label>» / «Разрешение: <title> → отвечено в терминале»; errors «Диалог изменился, обновляю…» (409 `prompt_changed`) and «Ответ не подтвердился» (SSE `session.quiz_answer_unconfirmed`).
- Answer body: `{"answers":[{"question_index":0,"option_indices":[i]}]}`; Esc = `option_indices:[-1]`.
- One tap = answer: no confirm step, no «Другое…» free-text row.
- Card visible/clickable for any session kind (workers included); composer hidden while pending.
- Regular quizzes unchanged. `web/dist` is not committed (gitignored) — do not commit a build.

## Review Focus

1. Question text without `\n\n` (no context) → title only, no empty monospace block.
2. Double tap on an option while the POST is in flight → only one POST (buttons disabled).
3. After «Ответ не подтвердился» the buttons become clickable again (retry possible) — otherwise the card is a dead end.
4. 409 `no_pending_quiz` (answered in terminal meanwhile) → refetch, no error text.
5. New prompt (different `asked_at`) after an error → error cleared, buttons enabled.

---

### Task 1: Types + mock fixtures

**Files:** Modify `web/src/lib/types.ts`, `web/src/mocks/fixtures.ts`, `web/src/mocks/handlers.ts`.

- `PendingQuiz` gains `source?: 'permission'`, `raw?: string`.
- `ChatRole` gains `'permission'`; `ChatEntry` gains `permission?: PermissionEntry` with `{title: string; context: string; answer_label: string; answered_via: 'chat' | 'terminal'}`.
- Fixtures: worker session `s-perm-demo-worker` (kind `worker`, running) with a permission `pending_quiz` (3 options, Title + `\n\n` + Context) and chat entries incl. two resolved `role:"permission"` entries (chat / terminal); export `permDemoPendingQuiz`.
- Handler: for `source:"permission"` answers accept exactly one option index, allow `-1` (Esc), reject `text` with 400 `invalid_answer`.

### Task 2: PermissionCard (pending)

**Files:** Modify `web/src/screens/chat/ChatScreen.tsx`, `ChatScreen.css`; Test `ChatScreen.test.tsx` (new `describe('ChatScreen permission prompt')`).

Tests first:
- renders badge «Разрешение», title, context in `<pre>`, one button per option, no textarea/«Ответить»;
- click option 2 → POST body `{answers:[{question_index:0, option_indices:[1]}]}`, buttons disabled after click;
- empty options → `raw` in `<pre>` + single «Esc» button → POST `option_indices:[-1]`;
- 409 `prompt_changed` → «Диалог изменился, обновляю…» shown, refetch, buttons re-enabled;
- SSE `session.quiz_answer_unconfirmed` → «Ответ не подтвердился» inside card, buttons enabled;
- worker session: card visible and clickable, composer hidden with «агент ждёт разрешения»;
- question without context → no `<pre>`.

Implementation: `splitPermissionQuestion(q) → {title, context}` (split on first `\n\n`); `PermissionCard({sessionId, quiz, unconfirmed, onRefetch})` with `sendState`, `error`, reset on `asked_at`; `unconfirmed` → idle + error copy. In `ChatScreen`, render `PermissionCard` when `pendingQuiz.source === 'permission'`, suppress the global unconfirmed banner for permission (card shows it inline).

### Task 3: Resolved permission entries

Tests first: entries with `role:"permission"` render «Разрешение: <title> → <answer_label>» and «… → отвечено в терминале»; not grouped with tools, not a markdown bubble.

Implementation: `PermissionEntryRow` + branch in feed loop before `EntryBubble`.

### Task 4: Verification + PR

- `cd web && npm test && npm run build`; go tests untouched (`go build ./...` only to confirm embed still compiles).
- Screenshots via vite dev + msw mocks (check `web/src/main.tsx` for mock mode) with Playwright; attach to PR.
- `gh pr create`, notify orchestrator.
