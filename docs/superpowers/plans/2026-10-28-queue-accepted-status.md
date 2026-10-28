# Queue `accepted` Status Implementation Plan (feature #5707, T4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A message injected into a Codex session becomes `accepted` and is marked `delivered` only once the Codex journal proves the model received it; unconfirmed messages are requeued or fail with `not_confirmed`.

**Architecture:** The queue resolves the recipient's agent adapter; when it implements `agent.DeliveryConfirmer`, a successful paste (`nil` or `runtime.ErrQueuedByAgent`) ends in `accepted` with `inject_at` recorded before the paste, and the FIFO worker moves on. A new confirmer goroutine in `internal/queue/confirm.go` polls accepted messages (3 s ticker while any exist, plus bus kicks), calls `ConfirmDelivered(text, inject_at)` and applies the requeue/fail rules. `codex.ConfirmDelivered` is changed to scan the journal tail backwards and stop at records not after `since`, so each call reads only the recent tail. The Claude Code path is untouched: no `DeliveryConfirmer` means the old `delivered` logic byte for byte.

**Tech Stack:** Go, SQLite migrations (`internal/store/migrations`), tmux runtime fakes in `internal/queue/queue_test.go`.

**Spec:** task #5707 docs `spec` v1 and `plan` v1 (`rocket task show 5707`), section T4, plus the orchestrator's amendments in `.rocket/inbox/msg-74486.md` (binding: `inject_at` instead of `accepted_at`; one journal record may confirm several different messages; dedup only for identical texts; no per-message timeout while the recipient is `active`).

## Global Constraints

- Only repo `rocket`; PR to `main`; CI green; `go test -race ./internal/queue/...` green.
- Claude Code behaviour byte-for-byte unchanged: every new branch is gated by `agent.DeliveryConfirmer` / `agent.AcceptsInputWhileBusy`.
- No real Codex in CI: fixtures under `testdata/`, fake runtime/agent/clock in queue tests.
- Interface names are fixed: `agent.BusyInputAccepter`, `agent.AcceptsInputWhileBusy`, `agent.DeliveryConfirmer`, `runtime.ErrQueuedByAgent`, `runtime.LooksLikeAgentQueued`. No new agent interfaces.
- Timeout "turn ended without confirmation": 60 s after `inject_at`. Confirmer tick: 3 s. `maxAttempts` stays 5.
- New column: `inject_at INTEGER NOT NULL DEFAULT 0` (unix **milliseconds**), migration `0024_message_inject_at.sql`.
- New status `accepted`; new bus event `message.accepted {id,to,from}`; new fail reason `not_confirmed`.
- Docs updated in the same PR: `docs/06-messaging.md`, `docs/10-agents.md`, `docs/04-cli.md`, `docs/05-state.md`.

## Review Focus

1. Two identical texts to one recipient: the second must need a second journal record (Task 5, `TestTwoIdenticalMessagesNeedTwoTranscriptRecords`).
2. One journal record holding two different merged messages confirms both (Task 5, `TestOneRecordConfirmsTwoMergedMessages`).
3. Daemon restart with an `accepted` message: confirmed after `Recover`, never stuck (Task 6, `TestRecoverKeepsAcceptedAndConfirmsAfterRestart`).
4. Recipient stays `active` for a long foreground command: the accepted message is neither expired nor requeued (Task 5, `TestAcceptedStaysWhileRecipientActive`).
5. Claude recipient: `active` waits, success is `delivered` directly, no `inject_at`, no `message.accepted` (Task 4, `TestClaudeRecipientPathUnchanged`).

---

### Task 1: Store — `inject_at` column, `accepted` status helpers

**Files:**
- Create: `internal/store/migrations/0024_message_inject_at.sql`
- Modify: `internal/store/messages.go`
- Test: `internal/store/messages_test.go` (append)

