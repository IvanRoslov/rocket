# Creative agents and contextual discussion

Create a pipeline/run with `rocket creative`, then launch its dedicated session:

```sh
rocket creative agent launch RUN --profile PROFILE [--allow cheap,deep]
rocket creative agent sessions RUN
```

The run uses its existing project's main/linked repositories and the existing
model profiles, isolated workspaces, runtime and delivery queue. Creative sessions
have no development task. The run snapshot (pipeline, ordered stages/items,
expected artifacts, review/approval mode and prior progress) is supplied at launch
and restore. Prompts tell agents to refresh all run/stage feeds, artifacts, worker messages and legacy questions and
preserve completed results. Existing development prompts and launch paths remain.

The human can restrict worker profiles per run:

```sh
rocket creative models RUN                        # view the run and allowed_profiles
rocket creative models RUN --allow cheap,deep      # replace the whole list
rocket creative models RUN --clear                 # all enabled profiles
rocket creative agent launch RUN --profile PROFILE --allow cheap,deep
```

`allowed_profiles: []` means all enabled profiles, including on existing runs.
Omitting `--allow` at launch preserves the current list; `--allow ""` clears it.
The list is validated and saved before the orchestrator starts, so its first
`rocket models ls` sees the restriction. Only humans edit it; any agent session,
including the run orchestrator, receives `403 human_only`. Unknown names return
`400 profile_not_found` without changing the list. Registered disabled profiles
can be listed but are never offered for new workers.

Before each spawn the orchestrator runs `rocket models ls`, chooses a profile
suited to the assignment from the returned list, and passes `--profile PROFILE`.
Both creative orchestrator and worker sessions see the run's currently allowed,
enabled profiles. Explicit profiles outside the list fail with
`profile_not_allowed: allowed: ...`; an agent alone picks its first allowed profile;
no selection uses the default worker profile if allowed, otherwise the first
allowed profile. Ordering follows registry position, then name. If no enabled
profile remains, spawn returns `no_profiles_allowed`. Refused spawns create no
sessions or workspaces. Clearing the list restores unrestricted enabled selection.
The restriction applies to new workers: the orchestrator's own profile is
unrestricted, existing workers continue, and restore uses each session's saved
profile snapshot.

Only the live run orchestrator spawns workers:

```sh
rocket creative agent spawn RUN --stage STAGE --item ITEM --name scene --repo REPO --file brief.md
```

Omit `--item` for a stage assignment. Workers publish results through the artifact
CLI and report immutable version IDs to their orchestrator. Stage/item transition
rules live in the creative core. Late changes require explicit human reopening,
an explanation of downstream effects and agreed rework; no automatic rerun.
Restore uses `rocket restore SESSION`, retaining the same creative context and
profile snapshot. A superseded orchestrator cannot be restored alongside a live
replacement. Creative workers are excluded from PR polling and merge cleanup.

The current live run orchestrator explicitly stops its workers with:

```sh
rocket creative agent close RUN WORKER_SESSION
```

Before closing a completed assignment, verify that its outputs are durably
published as the required artifact versions. This is semantic verification against
the assignment; file existence, a preview or a worker's report alone is insufficient.
After stage/item approval, verify publications and close the appropriate stage/item
workers. Approval never closes workers automatically; retain workers still needed
for unfinished work. Close permits workers belonging to the same run/project,
including those launched by a predecessor orchestrator, and prohibits foreign-run,
foreign-project and orchestrator targets. Only the current live bound orchestrator
may invoke this command. Human session administration remains available through
ordinary Rocket commands.

Close reuses ordinary session stop without cleanup or cascade: workspace files,
published artifacts/versions, session context and discussion history remain.
Repeating close on a terminal worker succeeds and retains its terminal state.

The conversation feed is the only human conversation channel. A run has a general
feed and one feed per stage. Item messages carry tags in their stage feed. Read
newest first; stage reads include all items unless filtered:

```sh
rocket creative agent feed RUN
rocket creative agent feed RUN --stage STAGE
rocket creative agent feed RUN --stage STAGE --item ITEM
rocket creative agent feed post RUN --stage STAGE --item ITEM --version VERSION --body "Updated the ending"
rocket creative agent feed propose RUN --stage STAGE --item ITEM --version VERSION --body "Generate 2 images; tool/provider/model; inputs and upload services; complete calls/count; price and basis"
rocket creative agent feed propose RUN --stage STAGE --item ITEM --replaces CARD_ENTRY_ID --file revised.md
rocket creative agent feed card RUN CARD_ENTRY_ID
```

Only the current run orchestrator writes to the human; workers must send their
questions/reports to it. Human messages and card decisions are durable, delivered
with context and an executable reply command, and readable after restore. On
launch/restore the agent memo requires reading the run feed and every stage feed.
Writing never changes pipeline status, including after approval. Feed messages
and proposal content cannot be edited/deleted. Attach exact immutable version IDs
with repeatable `--version`; use `--file PATH` (or `--file -`) for Markdown/long text.
A stage can reference versions from its items; an item must match exactly.
When reporting results (new versions, fixes or options to choose from), the
orchestrator must attach every discussed published version to that same feed
message with `--version`. Briefly state what each attachment is and which item it
belongs to. A result message without attachments violates this rule. Attach
intermediate/service files only when the human should look at them.

