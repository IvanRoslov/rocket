# Creative Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Correct Creative recipients and delivery reporting while improving existing role instructions and preserving ordinary Rocket.

**Architecture:** Validate Creative recipients before enqueue. Reuse the shared message waiter with optional recipient display while keeping its existing entry point and ordinary output unchanged. Expand fixed embedded instruction fragments before normal template variables.

**Tech Stack:** Go, Cobra, net/http, SQLite store, embedded Markdown.

**Spec:** docs/superpowers/specs/2026-11-05-creative-routing-design.md

## Global Constraints

- Rocket only; one PR; no live changes, resends, manual statuses or paid calls.
- Ordinary send contract unchanged. No queue/proof changes or general prompt framework.
- Accepted is nonterminal; delivered is confirmed; failed preserves reason.
- Preserve contextual metadata, override behavior and Creative lifecycle boundaries.

## Review Focus

- Omitted and explicit self-target: reject both before insertion and wake (task 1).
- Worker default reply and explicit worker target retain versions and scope (task 1).
- Accepted must not stop polling; failed must return error and reason (task 2).
- JSON terminal result includes actual recipient; ordinary JSON/output unchanged (task 2).
- Shared fragments validate nested variables and keep home overrides/main rendering intact (task 3).

### Task 1: Creative API recipient guard

**Files:** internal/api/creative_agents.go, internal/api/creative_agents_test.go
**Interfaces:** consumes existing Creative binding/auth helpers; produces same endpoint response with self_send 400 on self-recipient.

- [x] Write failing TestCreativeMessageRecipients using real test store: omitted/explicit self target returns 400 with --to hint; no added messages or queued event; valid worker reply defaults to orchestrator and explicit send targets worker with exact metadata.
- [x] Run go test ./internal/api -run TestCreativeMessageRecipients -count=1; expected failure: self-send accepted.
- [x] Add caller comparison after recipient resolution and before GetSession/AddCreativeMessage.
- [x] Run go test ./internal/api -run Creative -count=1; expected PASS.
- [x] Commit API fix and tests.

### Task 2: Reuse normal send wait/status

**Files:** internal/cli/creative_agents.go, internal/cli/creative_agents_test.go, internal/cli/send.go, internal/cli/send_test.go
**Interfaces:** existing waitForMessage(cmd,c,id) remains unchanged entry point; recipient-aware helper shares the same loop/status handling.

- [x] Add failing Creative send tests for --to/context payload and actual response recipient; --wait queued→accepted→delivered and queued→failed; JSON terminal result recipient/ID/status; POST failure performs no polling.
- [x] Run go test ./internal/cli -run TestCreative -count=1; expected missing --wait failure.
- [x] Implement Creative send response handling and --wait via shared waiter; optional recipient affects only Creative labels/JSON, no new status logic.
- [x] Run go test ./internal/cli -count=1; expected PASS including existing ordinary send tests.
- [x] Commit CLI change and tests.

### Task 3: Shared role instructions

**Files:** internal/prompts/prompts.go, internal/prompts/creative_test.go, internal/prompts/templates/creative-*.md, internal/prompts/templates/orchestrator.md, docs/prompts/orchestrator.md, embedded fragments.
**Interfaces:** Render(home,name,vars) unchanged; fixed fragment expansion before variable validation, no recursive/user include language.

- [x] Add failing rendered prompt tests for explicit role addressing, shared rules resolving variables, overrides and unresolved variables; characterize ordinary orchestrator rendering against pre-extraction text.
- [x] Run go test ./internal/prompts -count=1; expected missing addressing/fragment behavior failure.
- [x] Consolidate Creative common text, improve instructions and extract identical ordinary transport wording; sync ordinary reference doc without changing rendered behavior.
- [x] Run go test ./internal/prompts -count=1; expected PASS.
- [x] Commit prompt changes and tests.

### Task 4: Verify and deliver

**Files:** same branch and PR metadata only.
**Interfaces:** consumes all tasks; produces one reviewed PR.

- [x] Run go test ./... and go vet ./... and go build ./cmd/...; expected PASS, report any environmental failures by name.
- [x] Review whole branch against scope/ordinary regression and fix acceptance findings with TDD.
- [x] Push feature/creative-work/routing-prompts and open PR referencing creative-work, with concrete behavior and validation.

## Execution record

Tasks 1–3 passed their targeted RED→GREEN checks. Independent whole-branch review found one acceptance failure: a worker command explicitly targeting its original parent broke after orchestrator replacement. `TestCreativeWorkerReportFollowsReplacement` follows the rendered instruction through the real API: RED with recipient_terminal, GREEN when worker reports use the existing current-run default. Explicit orchestrator-to-worker addressing remains required.

Final verification: `go test ./...`, `go vet ./...`, `go build ./cmd/...`, ordinary web `npm test -- --run` (62 files, 722 tests), Creative `npm test` (7 files, 40 tests), and `git diff --check` passed. The first concurrent Go/vet/build run hit the existing 3-second deadline in `TestHookExtractsOnlyBoundedMetadata/valid`; that exact test passed in isolation and both subsequent full Go runs passed. No unrelated hook change.

Review rulings (no deferred minors):

- Queue/proof correctness stays sibling-owned; existing server status is authoritative. If that sibling leaves a defect, terminal reporting reflects it; duplicating proof logic here would violate scope.
- Wait cancellation/timeouts retain ordinary behavior. A user can still wait indefinitely while the existing status remains nonterminal; changing that policy is outside this recipient/output correction.
- Malformed successful server responses retain the existing server-contract assumption. A broken server response could produce incorrect output or polling; speculative hardening was not included.
- Provider readiness implementation stays sibling-owned; prompts describe uncertainty and configuration presence without assuming new fields. Users must read the tool's documented setup until that contract ships.

All checks use synthetic fixtures/local builds. No live session/config changes, old-message resends, manual status writes or paid calls.

PR: https://github.com/IvanRoslov/rocket/pull/199 (awaiting orchestrator review/merge).
