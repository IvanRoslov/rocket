# Global creative pipeline catalog (5994)

Spec v5 / plan v4, gate 293 corrects templates into a shared reusable catalog. Runs remain project-owned and retain their snapshots. No live database changes, daemon restart, paid calls, demo projects, new permissions framework or unrelated features.

## Approach and preservation

Remove only creative_pipelines.project_id with a new normal upgrade migration (0031). The current embedded modernc SQLite supports DROP COLUMN; the removed column has an inline outgoing project foreign key and no index/constraint outside that column. Incoming creative_runs.pipeline_id references the unchanged primary key. This avoids rebuilding/renaming the referenced parent table. The migration runner already executes each migration transactionally on a dedicated connection with foreign keys disabled and restores enforcement. Test the real pre-0031 schema and populated graph through Open, foreign_key_check and reopen.

Keep every template row, ID, name (including duplicates), ordered stage JSON, timestamp and autoincrement sequence. Do not rewrite runs or their stages, items, progress, reviews, artifact versions, sessions or discussion. Existing runs keep the old pipeline_name and definition snapshots; future runs snapshot the current shared template under their own valid project. Rebuilding the parent table would copy more state and introduce avoidable foreign-key/sequence risk; keeping a nullable ownership column would preserve the false ownership model.

## Contracts and authorization

Store pipeline input/output and list signatures have no project. API list always returns the catalog. Accept and ignore deprecated project_id request/query hints through API-only compatibility fields, never store or emit them. Keep CLI --project accepted but deprecated/ignored for pipeline listing; run listing still filters. Old pipeline JSON files remain usable. Global template reads/writes use existing transport trust and caller identity validation; project restrictions stay on run/agent/material/session operations. Run creation validates its project independently and removes only the template/project equality check.

## UI and documentation

Use one project-independent pipeline query/cache. Pipeline editor has no project prop or body field, remains available with no projects, and retains its selection/draft when the project chooser changes. Run/project selection still resets on a project change; new runs still require a project. Keep existing components/styles and stage ordering controls. Update current API/CLI/dashboard docs and mark the earlier project-bound design as superseded.

## Verification

Regression tests cover migration with equal-name templates and an active run graph, global projectless template CRUD and deprecated hints, one template creating runs in two projects, template edits preserving existing snapshots, invalid run projects and existing cross-project agent/material/session denials. Component tests cover projectless creation/editing, stage ordering, and unchanged catalog across project switches. Run targeted Go package tests, creative frontend tests/build, and relevant Go build/vet checks. No production execution.
