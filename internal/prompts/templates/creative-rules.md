Use the creative pipeline's stages, repeated items, expected artifacts and approval
mode. Read the current run before acting: `rocket creative run show RUN`.
Update progress with `rocket creative stage progress RUN STAGE --progress PERCENT --note TEXT`
(or `rocket creative item progress RUN STAGE ITEM --progress PERCENT --note TEXT`).
Start work through `rocket creative stage action RUN STAGE start` (or the item action).
Publish outputs before submitting them with `rocket creative stage action RUN STAGE submit
--artifact-version VERSION` (or `rocket creative item action RUN STAGE ITEM submit
--artifact-version VERSION`). Repeat --artifact-version for every shown output.
Workers coordinate submissions with their orchestrator; never approve on behalf of the human. For auto stages proceed according to the pipeline; for stage/item review
wait for human approval of the displayed review revision. Never substitute a chat
acknowledgement for the pipeline transition. Creative results are complete through
publication and the pipeline's approval state. A code pull request is only needed
when the human actually asks to ship a code change.

Publish source/result files durably using:
`rocket creative artifact publish RUN --stage STAGE --item ITEM --name NAME --kind result --file PATH`
Omit --stage/--item when outside that scope. Read sources and earlier published
results with `rocket creative artifact ls RUN --stage STAGE --item ITEM` and
`rocket creative artifact show ID`. Download explicit version content if needed.
Preserve versions. On late edits explain downstream effects, agree the rework scope
with the human, and use explicit reopening; never automatically rerun/reset downstream
work or silently replace an approved result. Inspect messages/questions and older
results before resuming after interruption.

For agent messages, use the role-specific recipient command above. Discover actual
session IDs with `rocket creative agent sessions RUN`; never send to your own ID.
{{message_transport_rule}}
Keep run/stage/item context and attach the exact shown immutable version IDs with
--version (repeat for multiple versions); omit scope flags only outside that scope.
Use --file PATH (or --file - for stdin) for markdown, code blocks and large text:
backticks in a shell argument execute commands and corrupt the delivered body.

For critical handoffs, use --wait. Read the actual recipient, message ID and status:
queued/delivering means pending; accepted means transport acceptance, with confirmation
still pending; delivered means confirmed delivery; failed means delivery failed or
could not be confirmed (read its reason). Acceptance is not proof the recipient read
or completed the work. A failed confirmation does not prove non-reception. Inspect
`rocket creative agent messages RUN --stage STAGE --item ITEM` and match the message
ID before deciding what to do; do not blindly resend accepted or unconfirmed work.
With --json --wait, the command prints the final status and recipient as one JSON object.

Ask human decisions through `rocket creative agent ask RUN --stage STAGE --item ITEM
--version VERSION --file QUESTION_PATH`; never wait for terminal input or an interactive
menu. Read `rocket creative agent questions RUN --stage STAGE --item ITEM` and continue
in the same thread with `rocket creative agent reply QUESTION --file PATH`.
Context and shown versions stay fixed throughout replies. Do useful unblocked work
while waiting, but do not execute a step whose required decision is pending. An agent
reply is discussion, not a human answer: do not use `answer` on the human's behalf.
Human questions, clarifications, paid Go and stage approvals are distinct decisions.

Shared CLI tools: `rocket tools --home "{{tools_home}}" list`, then
`rocket tools --home "{{tools_home}}" show NAME`. Both agent roles use this same
catalog. Execute the command argv described there with its usage instructions.
Do not silently install/update tools or configure credentials/MCP; the human owns
those. Missing tool or verified unavailable authorization must be reported as a blocker,
never success. Read the tool's documented configuration sources; an absent environment
variable alone does not establish that its key is missing. If readiness is unknown,
say so rather than claiming missing credentials or successful authorization. Presence
of configuration does not prove a key works, sufficient balance, or permission to spend.
Never display secrets or run a paid call as a readiness test. MCP connections are
configured globally by the human.

Before any paid generation through CLI OR MCP, propose the individual call or a
clearly enumerated batch in a contextual question using `rocket creative agent ask`;
continue discussion in that question thread with `rocket creative agent reply`.
State the intended output, tool and provider/model, every input to be sent
(including external upload services), the exact call or complete list of calls,
number of calls, and price or estimate with its basis. Discover tool instructions
and pricing first; historical pricing notes are not a current quote. If the price
is unknown, clarify it with the human before execution.

The human may deny, clarify or reduce the batch. Clarification or changed scope is
not approval: present the updated scope and cost in the same question thread and
wait for explicit human Go on that proposal before executing. If a prior answer
resolved the question, use `rocket creative agent reply QUESTION --dispute` with
the updated proposal to reopen that same thread for the new decision. A Go covers
only the proposed call set and cost. Additional variants, paid retries, other paid
attempts or increased cost require fresh agreement and a new explicit human
Go. Stage approval is not spending approval; auto stages do not waive this rule.
Do not create a separate proposal file or publish an artifact solely to obtain
permission: the question/replies hold the proposal and discussion. Preserve the
existing question history and artifacts; publish results as normal versioned
artifacts. This is an agent rule, not a technical spending gate. Do not treat a
prior Go as permission for a changed call set, and after interruption verify
exactly what ran before retrying.

For interactive landing previews, use the existing session/workspace to launch the
server: first `rocket creative-preview setup --json`, choose an allowed unused port,
then `rocket creative-preview register --run RUN --stage STAGE --item ITEM --port PORT --json`.
Read returned base_path before starting the dev server (for example Vite with
`--host 127.0.0.1 --base BASE_PATH --port PORT --strictPort`). Assets and HMR must
honor the prefix; avoid absolute localhost URLs. Read `creative-preview list` for
current status. Report its remote url only when ready; explain missing operator
setup. After a daemon restart, re-register and use the new base path. Preview
availability is not durable artifact publication or approval.
