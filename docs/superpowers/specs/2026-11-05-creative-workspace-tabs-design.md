# Creative central workspace tabs

Implements approved creative-work spec v6 / plan v5 deliverable 1 (gate 294).

Keep the process rail and replace the three-column workspace with a rail plus a broad central workspace. Use the existing shared Rocket Tabs component for Questions & discussion (default), Artifacts, and Landing preview. Keep discussion and artifacts mounted while switching tabs, preserving drafts, selected versions, and the existing Artifacts onShown contract. Preview retains its existing lifecycle.

Keep scope selection and discussion bindings in the existing Workspace owner. Tab changes do not modify URL scope, version selection, or draft context. Only the visible Artifacts tab enables result submit/approval/rejection; Show submitted versions navigates there and explicitly restores the submitted set. Existing review revision checks and actions remain unchanged.

Compact work metadata into a heading/progress line and collapsed work-details disclosure. Keep full descriptions, expected artifacts, progress notes, item management, session cards and terminal/chat/attach actions accessible through existing components. Run-level brief is similarly collapsed. Discussion becomes central without changing its questions, messages, composer or agent behavior.

Approaches: retaining a right discussion column cannot deliver the required width; replacing shared tabs or introducing a chat workflow exceeds the brief. Recomposition with shared Tabs is the bounded approach. No new dependencies, mobile work, backend behavior, live restart, deployment, real runs or paid calls.

Ownership: App.tsx, RunWorkspace.tsx, composition CSS and workspace tests here; sibling artifact-view owns Artifacts.tsx and its rendering/API tests. Preserve its interface. Verify default/shared tabs, context and version round trips, draft bindings, unseen-review gating, existing session actions, targeted suites and production build. Human evaluates real visual cases.
