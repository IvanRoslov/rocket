# Creative stage lifecycle (Gate300 deliverable1)

After a committed human approve/reject, enqueue one contextual message for the run orchestrator using AddCreativeMessage and the existing queue wake/delivery path. Use the transaction-returned review revision and immutable version set, server timestamp, and existing unattributed/no_session_header attribution; never invent a human identity. Record queued/message ID or failure details on the existing action event and log failures. Failure cannot undo or disguise the committed transition. No historical replay or second transport.

Withdraw extends the stage/item action endpoint and CLI with required review_revision and nonblank reason. A separate authenticated store entry accepts a caller session ID and checks current run orchestrator identity, binding, kind and live state under the existing creativeWrite transaction lock. The legacy boolean-human entry cannot withdraw. Only exact awaiting_approval becomes in_progress; versions and revision remain, subsequent submit increments revision. Approve and withdraw serialize through the same lock.

A successful withdrawal records reason, actor, revision and versions in the existing action event and creates a contextual FYI discussion (already answered, no approval requested), with human/orchestrator participants. Discussion creation failure is observable without reversing the withdrawal. Reuse the existing FYI semantics, allowing AddCreativeQuestion to preserve its default decision behavior.

## Approach and boundaries

The existing synchronous post-commit action publisher is the bounded integration point. An asynchronous event subscriber would need replay/deduplication and is outside this brief. Transactional delivery would incorrectly couple decision success to notification availability. Existing message rows remain the authority for queued versus delivered/failed status.

Owned files: store creative_progress.go/creative_types.go/creative_sessions.go and tests; API creative.go plus a focused lifecycle helper/test; CLI creative.go/tests; API documentation. Sibling owns close command and shared prompt guidance; no prompt edits here. No main Rocket lifecycle changes, migrations, live config/session actions, paid calls, deployment or replay.

## Evidence and acceptance

Current base includes #197/#199. ApplyCreativeAction obtains SQLite write lock before reading state and returns committed snapshot. publishCreativeAction uses that snapshot and existing actor attribution. AddCreativeMessage persists queued rows with discussion bindings; wakeCreativeMessage signals existing delivery. QuestionTypeFYI already exists for informational notices.

Synthetic tests cover stage/item decisions, exact metadata, stale/terminal/cross-run/worker/human withdrawal denial, resubmission, concurrent approve/withdraw, absent orchestrator and persistence failures, no notifications for failed/nondecision actions, and existing delivery regressions.
