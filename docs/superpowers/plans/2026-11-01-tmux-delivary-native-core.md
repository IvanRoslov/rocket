# Native Delivery Contract and Safe Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the reusable `pi-uds-v1` protocol, persisted native launch/message snapshots, and a queue that never falls back to tmux or blindly re-sends uncertain native messages, while preserving legacy behavior.

**Architecture:** A small dependency-free `nativeinput` package writes one bounded JSONL request and validates one bounded response. Session launch data supplies the native target; the queue snapshots transport and send time before each send, maps accepted/unknown to journal confirmation, and recovers native `delivering` rows as `accepted` rather than queued. Native confirmation is separate from legacy visual/requeue rules.

**Tech Stack:** Go 1.25, SQLite migrations, Unix domain sockets, existing Rocket queue/store/session runtime.

**Spec:** `docs/superpowers/specs/2026-10-31-tmux-delivary-design.md` at commit `6ff5d39`; shared contract in `docs/superpowers/plans/2026-10-31-tmux-delivary.md` Task 1.

## Global Constraints

- Implement Task 1 only: no Pi extension and no production Codex/Kimi/Pi launch changes.
- Protocol version 1; JSON fields `v`, `session_id`, `instance_id`, `message_id`, `text`; frames at most 1 MiB.
- Native `pi-uds-v1` never uses tmux Inject, SendKeys, Capture, or CaptureEscaped to decide delivery and never falls back to legacy when its endpoint is unavailable.
- Persist transport and `inject_at` before every send. Only `NotSent` may use bounded retry; `Accepted` and `Unknown` become `accepted` and require journal confirmation.
- Native `delivering` recovery becomes `accepted`; native terminal failure first checks the journal, including after recipient exit/restore. Disabled `QueueTimeout` waits while the recipient remains live.
- Empty transport means legacy. Legacy socket/held behavior, retry limits, expiry, visual checks, and interactive answers stay unchanged.

## Review Focus

- A response for another session, launch, or message must never prove acceptance: `TestSendRejectsWrongResponseIdentity`.
- A peer that closes after possibly reading the request must yield `Unknown`, not a retryable error: `TestSendDisconnectAfterWriteUnknown`.
- Two equal bodies with distinct IDs need two sends and two transcript records: `TestNativeDuplicateTextUsesDistinctMessageIDs`.
- A native crash between claim and send-result persistence must not cause a second send: `TestNativeUnknownDoesNotRetryAfterRecover`.
- A recipient that exited or restored may still have recorded the message: `TestNativeTerminalFailureChecksJournalFirst`.
- A provider failure must not silently launch legacy argv/env: `TestNativeLaunchProviderErrorDoesNotCreateRuntime`.

---

### Task 1: Native JSONL protocol and client

**Files:**
- Create: `internal/nativeinput/protocol.go`
- Create: `internal/nativeinput/client.go`
- Test: `internal/nativeinput/client_test.go`

**Interfaces:**
- Consumes: standard library `context`, `net`, `encoding/json`, bounded I/O.
- Produces: `Target{SessionID, InstanceID, SocketPath string}`, `Request{Version int, SessionID, InstanceID string, MessageID int64, Text string}`, `Outcome` (`NotSent`, `Accepted`, `Unknown`), and `Send(ctx context.Context, target Target, messageID int64, text string) (Outcome, error)`. Response JSON uses the request identity fields plus `outcome`; accepted outcomes are `accepted` and `not_sent`.

- [ ] **Step 1: Write failing client tests** for accepted/not_sent responses, dial refusal, disconnect after write, request/response over 1 MiB, malformed response, and wrong response identity. Assert the exact outcome and whether an error is returned.
- [ ] **Step 2: Run RED** with `go test ./internal/nativeinput`; expect package/test failure because the contract is absent.
- [ ] **Step 3: Implement protocol and client.** Validate target/request before dialing; marshal and size-check before writing; use a UDS with context deadlines; read at most 1 MiB plus one byte; require newline-terminated, single JSON response with matching v/session/instance/message. Map any post-write uncertainty to `Unknown`.
- [ ] **Step 4: Run GREEN** with `go test ./internal/nativeinput` and `go test -race ./internal/nativeinput`.
- [ ] **Step 5: Commit** `feat(nativeinput): add bounded UDS delivery client`.

### Task 2: Persist native session and message snapshots

**Files:**
- Create: `internal/store/migrations/0026_native_delivery.sql`
- Modify: `internal/store/sessions.go`, `internal/store/messages.go`
- Test: `internal/store/native_delivery_test.go`, `internal/store/messages_test.go`

**Interfaces:**
- Consumes: existing migration, scan, Add/Update, and accepted-CAS patterns.
- Produces: session `DeliveryTransport`, `DeliveryInstance`, `DeliveryEndpoint` strings; message `DeliveryTransport` string; `UpdateSessionDelivery`; `ClaimNativeMessage(id, transport, injectAt)` for atomic queued→delivering snapshot; `SetMessageNativeDelivery` for subsequent attempts; native-only delivering recovery to accepted.

- [ ] **Step 1: Write failing tests** `TestLegacySessionMigrationDefaults`, snapshot round trips, atomic native claim before send, and `TestResetDeliveringNativeBecomesAccepted` while legacy still becomes queued. Build a pre-0026 database from embedded migrations to prove old rows default to legacy/empty transport.
- [ ] **Step 2: Run RED** with `go test ./internal/store -run 'Native|LegacySessionMigrationDefaults|ResetDelivering'`; expect missing fields/methods/behavior.
- [ ] **Step 3: Implement migration and store changes.** Add nullable-free string columns with `''` defaults, update all session/message SELECT lists and scans, and make the native claim a single UPDATE carrying status, transport, and inject_at.
- [ ] **Step 4: Run GREEN** with `go test ./internal/store`.
- [ ] **Step 5: Commit** `feat(store): persist native delivery snapshots`.

