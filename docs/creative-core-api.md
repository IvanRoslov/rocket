# Creative workflow API and commands

Creative work is independent of development tasks. Templates, runs, stages and items use their own integer ID namespaces. Projects and authentication are shared with the rest of Rocket. No creative operation starts generation, launches an agent, requires a development brainstorming gate, or creates a task/PR.

## Templates and runs

Create a template with `POST /v1/creative/pipelines`, or replace its name and ordered stage array with `PUT /v1/creative/pipelines/{id}`:

```json
{
  "project_id": "my-project",
  "name": "Video",
  "stages": [
    {"name":"Script","description":"Write a script from the brief","expected_artifacts":"Script document","repeated":false,"approval":"stage"},
    {"name":"Scenes","description":"Produce each agreed scene","expected_artifacts":"Scene videos","repeated":true,"approval":"item"}
  ]
}
```

`approval` is required: `auto` completes on submission, `stage` waits for human approval of the whole stage, and `item` waits for human approval of each repeated item before later items progress. `item` requires `repeated:true`. Reordering the array reorders the template. Existing run definitions never change.

`GET /v1/creative/pipelines[?project_id=...]` returns an array. `GET /v1/creative/pipelines/{id}` returns one template.

Create a run with `POST /v1/creative/runs`:

```json
{"project_id":"my-project","pipeline_id":1,"name":"Launch video","brief":"Explain the product in thirty seconds"}
```

`GET /v1/creative/runs[?project_id=...]` returns an array of runs. `GET /v1/creative/runs/{run}` returns the run, copied `pipeline_name`, ordered `stages` and their ordered `items`. Every stage/item has `status`, integer `progress` (0–100), `progress_note`, `review_revision`, and `artifact_version_ids`. Positions are zero-based and IDs remain stable. Run status derives from its stages: pending before work, in_progress during work/between stages, awaiting_approval for the first unfinished stage's review, and completed when all stages complete. Per-item pending approvals remain visible on the item itself.

## Items, progress and approval

Under `/v1/creative/runs/{run}/stages/{stage}`:

| Method/path suffix | Body | Result |
|---|---|---|
| `POST /items` | `{name,description,expected_artifacts}` | Create ordered repeated item, return run (201) |
| `PATCH /items/{item}` | `{name,description,expected_artifacts}` | Edit pending item, return run |
| `DELETE /items/{item}` | None | Delete pending item, return run |
| `PATCH` | `{progress,progress_note}` | Update active stage progress, return run |
| `PATCH /items/{item}/progress` | `{progress,progress_note}` | Update active item progress, return run |
| `POST /actions` | Action below | Transition stage, return run |
| `POST /items/{item}/actions` | Action below | Transition item, return run |

Actions use `{ "action": "start|submit|approve|reject|reopen" }`. Start transitions pending work to in_progress. Only the first unfinished stage can progress. An item needs its stage in progress; in per-item mode all preceding items must be completed before any later item progression. Repeated stages need at least one item and all items completed before submission.

Submit may carry `artifact_version_ids:[10,11]`. These immutable version IDs become the review snapshot and `review_revision` increments. Auto work completes; human-gated work becomes awaiting_approval. Stage review accepts versions belonging to that run/stage, including its items. Item review accepts only that exact item. Other stages/runs never match. Empty lists allow text-only work; nonempty lists fail closed until the assets migration exists. Current artifact pointer changes cannot alter review snapshots.

Approve/reject require `review_revision` matching the displayed pending review. Only human callers may approve, reject or reopen. Approve completes; reject returns to in_progress. Reopen is explicit agreed rework of completed work: it returns the target (and its enclosing stage for an item) to in_progress, preserves existing version references/materials, and leaves downstream states/results untouched. Resubmission gets a new revision and follows normal approval rules again. Reopened predecessors block later progression without resetting their results. There is no writable completion status or alternate approval endpoint.

Items can be added to pending/active repeated stages. Only pending items can be edited/deleted; stage review/completion freezes the collection. Item definitions are full replacements, so omitted description/expected_artifacts become empty.

## Context and orchestrator integration

`GET /v1/creative/runs/{run}/context[?stage_id=...&item_id=...]` resolves `{run_id,stage_id?,item_id?,project_id,orchestrator_session_id}`. Item requires stage; ancestry is always checked. Core tables are `creative_runs`, `creative_stages`, and `creative_items`; siblings may reference their immutable IDs.

`PUT /v1/creative/runs/{run}/orchestrator` with `{ "session_id":"creative-orch" }` associates an existing same-project orchestrator/agent session without a development task. Launch/restore and contextual discussion belong to the creative agents module. Existing send/inbox/chat infrastructure stays available.

Public Go methods/types are in `internal/store/creative*.go`. `ResolveCreativeContext(runID,stageID,itemID)` resolves ownership. `GetCreativeRun`, `GetCreativeStage`, `GetCreativeItem` retrieve instances. `SetCreativeRunOrchestrator` stores association. `CreateCreativeItem` returns the item; HTTP item mutations return the full run.

Transport authentication is unchanged: Unix socket trust/session identity and authenticated TCP devices. Unknown session headers fail with 401. Mutations require a human or same-project session; approval/reject/reopen additionally require a human. JSON is strict and bounded to 1 MiB; errors use existing Rocket envelopes with 400 invalid input, 403 forbidden, 404 missing context, 409 transition conflict, or 500 internal failure.

## CLI

All commands print JSON. JSON definition files use the same bodies as the API:

```sh
rocket creative pipeline create --file pipeline.json
rocket creative pipeline update 1 --file pipeline.json
rocket creative pipeline ls --project my-project
rocket creative pipeline show 1
rocket creative run create --file run.json
rocket creative run ls --project my-project
rocket creative run show 1
rocket creative run context 1 --stage 2 --item 3
rocket creative run orchestrator 1 creative-orch
rocket creative item add 1 2 --file item.json
rocket creative item update 1 2 3 --file item.json
rocket creative item rm 1 2 3
rocket creative stage action 1 2 start
rocket creative item action 1 2 3 submit --artifact-version 10
rocket creative item action 1 2 3 approve --review-revision 1
rocket creative stage action 1 2 reopen
rocket creative stage progress 1 2 --progress 50 --note 'Draft complete'
rocket creative item progress 1 2 3 --progress 75 --note 'Rendering'
```

Agent identity comes from the existing `ROCKET_SESSION_ID` client behavior. CLI approval flags do not override identity. The human uses the dashboard or a human CLI session for human-only actions.
