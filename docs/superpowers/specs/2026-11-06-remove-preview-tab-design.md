# Remove Creative Landing preview tab

Gate302 deliverable1, creative-work task 6012. Accepted by creative-work-orch.

Remove the unused Landing preview tab from Creative Workspace. Keep Questions & discussion as the default and Artifacts as the other tab. Preserve drafts, selected immutable artifact versions, stage/item navigation and the session rail.

## Approach

Delete only the Preview import, tab choice/state member and conditional render in App.tsx. Hiding the tab would leave an unsupported state/render path; deleting the preview subsystem would exceed the brief. Retain the standalone Preview component/tests, listener/config, HTML view access isolation, media previews and published versions/history. Ordinary Rocket is unchanged. No migration, redesign, live run/session/config changes, restarts or paid calls.

Update App integration tests to exercise the two remaining tabs. Existing Artifacts tests verify HTML opening links, isolated new tabs, media and history. Coordinate App composition with review-card: it rebases after this independent first PR.
