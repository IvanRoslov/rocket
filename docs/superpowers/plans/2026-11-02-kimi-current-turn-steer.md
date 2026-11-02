# Kimi checked current-turn steering — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When `Enter` parks a Rocket message in a busy Kimi's own queue, send Kimi exactly one `Ctrl-S` if (and only if) a fresh capture proves the editor is empty, the queue holds exactly this one message, and Kimi advertises immediate steering; otherwise leave the message queued for Kimi's normal delivery.

**Architecture:** The Kimi pane parser gains two observed facts (explicit queue total and the footer's immediate-steer advertisement) plus a shared queue-item ownership comparison. `tmuxRuntime.Inject` runs one bounded steer probe after the post-Enter capture shows the composer is no longer a draft; the probe decides from the pane only (not from activity) and sends at most one `C-s`. Nothing else in the delivery pipeline changes: no `InjectOpts` flag, no queue-policy change, no new transport.

**Tech Stack:** Go, tmux `capture-pane`/`send-keys`, existing Rocket queue/confirmer.

**Spec:** `/tmp/kimi-delevary-5920/spec-stage3.md` (Spec v3), `/tmp/kimi-delevary-5920/plan-stage3.md` (Plan v4).

## Global Constraints

- Only Kimi terminal-input steering and its pane observation change; no schema, dependency, workflow, native-transport or other-agent change.
- `internal/queue/queue.go` stays untouched: PR #178's never-force clause and PR #176's user-origin `turn.steer` confirmer remain the published contracts.
- The steer phase sends **only** `C-s`: never `C-u`, paste, Enter, recall or any destructive key based on a stale capture.
- Never re-paste a message Kimi already owns; if safe steering is unprovable, press nothing and let the journal confirmer finish.
- Visible foreign draft/queue/dialog/unknown pane blocks steering. The capture→key window is the accepted 5920/Q4 risk: input arriving after the last capture may be steered early; it is unmeasured and no mechanism is built to close it.
- No live Kimi session, installed binary, daemon or existing session is touched. Test doubles are deterministic and isolated.

## Review Focus

- A stale `ready` activity while a turn runs must not suppress steering (the probe must not read activity at all).
- Scrolled queue rows: `N queued · ` must be parsed, so one visible row plus a hidden foreign row does not pass `QueueTotal == 1`.
- Long pane width truncation: a `…`-cut row shorter than 20 non-space runes must not count as ownership.
- Busy→idle during submission: Kimi draining the queue (turn end/WaitFor) must leave no extra key and no second paste.
- Idle delivery: an empty composer with no queue must submit normally, with no `C-s` and no extra paste.

---

### Task 1: Observe queue total and immediate-steer advertisement

**Files:**
- Modify: `internal/runtime/kimipane.go`
- Test: `internal/runtime/kimipane_test.go`

**Interfaces:**
- Produces: `kimiComposer.queueTotal int`, `kimiComposer.steerable bool`; `func (k kimiComposer) steerableQueue(text string) bool`; shared `func kimiQueuedMatches(item, text string) bool`.

- [ ] **Step 1: Write failing parser tests** for `kimi-busy-queued.pane` (total 1, steerable, owns `reply QUEUED_OK`), a two-item synthetic pane (total 2, not steerable for our text), a scrolled synthetic pane whose foot says `2 queued · ` with one visible row and `ctrl-s to steer immediately` (total 2 → refuses), a foot saying `will send after current task` (not steerable), and a `…`-cut row under 20 non-space runes (not owned).
- [ ] **Step 2: Run `go test ./internal/runtime -run Kimi -count=1`** and observe the compile failure / missing behavior, reported raw.
- [ ] **Step 3: Implement** foot detection (`↑ to edit · ` row, not starting with `❯ `), `^(\d+) queued · ` parsing (else `len(queue)`), `ctrl-s to steer immediately` detection, `kimiQueuedMatches`, and `steerableQueue` requiring `KimiBusy`, `queueTotal == 1`, `len(queue) == 1`, ownership.
- [ ] **Step 4: Re-run the same test command** and observe PASS.
- [ ] **Step 5: Commit.**

### Task 2: One bounded steer probe in `Inject`

**Files:**
- Modify: `internal/runtime/tmux.go`
- Test: `internal/runtime/tmux_inject_kimi_test.go`

**Interfaces:**
- Consumes: `parseKimiComposer`, `kimiComposer.steerableQueue`.
- Produces: `func (t *tmuxRuntime) steerKimiQueued(ctx context.Context, h Handle, text, pane string, interval, timeout time.Duration)`; called where `kimiSubmitted` currently returns nil.

- [ ] **Step 1: Write failing behavioral tests** (fake tmux, asserting only public effects):
  - single-line and multiline message queued with foot `ctrl-s to steer immediately` → exactly one `C-s`, exactly one paste, one Enter, no post-paste clear;
  - foreign draft/foreign item/hidden scrolled item/`will send after current task`/truncated ownership/unknown pane → zero `C-s`;
  - queue already drained before the decision → zero `C-s`;
  - idle submit (composer empty, no queue) → zero `C-s`, one paste;
  - `C-s` send failure → `Inject` returns nil and no second paste.
- [ ] **Step 2: Run `go test ./internal/runtime -run KimiInject -count=1`** and observe RED with the expected `C-s` count mismatch.
- [ ] **Step 3: Implement** the probe: reuse the capture that proved `kimiSubmitted`, then poll `capture-pane` every `pollInterval` until `pollTimeout`; each capture is judged by `steerableQueue`; on the first true, send one `C-s` and return. Log a failed key; never return an error (the message is already Kimi's).
- [ ] **Step 4: Re-run** the test command and observe PASS; run the whole `./internal/runtime` package.
- [ ] **Step 5: Commit.**

### Task 3: Real-tmux content regression with a stub TUI

**Files:**
- Test: `internal/runtime/tmux_inject_kimi_steer_e2e_test.go`
- No production file.

Test writes a small Python stub to `t.TempDir()`: raw stdin, prints the Kimi composer box + status bar, keeps a bracketed paste as a draft, on Enter (while busy) renders one queue row + the steer footer, on `C-s` appends the queued payload to a file and redraws idle. The test runs a real tmux session in that temp dir and calls the real `tmuxRuntime.Inject` with `Dialect: PaneDialectKimi, Bracketed: true` and a multiline message.

- [ ] **Step 1: Write the failing test** asserting the stub's recorded current-turn input equals the complete multiline message, with one paste and one `C-s`. Skips only when tmux/python3 is unavailable.
- [ ] **Step 2: Run `go test ./internal/runtime -run KimiSteerE2E -count=1`** and observe RED (recorded input absent/empty).
- [ ] **Step 3: No production change expected** (Task 2 already implements it); if the stub reveals a probe bug, use superpowers:systematic-debugging and fix in the runtime, not the stub's expectations.
- [ ] **Step 4: Re-run** and observe PASS.
- [ ] **Step 5: Commit.**

### Task 4: Documentation

**Files:**
- Modify: `docs/10-agents.md` (delivery and parser bullets), `docs/06-messaging.md` (delivery section).
- Include this plan in the commit.

- [ ] **Step 1: Describe the exact predicate** (empty editor, exactly one visible queued item that is our message, footer advertises immediate steering), the no-key outcomes (foreign/unknown/clipped/`after current task`), and that the message stays Kimi's (`accepted`) until journal proof.
- [ ] **Step 2: Document the accepted 5920/Q4 window** in the same places: capture and key are not atomic; input appearing after the capture can be steered early; the window is not measured; no destructive key is sent from a stale capture.
- [ ] **Step 3: Commit.**

### Task 5: Final-head verification

- [ ] **Step 1:** `gofmt -l` on changed Go files (expect no output).
- [ ] **Step 2:** `go vet ./internal/runtime ./internal/queue ./internal/agent/kimicode` (real exit codes).
- [ ] **Step 3:** affected tests: `go test ./internal/runtime ./internal/agent/kimicode -count=1` and `go test ./internal/queue -count=1 -run 'Kimi|Draft|Composer|Accepted'`; record raw output and exit codes.
- [ ] **Step 4:** one wider check on the committed head (`go test ./internal/queue -count=1 -timeout 15m`), classifying unrelated host-load failures by exact test name/source instead of chasing green.
- [ ] **Step 5:** `git diff --stat origin/main..HEAD` scope check: runtime parser/tmux + their tests + docs + plan only; no queue.go, Pi, native or workflow changes. Open the single PR; no merge.