### Task 3: Native queue send, recovery, and confirmation

**Files:**
- Create: `internal/queue/native.go`, `internal/queue/native_test.go`
- Modify: `internal/queue/queue.go`, `internal/queue/confirm.go`, `internal/queue/queue_test.go` (fake counters only)
- Test: native tests plus existing `confirm_test.go` and legacy queue tests

**Interfaces:**
- Consumes: Task 1 `nativeinput.Send`, Task 2 snapshots, existing `prepareText`, `accept`, `DeliveryConfirmer`, queue events, and `QueueTimeout`.
- Produces: `NativeSender` interface with `Send(ctx, target, messageID, text) (nativeinput.Outcome, error)` and `(*Queue).SetNativeSender(sender NativeSender)`.

- [ ] **Step 1: Write failing tests** `TestNativeDeliveryDoesNotTouchTmux`, `TestNativeUnavailableNeverFallsBack`, `TestNativeUnknownDoesNotRetryAfterRecover`, `TestNativeDuplicateTextUsesDistinctMessageIDs`, late confirmation, no confirmation transcript, disabled QueueTimeout, and `TestNativeTerminalFailureChecksJournalFirst`. Include accepted, unknown, NotSent retry/exhaustion, exit/restore, and same-text distinct-ID cases.
- [ ] **Step 2: Run RED** with `go test ./internal/queue -run Native`; expect missing native routing and unsafe recovery.
- [ ] **Step 3: Implement native delivery before visual gates.** Snapshot claim data before each send; map NotSent to existing bounded backoff, Accepted/Unknown to accepted; never invoke legacy fallback or visual probes. Resolve the native confirmer even for terminal recipient states and use it before failing with `native_delivery_unconfirmed`.
- [ ] **Step 4: Separate native confirmation from `giveUpAccepted`.** Native messages wait for journal proof until timeout/exit/restore; they never use acceptedTurnGrace, pane queued checks, or automatic requeue. Keep legacy confirmRecipient behavior unchanged.
- [ ] **Step 5: Run GREEN** with `go test ./internal/queue` and `go test -race ./internal/queue`.
- [ ] **Step 6: Commit** `feat(queue): confirm native delivery without blind retry`.

### Task 4: Optional native launch declaration and daemon wiring

**Files:**
- Modify: `internal/agent/agent.go`, `internal/session/manager.go`, `internal/daemon/daemon.go`
- Test: `internal/session/native_delivery_test.go`, existing session/agent tests

**Interfaces:**
- Consumes: existing `LaunchSpec`, `Agent`, SetupWorkspace/LaunchCommand/Env ordering, and store delivery snapshots.
- Produces: `agent.NativeLaunch{Transport, InstanceID, Endpoint string}`, optional `agent.NativeLaunchProvider` with `PrepareNativeLaunch(spec LaunchSpec) (NativeLaunch, error)`, `LaunchSpec.NativeDelivery`, and daemon `SetNativeSender(nativeinput.SenderFunc(nativeinput.Send))`.

- [ ] **Step 1: Write failing tests** for provider result visible before SetupWorkspace/LaunchCommand/Env, snapshot persisted before runtime Create, provider error preventing runtime creation, and `TestNativeRestoreChangesInstance`. Keep a zero NativeLaunch as legacy; existing fake-agent launch assertions must remain unchanged.
- [ ] **Step 2: Run RED** with `go test ./internal/session -run Native`; expect missing types and launch behavior.
- [ ] **Step 3: Implement the optional interface and apply it in Spawn, SpawnOrchestrator, and Restore.** On provider success set LaunchSpec.NativeDelivery and persist the session snapshot before SetupWorkspace and runtime creation. On provider error stop without launching another transport; without a provider preserve existing behavior/snapshots.
- [ ] **Step 4: Wire the real native sender in daemon setup** without opting any production adapter into native launch.
- [ ] **Step 5: Run GREEN** with `go test ./internal/session ./internal/agent/... ./internal/daemon`.
- [ ] **Step 6: Commit** `feat(session): prepare and persist native launch declarations`.

### Task 5: Verification and delivery

**Files:**
- Verify the complete diff; no Pi extension or Codex/Kimi launch changes.

**Interfaces:**
- Consumes: all preceding tasks and existing regression suites.
- Produces: one PR against `main` in `IvanRoslov/rocket`; no merge before orchestrator instruction.

- [ ] **Step 1: Run targeted verification:** `go test ./internal/nativeinput ./internal/store ./internal/queue ./internal/session ./internal/agent/... ./internal/daemon`.
- [ ] **Step 2: Run full and race verification:** `go test ./...` and `go test -race ./internal/nativeinput ./internal/queue ./internal/session`; run `go vet ./...` and `git diff --check`.
- [ ] **Step 3: Exercise the delivered scenarios through real public behavior:** real temporary UDS integration for protocol, real SQLite migration/claim/recovery, and queue-to-fake-sender end-to-end; verify native zero tmux calls and unchanged legacy tests.
- [ ] **Step 4: Request code review using superpowers:requesting-code-review**, address confirmed findings, and re-run affected checks.
- [ ] **Step 5: Open one PR with `gh --repo IvanRoslov/rocket`**, record head commit, test commands/results, shared response protocol for worker 2, CI availability, and unresolved findings. Report PR URL to `tmux-delivary-orch`; do not merge until instructed.