**Interfaces:**
- Produces:
  - `Message.InjectAt int64` (unix ms, 0 = never injected under confirmation); `Message.Status` comment becomes `queued|delivering|accepted|delivered|failed`.
  - `func (s *Store) SetMessageInjectAt(id int64, injectAt int64) error`
  - `func (s *Store) ListAcceptedMessages() ([]Message, error)` — all `status='accepted'`, `ORDER BY to_session, id`.
  - `func (s *Store) ConfirmAccepted(id int64, deliveredAt int64) (bool, error)` — CAS `accepted → delivered`, sets `delivered_at`.
  - `func (s *Store) RequeueAccepted(id int64) (bool, error)` — CAS `accepted → queued`, `attempts = attempts + 1`, `inject_at = 0`.
  - `func (s *Store) FailAccepted(id int64, reason string) (bool, error)` — CAS `accepted → failed`, sets `reason`.
  - Every `SELECT` of messages (GetMessage, ListMessages, NextQueuedMessage, ExpireQueuedBefore RETURNING) adds `inject_at`; `scanMessage` scans it.

- [ ] **Step 1: Write the failing tests** in `internal/store/messages_test.go`

```go
func TestMessageInjectAtRoundTrip(t *testing.T) {
	st := openTestStore(t) // use the file's existing helper name
	id, _ := st.AddMessage(Message{ToSession: "r", Body: "x"})
	if err := st.SetMessageInjectAt(id, 1700000000123); err != nil { t.Fatal(err) }
	m, _ := st.GetMessage(id)
	if m.InjectAt != 1700000000123 { t.Fatalf("InjectAt=%d", m.InjectAt) }
}

func TestAcceptedTransitionsAreCAS(t *testing.T) {
	st := openTestStore(t)
	id, _ := st.AddMessage(Message{ToSession: "r", Body: "x"})
	if ok, _ := st.ConfirmAccepted(id, 5); ok { t.Fatal("queued must not confirm") }
	_ = st.UpdateMessageStatus(id, "accepted", 1, 0, "")
	ok, _ := st.RequeueAccepted(id)
	m, _ := st.GetMessage(id)
	if !ok || m.Status != "queued" || m.Attempts != 2 || m.InjectAt != 0 { t.Fatalf("%+v", m) }
	_ = st.UpdateMessageStatus(id, "accepted", 2, 0, "")
	if ok, _ := st.FailAccepted(id, "not_confirmed"); !ok { t.Fatal("fail") }
	m, _ = st.GetMessage(id)
	if m.Status != "failed" || m.Reason != "not_confirmed" { t.Fatalf("%+v", m) }
	if ok, _ := st.ConfirmAccepted(id, 5); ok { t.Fatal("failed must not confirm") }
}

func TestListAcceptedMessagesOrderedByRecipientThenID(t *testing.T) {
	// three accepted rows for "b","a","a" → order a(id2), a(id3), b(id1)
}

func TestResetDeliveringKeepsAccepted(t *testing.T) {
	// one 'delivering' and one 'accepted' row → ResetDelivering returns 1; accepted row unchanged
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'InjectAt|AcceptedTransitions|ListAccepted|KeepsAccepted' -v`
Expected: compile errors (`InjectAt`, `SetMessageInjectAt` undefined).

- [ ] **Step 3: Add migration and store methods**

`0024_message_inject_at.sql`: `ALTER TABLE messages ADD COLUMN inject_at INTEGER NOT NULL DEFAULT 0;`
Implement the methods listed in Interfaces; keep `ResetDelivering` and `ExpireQueuedBefore` touching only `delivering`/`queued` rows.

- [ ] **Step 4: Run store tests**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrations/0024_message_inject_at.sql internal/store/messages.go internal/store/messages_test.go
git commit -m "store: inject_at column and CAS helpers for the accepted message status (#5707 T4)"
```

### Task 2: Codex `ConfirmDelivered` scans the journal tail backwards

**Files:**
- Modify: `internal/agent/codex/confirm.go`
- Test: `internal/agent/codex/confirm_test.go` (append)

**Interfaces:**
- Consumes: existing `matchUserRecord(line []byte, want string, since time.Time) (time.Time, bool)`.
- Produces: same `ConfirmDelivered` signature and semantics (earliest matching record strictly after `since`), but reading from EOF backwards in 64 KiB chunks and stopping at the first record whose `timestamp` is not after `since`. New unexported helper `func recordTimestamp(line []byte) (time.Time, bool)` extracting the `"timestamp":"…"` field without a full JSON decode.

- [ ] **Step 1: Write the failing tests**

```go
// TestConfirmDeliveredStopsAtSince: fixture with an earlier record whose line
// is deliberately corrupt JSON (would be skipped by a forward scan anyway);
// instead prove the scan bound by counting bytes: wrap the file in a
// counting reader is not possible through the public API, so assert
// behaviour: a matching record BEFORE since plus an identical matching
// record AFTER since → at == the later one (same as today), and a 20 MiB
// padding record older than since does not change the result within 200 ms.
func TestConfirmDeliveredTailScanIgnoresOldBulk(t *testing.T) { ... }

