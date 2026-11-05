# Creative agents and contextual discussion

Create a pipeline/run with `rocket creative`, then launch its dedicated session:

```sh
rocket creative agent launch RUN --profile PROFILE
rocket creative agent sessions RUN
```

The run uses its existing project's main/linked repositories and the existing
model profiles, isolated workspaces, runtime and delivery queue. Creative sessions
have no development task. The run snapshot (pipeline, ordered stages/items,
expected artifacts, review/approval mode and prior progress) is supplied at launch
and restore. Prompts tell agents to refresh artifacts/messages/questions and
preserve completed results. Existing development prompts and launch paths remain.

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

```sh
rocket creative agent send RUN --to WORKER_SESSION --stage STAGE --item ITEM --version VERSION --file note.md --wait
rocket creative agent ask RUN --stage STAGE --item ITEM --version VERSION --body "Generate one image with TOOL / PROVIDER / MODEL from VERSION; send INPUTS via UPLOAD_SERVICE; 1 call: EXACT_CALL; estimated PRICE based on BASIS. Go?" --option Go --option Revise --option Deny
rocket creative agent messages RUN --stage STAGE --item ITEM
rocket creative agent questions RUN --stage STAGE --item ITEM
rocket creative agent reply QUESTION --file reply.md
rocket creative agent answer QUESTION --choose 1
```

Use the actual worker session ID returned by spawn or listed by `sessions` when
sending from the orchestrator. Workers can explicitly use `--to ORCHESTRATOR_SESSION`
or omit `--to` to reply to the run orchestrator. Explicit and default self-send are
rejected before enqueue with a recipient hint; contextual metadata is preserved.

Without `--wait`, send prints the contextual message response, including its actual
recipient, ID and status. With `--wait`, it reuses ordinary `rocket send` polling:
queued/delivering is pending, accepted is transport acceptance awaiting confirmation,
delivered is confirmed delivery, and failed is terminal with its reason. Text output
identifies the recipient; `--json --wait` prints one terminal JSON object with the
recipient, message ID and status. Confirmation does not mean work is completed or
approved; missing confirmation does not establish non-reception or authorize a
blind resend. Read contextual `messages` and match the ID before taking action.
Ordinary `rocket send <session> ... [--wait]` keeps its existing contract.

`answer` is human-only for creative questions; agents reply. `--version` may be
repeated. Question context and shown versions remain fixed across replies and
answers. Stage-context discussion may reference that stage's item versions; item
context remains exact, as does run context. All references are explicit immutable
IDs. Artifact listing remains exact to its storage scope. Paid Go follows the
agent rule in [creative-tools.md](creative-tools.md); answering a question never
changes stage approval or technically authorizes tool execution.

Paid proposals belong in these existing question threads, for an individual call
or clearly enumerated batch. The example above uses placeholders: replace them
with the intended output, actual tool/provider/model, all transmitted inputs and
upload services, complete call list and count, and price/estimate basis. Omit
`--version` if no existing artifact is being discussed; do not create a proposal
file or artifact solely to ask permission. Existing results retain their versions
and newly generated results are published normally.

The human may deny, clarify or reduce a batch. Agents continue the discussion with
`reply QUESTION --body TEXT` (or `--file` for existing markdown/large text). A
clarification or reduction is not approval: reply in that thread with the revised
scope and cost, then wait for explicit human Go on that proposal. If the previous
answer resolved the question, include `--dispute` on the reply with the updated
proposal to reopen that same thread for a new answer; an ordinary agent reply to a
resolved question only adds history. Resolve unknown pricing with the human
before executing. Additional paid attempts or cost require
fresh agreement and a new Go; a previous Go never covers a changed proposal.
Question history remains intact throughout discussion.

HTTP contracts (snake_case, existing authentication):

- POST `/v1/creative/runs/{run}/sessions`: `{profile?,agent?,prompt?}`, human launch.
- POST `/v1/creative/runs/{run}/workers`: `{stage_id,item_id?,repo?,name,prompt,profile?,agent?}`, orchestrator-only.
- GET `/v1/creative/runs/{run}/sessions`: session array with `creative_context`.
- GET/POST `/v1/creative/runs/{run}/messages` and `/questions`: reads take exact
  `stage_id`/`item_id` query scope; writes take `{stage_id?,item_id?,artifact_version_ids?,body}`.
  Message adds optional `to` (default run orchestrator); self-send returns HTTP 400
  `self_send` before enqueue. Question adds `title`/`options`.
  Responses use existing message/question fields plus `creative_context` and
  `artifact_version_ids` (always an array). Lists are arrays.
- POST `/v1/questions/{id}/reply` and `/answer`: existing body/choose shape, original
  creative context retained. No separate paid Go endpoint or token.
- GET `/v1/tools`: shared CLI catalog array. Installation/registration is explicit
  local CLI administration, not a remote write API.

Messages carry readable context when delivered through the existing queue. Questions
share the existing question history, participants and attention store, with run-
scoped listing instead of development task threads. Human reads are global; agent
writes require their run and assigned scope. Terminal recipients report an error;
question answers remain durable when an agent is offline and are read on restore.

Interactive previews use the existing session/workspace: discover setup/allowed
ports, register context first, then launch the server with the returned base path.
See the preview CLI's setup/register/list commands. No extra agent runtime, live
daemon restart, external deployment or paid generation is part of installation.
