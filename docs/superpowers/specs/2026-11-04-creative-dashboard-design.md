# Creative workspace — 5991 / creative-work

A separately built React application in `creative-web/`, served at `/creative/` by the existing daemon. It shares Rocket cookies, project/model APIs and communication infrastructure. The development app stays at `/`; login returns across the application boundary with full browser navigation. No mobile UI, paid calls, daemon restart or demo projects.

## Approaches

Use a three-column creative workspace: a narrow process rail, a large result viewer, and discussion alongside it. This keeps shown artifact versions and their discussion visible together. A sequential full-page wizard would hide earlier results during rework; a kanban would emphasize task administration instead of reviewing creative output. The workspace approach is recommended within the approved scope.

## Product layout

- Top bar: Rocket Creative identity, existing project selector, run selector, New work, Pipelines. Deep links retain run/stage/item selection.
- Left rail: run brief, saved ordered stages, status/progress and concrete repeated items. Add pending items with name, work description and expected artifacts. Stage/item selection changes both results and discussion context. Active runs always show saved definitions.
- Center: selected stage/item work and expected artifacts; explicit start, submit, approve, reject and reopen actions as allowed by core. Review shows the submitted version IDs and revision, and approval sends that binding. Underneath, source upload and named result/source cards with current/version labels and retained history. Text, images, audio/video and downloads use immutable content URLs; unsupported media has a download fallback.
- Right: creative orchestrator launch/restore with existing model profiles; contextual messages and question threads, including explicit Go replies. The composer shows its run/stage/item and shown artifact-version context. Changing context does not silently retarget a draft. Agent call/cost proposals remain ordinary discussion requiring a deliberate human Go.
- Landing preview: an interactive center-panel view with backend availability/status, embedded preview and separate-tab link. Unavailable states explain the backend response.
- Pipelines: list and ordered editable stage cards with name, plain-text work, expected artifacts, repeated items, and automatic/stage/item approval; add/remove/reorder controls. Save edits templates only; New work captures name, brief and chosen pipeline.

## Implementation boundaries

Reuse the existing web toolchain/dependencies through separate Vite/TypeScript entry and output. Add a distinct embedded Go package and `/creative/` static registration, plus build/test targets. Reuse small API/auth primitives; never import the old app shell/router. Use sibling-owned `/v1/creative` snake_case contracts rather than parallel state. Poll queries for authoritative state; mutations surface errors and refresh dependent data. Keep drafts stable, disable in-flight duplicate actions, and show loading, empty, unavailable and failed states explicitly. Browser cookies authenticate media and API requests.

Verify focused component/API tests for pipeline snapshot editing, context/version-bound actions/messages, artifact history/media, preview states and auth return; Go static-route tests for distinct apps/deep links/assets and old dashboard preservation. Run both builds and existing relevant tests without launching the live daemon.

## Accepted integration correction

Whole-stage review and discussion include explicitly shown immutable versions from the same stage’s concrete items, retaining item identity. Item and run scopes remain exact. The viewer aggregates exact item lists; it never republishes copies or substitutes current pointers. A submitted set can include more than one version of a named artifact; all such versions display as separate immutable cards.