// TestConfirmDeliveredRecordWithoutTimestampDoesNotStopScan: a line lacking
// "timestamp" between since and the match must be skipped, not stop the scan.
func TestConfirmDeliveredRecordWithoutTimestampDoesNotStopScan(t *testing.T) { ... }

// TestConfirmDeliveredMatchSpansChunkBoundary: a matching user record
// longer than 64 KiB (pad its text with a long first line, want = its last
// line) is found.
func TestConfirmDeliveredMatchSpansChunkBoundary(t *testing.T) { ... }
```

Use `installConfirmFixture` with an `edit` func to prepend/append lines.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/codex/ -run 'TailScan|WithoutTimestamp|ChunkBoundary' -v`
Expected: the bulk test exceeds 200 ms or FAILs; the others may pass today — keep them as regression guards (note in commit).

- [ ] **Step 3: Implement the backward scan**

Algorithm (the only part the signature does not determine): seek to EOF; read 64 KiB chunks backwards into a carry buffer; split the buffer on `\n`, keep the first (possibly partial) segment as the new carry, process the rest newest → oldest; for each line: `ts, ok := recordTimestamp(line)`; if `ok && !ts.After(since)` → stop scanning; else `matchUserRecord` and remember the match with the smallest `at`. When offset reaches 0, process the carry as the first line. Honor `ctx.Err()` per chunk. Return the earliest remembered match.

- [ ] **Step 4: Run all codex tests**

Run: `go test ./internal/agent/codex/...`
Expected: PASS (all ten existing `TestConfirmDelivered*` stay green).

- [ ] **Step 5: Commit**

```bash
git add internal/agent/codex/confirm.go internal/agent/codex/confirm_test.go
git commit -m "codex: ConfirmDelivered scans the journal tail backwards, bounded by since (#5707 T4)"
```

### Task 3: Queue plumbing — agent resolver, clock, test fakes

**Files:**
- Modify: `internal/queue/queue.go` (struct fields + `New`)
- Modify: `internal/queue/queue_test.go` (harness)
- Create: `internal/queue/fakeagent_test.go`

**Interfaces:**
- Produces in `queue.go`:
  - field `resolveAgent func(name string) (agent.Agent, error)`, default `agent.Get` in `New`; `func (q *Queue) SetAgentResolver(f func(string) (agent.Agent, error))`.
  - field `now func() time.Time`, default `time.Now`.
  - field `confirmKick chan struct{}` (buffered 1) created in `New`.
  - `func (q *Queue) recipientCaps(sess store.Session) (busyOK bool, confirmer agent.DeliveryConfirmer)` — resolves `sess.Agent`; a resolver error yields `(false, nil)`.
- Produces in tests:
  - `type fakeAgent struct { busy bool; confirmFn func(text string, since time.Time) (bool, time.Time, error); mu sync.Mutex; calls []confirmCall }` implementing `agent.Agent` (stub methods), `agent.BusyInputAccepter` (`AcceptsInputWhileBusy() bool { return a.busy }`) and `agent.DeliveryConfirmer`.
  - `type fakeClock struct { mu sync.Mutex; t time.Time }` with `now()` and `advance(d)`.
  - harness fields `ag *fakeAgent`, `clock *fakeClock`; `newTestQueue` installs `q.SetAgentResolver(func(name string) (agent.Agent, error) { if name == "codex" { return h.ag, nil }; return nil, errors.New("unknown agent") })` and `q.now = h.clock.now`.
  - `func (h *testHarness) addCodexSession(t *testing.T, id string, state activity.State)` — like `addRunningSession` but `Agent: "codex"`.
  - `func (h *testHarness) startConfirmer()` — `go h.q.confirmLoop(h.ctx)` (defined in Task 5; stub it in Task 3 as an empty method so the file compiles? No: add the helper in Task 5 instead).

