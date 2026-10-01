# Permission prompts — daemon side Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** the daemon notices a Claude Code permission dialog on a session's pane, exposes it as `pending_quiz` with `source:"permission"`, answers it with one keypress via `POST /quiz/answer`, journals it in `permission_prompts` and shows resolved prompts in `GET /chat` as `role:"permission"`.

**Architecture:** the monitor sweep gets a `pollPermission` step next to `pollQuiz`: it captures `runtime.PermissionCaptureLines` rows, parses them with `runtime.ParsePermissionPrompt`, and drives a small state machine (appear / change / gone-2-ticks) over `sessions.pending_quiz` (compare-and-swap, so a hook quiz written concurrently is never clobbered) and the new `permission_prompts` table. `session.Manager.AnswerQuiz` branches on `Quiz.Source == "permission"`: strict validation, re-capture at the same depth and identity check, mark the journal row as answered from chat, then reuse the existing in-flight / resolved / unconfirmed machinery with a one-key sequence. `GET /chat` merges resolved rows by `ts` and carries a permission watermark inside its opaque cursor so incremental reads deliver each row exactly once.

**Tech Stack:** Go, SQLite (embedded sequential migrations), tmux runtime, net/http.

**Spec:** orchestrator spec v2 «Разрешения в режиме чата — дизайн (задача #4881)» and feature plan Task 2 (inbox `.rocket/inbox/msg-62494.md`, `msg-62495.md`), brief `msg-62493.md`; recon `docs/superpowers/recon/2026-10-01-permission-prompt-recon.md` §5.

## Global Constraints

- Only sessions with `Agent == "claude-code"`; Codex out of scope.
- A quiz without `source` (hook-driven AskUserQuestion) is never touched by the permission logic; `pollQuiz` keeps owning it and must skip permission quizzes.
- Answer = exactly one digit `N+1`, no Enter; `[-1]` → Escape; `text` → 400 `invalid_answer`.
- Before pressing, re-capture the pane; different `Title`+`Context` (or no dialog) → 409 `prompt_changed`, no key sent.
- Monitor capture and pre-press re-capture use ONE constant, `runtime.PermissionCaptureLines = 40`.
- Gone = absent 2 sweeps in a row.
- API contract (clients #93/#94 already built on it): `pending_quiz.source`, `pending_quiz.raw`; question `header:"Разрешение"`, `question:"<Title>\n\n<Context>"` (just `<Title>` when Context is empty), `multi_select:false`, options `{label, description:""}`; chat entry `{role:"permission", ts, text:<title>, permission:{title, context, answer_label, answered_via:"chat"|"terminal"}}`.

## Review Focus

1. Human answers in the terminal while the chat click is in flight → 409 `prompt_changed`, zero keys sent (Task 4 test).
2. Two dialogs in a row (consecutive Bash «Do you want to proceed?» with different Context) → old row closed, new row opened, `quiz_resolved` + `quiz_asked` published, card switches (Task 5 test). `quiz_resolved` for the old one also releases the answer in-flight wait, so no false «Ответ не подтвердился».
3. Daemon restart with a dialog open → the open row with the same identity is reused, no duplicate rows (Task 2 + Task 5 tests).
4. Old client sends `text` for a permission quiz → 400 `invalid_answer`, nothing typed (Task 4 test).
5. Incremental `/chat` reads must not re-deliver permission rows (web treats a repeated first entry as a cursor rollback and replaces the whole feed) → cursor watermark test (Task 6).

---

### Task 1: shared capture depth and dialog identity (runtime)

**Files:** Modify `internal/runtime/permission.go`; Test `internal/runtime/permission_test.go`.

**Produces:**
```go
const PermissionCaptureLines = 40
func (p PermissionPrompt) SameDialog(q PermissionPrompt) bool // Title && Context equal
```

- [ ] Failing test: two prompts with equal Title but different Context are not `SameDialog`; equal Title+Context with different Options/Raw are. Test that parsing a fixture captured at `PermissionCaptureLines` twice yields `SameDialog`.
- [ ] Implement; `go test ./internal/runtime/`; commit.

### Task 2: store — `permission_prompts` and pending-quiz CAS

**Files:** Create `internal/store/migrations/0017_permission_prompts.sql`, `internal/store/permission_prompts.go`, `internal/store/permission_prompts_test.go`; Modify `internal/store/sessions.go`.

```sql
CREATE TABLE permission_prompts (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id   TEXT NOT NULL,
  title        TEXT NOT NULL,
  context      TEXT NOT NULL DEFAULT '',
  options_json TEXT NOT NULL DEFAULT '[]',
  asked_at     INTEGER NOT NULL,
  resolved_at  INTEGER,
  answer_label TEXT NOT NULL DEFAULT '',
  answered_via TEXT NOT NULL DEFAULT ''
);
CREATE INDEX permission_prompts_session ON permission_prompts(session_id, id);
```

**Produces:**
```go
type PermissionPromptRow struct{ ID int64; SessionID, Title, Context, OptionsJSON string; AskedAt, ResolvedAt int64; AnswerLabel, AnsweredVia string }
// Reuses the open row with the same (title, context) for the session; closes
// every other open row of the session (answered_via chat if answer_label set,
// else terminal). Returns the row id and whether it was reused.
func (s *Store) OpenPermissionPrompt(sessionID, title, context, optionsJSON string, askedAt int64) (int64, bool, error)
func (s *Store) MarkPermissionPromptAnswered(id int64, label string) error // label "" clears
func (s *Store) ResolvePermissionPrompt(id int64, resolvedAt int64) error  // no-op if already resolved
func (s *Store) ListResolvedPermissionPrompts(sessionID string) ([]PermissionPromptRow, error) // by id asc
func (s *Store) CompareAndSwapPendingQuiz(id, old, new string) (bool, error) // "" = NULL
```

- [ ] Failing tests: open → open same identity reuses (no duplicate); open different identity closes the previous as `terminal`; marked row closes as `chat` with its label; resolve idempotent; list returns only resolved rows of that session; CAS succeeds on match, fails (false, nil) on mismatch, handles NULL both ways.
- [ ] Implement (transaction in Open); `go test ./internal/store/`; commit.

### Task 3: session — permission quiz shape

**Files:** Create `internal/session/permission.go`, `internal/session/permission_test.go`; Modify `internal/session/quiz.go` (Quiz fields).

`Quiz` gains `Source string \`json:"source,omitempty"\``, `Raw string \`json:"raw,omitempty"\``, and a `Permission *PermissionRef \`json:"permission,omitempty"\`` (`{prompt_id, title, context}`) used internally for identity/journal; the public API never exposes it.

**Produces:**
```go
const QuizSourcePermission = "permission"
type PermissionRef struct{ PromptID int64 `json:"prompt_id"`; Title string `json:"title"`; Context string `json:"context"` }
func NewPermissionQuiz(p runtime.PermissionPrompt, promptID, askedAt int64) Quiz
func ParseQuiz(raw string) (Quiz, bool)
func (q Quiz) IsPermission() bool
func (q Quiz) PermissionPrompt() runtime.PermissionPrompt // Title/Context from Permission ref
```

- [ ] Failing test: JSON of NewPermissionQuiz has `source`, `raw`, `asked_at`, one question with header «Разрешение», question «Title\n\nContext» (or bare Title), multiSelect false, options with empty description; empty Options → `"options":[]`.
- [ ] Implement; commit.

### Task 4: session — answering a permission quiz

**Files:** Modify `internal/session/quiz.go`; Test `internal/session/permission_answer_test.go`; Modify `internal/api/sessions.go` (`prompt_changed` → 409).

Flow in `AnswerQuiz` when `quiz.IsPermission()`:
1. `validatePermissionAnswers`: exactly one answer, `question_index 0`, no text, exactly one option index, `-1` or `0 ≤ i < len(options)` and `i ≤ 8` → else `invalid_answer` (400).
2. `tryStartQuizInFlight` → else `quiz_answer_in_flight`.
3. `rt.Capture(h, runtime.PermissionCaptureLines)` + `ParsePermissionPrompt`; not ok or `!SameDialog` → clear in-flight, `prompt_changed` (409). Capture error → clear in-flight, return error (500).
4. `st.MarkPermissionPromptAnswered(promptID, label)` (label = option label or "Esc").
5. `go m.runQuizAnswer(id, h, steps)` with steps `[digit N+1]` or `[escape]` — no Enter. On send failure, the mark is cleared.

- [ ] Failing tests (fake runtime recording SendKeys and Capture depth): option 1 → exactly one key "2", no Enter; `[-1]` → exactly "Escape"; text → `invalid_answer`, zero keys, zero captures; two indices → `invalid_answer`; changed pane (different Context) → `prompt_changed`, zero keys, in-flight cleared; pane without dialog → `prompt_changed`; capture depth == `runtime.PermissionCaptureLines`; label recorded on the row; hook quiz path unchanged (existing tests).
- [ ] Implement; `go test ./internal/session/ ./internal/api/`; commit.

### Task 5: monitor — permission lifecycle

**Files:** Create `internal/monitor/permission.go`, `internal/monitor/permission_test.go`; Modify `internal/monitor/monitor.go` (sweep call, `permMiss` map + prune, `pollQuiz` skips permission quizzes).

`pollPermission(ctx, sess)`:
- skip unless `sess.Agent == "claude-code"` (`claudeCodeAgent` const);
- parse `sess.PendingQuiz`; a hook quiz → return untouched;
- run only if pending permission quiz exists or the current activity (cache via `m.Activity`) is `waiting_input`/`blocked`;
- capture `runtime.PermissionCaptureLines`; capture error → return;
- dialog present: reset miss; if no pending or `!SameDialog(pending)` → `OpenPermissionPrompt` (closes the previous), build quiz, CAS `pending_quiz` old→new; on success publish `session.quiz_resolved` first when replacing a previous permission quiz, then `session.quiz_asked`. Same dialog → nothing (keeps asked_at).
- dialog absent with a pending permission quiz: miss++; at 2 → CAS pending→"" ; on success `ResolvePermissionPrompt` and publish `session.quiz_resolved`.

- [ ] Failing tests with the shared `quizFakeRuntime` (+ recorded capture depth) and fixtures from `internal/runtime/testdata`: appear → pending with source permission + quiz_asked + one open row; same dialog over 3 sweeps → still one row, no new events; Context change → old row resolved `terminal`, new open, resolved+asked published; gone 1 tick → still pending; gone 2 ticks → cleared, quiz_resolved, row `terminal`; marked row → `chat`; hook quiz pending + dialog-like pane → untouched, no rows; non-claude-code agent → untouched; restart (fresh Monitor over same store with pending + open row) → no duplicate row; activity `ready` with no pending → no capture; `pollQuiz` leaves a permission quiz alone; capture depth == `runtime.PermissionCaptureLines`.
- [ ] Implement; `go test ./internal/monitor/`; commit.

### Task 6: API — response shape and chat merge

**Files:** Modify `internal/api/quiz.go`, `internal/api/chat.go`; Tests `internal/api/quiz_test.go`, `internal/api/chat_test.go`.

- `quizResponse` gains `Source string \`json:"source,omitempty"\``, `Raw string \`json:"raw,omitempty"\``.
- `chatEntryResponse` gains `Permission *permissionEntry \`json:"permission,omitempty"\``.
- Merge: resolved rows → entries `{role:"permission", ts: asked_at, text: title, permission{...}}`. Tail read: merge all, stable-sort by ts, then cut to `limit`. Cursor read: only rows with `id > watermark`, appended after transcript entries in ts order. `next_cursor = adapterCursor + "#p" + maxResolvedID` (only when the session has resolved rows); an incoming cursor is split at a trailing `#p<digits>` suffix (adapter cursors end in `:<offset>`, so the suffix is unambiguous). A non-empty cursor without suffix (a client loaded before the upgrade) ⇒ watermark = current max id: nothing historic is dumped at the end of its feed; it starts tracking from now, and its next tail load shows the history in place.

- [ ] Failing tests: `/sessions` and `/chat` pending_quiz shows `source`, `raw`, header, question, options; hook quiz has no `source`; tail read interleaves a permission entry by ts with `answered_via`/`answer_label`; next_cursor carries `#p<id>`; a follow-up read with that cursor returns no permission entry again; a row resolved after the first read appears exactly once in the next read; open rows never appear; answer endpoint maps `invalid_answer`→400, `prompt_changed`→409.
- [ ] Implement; `go test ./internal/api/`; commit.

### Task 7: docs, full verification, e2e

- [ ] `docs/13-chat.md`: new section «Разрешения (permission-диалоги)»: detection, shape, answer rules, errors, chat entry, cursor suffix. `docs/03-daemon-api.md`: quiz/answer row (`[-1]`, `invalid_answer`, `prompt_changed`), events note.
- [ ] `go vet ./... && go test ./...`.
- [ ] Manual e2e on a throwaway daemon (`ROCKET_HOME` in scratchpad, own socket) and a throwaway claude session: plan mode → «2. Yes, manually approve edits» → a Bash command → curl `/chat` shows pending permission → curl answer → agent continues; terminal answer → entry `terminal`. Describe in PR.
- [ ] PR.
