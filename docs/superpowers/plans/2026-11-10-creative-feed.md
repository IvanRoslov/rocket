# Creative Feed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver task 6062's durable per-run/stage human-orchestrator feeds.
**Architecture:** Separate SQLite feed entries/cursors and durable delivery outbox;
reuse session message queue. Preserve old question archive and worker transport.
**Tech Stack:** Go, SQLite, Cobra, existing Rocket HTTP/session/queue packages.
**Spec:** ../specs/2026-11-10-creative-feed-design.md; API in docs/creative-agents.md.

## Global Constraints

- No web UI, paid calls, live daemon restart or binary installation.
- Only current run orchestrator writes; human decides once; feeds never approve.
- One branch and PR; preserve immutable versions and old questions.

## Review Focus

- Concurrent decision/replacement: exactly one wins; loser gets current entry.
- Offline/replaced orchestrator: feed events survive and route on launch/restore.
- Mark-read races: later arrivals stay unread; stale markers never regress.
- Wrong scope: foreign runs/stages/items/versions cannot cross permissions.
- Restored agent: commands and button-only consent memo appear in standard context.

### Task 1: Store and durable delivery

**Files:** new internal/store/creative_feed.go, creative_feed_test.go,
migrations/0033_creative_feed.sql.
**Interfaces:** CreativeFeedEntry/Card/Unread structs; AddCreativeFeedEntry,
ListCreativeFeed, GetCreativeFeedCard, DecideCreativeFeedCard,
CreativeFeedUnread, MarkCreativeFeedRead, QueueCreativeFeedDeliveries.

- [ ] Write store tests for ordering, item tags/filter, version scope, one-time
  decisions/replacement (including races), unread/reset/stale cursor, offline
  delivery and exact reply command.
- [ ] Run `go test ./internal/store -run CreativeFeed -count=1`; observe missing behavior.
- [ ] Implement schema and transactional conditional card updates, cursor and
  outbox, enqueue events transactionally with entries/decisions.
- [ ] Rerun focused store tests and commit.

### Task 2: HTTP and session integration

**Files:** new internal/api/creative_feed.go and tests; modify creative_agents.go,
creative.go route registration, internal/session/creative.go and tests.
**Interfaces:** Exact published HTTP contract; QueueCreativeFeedDeliveries(runID)
called on mutation and launch/restore; no state transition from feed writes.

- [ ] Write API tests for contract, own orchestrator/human/worker/foreign access,
  empty body, card conflicts, unread and retired ask; session tests for offline
  event recovery after launch/restore.
- [ ] Run `go test ./internal/api ./internal/session -run 'CreativeFeed|Creative.*Question' -count=1`.
- [ ] Implement handlers using store APIs; refuse new questions with commands;
  drain durable deliveries on successful launch/restore and mutation.
- [ ] Run focused tests, update legacy question creation tests to seed archives
  directly instead of creating new questions through retired API, commit.

### Task 3: Agent CLI and memos

**Files:** new internal/cli/creative_feed.go and tests; modify creative_agents.go,
internal/prompts/templates/creative-{rules,orchestrator,worker}.md,
internal/prompts/creative_test.go, internal/session/creative.go, docs/creative-*.md.
**Interfaces:** `rocket creative agent feed RUN [--stage --item]`,
`feed post RUN --body/--file [--version]`, `feed propose RUN [--replaces]`,
`feed card RUN ENTRY`; JSON responses use store types.

- [ ] Add CLI tests for routes/body/file/version/replacement flags and help;
  prompt tests require feed memo and forbidden behavior in launch/restore.
- [ ] Run focused CLI/prompt/session tests to see missing commands/memos.
- [ ] Implement commands and standard-context memo; replace question-based paid
  Go docs/rules; keep legacy archive reply/answer commands and worker sends.
- [ ] Run focused suites and commit.

### Task 4: Verification and delivery

- [ ] Run `make test`; diagnose failures with systematic-debugging and rerun
  affected tests after changes. Run Go vet/build checks appropriate to core.
- [ ] Build isolated binary inside this worktree, start isolated daemon with
  temporary home/socket, launch real creative orchestrator with standard context,
  walk scenarios 1–4 without paid calls; preserve transcript and clean test runtime.
- [ ] Review diff against acceptance and contract, record verification evidence.
- [ ] Push and create single PR referencing creative-messages and tasks 6061/6062;
  send PR URL and completion state to orchestrator. Address review until merged.
