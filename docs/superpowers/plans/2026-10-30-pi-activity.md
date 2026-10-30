# Pi Activity and Safe Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement journal-driven Pi activity/failure reporting and delivery confirmation, plus a fail-closed Pi composer/steering dialect for tmux and queue delivery.

**Architecture:** Consume Task 2's exact-session locator and active-branch reader unchanged. Add small Pi adapter methods and an additive `PaneDialectPi` path in runtime/queue, leaving all default, Codex, and Kimi branches intact.

**Tech Stack:** Go standard library, existing Rocket agent/runtime/queue interfaces, Go `testing`, Task 1 JSONL/pane fixtures.

**Spec:** `docs/superpowers/specs/2026-10-30-pi-activity-design.md`; binding extracted brief at `.superpowers/sdd/2026-10-29-pi-deepseek-agents/task-3-brief.md` in the orchestrator worktree.

## Global Constraints

- No Task 2 reader changes, Task 4 chat/usage work, new dependencies, subagents, global Pi configuration, or unrelated refactors.
- Terminal text is for input safety and steering recognition only, never activity detection.
- Only the exact session's active branch and records strictly after `since` may confirm delivery; queued pane text alone is not delivered.
- Unknown panes and human drafts must not receive clear/paste/Enter. Pi uses bracketed paste and `C-c` clear, per live recon.
- Follow red/green TDD, keep the two implementation units in separate commits, and deliver one unmerged PR.

## Review Focus

- Trailing usage/custom/compaction records must not change the current conversation state: `TestActivityMetadataDoesNotChangeState`.
- A queued steering row without its later user record must not confirm: `TestConfirmQueuedPaneIsNotDelivery`.
- An abandoned branch containing the same text must not confirm or alter activity: `TestConfirmAbandonedBranchDoesNotCount` and `TestActivityUsesActiveBranch`.
- A capture failure or unknown/other-agent pane must not receive keys: `TestTmux_Inject_PiCaptureErrorDefers` and `TestTmux_Inject_PiUnrecognizedPaneDefersEvenForced`.
- Repeated identical text needs distinct post-`since` records: `TestConfirmEarliestMatch` and strict boundary coverage in `TestConfirmIgnoresRecordAtOrBeforeSince`.

---

### Task 1: Journal Activity, Turn Failure, and Delivery Confirmation

**Files:**
- Modify: `internal/agent/pi/activity.go`
- Create: `internal/agent/pi/activity_test.go`
- Create: `internal/agent/pi/confirm.go`
- Create: `internal/agent/pi/confirm_test.go`

**Interfaces:**
- Consumes: `findSession(home, worktree, sessionID string)`, `readActiveBranch(path string)`, `sessionRecord`, and `piHome()` from Task 2.
- Produces: real `Pi.Activity`, `Pi.LastTurnFailure`, `Pi.AcceptsInputWhileBusy() bool`, `Pi.ConfirmDelivered`, `Pi.PaneDialect() string`, and `Pi.BracketedPaste() bool`.

- [ ] **Step 1: Write failing activity/failure tests**

Add an isolated `PI_CODING_AGENT_DIR` helper that writes a named session into the encoded physical-worktree directory and controls its mtime. Use `pi-session.jsonl` and `pi-provider-error.jsonl` plus minimal synthetic records for tool completion and branch cases.

Tests and assertions:
- `TestActivity`: open user turn and unfinished tool with ten-minute-old mtime are `active`; final assistant with old mtime is `ready`; provider error is `blocked`; fresh final assistant is `active`; no exact journal is `agent.ErrNoSignal`.
- `TestActivityMetadataDoesNotChangeState`: trailing usage/custom/model records do not replace the latest conversation state.
- `TestActivityUsesActiveBranch`: an abandoned error or open turn cannot override the current final response.
- `TestLastTurnFailure`: provider error returns `errorMessage` and a stable ID containing the exact session and record IDs; final response returns `ok=false`; no journal returns `ErrNoSignal`.
- `TestActivityReadErrorPropagates`: a real session-store read failure is not translated into `ErrNoSignal`.

- [ ] **Step 2: Write failing delivery/capability tests**

