# Creative stage action safety (Gate299 deliverable3)

Confirm submission of the exact results currently shown, prevent unintended rapid/state-changing clicks, and record successful stage/item transitions through Rocket's existing event bus. The historical cause is unknown; double clicking is only a hypothesis.

## Approach

Use a local confirmation panel in ReviewActions, listing the stage/item name and result artifact names, version numbers and immutable version IDs (including an explicit empty set). Bind confirmation to scope, status, review revision and shown result IDs; invalidate it when these change. Send only that confirmed set. Cancel sends nothing. Explicitly confirming the same versions after rejection remains supported.

Guard handlers synchronously while an action plus query invalidation is pending, and ignore repeated clicks in a native click sequence (detail > 1). Existing action keys keep replacement buttons distinct. Apply guards to every stage/item action and to opening/accepting confirmation. Keyboard activation remains supported.

Publish creative.stage_action / creative.item_action only after a successful ApplyCreativeAction, using the returned committed snapshot for status, review revision and artifact version IDs. Include run/stage/item/project IDs, action, and caller attribution. Registered callers use the resolved session ID; absent session header is recorded as an unattributed human API caller under existing Rocket authorization semantics, with no invented personal identity. Event TS comes from the bus. The existing bus persists and broadcasts events; its existing logged persistence-failure behavior remains unchanged.

## Alternatives and boundaries

A generic debounce alone would not confirm the shown versions; a new event database or dashboard exceeds scope. An inline confirmation plus existing bus is the smallest mechanism. No transport changes, extra generation gates, downstream resets, replay, historical backfill, live config/restart, paid calls or real-run mutations. Development workflow and Creative approval semantics remain unchanged. Human owns real-case visual evaluation.

## Verification

Synthetic UI tests cover exact older shown versions, cancel, empty sets, changed scope/versions/state while confirming, rapid repeated clicks, state-button replacement, pending actions, and deliberate same-version resubmission. API tests cover all five actions at stage/item scopes, committed review context, server time and resolved/unknown attribution, and no events on denied/conflicting actions. Run relevant Go tests and both frontend build/test suites.