- [ ] **Step 1: Write the failing test** `TestRecipientCapsResolvesCodexOnly` in `queue_test.go`

```go
func TestRecipientCapsResolvesCodexOnly(t *testing.T) {
	h := newTestQueue(t)
	h.ag.busy = true
	busy, conf := h.q.recipientCaps(store.Session{Agent: "codex"})
	if !busy || conf == nil { t.Fatal("codex must be busy-ok and a confirmer") }
	busy, conf = h.q.recipientCaps(store.Session{Agent: "claude-code"})
	if busy || conf != nil { t.Fatal("claude-code must keep the old path") }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/queue/ -run TestRecipientCapsResolvesCodexOnly`
Expected: compile error (`recipientCaps`, `ag` undefined).

- [ ] **Step 3: Implement fields, `SetAgentResolver`, `recipientCaps`, fakes**

- [ ] **Step 4: Run the whole package**

Run: `go test ./internal/queue/...`
Expected: PASS (existing tests unaffected: default resolver returns an error for "claude-code" in tests since no agent is registered, which maps to the old path).

- [ ] **Step 5: Commit**

```bash
git add internal/queue/queue.go internal/queue/queue_test.go internal/queue/fakeagent_test.go
git commit -m "queue: agent resolver, injectable clock and test fakes for the accepted path (#5707 T4)"
```

### Task 4: Delivery — busy Codex is ready, success becomes `accepted`

**Files:**
- Modify: `internal/queue/queue.go` (`deliver`, `attemptDelivery`, new `accept`)
- Delete: `internal/queue/queue_queuedbyagent_test.go`
- Create: `internal/queue/queue_accepted_test.go`

**Interfaces:**
- Consumes: Task 1 `SetMessageInjectAt`, `UpdateMessageStatus(id,"accepted",…)`; Task 3 `recipientCaps`, `q.now`, `q.confirmKick`.
- Produces:
  - `func (q *Queue) accept(msg store.Message)` — status `accepted` (attempts kept, `delivered_at` 0), publish `message.accepted {id,to,from}`, non-blocking send on `q.confirmKick`.
  - `deliver`: `busyOK, confirmer := q.recipientCaps(sess)` once per pass; readiness switch gains `case known && state == activity.Active && busyOK:` → proceed; passes `confirmer` into `attemptDelivery(ctx, msg, sess, force, viaSocket, confirmer)`.
  - `attemptDelivery`: when `confirmer != nil`, right before `q.rt.Inject`: `injectAt := q.now().UnixMilli(); q.st.SetMessageInjectAt(msg.ID, injectAt)`; result mapping with a confirmer: `nil` → `accept`; `ErrQueuedByAgent` → `accept`; `ErrSubmitUnconfirmed` with marker gone → `accept` (instead of `deliverSuccess`). Without a confirmer every branch is unchanged, including `ErrQueuedByAgent → deliverSuccess` (kept as the no-duplicate fallback, TODO comment removed).

- [ ] **Step 1: Write the failing tests** in `queue_accepted_test.go`

```go
func TestCodexBusyRecipientGetsMessageWhileActive(t *testing.T) {
	h := newTestQueue(t); h.ag.busy = true
	h.addCodexSession(t, "cx", activity.Active)
	id := addMsg(t, h, "cx", "hello")
	h.q.Wake("cx")
	waitUntil(t, func() bool { return h.rt.callCount() == 1 }, "inject while active")
	waitUntil(t, func() bool { return messageStatus(t, h.st, id) == "accepted" }, "accepted")
	m, _ := h.st.GetMessage(id)
	if m.InjectAt == 0 || m.InjectAt > h.clock.now().UnixMilli() { t.Fatalf("inject_at=%d", m.InjectAt) }
}

func TestQueuedByAgentBecomesAccepted(t *testing.T) {
	// injectFn returns ErrQueuedByAgent; codex session Ready → status accepted, 1 Inject call,
	// bus got message.accepted{id,to:"cx"}; no message.delivered yet.
}

func TestAcceptedDoesNotBlockNextMessage(t *testing.T) {
	// two messages to "cx"; confirmFn always returns false; both Injected (callCount==2)
	// and both "accepted" while the confirmer is not running.
}

func TestClaudeRecipientPathUnchanged(t *testing.T) {
	// addRunningSession("cl", activity.Active): no Inject within 150 ms;
	// set Ready → Inject nil → "delivered", InjectAt==0, no message.accepted on the bus,
	// and injectFn=ErrQueuedByAgent on a second message → "delivered" after 1 call.
}
```

