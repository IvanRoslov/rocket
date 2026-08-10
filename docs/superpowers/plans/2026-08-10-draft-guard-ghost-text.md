# Draft guard: ghost-text false positive — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans / test-driven-development.

**Goal:** `LooksLikeUserDraft` must not treat Claude Code's dim ghost-text autosuggestion as a human draft.

**Architecture:** Both guard call sites capture the pane with `tmux capture-pane -p -e` (escape sequences kept). `draft.go` gains a tiny SGR parser that turns a captured pane into lines of visible text plus a per-rune "dim" mask (SGR 2 on, SGR 22/0 off). The existing shape heuristics run on the visible text; composer *content* drops dim runes. Fully-dim content ⇒ empty ⇒ not busy. Plain (escape-free) captures parse to an all-false mask ⇒ today's behavior unchanged.

**Tech Stack:** Go, stdlib only.

## Global Constraints

- Conservative bias unchanged: anything not positively recognized ⇒ `false` (not busy).
- Chrome detection (prompt sigil, rules, box borders) uses visible text regardless of dim — Claude Code paints its rules dim-ish colored, and dropping them would disable the guard.
- Existing tests extended, not rewritten.
- `go build ./... && go vet ./... && go test ./...` green.

---

### Task 1: SGR-aware pane parsing in `draft.go`

**Files:**
- Modify: `internal/runtime/draft.go`
- Test: `internal/runtime/draft_test.go` (new escape-sequence table cases)

**Interfaces:**
- Produces: `parsePaneLines(pane string) []paneLine`, `type paneLine struct { text string; dim []bool }`, `func (l paneLine) plain() string`.

- [ ] Step 1: failing tests — ghost suggestion (`ESC[39m❯ ESC[2m…ESC[0m`) ⇒ false; real draft (`ESC[38;5;231m…`) ⇒ true; mixed typed+dim ⇒ true; empty ⇒ false; plain fixtures keep passing.
- [ ] Step 2: run `go test ./internal/runtime/ -run Draft` — expect failures.
- [ ] Step 3: implement parser + dim-aware content extraction.
- [ ] Step 4: run tests — pass.
- [ ] Step 5: commit.

### Task 2: escape-aware capture at both guard call sites

**Files:**
- Modify: `internal/runtime/tmux.go` (in-`Inject` guard capture adds `-e`; new `CaptureEscaped`)
- Modify: `internal/runtime/runtime.go` (interface)
- Modify: `internal/queue/queue.go` (pre-claim probe uses `CaptureEscaped`)
- Test: `internal/queue/queue_draft_test.go`, runtime fakes

- [ ] Step 1: failing test — queue fake serves escaped ghost pane, delivery must NOT defer.
- [ ] Step 2: run, expect failure.
- [ ] Step 3: add `CaptureEscaped` to the `Runtime` interface + tmux impl + all fakes; switch call sites.
- [ ] Step 4: run tests — pass.
- [ ] Step 5: commit.

### Task 3: docs + full verification

**Files:**
- Modify: `docs/06-messaging.md`

- [ ] Step 1: document the dim/ghost-text exclusion in the heuristic notes.
- [ ] Step 2: `go build ./... && go vet ./... && go test ./...`.
- [ ] Step 3: manual check against a live pane with a visible suggestion.
- [ ] Step 4: commit + PR.
