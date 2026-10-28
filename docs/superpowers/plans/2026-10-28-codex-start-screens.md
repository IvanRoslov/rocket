# Codex start screens Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Codex session stuck on an interactive start screen (trust, update, background server, or a new one of the same shape) becomes `blocked`, shows the screen as a permission quiz in the dashboard, and alerts its parent orchestrator once. rocket never presses keys on its own.

**Architecture:** `runtime.ParseStartScreen` reads a Codex selection screen off the pane. The codex adapter implements the optional `agent.StartupProber` (`HasStartedTurn`: there is a `task_started` in the journal). The monitor runs the check (`checkStartScreen`) inside `pollSession`, like `correctStaleInputWait`, only while `HasStartedTurn` is false. It reuses the permission-quiz machinery (journal row, `pending_quiz` with source `permission`) and flags the quiz `StartScreen`, so the answer path re-reads the pane with `ParseStartScreen`.

**Tech Stack:** Go, tmux, SQLite.

**Spec:** task #5707 docs `spec` v1 §4 and `plan` v1 T5 (`rocket task show 5707`).

## Global Constraints

- Claude Code behaviour does not change: every new path is gated by `agent.StartupProber`, which only codex implements, or by the `StartScreen` quiz flag.
- No real Codex in CI: tests use pane snapshots in `internal/runtime/testdata/`.
- Parent alert text (from the plan, binding): `[rocket] session <id> is stuck on a Codex start screen: «<Title>»…`
- Docs in Russian: `docs/10-agents.md`.

## Review Focus

1. Trust screen: digit `1` only moves the cursor in Codex 0.160 (`trust_directory.rs:222`), so the answer must follow it with Enter. The background server's «Restart with these settings» works the same way. Test: `TestAnswerStartScreenSendsEnterWhenScreenStays` (Task 4).
2. The same screen across many sweeps produces exactly one parent message. Test: `TestStartScreenAlertsParentOnce` (Task 3).
3. A Codex idle screen (`› Ask Codex to do anything`) is not mistaken for a selection screen: its `›` row is not numbered. Test: `TestParseStartScreenNegatives` (Task 1).
4. Once the screen goes away, the session leaves `blocked` and the quiz closes. Test: `TestStartScreenClearsWhenGone` (Task 3).
5. Unmanaged background-server variant: a disabled row without a number sits between choices. Options are only the numbered rows, so digits still match. Test: a case in `TestParseStartScreen` (Task 1).

---

### Task 1: `runtime.ParseStartScreen`

**Files:** Create `internal/runtime/startscreen.go`, `internal/runtime/startscreen_test.go`, fixtures `internal/runtime/testdata/codex-start-{trust,trust-subdir,trust-80col,update,daemon,daemon-unmanaged,neg-idle}.pane`.

**Produces:** `func ParseStartScreen(pane string) (PermissionPrompt, bool)`. `Title` is the screen's question or heading, `Context` is the rest of the header (wrapped rows joined), `Options` are the numbered labels in order, and `Raw` is the bottom 20 rows.

Rule: the last numbered row (`^ *(› +)?N\. label`) sits within 3 non-blank rows of the bottom (a footer hint may follow). Options run 1..n, n ≥ 2, with exactly one `›` cursor. No composer row (`› ` not followed by a number) may come below. The header is the block of rows above the options, up to a blank-row gap of 3 or more rows or 20 rows. Title is the first sentence ending in `?` in the header; otherwise the first header row that is not a heading label like «Folder access», a path or a URL. To keep this simple, use the first header row that ends without `:` and does not start with `/`; a «Folder access» first row is skipped explicitly.

- [ ] Test `TestParseStartScreen` (table): trust → Title `Trust this folder?`, Options `[Trust and continue, Quit]`; trust-subdir → same; trust-80col → same; update → Title `Update available · 0.0.0 → 9.9.9` (fixture text), Options `[Update now (runs `+"`npm install -g @openai/codex`"+`), Skip, Skip until next version]`; daemon → Title `Background server has incompatible feature settings`, Options `[Run without daemon this time, Restart with these settings, Cancel]`; daemon-unmanaged → Options `[Run without daemon this time, Cancel]`.
- [ ] Test `TestParseStartScreenNegatives`: `codex-start-neg-idle.pane`, the Claude Code fixtures `permission-bash-rm.pane`, `permission-neg-idle.pane`, `permission-neg-trust.pane` → false.
- [ ] Implement, run `go test ./internal/runtime/ -run StartScreen`, commit.

### Task 2: `agent.StartupProber` + codex `HasStartedTurn`

