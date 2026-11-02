# Kimi current-turn delivery — accepted partial: Kimi draft is never force-cleared

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `composer_busy_deadline` must never authorize clearing a human draft
in a Kimi session: the message stays queued until the composer frees, and the
global `queue_timeout` is the truthful backstop.

**Architecture:** One condition in `queue.deliver`'s draft guard: the Kimi pane
dialect joins Pi in the never-force set. Kimi's composer text cannot be told
apart from a human's typing, so the deadline that exists for abandoned drafts
must not erase it. Nothing else changes: no parser, key, flag or transport.

**Tech Stack:** Go, Rocket's per-recipient delivery queue.

**Spec:** Spec v2 acceptance 3 (`/tmp/kimi-delevary-5920/spec-stage2.md`), on
top of stage 1 (merge 595e229). Auto-steering (Ctrl-S into the running turn)
is **deferred**: human decision thread 5920/Q4 asks whether the residual
capture-to-key race is acceptable or Ctrl-S must be omitted. No steering
code/parser/key is part of this partial plan.

## Global Constraints

- No new transport, schema, dependency, CI workflow, daemon or live-session change; no edits to task #5902 or its native routing/protocol code.
- Kimi 2.4.0 stock TUI only.
- A foreign draft (single or multiline) is never erased, appended to, submitted or recalled; `composer_busy_deadline` must not lift that for Kimi.
- Other agents' delivery paths (including the Claude/Codex force-through) stay byte-for-byte unchanged; Pi's existing exemption is untouched.
- No partial PR and no review request until the orchestrator releases the PR (the branch may later grow with the Q4 outcome).

## Review Focus

- A Kimi draft that outlives `composer_busy_deadline` must keep the message `queued`, with zero Inject calls and zero attempts burned.
- The same protection must not wedge the queue: once the human's draft is gone, ordinary delivery resumes without `Force`.
- Claude Code must still be force-delivered past the deadline (existing test pins that).
- Kimi's other paths (first brief, cache-hint stash, accepted/confirmer) must stay unchanged.

---

### Task 1: Exclude the Kimi dialect from the busy-deadline force-through

**Files:**
- Modify: `internal/queue/queue.go`
- Test: `internal/queue/queue_kimi_test.go`

**Interfaces:**
- Consumes: `runtime.PaneDialectKimi`, existing `kimi-draft.pane` / `kimi-idle.pane` fixtures.
- Produces: no exported names.

- [x] **Step 1: Write the failing tests**

Added `TestQueue_KimiDraftNeverForcedByDeadline` and
`TestQueue_KimiDraftResumesWhenComposerFrees` in `internal/queue/queue_kimi_test.go`.
The first defers once, wakes a re-check past a 10 ms `ComposerBusyDeadline`,
and asserts the message is still `queued` with zero Inject calls and zero
attempts. The second additionally proves resume: after the same post-deadline
re-check still defers, flipping the pane to `kimi-idle.pane` and waking again
delivers once with `Force == false`.

- [x] **Step 2: Run them to verify they fail**

Run: `go test ./internal/queue -run 'TestQueue_KimiDraftNeverForcedByDeadline|TestQueue_KimiDraftResumesWhenComposerFrees' -count=1`
Observed RED:

```
WARN queue: composer busy past the deadline, delivering anyway (draft will be cleared) id=1 to=kimi deadline=10ms
--- FAIL: TestQueue_KimiDraftNeverForcedByDeadline (0.29s)
    queue_kimi_test.go:299: status = "accepted", want queued while a human draft is on screen
--- FAIL: TestQueue_KimiDraftResumesWhenComposerFrees (0.19s)
    queue_kimi_test.go:333: Inject called 1 times past the busy deadline, want 0
```

- [x] **Step 3: Implement the exclusion**

In `deliver`'s `case draft:`:

```go
neverForce := dialect == runtime.PaneDialectPi || dialect == runtime.PaneDialectKimi
if neverForce || time.Since(busySince) < q.composerBusyDeadline() {
	// defer: stays queued, no attempt burned
}
force = true
```

The comment above the guard now states why Kimi and Pi never force: their
composer text is indistinguishable from human typing, and the global
`queue_timeout` fails the message truthfully instead of destroying it.

- [x] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/queue -run 'TestQueue_KimiDraftNeverForcedByDeadline|TestQueue_KimiDraftResumesWhenComposerFrees' -count=1`
Observed GREEN (`ok`, both PASS).

- [x] **Step 5: Commit**

```bash
git add internal/queue/queue.go internal/queue/queue_kimi_test.go
git commit -m "queue: never force-clear a Kimi draft on the busy deadline"
```

---

### Task 2: Document the Kimi draft protection

**Files:**
- Modify: `docs/06-messaging.md` (draft-guard rule)
- Modify: `docs/10-agents.md` (Kimi "Доставка в занятый Kimi" bullet)

- [x] **Step 1: Update `docs/06-messaging.md`**

After the `composer_busy_deadline` sentence, state that the deadline does not
apply to `kimi-code` (like `pi`): the composer text cannot be told apart from
a human's, so the message stays `queued` and `queue_timeout` fails it
truthfully if the input never frees.

- [x] **Step 2: Update `docs/10-agents.md`**

In the Kimi "Доставка в занятый Kimi" bullet, state that a human draft is never
erased: `composer_busy_deadline` does not apply to Kimi (unlike Claude
Code/Codex), the message waits in `queued`, and `queue_timeout` is the
backstop.

- [x] **Step 3: Commit**

```bash
git add docs/06-messaging.md docs/10-agents.md docs/superpowers/plans/2026-11-02-kimi-current-turn-delivery.md
git commit -m "docs: Kimi drafts are never cleared by the busy deadline"
```

---

### Task 3: Verification on the committed head

- [ ] **Step 1: Format and vet**

Run: `gofmt -l internal cmd; go vet ./internal/queue ./internal/runtime ./internal/agent/kimicode`
Expected: no output.

- [ ] **Step 2: Targeted suites**

Run: `go test ./internal/runtime ./internal/agent/kimicode -count=1` and
`go test ./internal/queue -count=1 -run 'Kimi|Draft|Composer'`
Expected: `ok`.

- [ ] **Step 3: Whole package and repo**

Run: `go test ./internal/queue -count=1 -timeout 15m` and `go test ./... -count=1 -timeout 20m`
Expected: all `ok`. Note the host load when running: other Rocket sessions can
push the 1-minute load average above 10 and stretch the queue suite well past
its idle baseline, so use the test-binary timeout rather than a short shell
timeout and record the observed load.

- [ ] **Step 4: Confirm scope**

Run: `git diff --stat 595e229..HEAD && git log --oneline 595e229..HEAD`
Expected: `internal/queue/queue.go`, `internal/queue/queue_kimi_test.go`,
`docs/06-messaging.md`, `docs/10-agents.md`, this plan. No `#5902` native
files, no workflows, no dependency changes.

---

## Deferred: steering into the running turn (not implemented)

Pending 5920/Q4. The accepted-pending design was: after Enter parks the
message in Kimi's queue, send one `Ctrl-S` only when a capture proves the
editor is empty, the queue total is exactly this message, and Kimi's queue
footer advertises `ctrl-s to steer immediately`. Its residual capture-to-key
race is unquantified and is the reason for the Q4 gate. No parser field, no
`InjectOpts` flag and no `send-keys C-s` code exists on this branch.
