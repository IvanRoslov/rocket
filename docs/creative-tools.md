# Shared creative tools

The catalog is `<ROCKET_HOME>/tools/registry.yaml` (default `~/.rocket`). Both
creative orchestrators and workers use `rocket tools list` and `rocket tools show
NAME` to read the same commands and instructions. Commands are argv arrays;
invoke them directly, preserving argument boundaries. No execution proxy, model
catalog, price database or spending gate is introduced.

A fresh Rocket installation has an empty catalog. Rocket ships no provider
clients and does not install tools. Install or maintain any CLI you choose
outside Rocket, then register its command and usage in this user-owned catalog.
Local tools can live under `<ROCKET_HOME>/tools/` or at another stable path;
they do not depend on a particular source repository or Rocket binary.
Existing local files and registrations remain yours to maintain.

## Live Creative rules

`<ROCKET_HOME>/tools/rules.md` (default `~/.rocket/tools/rules.md`) is a
user-owned, free-form text file for rules about which tool, provider or model
to use for each job. It uses the same Rocket home as the tools catalog. Rocket
does not ship or create this file; a missing or empty file means there are no
rules, and agents proceed as before without a blocker.

Both Creative orchestrators and workers read the current file at the start of
work, after a restore, and again before every tool, provider or model choice.
This includes writing a worker brief that names a tool or model and preparing
a paid-generation proposal. Agents read the live file rather than a copy
captured at launch, so the human can change rules through an agent without a
Rocket release.

The human's direct instruction in the current run takes priority, followed by
the rules file, then the agent's own judgement. A one-off instruction does not
change the file. Rules never replace paid-generation Go or stage approval.
When a rule cannot be followed because a tool is not registered, authorization
is unavailable or a model is unavailable, agents report a blocker or ask; they
never silently substitute another tool or model. Workers communicate through
the orchestrator.

Any agent may edit the file only when the human directly asks to change a rule,
never on its own initiative. If the request could mean either a one-off
exception or a new rule, the agent asks. It preserves the other rules and tells
the human that the change applies to all runs. Rocket does not enforce these
rules; following them is an agent responsibility.

## Registering and using tools

Register a CLI with `rocket tools register renderer.yaml`; replacement requires
`--update`. Use `--home PATH` to select a different Rocket home for catalog
commands. For example:

```yaml
name: renderer
description: Local scene renderer
command: [node, /absolute/path/render.mjs]
usage: |
  Add --help to inspect options. Render into the current workspace, then publish
  the completed result through rocket creative artifact publish.
environment: []
```

Human operators configure environment/local authorization themselves. Catalog
`environment` lists names only, never values. Follow each tool's own
authorization instructions; never put credentials in registry metadata.
Read the client's help before using uploads: a provider may use another
storage service for local references. Include all transmitted inputs and storage
services in the proposal to the human. Missing tools or authorization is a
reported failure, never successful generation.

Before any paid CLI or MCP generation, the orchestrator proposes an individual
call or clearly enumerated batch through `rocket creative agent feed propose RUN`
in the relevant stage/item context. Ordinary discussion uses `feed post RUN`.
Workers send their request to the current orchestrator with `creative agent send`;
they never write to the human. New Creative questions are refused.

Include intended outputs, tool/provider/model, every transmitted input and external
upload service, the exact call or complete call list and count, and price/estimate
with its basis. Pin existing artifact versions with repeatable `--version`.
Discover tool instructions and pricing first; historical notes are not a current
quote. Clarify unknown pricing in the feed before execution. Use `--body` directly
or `--file` for existing Markdown/large text; do not create a proposal artifact or
file solely to ask permission.

The human decides a card once using Go / Revise / Deny buttons. A typed Go is
discussion and never consent. Read `rocket creative agent feed card RUN ENTRY`
before execution: require `status:decided` and `decision:go` on that exact proposal.
Never execute pending, replaced, revise or deny cards. Changes such as “make it 2”
require a new card with updated scope/cost and `--replaces OLD_ENTRY` for the
previous undecided card. The old card becomes replaced with a link to the new one.
If the old card was already decided, publish a fresh card without replacement.
Additional variants, paid retries, other paid attempts or increased cost require
fresh agreement and Go on a new card. Previous Go never covers changed scope.

Stage approval remains submit + formal review card; auto stages do not waive
paid consent. Feed text never approves a stage. After interruption inspect all
run/stage feeds, card states and what already ran before retrying. Preserve
history/artifacts and publish results as normal versioned artifacts. Cards are
agreement records, not tool execution or a technical spending gate. The HTTP
feed/card/unread/mark-read contract and dashboard polling behavior are documented
in [creative-agents.md](creative-agents.md#conversation-feed-http-contract-creative-messages-task-6061).
