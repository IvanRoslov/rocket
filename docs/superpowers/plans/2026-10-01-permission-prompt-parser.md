# Permission prompt parser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** a pure function `runtime.ParsePermissionPrompt(pane string) (PermissionPrompt, bool)` that recognises a Claude Code permission dialog at the bottom of a tmux pane and reads its title, context and options.

**Architecture:** structural parse over ANSI-stripped pane rows (reusing `parsePane` from `draft.go`): find the bottom numbered selector, require a «?» question right above it and no composer rule below it, rejoin wrapped labels by geometry, collect the dialog body up to its top rule as context. A title-only fallback covers recognised questions with unparsable choices. Quiz and held-peer dialogs are excluded explicitly.

**Tech Stack:** Go 1.25, `internal/runtime`, table tests over live tmux captures.

**Spec:** `docs/superpowers/specs/2026-10-01-chat-permission-prompts-design.md` (feature #4881, §1), recon: `docs/superpowers/recon/2026-10-01-permission-prompt-recon.md`.

## Global Constraints

- Signature is fixed (consumed by permission-daemon): `type PermissionPrompt struct { Title, Context string; Options []string; Raw string }`, `func ParsePermissionPrompt(pane string) (PermissionPrompt, bool)`.
- `Options[i]` must be answerable by the single digit `i+1`, no Enter.
- Context ≤ ~10 lines; Raw = bottom ~20 pane rows.
- Must not fire on `LooksLikeQuizWidget` or `LooksLikeHeldPeerDialog` panes.
- Recognised title + unparsable options → `ok=true`, empty `Options`, `Raw` set.
- Agreed with orchestrator: recognise the ExitPlanMode dialog; drop a trailing free-text option (hint «shift+tab to approve with this feedback»); free-text option not last → empty `Options`.

## Review Focus

1. A numbered list in the agent's own reply right above the composer must not parse → `permission-neg-numbered-output.pane` (Task 2).
2. Dialog wording echoed in output above the composer → synthetic «prompt text echoed above the composer» case (Task 2).
3. Long paths hard-broken mid-word in option labels must rejoin without a space; word wraps with one → `permission-bash-rm.pane`, `permission-narrow-60col.pane` (Task 2).
4. A free-text choice must never be offered as a digit, even when its hint wraps in a narrow pane → `TestParsePermissionPromptFreeTextHintInNarrowPane` (Task 2).
5. A quiz widget that happens to have no internal rule is still rejected → synthetic «AskUserQuestion widget without the chat rule» (Task 2).

---

### Task 1: Recon and fixtures

**Files:**
- Create: `internal/runtime/testdata/permission-*.pane`
- Create: `docs/superpowers/recon/2026-10-01-permission-prompt-recon.md`

- [x] **Step 1:** scratch git repo in the scratchpad; `tmux new-session -d -s pprecon -x 120 -y 40 "claude …"` in the three launch variants (user config + bypass; `--setting-sources project,local` + bypass; same + `--permission-mode default`).
- [x] **Step 2:** trigger Edit of `.claude/settings.json`, Write of a new file, Edit of a normal file, Bash outside cwd, `rm -rf ./build`, a 14-line Bash script, WebFetch, EnterPlanMode+ExitPlanMode; a 60-column pane; negatives (numbered reply, AskUserQuestion, idle, folder trust). Capture each with `tmux capture-pane -p -S -40` (one also with `-e`).
- [x] **Step 3:** per dialog, try digit, out-of-range digit, Enter, Esc, «No»; record results in the recon doc.
- [x] **Step 4:** report deviations from the design to the orchestrator (bypass suppresses ordinary dialogs; plan dialog option 3 is a text field) and get agreement.

### Task 2: `ParsePermissionPrompt`

**Files:**
- Create: `internal/runtime/permission.go`
- Test: `internal/runtime/permission_test.go`

**Interfaces:**
- Consumes: `parsePane`, `trimTrailingBlankLines` (`draft.go`), `LooksLikeQuizWidget` (`tmux.go`), `LooksLikeHeldPeerDialog` (`heldpeer.go`), test helper `fixture` (`heldpeer_test.go`).
- Produces: `PermissionPrompt`, `ParsePermissionPrompt`, const `permissionRawRows = 20`.

- [x] **Step 1: failing tests** — `TestParsePermissionPromptOnLiveCaptures` (table: fixture → Title/Context/Options, Raw non-empty without escapes), `TestParsePermissionPromptRejects` (negatives incl. Review Focus 1, 2, 5, selector without «?», single option, numbering not from 1, no cursor, empty), `TestParsePermissionPromptBoxedLayout` (`permissionPromptPane`), `TestParsePermissionPromptFallback` (unnumbered choices; free-text option not last), `TestParsePermissionPromptFreeTextHintInNarrowPane`, `TestParsePermissionPromptRawIsPaneTail`.
- [x] **Step 2:** `go test ./internal/runtime/ -run ParsePermission` → FAIL `undefined: ParsePermissionPrompt`.
- [x] **Step 3: implement** —
  1. rows = `parsePane` text, right-trimmed, trailing blanks dropped, `│…│` borders stripped; Raw = last 20 rows.
  2. reject if `LooksLikeQuizWidget(raw) || LooksLikeHeldPeerDialog(raw)`.
  3. scan up from the bottom for the last `^( *)(❯ *)?(\d+)\. +(\S.*)$` row; any `────`/`╭` rule on the way → reject; > 8 non-blank rows → reject.
  4. option block = option rows plus continuation rows indented past the digit column; ≤ 3 non-blank rows after it.
  5. options numbered 1..n, n ≥ 2, exactly one `❯`. Continuation row: hint «shift+tab to approve» or any non-wrap sub-row → free-text; otherwise join via `wrapSeparator` (`""` for a mid-word hard break: prev row full width and last+first word longer than the row; `" "` for a word wrap: next first word would not fit on prev). Drop trailing free-text options; free-text elsewhere → nil Options.
  6. title = row above the block (one blank row allowed), must end in «?», wrapped rows above rejoined.
  7. context = rows above the title up to the dialog's top `────` rule (a rule with no content collected yet is stepped over — plan dialog), wraps rejoined, `╌` rules and «Tip:» lines dropped, dedented, first 10 lines + «…» (last 10 if no frame found).
  8. fallback: no numbered parse → a row in the bottom 12 non-blank rows ending in «?» and containing «Do you want to » / «Would you like to proceed?», with no rule below → Title only.
- [x] **Step 4:** `go test ./internal/runtime/` → PASS; temporarily disable the quiz/held exclusion and confirm the synthetic quiz case fails.
- [x] **Step 5:** commit.

### Task 3: `LooksLikeInputWait` reuses the parser

**Files:**
- Modify: `internal/runtime/inputwait.go`
- Test: `internal/runtime/inputwait_test.go`

- [x] **Step 1: failing test** `TestLooksLikeInputWaitRecognisesParsedPermissionPrompts` — exit-plan, bash-rm, webfetch fixtures → true; numbered output, idle → false. Fails on exit-plan (wording not in `inputWaitMarkers`).
- [x] **Step 2:** add `if _, ok := ParsePermissionPrompt(tail); ok { return true }` after the quiz check; keep the literal markers. Safe: only positives are added, and only the negative answer is acted on.
- [x] **Step 3:** `go test ./internal/runtime/` → PASS; commit.

### Task 4: Verify and ship

- [x] `gofmt -l internal/`, `go vet ./...`, `go test ./...` green.
- [x] PR into main referencing feature task-4881; do not merge until the orchestrator clears spec v2.