`addMsg(t, h, to, body) int64` is a tiny helper added to `queue_test.go`. Bus assertions subscribe via `h.b.Subscribe()` before `Wake`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/queue/ -run 'CodexBusy|QueuedByAgentBecomesAccepted|DoesNotBlockNext|ClaudePath' -v`
Expected: FAIL (`accepted` never reached; busy recipient never injected).

- [ ] **Step 3: Implement `accept`, readiness and result mapping; delete `queue_queuedbyagent_test.go`**

- [ ] **Step 4: Run package with race detector**

Run: `go test -race ./internal/queue/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A internal/queue
git commit -m "queue: busy Codex is ready to receive; confirmed recipients get status accepted (#5707 T4)"
```

### Task 5: Confirmer loop — `accepted → delivered | queued | failed`

**Files:**
- Create: `internal/queue/confirm.go`
- Create: `internal/queue/confirm_test.go`
- Modify: `internal/queue/queue.go` (`Run` starts the loop; `expireTimedOut` untouched)
- Modify: `internal/queue/queue_test.go` (harness `startConfirmer()`)

**Interfaces:**
- Consumes: Task 1 `ListAcceptedMessages`, `ConfirmAccepted`, `RequeueAccepted`, `FailAccepted`; Task 3 `recipientCaps`, `q.now`, `q.confirmKick`; `runtime.LooksLikeAgentQueued`; `agent.ActivityRef`.
- Produces:
  - constants `confirmInterval = 3 * time.Second`, `acceptedTurnGrace = 60 * time.Second`, capture size `acceptedPaneLines = 60`.
  - `func (q *Queue) confirmLoop(ctx context.Context)` — started by `Run` as `go q.confirmLoop(ctx)`; subscribes to the bus; loop: `pending := q.confirmPass(ctx)`; wait on `ctx.Done`, `q.confirmKick`, bus events of type `session.activity_changed` / `session.restored` / `session.state_changed`, and — only when `pending > 0` — a 3 s timer.
  - `func (q *Queue) confirmPass(ctx context.Context) (pending int)` — one pass over `ListAcceptedMessages()` grouped by recipient in id order; returns how many stay `accepted`.
  - `func (q *Queue) injectedText(msg store.Message, sess store.Session) string` — the text that was pasted: `formatBody` unless the body is large and `<worktree>/.rocket/inbox/msg-<id>.md` exists, then the pointer line (same string `prepareText` builds, without rewriting the file). Refactor `prepareText` to share the pointer-formatting helper `inboxPointer(msg, relPath) string`.
  - per-recipient dedup state `q.confirmedRecords map[string]map[recordKey]struct{}` with `type recordKey struct{ at int64 /*unix ns*/; text string /*whitespace-normalized*/ }`; entries for a recipient are dropped when it has no accepted messages left.
  - restart requeue: a `session.restored` event for recipient R requeues every accepted message of R on the next pass (set `q.restarted[R] = true`, consumed by the pass).

Per-message decision in `confirmPass` (the algorithm the tests pin):

```
sess, err := q.getSession(to)
exited := errors.Is(err, store.ErrNotFound) || sess.State != "running" || (known && state == activity.Exited) || restarted[to]
if !exited && err == nil:
    since := time.UnixMilli(msg.InjectAt)
    text  := q.injectedText(msg, sess); key.text = normalizeSpace(text)
    for:
        found, at, cerr := confirmer.ConfirmDelivered(ctx, ref, text, since)
        if cerr != nil || !found: break
        if used[to][{at,text}]: since = at; continue   // identical text already proven by this record
        used[to][{at,text}] = {}; ConfirmAccepted(id, now); publish message.delivered; next message
    if state == activity.Active: keep accepted (pending++); next message
    if now - inject_at < 60s: keep accepted (pending++); next message
    pane, _ := q.rt.Capture(ctx, handle, acceptedPaneLines)
    if runtime.LooksLikeAgentQueued(pane, text): keep accepted (pending++); next message
