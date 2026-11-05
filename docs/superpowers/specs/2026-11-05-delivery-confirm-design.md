# Gate299 delivery confirmation evidence and design

Read-only inspection on 2026-11-05 found messages 81163 and 81165 now terminal `failed/delivery_unconfirmed`, attempts 1. Their immutable journal device/inode still match. Both exact envelopes have a `queue-operation` removal with reason `absorbed_mid_turn`, followed by a `queued_command` attachment with matching command/delivery IDs, session UUID, exact prompt, peer wire ID and sender address, and a rendered system reminder. These records occur before the saved deadlines. No qualifying ordinary user record exists. API-request blobs also contain the envelope but are not independent proof.

The current FindProof accepts only exact incoming user records. Successful Claude deliveries 80758/80760/80761 (ordinary Rocket sessions) and 81176 (same Creative recipient) use that existing user-record path. Their proof is stored in claude_deliveries and claude_consumed_records; messages.delivery_proof_at remains zero even on successful Claude delivery. That column alone cannot diagnose this transport. This issue is separate from session-owner readiness fix #196.

## Approach

Extend only shared FindProof to recognize the observed mid-turn consumption form: a complete post-boundary, same-session `remove/absorbed_mid_turn` record correlated to a subsequent complete `attachment/queued_command` record by nonempty command and delivery IDs. Require exact envelope, peer wire ID and reply address, prompt mode, metadata flag, and exact rendered system-reminder wrapper with its matching reminder ID. Return the attachment UUID and physical offsets through the existing store transaction, proof ownership, ambiguity checks, deadlines and terminal-state CAS. Keep the existing ordinary user-record path intact. Multiple qualifying records remain ambiguous.

Alternatives rejected: treating API blobs, enqueue, socket acceptance or reported reception as proof weakens consumption guarantees; resends or manual terminal rewrites violate at-most-once and recovery boundaries. No second Creative transport, waiter/API changes or live mutations.

## Validation and recovery

Use synthetic RED/GREEN tests for valid correlated consumption; missing/foreign/mismatched/duplicate/incomplete records; ordinary user proof; and queue/store finalization without replay or proof reuse. Run existing claudeinstance, store and queue suites. The fix applies to unresolved deliveries with valid proof inside their saved deadlines. Already failed rows 81163/81165 remain failed: no automatic resurrection, resend or historical database repair. Their historical receipt can be reported separately from the stored terminal state.

Classification: shared finalization parser gap breaks acceptance; assuming delivery_proof_at=0 means no Claude proof is a wrong status interpretation; historical operator recovery tooling is an optional extra and excluded.

## Verification outcome

Synthetic parser and queue/store regressions failed before implementation and passed afterward. Independent review identified interleaved duplicate removal/attachment ambiguity; additional RED/GREEN tests now require rejection across the complete scan. Reply addresses use the existing percent-encoding function, with a synthetic escaped-path regression.

The final `go test ./...`, `go vet ./...`, and focused mid-turn `-race` tests pass. The broader claudeinstance/store/queue race suites also passed before the review corrections. An initial full-suite run hit `TestAnswerQuiz_SecondCallWhileInFlightIs409ThenClearsAfterResolved` (2-second timeout); three isolated repetitions and subsequent full runs passed. No quiz code changed.

A read-only run of the new FindProof against the saved snapshots returns the historical attachment offsets 7069130..7072321 for 81163 and 7104711..7110618 for 81165. The four successful comparison deliveries retain their previously stored proof offsets. These parser results do not alter terminal status or override saved deadlines.
