# Creative worker close

Gate300 deliverable2 of creative-work adds an explicit command for the current
live Creative run orchestrator to stop an own-run worker. The orchestrator accepted
this bounded design, including replacement-orchestrator ownership by run binding
rather than original ParentID.

## Approach

Add `rocket creative agent close RUN WORKER_SESSION`, posting to
`/v1/creative/runs/{run}/workers/{session}/close`. The endpoint verifies that the
caller is the run's current live orchestrator, bound to this run/project, and that
the target is a worker bound to the same run/project with no development task.
Predecessor-parent workers are allowed. Human calls, worker calls, stale
orchestrators, foreign sessions and orchestrator targets are forbidden.

Reuse `Manager.Kill(ctx, id, false)`: no cascade or cleanup. Already-terminal
sessions succeed without changing their state. Return the existing killed status
contract. Keep session/context rows, published artifacts/versions, messages,
questions and workspace files. Do not add automatic closure.

An alias to unrestricted session kill would not enforce Creative ownership;
a new termination mechanism would duplicate existing Rocket lifecycle. A scoped
endpoint delegating to Kill provides authorization while preserving that lifecycle.

## Guidance and boundaries

The orchestrator must semantically verify that completed assignment outputs have
been durably published before close. File existence is not that verification.
After approval, verify publications and explicitly close appropriate stage/item
workers. Stage-lifecycle sibling owns approval notifications and withdrawal;
this task owns close and Creative prompt guidance. Ordinary development prompt
rendering and lifecycle remain unchanged.

## Acceptance

Exercise the real handler/store/manager with fake runtime/workspace only: allowed
own-worker and replacement ownership; forbidden foreign run/project, nonworker,
unbound, stale and worker callers; terminal idempotency; runtime stop with no
workspace cleanup; retained files, artifact versions and discussion history.
Verify CLI path/arguments and existing Rocket session tests. No live session
actions, daemon/config changes, paid calls, mass cleanup or audit UI.