**Files:** Modify `internal/agent/agent.go`; create `internal/agent/codex/startup.go`, `startup_test.go`.

**Produces:**
```go
type StartupProber interface {
    // HasStartedTurn reports whether the session's transcript records at least one task_started.
    HasStartedTurn(ctx context.Context, ref ActivityRef) (bool, error)
}
```
Codex: `findMatchingSession`, then stream the file line by line until an `event_msg` record has `payload.type == "task_started"`. No journal → `(false, agent.ErrNoSignal)`.

- [ ] Tests `TestHasStartedTurn{NoJournal,MetaOnly,WithTaskStarted}` with `CODEX_HOME` pointing at a temp dir (same pattern as `activity_test.go`). The cases return ErrNoSignal, false and true.
- [ ] Implement, `go test ./internal/agent/...`, commit.

### Task 3: monitor `checkStartScreen`

**Files:** Create `internal/monitor/startscreen.go`, `startscreen_test.go`; modify `monitor.go` (call from `pollSession`, state maps, `SetWake`, prune); `internal/session/permission.go` (`PermissionRef.StartScreen bool \`json:"start_screen,omitempty"\``, `NewStartScreenQuiz`); `internal/daemon/daemon.go` (`mon.SetWake(q.Wake)`).

**Consumes:** Task 1, Task 2.

Behaviour of `checkStartScreen(ctx, sess, state, ts) (activity.State, time.Time)`:
- Skip when the adapter is not a `StartupProber`, when the session is already known to have started (cached in `m.started`), or when the session holds a non-start-screen quiz.
- `HasStartedTurn` true → cache it, close any pending start-screen quiz, return the state unchanged. Any error other than ErrNoSignal → return unchanged.
- Capture `runtime.PermissionCaptureLines` rows, then `ParseStartScreen`:
  - found → state `Blocked`, ts now. If there is no pending quiz, or the pending one is a different dialog: publish a quiz (`OpenPermissionPrompt` + CAS + `session.quiz_asked`) and alert. Alerting is deduplicated per session by Title+Context.
  - not found while a start-screen quiz is pending → after 2 misses, CAS-clear it, resolve the row, publish `session.quiz_resolved`, and set the state to `Ready`, ts now.
- Alert: the session has `ParentID` → `AddMessage{ToSession: parent, Body}`, `message.queued`, `wake(parent)`. Otherwise `GetTaskBySessionID` → `AddTaskLog{Kind: "problem"}`.

- [ ] `TestStartScreenBlocksAndAsksQuiz`: codex fake (StartupProber false/ErrNoSignal), pane trust → activity `blocked`, quiz IsPermission with `StartScreen`, options `[Trust and continue, Quit]`.
- [ ] `TestStartScreenAlertsParentOnce`: 3 sweeps → exactly 1 message to the parent, body has the prefix `[rocket] session sess1 is stuck on a Codex start screen: «Trust this folder?»`.
- [ ] `TestStartScreenWithoutParentLogsProblem`: task row with session_id → one `problem` log entry after 2 sweeps.
- [ ] `TestStartScreenSkippedAfterTurnStarted`: HasStartedTurn true → no capture at all (`captured(40) == 0`), activity from the adapter.
- [ ] `TestStartScreenClearsWhenGone`: trust, then idle pane for 2 sweeps → quiz cleared, activity `ready`.
- [ ] `TestStartScreenIgnoresClaude`: claude fake (no StartupProber) with a trust-like pane → no quiz from this path.
- [ ] Implement, `go test ./internal/monitor/ ./internal/session/`, commit.

### Task 4: answer path for start screens

**Files:** Modify `internal/session/permission.go` (`answerPermission`), test `internal/session/permission_answer_test.go`.

- [ ] `TestAnswerStartScreenUsesStartParser`: a pending start-screen quiz with a trust pane → answer accepted (no `prompt_changed`), digit `1` sent.
- [ ] `TestAnswerStartScreenSendsEnterWhenScreenStays`: the pane still shows the same screen after the digit with the cursor on the chosen option → Enter sent. The update pane gone after the digit → no Enter.
- [ ] Implement: the re-read uses `ParseStartScreen` when `quiz.Permission.StartScreen`. After a successful digit, a follow-up step re-captures and sends Enter only if the same dialog is still there and the `›` row is the chosen number. Commit.

### Task 5: docs + verification

- [ ] `docs/10-agents.md`, codex section: «Экраны старта» (detector, blocked + quiz, alert, digit+Enter, limitation: a non-list full-screen text is not detected; restore in the same worktree with an old journal counts as started).
- [ ] `go vet ./... && go test ./...` green; `make` if present; PR.