// requeue or fail:
if msg.Attempts+1 >= maxAttempts || now - created_at >= cfg.QueueTimeout:
    FailAccepted(id, "not_confirmed"); publish message.failed{reason:"not_confirmed"}; notifySenderOfFailure
else:
    RequeueAccepted(id); q.Wake(to)
```

`confirmer` comes from `recipientCaps(sess)`; if nil (agent changed), treat as the 60 s rule with no pane check. Messages of one recipient are processed in id order so a shared record is consumed by the earliest message first.

- [ ] **Step 1: Write the failing tests** in `confirm_test.go` (each: `h := newTestQueue(t); h.startConfirmer()`; codex session; `h.ag.confirmFn` scripted; `h.q.Wake`)

```go
func TestQueuedByAgentBecomesAcceptedThenDelivered(t *testing.T)
// injectFn → ErrQueuedByAgent; confirmFn returns (true, since+1s) once text == "[from o] hi"
// → status delivered, DeliveredAt>0, bus saw message.accepted then message.delivered; 1 Inject.

func TestOneRecordConfirmsTwoMergedMessages(t *testing.T)
// two different bodies A, B; confirmFn returns (true, T) for both texts with the same T
// → both delivered, Inject called twice.

func TestTwoIdenticalMessagesNeedTwoTranscriptRecords(t *testing.T)
// two messages with the same body; confirmFn: records at T1 (and T2 only after a flag is set);
// semantics: returns the earliest record strictly after since from []time{T1[,T2]}.
// Phase 1: only T1 → first delivered, second stays accepted (confirmFn was asked with since=T1
// and found nothing). Phase 2: add T2 → second delivered.

func TestAcceptedStaysWhileRecipientActive(t *testing.T)
// confirmFn false; state Active; clock.advance(30*time.Minute) with QueueTimeout 30m
// → after a kick + 100 ms the message is still accepted, Inject count 1, no message.failed.

func TestAcceptedRequeuedAfterTurnEndsWithoutConfirmation(t *testing.T)
// confirmFn false; state Active → set Ready (publish session.activity_changed on h.b);
// clock.advance(59s) → still accepted; advance(2s) + kick → requeued (attempts 2) → worker injects
// again (callCount 2). Pane capture returns "" (no queue block).

func TestAcceptedKeptWhilePaneShowsQueuedItem(t *testing.T)
// as above but captureFn returns a pane with the Codex queue block holding the text → stays accepted.

func TestAcceptedRequeuedWhenRecipientExits(t *testing.T)
// state → Exited (publish activity_changed) immediately → requeued; worker then fails it
// recipient_unavailable (existing rule) — assert status "queued" first, then "failed".

func TestAcceptedRequeuedWhenRecipientRestored(t *testing.T)
// publish session.restored for the recipient → requeued → re-injected (callCount 2).

func TestAcceptedFailsNotConfirmedAfterMaxAttempts(t *testing.T)
// message pre-seeded with Attempts: maxAttempts-1, status accepted, inject_at = now-61s, state Ready,
// confirmFn false, sender session "o" running → status failed, reason not_confirmed,
// sender "o" has a queued notice containing "not_confirmed".
```

Bus events in tests are published with `h.b.Publish("session.activity_changed", "cx", nil)` after `h.ac.set`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/queue/ -run 'ThenDelivered|MergedMessages|TwoTranscript|StaysWhile|TurnEnds|PaneShows|RecipientExits|RecipientRestored|NotConfirmed' -v`
Expected: compile error (`startConfirmer` / `confirmLoop` undefined).

- [ ] **Step 3: Implement `confirm.go`, `injectedText`, start the loop from `Run`, harness `startConfirmer`**

- [ ] **Step 4: Run the package with race detector, three times**

Run: `go test -race -count=3 ./internal/queue/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/queue/confirm.go internal/queue/confirm_test.go internal/queue/queue.go internal/queue/queue_test.go
git commit -m "queue: confirmer turns accepted into delivered from the Codex journal, requeues or fails the rest (#5707 T4)"
```

### Task 6: Recover keeps `accepted`; daemon wiring; CLI `send --wait`

