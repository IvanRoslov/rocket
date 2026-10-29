# Kimi Cache-Expiry Auto-Compact Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically select Kimi's default `Compact and continue` action when its cache-expiry dialog intercepts a Rocket message.

**Architecture:** Add one narrow predicate beside the existing Kimi pane parsers and use it only inside `tmuxRuntime.Inject`. A pre-insert dialog starts compaction and defers delivery; a post-submit dialog confirms compaction without re-inserting Kimi's stashed message.

**Tech Stack:** Go, existing tmux runtime and fake-runtime tests; no new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-29-kimi-cache-expiry-autocompact-design.md`

## Global Constraints

- Only `Compact and continue` may be selected automatically, and only when the cursor is already on that option.
- Never modify global Kimi configuration or `tui.toml`.
- Never auto-answer other dialogs or select new session, continue as-is, or never ask again.
- Never paste a stashed message a second time; existing Kimi journal confirmation proves final delivery.
- Do not run git commit unless the user explicitly requests it.

## Review Focus

- A human moved the cursor to another option: Rocket must not move it back or press Enter.
- Trust and AskUserQuestion screens resemble the cache dialog structurally: neither may trigger compaction.
- Resume and idle-submit dialogs have different message ownership: the former defers Rocket's message, the latter leaves it stashed in Kimi.
- A failed Enter after idle submit must produce `ErrSubmitUnconfirmed`, not a retry that could duplicate the message.
- Compaction is asynchronous: successful auto-selection must not be treated as proof that the message reached the model.

---

### Task 1: Recognize and answer the cache-expiry dialog during injection

**Files:**
- Create: `internal/runtime/testdata/kimi-cache-hint.pane`
- Modify: `internal/runtime/kimistartscreen.go`
- Modify: `internal/runtime/kimipane_test.go`
- Modify: `internal/runtime/tmux.go:271-299,433-515`
- Modify: `internal/runtime/tmux_inject_kimi_test.go`

**Interfaces:**
- Consumes: existing `parseKimiStartScreen(pane string) (PermissionPrompt, int, bool)`, `tmuxRuntime.run`, `ErrComposerBusy`, and `ErrSubmitUnconfirmed`.
- Produces: `KimiCacheHintCompactSelected(pane string) bool` — true only for the cache-expiry dialog with the cursor on `Compact and continue`.

- [ ] **Step 1: Add a source-shaped fixture and failing parser tests**

Create `kimi-cache-hint.pane` with the Kimi 2.4.0 layout: top/bottom rules, title `This session has been idle for 2h and is ~150k tokens.`, the `Enter select` hint, `Cache expired`, and options `Compact and continue`, `Start a new session`, `Continue as-is`, `Don't ask me again`; cursor starts on compact.

Add `TestKimiCacheHintCompactSelected` asserting true for that fixture and false when the cursor is moved to `Continue as-is`. Add negative assertions for `kimi-trust.pane`, `kimi-question.pane`, and `kimi-idle.pane`. Update the generic start-screen negative test to exclude this new valid dialog fixture.

- [ ] **Step 2: Run the parser tests and verify RED**

Run: `go test ./internal/runtime -run 'TestKimiCacheHintCompactSelected' -count=1`
Expected: FAIL because `KimiCacheHintCompactSelected` is undefined.

- [ ] **Step 3: Implement the narrow predicate**

In `kimistartscreen.go`, implement `KimiCacheHintCompactSelected(pane string) bool` using `parseKimiStartScreen`. Require `Cache expired` in the parsed context, all four expected option prefixes in order, a valid cursor, and `Compact and continue` as the selected option's prefix. Do not match on the title alone.

- [ ] **Step 4: Run parser and existing Kimi pane tests**

Run: `go test ./internal/runtime -run 'Test(KimiCacheHintCompactSelected|ParseKimi|CodexStartScreenIgnoresKimiTrust)' -count=1`
Expected: PASS.

- [ ] **Step 5: Add failing injection tests**

In `tmux_inject_kimi_test.go`, add:

- `TestTmux_Inject_KimiCacheHintBeforePasteStartsCompact`: initial cache fixture; expect `ErrComposerBusy`, exactly one Enter, and no paste.
- `TestTmux_Inject_KimiCacheHintAfterSubmitContinuesStashedMessage`: script empty composer → draft after paste → cache dialog after the first Enter; expect success, one paste, and exactly two Enters total (submit + compact), with no second paste.
- `TestTmux_Inject_KimiCacheHintOtherSelectionIsUntouched`: cursor on continue; expect `ErrComposerBusy`, no keys, and no paste.
- `TestTmux_Inject_KimiCacheHintCompactKeyFailureIsUnconfirmed`: fail the second Enter after idle submit; expect `ErrSubmitUnconfirmed` and one paste.

- [ ] **Step 6: Run the injection tests and verify RED**

Run: `go test ./internal/runtime -run 'TestTmux_Inject_KimiCacheHint' -count=1`
Expected: the new tests FAIL: no automatic Enter is currently sent, and key failure is not classified as unconfirmed.

- [ ] **Step 7: Handle both dialog positions in `Inject`**

At the initial Kimi capture in `tmux.go`, when `KimiCacheHintCompactSelected(out)` is true, send Enter and return `ErrComposerBusy`; do not clear or paste the draft. Return the same deferral if that Enter fails.

Before every `kimiSubmitted` acceptance in the post-submit/retry paths, check the same predicate. On true, send Enter once and return nil on success; on key failure return an error wrapping `ErrSubmitUnconfirmed`. Do not paste again and do not wait for compaction or journal delivery inside `Inject`.

- [ ] **Step 8: Run focused tests and verify GREEN**

Run: `go test ./internal/runtime -run 'Test(KimiCacheHintCompactSelected|Tmux_Inject_Kimi)' -count=1`
Expected: PASS, including all pre-existing Kimi injection tests.

- [ ] **Step 9: Run the full project verification**

Run: `make test`
Expected: PASS for Go and web. Then run `git status --short` and ensure only the intended design, plan, runtime source, tests, and fixture changed; do not alter pre-existing unrelated changes.

- [ ] **Step 10: Review the delivered behavior against the spec**

Confirm from the final diff that no global configuration changed, no monitor behavior changed, no other dialog is auto-answered, and the post-submit path contains no second paste. Do not commit unless explicitly requested.
