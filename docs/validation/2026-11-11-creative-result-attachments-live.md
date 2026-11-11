# Creative result attachments: isolated orchestrator validation

Date: 2026-11-11. Rocket task 6067, parent 6061, spec v6, feature
creative-messages.

## Environment and protocol

Built the changed tree with `go build -o .rocket/attach-rule/rocket ./cmd/rocket`.
Used a separate Rocket home/config/database, an explicit isolated Unix socket,
an ephemeral loopback HTTP port, and a synthetic local Git repository/project
inside this worktree's ignored validation directory. The fixture had its own
Codex home, trust/config/journals and copied authentication; its command environment
selected the test CLI. No operator daemon, installed binary or existing run was
changed. The first daemon attempt inherited the operator socket and refused to
start because it was in use; explicitly selecting the isolated socket resolved
that fixture setup issue.

Created a pipeline with one repeated Scenes stage (`approval: item`), then a run
with this assignment:

> This is an isolated local validation run. No paid generation, provider calls,
> uploads, workers or external services. In the Scenes stage, create items Opening
> and Ending. Write and publish one short text scene option for Opening and two
> short options for Ending, then report the available results to the human in a
> single stage message, identifying which option belongs to which item. Do not
> submit for formal acceptance yet. Stop after reporting and wait for human
> feedback. Use only this fixture workspace and this run.

Launched a real Codex creative orchestrator (CLI 0.166.0, GPT-6.1-Sol, low effort)
through `POST /v1/creative/runs/1/sessions` with `{"agent":"codex"}`. It received
the normal generated orchestrator prompt and launch memo from the changed binary.
The assignment did not supply feed commands or tell it to attach versions.
Run 1, stage 1, Opening item 1, Ending item 2; session `creative-1-orch-2`.

After its result report, posted this human message through the ordinary feed API,
scoped to stage 1 / item 1, without attachments or attachment instructions:

> Revise Opening Option 1 so the mysterious beam is red. Publish the revised scene
> and report the fix here. Leave formal acceptance pending. No paid calls, workers,
> uploads or external services.

Read the durable stage feed, run state and each artifact's retained version
metadata through the isolated API. Checked the agent journal for the actual
publication/report commands. Assertions verified the exact attachment arrays,
message scopes, item/option descriptions, artifact ownership, preserved original
version and unchanged review revisions.

## Observed results

| Scenario | Durable evidence |
| --- | --- |
| Report new results and alternatives from two items together | Orchestrator entry 2 in stage 1, no item filter, attached versions `[1,2,3]`. It described Opening / The Light (1), Ending / Homecoming (2), and Ending / The Watch (3) in that same body. |
| Report an item-scoped fix | Human entry 3 requested the red beam. Orchestrator entry 5 in stage 1 / item 1 attached `[4]` and said the revised scene belonged to Opening, quoting the corrected red beam. |
| Preserve published history | Opening artifact 1 retained versions 1 and 4; Ending artifacts 2 and 3 retained versions 2 and 3. The fix created revision 2 rather than replacing version 1. |
| Keep service files out of result attachments | Local item-definition JSON files were not attached; both result reports attached only the published scenes discussed in their text. |
| Preserve acceptance semantics | Stage/item review revisions remained 0; no formal submission or human approval occurred. A premature attempt to start Ending returned the existing previous-item approval conflict; it did not prevent publication or alter acceptance. |

Actual result-report commands (bodies abbreviated here):

```sh
rocket creative agent feed post 1 --stage 1 --version 1 --version 2 --version 3 --body 'Three short scene options are published and attached: ...'
rocket creative agent feed post 1 --stage 1 --item 1 --version 4 --body 'Opening Option 1 now reads “A red beam sweeps across the empty sea.” Attached is the revised scene, immutable version 4 (revision 2), for the Opening item. ...'
```

The agent used the repeatable `--version` flag without additional coaching on
attachments. Progress announcements preceded publication; the two messages that
reported published results both carried the versions they discussed. No paid
generation/provider calls, uploads, workers or external creative services were
used. The fixture session and isolated daemon were stopped after verification.

## Automated evidence

- Baseline: `go test ./internal/prompts ./internal/session` passed.
- New rendered-prompt and launch/restore memo assertions failed for the absent
  attachment guidance before the prompt/memo edits, then passed afterward.
- `go test ./internal/prompts ./internal/session`: passed.
- `go vet ./internal/prompts ./internal/session`: passed.
- Final `make test`: all Go packages passed; development web 62 files / 722 tests;
  Creative web 11 files / 81 tests.
- Independent review of the prompt/docs/test change found no issues.

The initial broad run hit the existing three-second Claude hook fixture timeout
in `TestHookExtractsOnlyBoundedMetadata/valid`. Its focused rerun passed, and the
subsequent complete `make test` passed. Web dependencies were installed from their
lockfiles; no unrelated source changes were made.