Paid proposals are cards, with human-only one-time Go / Revise / Deny decisions.
See [creative-tools.md](creative-tools.md) for required contents. Changed scope or
cost requires a new card; replace the previous undecided card with `--replaces`.
Replaced cards retain links/history and cannot be decided. Already-decided cards
remain history; create a fresh card without replacement. Inspect `feed card`
before execution: only `status:decided, decision:go` on the exact call set counts.
Typed Go or other ordinary messages are discussion. Cards record agreement and
neither execute tools nor implement a technical spending gate.

Worker transport is unchanged:

```sh
rocket creative agent send RUN --to WORKER_SESSION --stage STAGE --item ITEM --version VERSION --file note.md --wait
rocket creative agent messages RUN --stage STAGE --item ITEM
```

Orchestrators name the worker recipient; workers omit `--to` to address the
current run orchestrator (including after replacement). Self-send is refused.
Without `--wait`, send prints the message including actual recipient, ID and
status. With `--wait`, queued/delivering is pending, accepted is transport
acceptance awaiting confirmation, delivered is confirmed delivery, and failed
is terminal with its reason. `--json --wait` prints one terminal JSON object.
Delivery is not completion/approval; missing confirmation is not proof of
non-reception. Inspect messages before resending uncertain work.

New `creative agent ask` and POST questions are refused with feed-command hints.
The existing question archive remains readable and its replies/answers keep their
original context/versions:

```sh
rocket creative agent questions RUN --stage STAGE --item ITEM
rocket creative agent reply QUESTION --file reply.md
rocket creative agent answer QUESTION --choose 1  # human only; old questions
```

HTTP contracts (snake_case, existing authentication):

- POST `/v1/creative/runs/{run}/sessions`: `{profile?,agent?,prompt?,allowed_profiles?}`, human launch.
- PUT `/v1/creative/runs/{run}/models`: `{"allowed_profiles":["cheap","deep"]}`,
  human-only full replacement; `[]` clears. Returns `200 {"allowed_profiles":[...]}`.
  Read via GET `/v1/creative/runs/{run}`; omission/null is invalid for replacement.
- POST `/v1/creative/runs/{run}/workers`: `{stage_id,item_id?,repo?,name,prompt,profile?,agent?}`, orchestrator-only.
- POST `/v1/creative/runs/{run}/workers/{session}/close`: current run orchestrator
  only, own-run/project worker target; no body or cleanup/cascade options. Returns
  the ordinary session-stop response `{"status":"killed"}` (already-terminal
  targets keep their stored state).
- GET `/v1/creative/runs/{run}/sessions`: session array with `creative_context`.
- GET/POST `/v1/creative/runs/{run}/messages` and `/questions`: reads take exact
  `stage_id`/`item_id` query scope; writes take `{stage_id?,item_id?,artifact_version_ids?,body}`.
  Message adds optional `to` (default run orchestrator); self-send returns HTTP 400
  `self_send` before enqueue. New POST questions returns 409 `creative_questions_retired`.
  Responses use existing message/question fields plus `creative_context` and
  `artifact_version_ids` (always an array). Lists are arrays.
- POST `/v1/questions/{id}/reply` and `/answer`: existing body/choose shape, original
  creative context retained. For archived questions only; feed cards carry new paid decisions.
- GET `/v1/tools`: shared CLI catalog array. Installation/registration is explicit
  local CLI administration, not a remote write API.

Messages carry readable context when delivered through the existing queue. Questions
share the existing question history, participants and attention store, with run-
scoped listing instead of development task threads. Human reads are global; agent
writes require their run and assigned scope. Terminal worker-message recipients report an error. Human feed messages and
card decisions persist without a session, are queued on launch/restore, and stay
readable independently of transport status. Transport deliveries with uncertain
reception are not blindly resent; restore instructions require reading all feeds.

Interactive previews use the existing session/workspace: discover setup/allowed
ports, register context first, then launch the server with the returned base path.
See the preview CLI's setup/register/list commands. No extra agent runtime, live
daemon restart, external deployment or paid generation is part of installation.

## Formal review

The orchestrator publishes results and submits their exact immutable versions with
`rocket creative stage action RUN STAGE submit --artifact-version VERSION` (or
item action). Submission creates the existing pending revision and displays its
review card above the workspace tabs. Do not ask a separate approval question for
the same stage/item, ask the human to submit results, or treat an ordinary chat
answer as a pipeline transition. Paid Go uses a feed card; clarifications use ordinary feed messages.
The human approves or returns the submitted revision from that card. Return
feedback is required and arrives through the existing contextual notification
with the decision, revision and versions; revise results before submitting again.
Automatic stages retain their saved behavior. Workers coordinate publication and
submission with their orchestrator and never approve on behalf of the human.

## Conversation feed HTTP contract (creative-messages, task 6061)

