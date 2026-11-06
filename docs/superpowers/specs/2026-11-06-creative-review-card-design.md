# Unified Creative review card

Task 6013, creative-work Gate302 deliverable 2.

Render one review card between WorkDescription and Workspace tabs for the selected stage/item. It uses the existing status, review_revision and artifact_version_ids, including already pending reviews. Resolve each submitted version directly through the existing immutable version endpoint, then artifact metadata for meaningful labels (including descendant item names). Clicking a submitted result opens that exact version using the existing Media component and isolated HTML view link. Browsing artifact history never changes the review set.

The card alone offers Approve and Return for changes. Return expands a labelled What to fix field, requires trimmed nonblank text, and sends it as the existing reason field with the displayed review revision. Preserve the draft on request failure; disable both decisions during submission, guard rapid duplicate clicks, and suppress actions for a successfully decided revision until refreshed server state arrives. Scope/revision changes reset obsolete forms. Completed work and returned/withdrawn work show clear current state, without pending-review actions; retain Start and Reopen controls. Remove human Submit and duplicate decision controls from Artifacts.

Enforce reject reason in the sole store transition path and CLI validation, preserving human authorization, revision checks and withdrawal rules. Include trimmed reason in the existing persisted stage/item action event and existing orchestrator notification payload, which already includes revision and exact version IDs. Keep delivery post-commit and existing delivery failure semantics; no new queue or audit subsystem.

Update Creative agent instructions: publish then submit exact versions; ordinary questions are discussion, never a second approval gate or a transition inferred from chat. Keep automatic stages, stage/item approval modes, main Rocket, historical artifacts and transport unchanged.

## Approaches

Chosen implementation: independently resolve the immutable review snapshot in the card. It stays usable on every tab and is unaffected by browsing history. Keeping decisions coupled to Artifacts' currently displayed versions would retain the split flow; a new review entity or transport would duplicate existing state and is excluded by the brief.

## Verification

Targeted UI tests cover pending reviews above tabs, exact links including descendant results, both decisions, required feedback, preserved failure drafts, duplicate clicks, revision/scope changes, withdrawal/resubmission and auto mode. Store/API/CLI tests cover blank rejection, unchanged state on failures, authorization, stale revisions, persisted feedback and queued delivery. Prompt regressions verify the single formal submission flow. Run Creative frontend build and relevant Go tests/build. Synthetic data only; no running service/configuration changes.

## Coordination

Task 6012 owns Preview import/tab/removal hunks in App and related tests. Rebase on its first PR before finalizing App integration; preserve HTML viewing.
