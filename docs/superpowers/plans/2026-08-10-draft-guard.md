# Draft Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop tmux message delivery from erasing a human's in-progress composer draft: detect the draft before `C-u`, defer the delivery, and force it through after a configurable deadline.

**Architecture:** A pure, testable heuristic (`LooksLikeUserDraft`) in `internal/runtime` reads a captured pane tail and reports whether a human draft sits in the composer. `Inject` runs it immediately before the `C-u` that clears the composer and returns the new sentinel `ErrComposerBusy` without touching anything; a new `InjectOpts{Force bool}` parameter bypasses the guard. The queue holds a busy recipient the same way it already holds one with a pending quiz — message stays `queued`, no attempt consumed, global `queue_timeout` semantics untouched — and switches to `Force: true` once the draft has been continuously busy for `composer_busy_deadline` (default 10m).

**Tech Stack:** Go, tmux, `go test ./...`.

## Global Constraints

- Language of code comments and docs in this repo: comments in English, `docs/*.md` in Russian. Match the surrounding file.
- Heuristic must be conservative: unknown composer rendering ⇒ **not** busy (today's behavior preserved).
- Interface change to `runtime.Runtime` requires updating all 9 test fakes: `internal/monitor/monitor_test.go`, `internal/ghpoller/reactions_test.go`, `internal/queue/queue_test.go`, `internal/api/messages_test.go`, `internal/api/sessions_test.go` (×2), `internal/api/quiz_test.go`, `internal/api/system_test.go`, `internal/session/manager_test.go`.
- Recognized composer shapes (verified against live panes 2026-08-10):
  - Claude Code, rule style: a `❯ ` line at column 0 between two horizontal-rule lines (`────…`), footer below.
  - Claude Code, boxed style: `│ > … │` inside a `╭─…╯` box.
  - Placeholders that are NOT drafts: `Press up to edit queued messages`, `Try "…`, `? for shortcuts`.
  - Codex: not recognized (⇒ never busy) until a verified sample exists.

## File Structure

- Create `internal/runtime/draft.go` — the heuristic and its documentation. One responsibility: "does this pane tail show a human draft?". Kept out of `tmux.go` (already ~780 lines) and modelled on the existing sibling `inputwait.go`.
- Create `internal/runtime/draft_test.go` — table-driven tests over real captured pane shapes.
- Modify `internal/runtime/runtime.go` — `Inject` signature + `InjectOpts` + error contract docs.
- Modify `internal/runtime/tmux.go` — `ErrComposerBusy`, the guard before `C-u`.
- Modify `internal/queue/queue.go` — busy hold in `deliver`, `force` plumb-through in `attemptDelivery`, `ErrComposerBusy` race handling.
- Modify `internal/config/config.go` — `ComposerBusyDeadline` knob, default 10m.
- Modify `docs/06-messaging.md` — «Правила» entry.

---

### Task 1: Draft-detection heuristic

**Files:**
- Create: `internal/runtime/draft.go`
- Create: `internal/runtime/draft_test.go`

**Interfaces:**
- Consumes: `tailLines`, `trimTrailingBlank` from `internal/runtime/tmux.go`.
- Produces: `func LooksLikeUserDraft(pane string) bool` — true only when a recognized composer shape holds non-placeholder text.

- [ ] **Step 1: Write the failing test**

`internal/runtime/draft_test.go`, table-driven, with these cases (real captures):

```go
func TestLooksLikeUserDraft(t *testing.T) {
	tests := []struct {
		name string
		pane string
		want bool
	}{
		{"empty composer", "" +
			"  ⎿  Tip: use /btw\n" +
			"────────────────────────────────\n" +
			"❯ \n" +
			"────────────────────────────────\n" +
			"  ⏵⏵ bypass permissions on\n", false},
		{"human draft", "" +
			"✻ Brewed for 2m 38s\n" +
			"────────────────────────────────\n" +
			"❯ drop the deprecated always-auth key\n" +
			"────────────────────────────────\n" +
			"  ⏵⏵ bypass permissions on\n", true},
		{"queued-messages placeholder", "" +
			"  ❯ можно позже\n" +
			"────────────────────────────────\n" +
			"❯ Press up to edit queued messages\n" +
			"────────────────────────────────\n" +
			"  ⏸ manual mode on\n", false},
		{"try placeholder", "────────────────\n❯ Try \"how does auth work?\"\n────────────────\n  footer\n", false},
		{"boxed empty", "╭──────────╮\n│ >        │\n╰──────────╯\n  ? for shortcuts\n", false},
		{"boxed draft", "╭──────────╮\n│ > hello  │\n╰──────────╯\n  ? for shortcuts\n", true},
		{"multiline draft", "────────────────\n❯ first line\n  second line\n────────────────\n  footer\n", true},
		{"no composer at all", "running tests…\n  PASS ok 1.2s\n", false},
		{"history quote lines only", "  > quoted output\n  > more output\n", false},
		{"trailing blank padding", "────────────────\n❯ draft text\n────────────────\n  footer\n\n\n\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LooksLikeUserDraft(tt.pane); got != tt.want {
				t.Errorf("LooksLikeUserDraft() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runtime/ -run TestLooksLikeUserDraft`
Expected: FAIL — `undefined: LooksLikeUserDraft`.

- [ ] **Step 3: Write the implementation**

`internal/runtime/draft.go`: window of the last `draftWindow = 8` rows after `trimTrailingBlank`; find the last composer prompt line (raw line starting with `❯` / `>` , or `│` + spaces + `>`); take it plus following lines up to the closing rule/border; strip sigil, box borders and cursor glyphs; empty ⇒ false; matches a `draftPlaceholders` prefix ⇒ false; otherwise true. Full doc comment listing the recognized shapes and the conservative rule.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runtime/ -run TestLooksLikeUserDraft -v`
Expected: PASS, all sub-tests.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/draft.go internal/runtime/draft_test.go
git commit -m "runtime: эвристика распознавания черновика в composer'е"
```

---

### Task 2: Guard inside Inject + `InjectOpts`

**Files:**
- Modify: `internal/runtime/runtime.go` (Runtime interface, `Inject` doc)
- Modify: `internal/runtime/tmux.go` (`ErrComposerBusy`, guard before the `C-u` at ~line 230)
- Modify: 9 test fakes (see Global Constraints)
- Test: `internal/runtime/tmux_draft_guard_test.go` (new)

**Interfaces:**
- Consumes: `LooksLikeUserDraft` (Task 1).
- Produces:
  - `type InjectOpts struct { Force bool }`
  - `Inject(ctx context.Context, h Handle, text string, opts InjectOpts) error`
  - `var ErrComposerBusy = errors.New("inject: composer holds a user draft; nothing was cleared or sent")`

- [ ] **Step 1: Write the failing tests**

In `internal/runtime/tmux_draft_guard_test.go`, using the existing fake-tmux harness from `tmux_inject_orphan_test.go`:
- draft in the pane, `InjectOpts{}` ⇒ returns `ErrComposerBusy`, and **zero** `send-keys`/`paste-buffer`/`load-buffer` calls were made.
- draft in the pane, `InjectOpts{Force: true}` ⇒ normal delivery, `C-u` sent.
- empty composer, `InjectOpts{}` ⇒ normal delivery.
- capture failure before the guard ⇒ proceeds (fail-open), no `ErrComposerBusy`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/runtime/`
Expected: FAIL (compile error: too many arguments / undefined `ErrComposerBusy`).

- [ ] **Step 3: Implement**

Add `InjectOpts`, `ErrComposerBusy`, and at the top of `Inject` (before the `C-u`): if `!opts.Force`, `capture-pane -p`; on capture error proceed (fail-open, documented); if `LooksLikeUserDraft(out)` return `ErrComposerBusy`. Update the `Runtime` interface doc comment with the third failure outcome. Update all 9 fakes and every existing `Inject(` call site.

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "runtime: гвард черновика перед C-u, ErrComposerBusy и InjectOpts{Force}"
```

---

### Task 3: Config knob

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config.ComposerBusyDeadline time.Duration` (`yaml:"composer_busy_deadline"`), default `10 * time.Minute`; `runtime`-independent constant `DefaultComposerBusyDeadline` next to the other `Default*` constants for zero-value fallback.

- [ ] **Step 1: Write the failing test** — default is 10m when config.yaml is absent; a yaml value overrides it.
- [ ] **Step 2: Run** `go test ./internal/config/` → FAIL (unknown field).
- [ ] **Step 3: Implement** the field, the default in `Load`, and a doc comment explaining what the deadline protects against (abandoned draft blocking the queue forever).
- [ ] **Step 4: Run** `go test ./internal/config/` → PASS.
- [ ] **Step 5: Commit** `git commit -am "config: composer_busy_deadline, дефолт 10m"`.

---

### Task 4: Queue hold on a busy composer

**Files:**
- Modify: `internal/queue/queue.go` (`deliver`, `attemptDelivery`)
- Test: `internal/queue/queue_draft_test.go` (new)

**Interfaces:**
- Consumes: `runtime.LooksLikeUserDraft`, `runtime.ErrComposerBusy`, `runtime.InjectOpts`, `config.Config.ComposerBusyDeadline`.
- Produces: `attemptDelivery(ctx context.Context, msg store.Message, sess store.Session, force bool)`.

Behavior:
1. In `deliver`'s loop, after the pending-quiz hold and the activity gate: `q.rt.Capture(ctx, handle, composerWindow)`; on error ⇒ proceed (fail-open). If `LooksLikeUserDraft` ⇒ remember `busySince` (local to this `deliver` call), `waitForReady`, `continue` — message stays `queued`, attempts untouched, `expireTimedOut` still applies the global `queue_timeout`.
2. Once `time.Since(busySince) >= deadline` ⇒ `attemptDelivery(..., force=true)`.
3. In `attemptDelivery`, `runtime.ErrComposerBusy` (the race: the human started typing between the probe and the `C-u`) ⇒ do not consume the attempt (`msg.Attempts--`), put the status back to `queued`, return; `deliverLoop` re-fetches and the probe holds it properly.

- [ ] **Step 1: Write the failing tests** in `internal/queue/queue_draft_test.go`:
  - busy composer ⇒ within the deadline the message is still `queued`, `Attempts == 0`, `Inject` never called.
  - busy composer past the deadline ⇒ `Inject` called with `InjectOpts{Force: true}`, message `delivered`.
  - `Inject` returns `ErrComposerBusy` ⇒ status back to `queued`, `Attempts` not incremented.
  - empty composer ⇒ unchanged normal path: `Inject` with `InjectOpts{}`, `delivered`.
- [ ] **Step 2: Run** `go test ./internal/queue/` → FAIL.
- [ ] **Step 3: Implement** the three points above, with a comment tying the hold to the existing `PendingQuiz` hold and explaining why it must happen before `ClaimMessage`.
- [ ] **Step 4: Run** `go test ./...` → PASS.
- [ ] **Step 5: Commit** `git commit -am "queue: удержание доставки при непустом черновике, дедлайн на форс"`.

---

### Task 5: Docs + full verification

**Files:**
- Modify: `docs/06-messaging.md` («Правила»)

- [ ] **Step 1** Add a rule (Russian) describing: draft in composer ⇒ delivery deferred, no `C-u`, no attempt burned, message stays queued; after `composer_busy_deadline` (default 10m) delivery proceeds as before and the stale draft is cleared; the heuristic is conservative and only covers the recognized Claude Code composer shapes.
- [ ] **Step 2** Run `make test` (or `go test ./...`) and `make lint` if present; capture the output.
- [ ] **Step 3** Commit and open the PR referencing feature `claude-code`.

## Self-Review

- Spec coverage: sentinel error (T2), heuristic + limits documented (T1), queue defer without attempt consumption (T4), busy deadline config default 10m (T3), timeout semantics intact (T4 — hold happens pre-claim, message stays `queued`), tests in runtime and queue (T1/T2/T4), docs (T5). No gaps.
- Placeholders: none — every step names files, commands and expected output.
- Type consistency: `LooksLikeUserDraft`, `InjectOpts{Force}`, `ErrComposerBusy`, `ComposerBusyDeadline` used identically across tasks.