This contract is the boundary for the feed web implementation. Existing session
messages remain orchestrator/worker transport; feeds are separate durable records.
All endpoints use existing authentication and snake_case JSON. IDs are integers;
times are Unix seconds. A feed is selected by run and optional `stage_id` (omitted
or zero means the run feed). An `item_id` requires its owning stage and is a tag
or read filter, never a separate feed. Stage reads include all item tags unless
filtered. Version IDs must belong to the selected scope (a stage may reference
its items). Writing never changes pipeline/review state, even after approval.

| Method and path | Request | Success |
| --- | --- | --- |
| GET `/v1/creative/runs/{run}/feed` | Query: `stage_id?`, `item_id?` | `200` entry array, newest first by ID; no pagination in this contract |
| POST `/v1/creative/runs/{run}/feed` | `{stage_id?,item_id?,body,artifact_version_ids?}` | `201` created message entry |
| POST `/v1/creative/runs/{run}/feed/cards` | `{stage_id?,item_id?,body,artifact_version_ids?,replaces_entry_id?}` | `201` created proposal entry; orchestrator only |
| GET `/v1/creative/runs/{run}/feed/cards/{entry}` | No body | `200` proposal entry, including current decision/replacement |
| POST `/v1/creative/runs/{run}/feed/cards/{entry}/decision` | `{"decision":"go"}` (`go`, `revise`, `deny`) | `200` updated entry; human only |
| GET `/v1/creative/runs/{run}/feed/unread` | No body | `200` unread summary below |
| POST `/v1/creative/runs/{run}/feed/read` | `{stage_id?,through_entry_id}` | `200 {"stage_id":0,"through_entry_id":42}`; human only |

An entry always has this shape (zero scope IDs and empty arrays are explicit):

```json
{
  "id": 42,
  "run_id": 7,
  "stage_id": 9,
  "item_id": 12,
  "kind": "proposal",
  "author": "orchestrator",
  "author_session_id": "creative-7-orch",
  "body": "Generate two images; tool/provider/model ...; inputs/upload services ...; 2 calls: ...; estimate $0.20 based on ...",
  "artifact_version_ids": [101],
  "created_at": 1794340000,
  "card": {
    "status": "pending",
    "decision": "",
    "decided_at": 0,
    "replaces_entry_id": 0,
    "replaced_by_entry_id": 0
  }
}
```

Messages have `kind:"message"` and `card:null`. `author` is `human` or
`orchestrator`; human entries have `author_session_id:""`. Display names for
stage/item tags come from the existing run snapshot. Proposal content is immutable
Markdown in `body`, following the paid-call content rules (outputs, tool/provider/
model, inputs and upload services, complete calls/count, price and estimate basis,
with pinned versions). The API checks nonblank body and valid version scope;
semantic completeness is an agent rule, not provider execution validation.

Card status is `pending`, `decided`, or `replaced`. `decision` is empty until
decided, then `go`, `revise`, or `deny`; `decided_at` is zero until decided.
Publishing a replacement atomically changes the earlier pending card to
`replaced` and records both links. Replacement may cross feeds within the same
run; item/stage ownership still applies. Decided/replaced cards cannot be replaced
or decided again. A conflict returns HTTP `409` with
`{"error":{"code":"creative_card_conflict","message":"...current state and command hint..."},"entry":{...current entry...}}`.
The web replaces its stale card with `entry` and refetches the feed after a
replacement. Only a card decision records paid consent; typing Go does not.

Unread summary:

```json
{
  "run_id": 7,
  "total": 3,
  "run_feed": 1,
  "stages": [{"stage_id":9,"unread":2}]
}
```

`stages` contains every run stage, including zero counts. Count only orchestrator
messages and proposal entries, once per entry. Human messages, decisions and
replacement state changes do not add unread. A stage count includes all its items.
GET never marks read. On opening a feed, POST read with the highest displayed
entry ID (or zero for empty). Read markers only advance; concurrent later entries
remain unread. An item-filtered view marks the containing stage feed read through
that ID. `through_entry_id` must be zero or an entry in that run/feed.

The web polls feed and unread every 2 seconds while a run is open and refetches
after successful mutations; mutation responses permit immediate local insertion.
Polling also recovers missed updates after reconnect and updates replaced cards.
No new event subscription is required. Pending cards can be highlighted directly
from the returned full entry array. Existing question archive uses the existing
GET questions endpoint with its existing exact stage/item scope; replies/answers
remain unchanged. New POST questions (including `creative agent ask`) returns
`409 creative_questions_retired` with feed command hints.

Human callers may read/write any feed. Agents can read their own run; only its
currently bound orchestrator can post messages/cards. Workers receive
`403 creative_feed_forbidden` with `rocket creative agent send RUN ...` guidance;
foreign-run agents receive 403 with guidance to their own run. Human-only actions
return `403 human_only` for agents. Empty body returns `400 empty_body`; invalid
scope/version/request returns 400; absent run/card returns 404. Errors otherwise
use the existing `{"error":{"code":"...","message":"..."}}` shape.

Human messages and decisions persist even without an active orchestrator. They
are queued to an available run orchestrator through existing delivery transport;
feed reads after restore remain authoritative. Every delivery names the
run/stage/item, versions, entry/card ID and an exact reply command in that feed.
