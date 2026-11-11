# Global Creative overview — release 2

Task 6076, parent 6073, gate 332 spec v2 / plan v2. Build on release 1 commit 4f3cb50.

## Intent and constraints

Replace the two native top selectors with a global overview so works can be found across projects without choosing a project first. Creative only. Preserve release 1 final acceptance, read-only archives, stage/item workspaces and existing unread semantics. No search, quick switcher, extra notification semantics, deployment, restart or real-work acceptance.

## Approach

Use the existing unfiltered GET /v1/creative/runs (the store already supports an empty project filter), existing projects catalog, and existing per-run useUnread queries. Poll the global run list and visible cards' unread queries at the existing two-second cadence. This avoids a new backend aggregate contract and preserves cache invalidation when a conversation is read. An aggregate API could reduce requests for very large catalogs but adds unnecessary backend surface for this release; project-by-project fetching adds avoidable partial loading/error states.

## Navigation and presentation

The default /creative/ shows Active work cards with work name, project name, current stage/state and positive unread count. Completed is a separate URL-backed view (/creative/?view=completed), containing only closure_state=accepted. All other states, including derived completed and failed/incomplete closing, stay Active. Cards share the existing design system. Loading, list errors, known empty lists, and unread unavailable/loading are distinguishable; unread errors never disable a card or show a fabricated zero. Overview never mounts a conversation/read marker.

A run's existing query or /creative/runs/:id deep link remains supported. Its authoritative project_id supplies header context even when a stale project query disagrees. The header shows project/work and a prominent All works action. All works, Completed, New work and Pipelines use history-backed view state; popstate reconstructs the view and selection, and stage/item navigation retains existing query links. Invalid work/stage/item errors remain explicit.

New work includes a required Project select. Opening it from the overview starts blank, including legacy project-only URLs; opening it from a run initializes that run's project. Creation navigates using the returned run.project_id. Pipelines and Development remain reachable, including when no projects exist.

## Validation

Extend App integration tests with same-name works in different projects; global queries; Active/Completed membership; delayed/error/empty lists; URL reload/popstate; stale project deep links; explicit creation and contextual defaults. Exercise live unread polling/error/recovery and actual feed-read invalidation with MSW. Preserve workspace and acceptance regression coverage while updating obsolete selector interactions. Run all Creative tests/build and repository checks. Add an isolated Playwright browser scenario intercepting every API request for real navigation/back/forward/reload, unread, creation, archive and responsive screenshots. No live daemon is involved.