Tests and assertions:
- `TestAcceptsInputWhileBusy`, `TestPaneDialectAndBracketedPaste`: true, `runtime.PaneDialectPi`, true.
- `TestConfirmExactAfterSince`, `TestConfirmWhitespaceNormalized`, `TestConfirmRunOfWholeLines`, `TestConfirmInboxReference`, `TestConfirmEarliestMatch`: expected match and earliest timestamp.
- `TestConfirmIgnoresRecordAtOrBeforeSince`: the boundary record and older identical text do not match.
- `TestConfirmOnlyExactSession`: same worktree/text under another session ID does not match.
- `TestConfirmQueuedPaneIsNotDelivery`: no later user record means `found=false` even with steering-shaped input elsewhere.
- `TestConfirmSteerAndNonUserRecordsDoNotCount`: system/custom/skill content cannot confirm.
- `TestConfirmAbandonedBranchDoesNotCount`: matching text only on the abandoned branch is ignored.
- `TestConfirmNoJournal`: `ErrNoSignal`.
- `TestConfirmReadErrorPropagates`: a real session-store read failure is not translated into `ErrNoSignal`.

- [ ] **Step 3: Run the focused tests and verify RED**

Run: `go test ./internal/agent/pi -run 'Test(Activity|LastTurnFailure|AcceptsInputWhileBusy|Confirm|PaneDialectAndBracketedPaste)' -count=1`
Expected: FAIL because activity/delivery methods are stubs or missing.

- [ ] **Step 4: Implement activity and turn failure**

In `activity.go`, locate the exact session, stat its mtime, and inspect only the active branch's latest conversation message. Open user/tool work is `active`; assistant `stopReason=error` is `blocked`; completed work is `ready`, with the existing 30-second fresh-mtime rule. `LastTurnFailure` reports only a current assistant error, using exact session + journal record ID for stability. Return `ErrNoSignal` only for a missing journal/signal; propagate real store open/stat/read failures.

- [ ] **Step 5: Implement delivery and capabilities**

In `confirm.go`, add compile-time interface assertions and the three constant capabilities. Scan active-branch user messages backward, stopping at `since`, and match normalized text or contiguous whole-line runs. Ignore non-user records, return `ErrNoSignal` only for a missing journal/signal, and propagate real store open/stat/read failures.

- [ ] **Step 6: Run focused/package tests, then commit**

Run: `go test ./internal/agent/pi -run 'Test(Activity|LastTurnFailure|AcceptsInputWhileBusy|Confirm|PaneDialectAndBracketedPaste)' -count=1`
Run: `go test ./internal/agent/pi -count=1`
Expected: PASS.
Commit: `feat(pi): track turns and confirm delivery`.

### Task 2: Pi Composer, Injection, and Queue Dialect

**Files:**
- Create: `internal/runtime/pipane.go`
- Create: `internal/runtime/pipane_test.go`
- Create: `internal/runtime/tmux_inject_pi_test.go`
- Modify: `internal/runtime/tmux.go`
- Modify: `internal/queue/queue.go`
- Modify: `internal/queue/confirm.go`
- Create: `internal/queue/queue_pi_test.go`
- Use existing: `internal/runtime/testdata/pi-*.pane`

**Interfaces:**
- Consumes: Task 1 capabilities and existing `InjectOpts.Dialect`, queue draft/confirmation paths, and recon pane fixtures.
- Produces: `const PaneDialectPi = "pi"`; `type PiComposer int` with `PiEmpty | PiDraft | PiBusy`; `ParsePiComposer(pane string) (PiComposer, bool)`; `LooksLikePiQueued(pane, text string) bool`.

- [ ] **Step 1: Write failing parser tests**

- `TestParsePiComposerSnapshots`: idle/cleared/finished are `PiEmpty`, plain/multiline drafts are `PiDraft`, busy and busy-queued are `PiBusy`.
- `TestParsePiComposerNegatives`: Claude/Codex/Kimi/unknown panes and empty input are not recognized.
- `TestLooksLikePiQueued`: exact steering matches, different text does not, finished scrollback does not count.

- [ ] **Step 2: Write failing injection tests**

