# Creative conversation feed core

Deliver task 6061's core in subtask 6062: one run feed and one per stage, with
item tags, immutable messages/proposal content, one-time human card decisions,
replacement links, read cursors, durable orchestrator delivery, CLI and agent
memos. No UI, question migration, review-flow changes or paid execution.

## Approach

Use dedicated SQLite feed entries and feed read cursors, separate from the
session-addressed message transport and old questions. Reusing questions would
retain the thread/attention semantics the spec removes; reusing transport messages
would couple human conversation to a recipient session and delivery status.
A separate feed preserves history across replacement/restoration of orchestrators.
Reuse existing messages/queue solely for self-contained human-event delivery.
The HTTP contract is in docs/creative-agents.md; web refresh uses two-second polling.

## Persistence and concurrency

Store entry scope/author/body/version IDs with card decision and replacement
metadata. New messages never change pipeline state. Conditional transactional
updates ensure decisions/replacements race safely; losing requests return the
current card. New replacement and old-card invalidation commit together.
Read markers use monotonic entry IDs per run/stage and only count orchestrator
entries. Marking displayed entries read cannot consume concurrent later writes.
Human mutations record durable delivery work in the same transaction; an active
or restored orchestrator receives it via the existing queue, with enough context
and a ready-to-run reply command. Feeds are readable independently of delivery.

## Agent surface

Add creative agent feed read/post/propose/card commands with scope/version flags
and examples. Feed writes enforce current orchestrator identity, not merely role.
Refuse all new creative questions with role-appropriate command hints, preserving
old question reads/replies/answers and all agent-to-agent messages.
Rewrite shared creative rules and role templates and include a short memo in
launch/restore first-message context, covering commands, card replacement and
button-only consent, review-only stage approval and worker routing.

## Verification

TDD store/API/client/CLI and prompt behavior; tests cover ordering/filtering,
permissions, immutable versions, atomic decisions/replacement, unread race safety,
offline persistence/delivery commands and launch/restore memo. Run make test and
an isolated real-orchestrator scenario walkthrough without paid calls, attach
transcript to the single PR. Never restart/install the operator's daemon/binary.
