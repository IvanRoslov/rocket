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

Before any paid CLI or MCP generation, propose an individual call or clearly
enumerated batch through `rocket creative agent ask` in the relevant run/stage/item
context. Put the proposal in the question body and continue discussion through
`rocket creative agent reply` in that thread. Do not create a separate proposal
file or publish an artifact solely to obtain permission.

Include intended outputs, tool and provider/model, every transmitted input and
external upload service, the exact call or complete list of calls, number of calls,
and price or estimate with its basis. Discover tool instructions and pricing
first. Historical tool pricing notes are not a current quote. If the price is
unknown, clarify it with the human before execution.

The human may deny, clarify or reduce the batch. Clarification or changed scope is
not approval: present the updated scope and cost in the same question thread and
wait for explicit human Go on that proposal before executing. If a prior answer
resolved the question, use `rocket creative agent reply QUESTION --dispute` with
the updated proposal to reopen the same thread for the new decision. A Go covers
only the proposed call set and cost. Additional variants, paid retries, other paid
attempts or increased cost require fresh agreement and a new explicit Go.
Stage approval is separate, and auto stages do not waive paid consent. After an
interruption, inspect the question history and verify exactly what ran before
retrying. Preserve existing history and artifacts; publish results as normal
versioned artifacts. This is an agent rule, not a technical enforcement boundary.