- `TestTmux_Inject_PiDraftGuard` and `TestTmux_Inject_PiUnrecognizedPaneDefersEvenForced`: `ErrComposerBusy`, no send-keys or paste.
- `TestTmux_Inject_PiCaptureErrorDefers`: same fail-closed result.
- `TestTmux_Inject_PiForcedClearUsesCtrlC`: clear argv is exactly `send-keys -t <pane> C-c`.
- `TestTmux_Inject_PiBracketedPaste`: `paste-buffer -p -d ...` on the recognized empty composer.
- `TestTmux_Inject_PiSteeringReturnsQueued`: steering visible after submit returns `ErrQueuedByAgent`, with no duplicate Enter.

- [ ] **Step 3: Write failing queue tests**

Use a fake Pi adapter with bracketed paste/dialect and real fixtures.
- `TestQueue_PiFirstMessageWaitsForEmptyComposer`: unknown and draft panes hold; idle injects once with `Bracketed=true`, `Dialect=PaneDialectPi`, `Force=false`, status `accepted`.
- `TestQueue_PiDraftHoldsDelivery`: no injection over a human draft; busy composer allows steering while activity is active.
- `TestQueue_PiAcceptedKeptWhileInPisQueue`: after grace and blocked activity, visible steering keeps status `accepted` and causes no second injection; later journal proof delivers without repaste.
- `TestQueue_DefaultInjectOptsUnchanged` already exists and must remain green.

- [ ] **Step 4: Run the focused tests and verify RED**

Run: `go test ./internal/runtime ./internal/queue -run 'Test.*Pi' -count=1`
Expected: FAIL because `PaneDialectPi`, parser, and wiring do not exist.

- [ ] **Step 5: Implement `pipane.go`**

Recognize the bottom model/context and cwd/branch footer, horizontal composer rules, draft content, `Working` row, and current `Steering:` block. Return `PiDraft` only for non-empty draft content, `PiBusy` for the working state, and never infer queue state from finished scrollback.

- [ ] **Step 6: Wire Pi into injection and queue**

In `tmux.go`, fail closed on capture errors/drafts/unknown panes; use `C-c` clear and existing bracketed-paste option; treat visible steering as `ErrQueuedByAgent`; recognize empty/busy composer after Enter; use `C-c` for final cleanup. In `queue.go`, add the Pi 20-line draft probe and dialect parsing. In `confirm.go`, keep accepted messages while `LooksLikePiQueued` finds their text. Do not alter other dialect branches.

- [ ] **Step 7: Run focused/package regressions, then commit**

Run: `go test ./internal/runtime ./internal/queue -run 'Test.*Pi' -count=1`
Run: `go test ./internal/agent/pi ./internal/runtime ./internal/queue -count=1`
Expected: PASS.
Commit: `feat(pi): add safe pane delivery`.

### Task 3: Full Verification, Review, and Delivery

**Files:**
- Create: `.superpowers/sdd/2026-10-29-pi-deepseek-agents/task-3-report.md`
- Modify only if verification exposes a Task 3 defect: files above.

**Interfaces:**
- Consumes: all Task 1–2 interfaces.
- Produces: one reviewed PR, full verification evidence, and the required report; no merge.

- [ ] **Step 1: Run formatting and all required checks**

Run: `gofmt -w internal/agent/pi/*.go internal/runtime/pipane.go internal/runtime/pipane_test.go internal/runtime/tmux_inject_pi_test.go internal/queue/queue_pi_test.go`
Run: `go test ./internal/agent/pi ./internal/runtime ./internal/queue -count=1`
Run: `make test`
Run: `git diff --check`
Expected: all PASS. Inspect `git status` and remove only artifacts created by these checks.

- [ ] **Step 2: Self-review and end-to-end scenario review**

Inspect the complete diff against every brief checkbox. Confirm exact-session/after-`since` active-branch confirmation, no Task 2/4 changes, no secrets/dependencies/subagents, no terminal activity inference, fail-closed unknown panes, `C-c` + bracketed paste, and unchanged default/Codex/Kimi behavior. Use systematic debugging for any failure before changing code.

- [ ] **Step 3: Open one PR and write the report**

Push `feature/pi-and-deepseek2/pi-activity` and open one PR referencing feature `pi-and-deepseek2`; do not merge. Write status, commits, PR URL, every verification command/result, produced interfaces, and concerns in the report. Send the orchestrator only status, commits, PR URL, one-line test summary, and concerns. If CI exists, follow it to green; otherwise apply the Q11 local-test ruling with independent review.
