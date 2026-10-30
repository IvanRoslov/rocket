# Pi activity, delivery confirmation, and safe pane delivery

## Intent

Complete Task 3 of feature `pi-and-deepseek2` (#5867): make Rocket understand Pi turns from its own journal, confirm steering only when Pi records it, and inject text through a Pi-specific pane dialect without risking human drafts or unknown screens.

## Goals

- Implement Pi `Activity`, `LastTurnFailure`, `AcceptsInputWhileBusy`, `ConfirmDelivered`, `PaneDialect`, and `BracketedPaste` using the Task 2 journal interfaces and Task 1 recon fixtures.
- Add `PaneDialectPi`, `ParsePiComposer`, and `LooksLikePiQueued` and wire them into tmux injection and the delivery queue.
- Prove behavior with red/green tests for open turns, long tools, final answers, provider failures, active branches, exact-session/after-`since` delivery, composer states, steering, draft/unknown-pane refusal, and queue ownership.
- Keep default, Codex, and Kimi behavior unchanged and finish with focused tests, full `make test`, `git diff --check`, and one unmerged PR.

## Non-goals and anti-goals

- No Task 2 locator/reader rewrites, Task 4 chat/usage work, startup probes, new dependencies, subagents, global Pi configuration, or unrelated refactors.
- No activity detection from terminal text. The pane parser is only for input safety and recognizing Pi's steering queue.
- Never paste, clear, or press Enter on an unrecognized Pi pane; never overwrite a human draft except through the existing explicit force deadline.
- Do not treat steering visible in Pi's queue as delivered. Delivery requires the user record in the same session's active branch after the injection timestamp.

## Constraints

- Consume `findSession`, `readTail`, `readActiveBranch`, and `sessionRecord` as merged in Task 2.
- Pi accepts steering while busy, requires bracketed paste, and clears a draft with `C-c`, as established by live recon.
- Provider failures use the assistant record's `stopReason=error` and `errorMessage`; queued steering persists later as a normal `role=user` message with no queued flag.
- Work only in the listed Task 3 files plus this design, implementation plan, and required report.

## Approaches

1. **Active-branch journal semantics plus an additive pane dialect (chosen).** Reuse Task 2's exact-session locator and active-branch reconstruction. Implement the Pi state machine and delivery checks in `internal/agent/pi`, and add a separate `internal/runtime/pipane.go` with narrow wiring branches for Pi. This follows the binding spec and prevents abandoned branches or metadata from affecting activity or confirmation.
2. **Global journal scanning.** Rejected: standalone usage/custom records and abandoned branches could change activity or falsely confirm delivery.
3. **Terminal-derived activity.** Rejected: terminal heuristics are explicitly forbidden for activity and cannot safely replace journal state.

## Design

### Journal activity and failures

Read the exact session selected by `ActivityRef.SessionID` and work only on its active branch. Only conversation messages determine the current turn; session/model/usage/custom/compaction metadata does not reset it.

- A latest user message, a tool result awaiting its assistant, or an assistant tool call without its result is `active` regardless of mtime.
- A latest assistant `stopReason=error` is `blocked`. `LastTurnFailure` returns the same error text and a stable ID derived from the session and journal record ID.
- A completed final assistant response is `ready`, subject to the shared 30-second fresh-journal rule.
- A missing journal or genuinely absent signal returns `agent.ErrNoSignal`. Real session-store open/stat/read failures propagate unchanged; only malformed or partially written JSONL records are skipped.

### Delivery confirmation

Search only user messages on the active branch, newest to oldest, stopping at records at or before `since`. Match normalized text, including contiguous whole-line runs used by existing Rocket messages and inbox-file pointers. Ignore system, skill, custom, metadata, and abandoned-branch records. Pi returns `AcceptsInputWhileBusy() true`, `PaneDialect() == "pi"`, and `BracketedPaste() true`.

### Pi composer and steering parser

Recognize the bottom-of-pane Pi structure from recon: model/context footer, cwd/branch row, horizontal rules, draft contents or the `Working` row, and optional `Steering:` rows.

- Empty composer: `PiEmpty`; non-empty draft between the rules: `PiDraft`; active `Working` composer: `PiBusy` (with or without steering).
- `LooksLikePiQueued` matches text only in the current `Steering:` block, not finished scrollback.
- Other agents, start/question screens, and unknown panes are not recognized.

### Injection and queue

Add fail-closed Pi branches alongside the existing Kimi/default paths. Draft or unknown panes return `ErrComposerBusy` before clearing or pasting, including under `Force`. Recognized steering returns `ErrQueuedByAgent`, so the queue marks the paste accepted and waits for journal proof. Pi uses `C-c` to clear a recognized draft and bracketed paste for injection. The queue uses the Pi dialect/window for draft probes and keeps accepted messages while their text remains visibly queued. All existing non-Pi branches remain as they are.

## Builder calls

- Reuse small package-local whitespace/line matching helpers rather than exporting a new shared API.
- Use the Pi journal record ID in the stable turn-failure ID; no synthetic turn counter is needed.
- Use a 20-line Pi composer probe, matching the existing Kimi probe depth; the queue confirmer retains its wider 60-line capture.

## Verification

- Red/green focused tests for `internal/agent/pi`, then `internal/runtime` and `internal/queue`.
- Package regressions for all three packages, `make test`, `gofmt`, and `git diff --check`.
- Inspect the final diff against the brief, ensure no generated artifacts or out-of-scope changes remain, open one PR, and follow configured CI to green without merging.
