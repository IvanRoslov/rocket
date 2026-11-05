# Creative Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

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

- [ ] Write failing TestCreativeMessageRecipients using real test store: omitted/explicit self target returns 400 with --to hint; no added messages or queued event; valid worker reply defaults to orchestrator and explicit send targets worker with exact metadata.
- [ ] Run go test ./internal/api -run TestCreativeMessageRecipients -count=1; expected failure: self-send accepted.
- [ ] Add caller comparison after recipient resolution and before GetSession/AddCreativeMessage.
- [ ] Run go test ./internal/api -run Creative -count=1; expected PASS.
- [ ] Commit API fix and tests.

### Task 2: Reuse normal send wait/status

**Files:** internal/cli/creative_agents.go, internal/cli/creative_agents_test.go, internal/cli/send.go, internal/cli/send_test.go
**Interfaces:** existing waitForMessage(cmd,c,id) remains unchanged entry point; recipient-aware helper shares the same loop/status handling.

- [ ] Add failing Creative send tests for --to/context payload and actual response recipient; --wait queued→accepted→delivered and queued→failed; JSON terminal result recipient/ID/status; POST failure performs no polling.
- [ ] Run go test ./internal/cli -run TestCreative -count=1; expected missing --wait failure.
- [ ] Implement Creative send response handling and --wait via shared waiter; optional recipient affects only Creative labels/JSON, no new status logic.
- [ ] Run go test ./internal/cli -count=1; expected PASS including existing ordinary send tests.
- [ ] Commit CLI change and tests.

### Task 3: Shared role instructions

**Files:** internal/prompts/prompts.go, internal/prompts/creative_test.go, internal/prompts/templates/creative-*.md, internal/prompts/templates/orchestrator.md, docs/prompts/orchestrator.md, embedded fragments.
**Interfaces:** Render(home,name,vars) unchanged; fixed fragment expansion before variable validation, no recursive/user include language.

- [ ] Add failing rendered prompt tests for explicit role addressing, shared rules resolving variables, overrides and unresolved variables; characterize ordinary orchestrator rendering against pre-extraction text.
- [ ] Run go test ./internal/prompts -count=1; expected missing addressing/fragment behavior failure.
- [ ] Consolidate Creative common text, improve instructions and extract identical ordinary transport wording; sync ordinary reference doc without changing rendered behavior.
- [ ] Run go test ./internal/prompts -count=1; expected PASS.
- [ ] Commit prompt changes and tests.

### Task 4: Verify and deliver

**Files:** same branch and PR metadata only.
**Interfaces:** consumes all tasks; produces one reviewed PR.

- [ ] Run go test ./... and go vet ./... and go build ./cmd/...; expected PASS, report any environmental failures by name.
- [ ] Review whole branch against scope/ordinary regression and fix acceptance findings with TDD.
- [ ] Push feature/creative-work/routing-prompts and open PR referencing creative-work, with concrete behavior and validation.
