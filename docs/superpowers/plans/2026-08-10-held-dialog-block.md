# Held-dialog inject blocker — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:test-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Stop rocket from injecting into a tmux pane while Claude Code's held-peer-message
approval dialog is open, and stop the scrollback confirmation from false-matching that dialog's
banner preview.

**Architecture:** A new conservative pane detector in `internal/runtime` recognises the *open*
dialog (never its permanent scrollback banner). The queue treats it as a delivery blocker exactly
like a pending quiz — message stays `queued`, no attempt burned — and `Inject` re-checks
immediately before the destructive `C-u` to close the race. The final delivery-confirmation
capture strips held-banner chrome before looking for the marker.

**Tech Stack:** Go, tmux, Claude Code CLI 2.1.226.

## Recon (live, this worktree, CLI 2.1.226)

A held message renders **two** distinct things:

1. A permanent scrollback **banner** (never disappears):
   `⏺ Held peer message — from an unidentified session [verified pid N] (peer claims name: rocket); preview:`
   `«<message text>» — not delivered to Claude (1 held). The sender did not attest its permission`
   `mode and this session bypasses prompts. Review it below, or set "crossSessionInbound" to "accept".`
2. A transient **dialog** at the bottom of the visible pane, which vanishes on approve/deny/expiry:
   ```
   ────────────────────────────────────────────────────
    Held message from another session

     Another Claude session sent a message: from an unidentified session [verified pid N] (peer claims name: rocket)

     The sender did not attest its permission mode, and this session bypasses permission prompts.

     Message body (this is what will be delivered):
     «<message text>»

     ❯ Deny — drop it and tell the sender it was declined
       Deliver this message to Claude
   ```

Reproduced live: `tmux send-keys -l "<text>"` + `Enter` into that pane **dismissed the dialog**
(the keystrokes drove the selector, not the composer), the injected text never reached the
conversation, the held copy was dropped — and the banner (which contains the same text in its
preview) stayed in scrollback, which is what makes `Inject`'s final scrollback check report
"delivered".

Detection therefore keys on the **dialog**, never on the banner. It requires the dialog title
*and* one of the two option captions: a single phrase could appear inside a rocket message that
an agent echoed into its own pane, and blocking on that would deadlock the recipient's queue.

There is deliberately **no** force-through deadline (unlike the draft guard). Forcing an injection
through a real dialog destroys both copies of the message — the exact bug being fixed. The
existing global `queue_timeout` (default 30m, `expireTimedOut`) is the backstop: a message stuck
behind a pathological false positive fails visibly instead of dying silently.

## File structure

- Create: `internal/runtime/heldpeer.go` — `LooksLikeHeldPeerDialog`, `StripHeldPeerBanner`.
- Create: `internal/runtime/heldpeer_test.go` — fixtures from the live captures above.
- Modify: `internal/runtime/tmux.go` — `ErrHeldDialog`, pre-`C-u` guard, banner-stripped final check.
- Modify: `internal/queue/queue.go` — pre-inject gate in `deliver`, `ErrHeldDialog`/`ErrHeld`
  requeue in `attemptDelivery`, banner-stripped `markerPresent`.
- Modify: `internal/queue/queue_test.go` (new cases), `docs/06-messaging.md`.

---

### Task 1: dialog detector

**Files:** create `internal/runtime/heldpeer.go`, `internal/runtime/heldpeer_test.go`.

**Produces:** `func LooksLikeHeldPeerDialog(pane string) bool`.

- [ ] Step 1: failing tests — open-dialog fixture ⇒ true; banner-only scrollback ⇒ false;
      empty pane ⇒ false; ordinary composer ⇒ false; quiz widget ⇒ false; title-only prose ⇒ false.
- [ ] Step 2: `go test ./internal/runtime/ -run HeldPeer` ⇒ FAIL (undefined).
- [ ] Step 3: implement: true iff pane contains `"Held message from another session"` AND
      (`"Deliver this message to Claude"` OR `"Deny — drop it and tell the sender it was declined"`).
- [ ] Step 4: tests PASS. Step 5: commit.

### Task 2: Inject guard

**Files:** modify `internal/runtime/tmux.go`, test in `internal/runtime/tmux_test.go`.

**Produces:** `var ErrHeldDialog = errors.New(...)`.

- [ ] Step 1: failing test — fake tmux whose `capture-pane` returns the dialog fixture; `Inject`
      returns `ErrHeldDialog`, and no `send-keys`/`paste-buffer` was executed. Also with
      `InjectOpts{Force: true}` (the guard is not a draft guard; Force must not bypass it).
- [ ] Step 2: run ⇒ FAIL. Step 3: implement the check before the draft guard, on its own
      whole-pane escaped capture, fail-open on capture error. Step 4: PASS. Step 5: commit.

### Task 3: queue gate

**Files:** modify `internal/queue/queue.go`, tests in `internal/queue/queue_test.go`.

- [ ] Step 1: failing tests — (a) dialog present, no socket ⇒ message stays `queued`, attempts 0,
      no Inject call; (b) dialog gone on the next pass ⇒ delivered with attempts 1;
      (c) `Inject` returning `ErrHeldDialog` ⇒ requeued, attempts back to 0;
      (d) socket `ErrHeld` ⇒ requeued (no immediate tmux inject in the same pass).
- [ ] Step 2: run ⇒ FAIL. Step 3: implement — one escaped whole-pane capture per pass feeding both
      the held-dialog gate and the existing draft probe; `ErrHeldDialog` handled next to
      `ErrComposerBusy`; the `ErrHeld` branch requeues instead of falling through to Inject.
- [ ] Step 4: PASS. Step 5: commit.

### Task 4: confirmation hardening

**Files:** modify `internal/runtime/heldpeer.go` (`StripHeldPeerBanner`), `internal/runtime/tmux.go`
final scrollback check, `internal/queue/queue.go` `markerPresent`; tests in both packages.

- [ ] Step 1: failing tests — a scrollback containing only the held banner whose preview holds the
      marker ⇒ `StripHeldPeerBanner` removes the whole wrapped banner block; `markerPresent`
      returns false for it; a normal echoed message is untouched.
- [ ] Step 2: run ⇒ FAIL. Step 3: implement — drop lines from one starting with the banner opener
      (`⏺`-prefixed `Held peer message —`) through the next blank line. Step 4: PASS. Step 5: commit.

### Task 5: docs

- [ ] Update `docs/06-messaging.md` next to the pending-quiz rule; note the no-deadline decision
      and the `queue_timeout` backstop. Commit.

### Task 6: verification

- [ ] `go build ./... && go vet ./... && go test ./...` green.
- [ ] Manual E2E on the scratch bypass session: socket send ⇒ dialog opens ⇒ rocket defers
      (no injection, message still queued) ⇒ dismiss the dialog ⇒ the tmux copy arrives.