**Files:**
- Modify: `internal/queue/queue.go` (`Recover` comment + kick), `internal/queue/confirm_test.go`
- Modify: `internal/cli/send.go`, `internal/cli/send_test.go` (if a test file exists; otherwise add `TestWaitForMessagePrintsAcceptedOnce` using the package's existing httptest pattern — check `internal/cli/*_test.go` for one)
- Verify (no change expected): `internal/daemon/daemon.go` — `Run` already starts the confirmer.

**Interfaces:**
- Consumes: Task 5 `confirmLoop`.
- Produces: `Recover` logs the count of accepted messages it leaves for the confirmer and kicks `q.confirmKick`; `waitForMessage` prints `message %d accepted (waiting for the agent to pick it up)` once, then keeps polling.

- [ ] **Step 1: Write the failing tests**

```go
func TestRecoverKeepsAcceptedAndConfirmsAfterRestart(t *testing.T) {
	// seed store: codex session "cx" running, message status accepted, attempts 1, inject_at = now-5s;
	// build a SECOND Queue over the same store (new harness fn newTestQueueOn(t, st)), confirmFn true
	// → Recover(ctx); startConfirmer → status delivered; Inject never called (callCount 0).
}
```

CLI: `TestWaitForMessagePrintsAcceptedOnce` — server returns statuses `accepted, accepted, delivered` on consecutive polls; stdout contains exactly one `accepted` line and ends with `message 7 delivered`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/queue/ -run RecoverKeepsAccepted -v && go test ./internal/cli/ -run PrintsAcceptedOnce -v`
Expected: FAIL / compile error.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run**

Run: `go test -race ./internal/queue/... ./internal/cli/... ./internal/store/... ./internal/agent/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/queue internal/cli/send.go internal/cli/send_test.go
git commit -m "queue: Recover hands accepted messages to the confirmer; send --wait shows accepted (#5707 T4)"
```

### Task 7: Documentation

**Files:**
- Modify: `docs/06-messaging.md` — status flow diagram gains `accepted`; replace the sentence «Пока статуса `accepted` нет, очередь считает это `delivered`» with a new subsection «Статус `accepted` (получатели с `DeliveryConfirmer`)»: readiness of `active` for `BusyInputAccepter`, `inject_at` taken before the paste, confirmer cadence (3 с + события `session.activity_changed`/`session.restored`), one record may confirm several different messages, identical texts need separate records, requeue rules (exited/killed/restored; ≥60 с not active, no journal record, no queue block on screen), `failed` с `reason=not_confirmed` after `maxAttempts` or `queue_timeout`, no expiry while the recipient is `active`, `Recover` keeps `accepted`, event `message.accepted`.
- Modify: `docs/05-state.md` — messages schema comment `queued|delivering|accepted|delivered|failed`, column `inject_at` (unix ms).
- Modify: `docs/10-agents.md` — `ConfirmDelivered` reads the journal tail backwards and stops at the first record not after `since`.
- Modify: `docs/04-cli.md` — `rocket send --wait` prints the intermediate `accepted` line.
- Modify: `docs/03-daemon-api.md` — if the events table lists `message.delivered`, add `message.accepted`.

- [ ] **Step 1: Edit the docs (Russian, matching surrounding style)**
- [ ] **Step 2: Verify** `grep -n accepted docs/06-messaging.md docs/05-state.md docs/04-cli.md docs/10-agents.md` shows each addition.
- [ ] **Step 3: Commit** `git commit -am "docs: accepted message status, journal confirmation, send --wait (#5707 T4)"`

### Task 8: Verification and PR

- [ ] `go build ./... && go vet ./internal/queue/... ./internal/store/... ./internal/agent/codex/... ./internal/cli/...`
- [ ] `go test -race -count=2 ./internal/queue/...` and full `go test ./...` (re-run the three known flaky tests from the brief before blaming the change).
- [ ] `rocket task log 5716 --kind decision` entries for: `inject_at` before paste; ErrSubmitUnconfirmed+marker-gone → accepted for confirmer recipients; tail-scan instead of a batch interface; `session.restored` as the restore signal.
- [ ] `gh pr create --base main` titled `Queue status accepted with Codex journal confirmation (#5707 T4)`; body lists rules, amendments honoured, test names; ends with the attribution line.
- [ ] `rocket send rocket-codex-inject-codex-orch "<status + PR url>"`.
