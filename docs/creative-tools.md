# Shared creative tools

The catalog is `<ROCKET_HOME>/tools/registry.yaml` (default `~/.rocket`). Both
creative orchestrators and workers use `rocket tools list` and `rocket tools show
NAME` to read the same commands and instructions. Commands are argv arrays;
invoke them directly, preserving argument boundaries. No execution proxy, model
catalog, price database or spending gate is introduced.

Install the bundled generation clients explicitly with `rocket tools install`.
Use `--home PATH` to stage an installation elsewhere. If any bundled registration
already exists, installation refuses it unless `--update` is supplied. Update
selects this binary's versioned distribution and preserves older directories and
unrelated registrations. It never copies or modifies authorization, installs
MCP, runs generation or installs packages. Node >=26 is required. Provider
clients are the pinned upstream code; see installed PROVENANCE.md and README.md.

Register another CLI with `rocket tools register renderer.yaml`; replacement
requires `--update`. For example:

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
`environment` lists names only, never values. Some upstream clients optionally
read a `.env` beside their installed package or a configured 1Password reference;
prefer explicit process environment for shared installation. No secrets are
included. Read the client's help before using uploads: a provider may use another
storage service for local references. Include all transmitted inputs and storage
services in the proposal to the human. Missing tools or authorization is a
reported failure, never successful generation.

Before any paid CLI or MCP generation, the agent must show what will be
produced, inputs to be sent, provider/model, exact call or list of calls, and cost
basis. Wait for explicit human Go for that set. Additional variants, paid
retries or an increase to approved cost require a new Go. Stage approval is
separate. State unknown cost as unknown; historical upstream pricing notes are
not a current quote. This is an agent rule, not a technical enforcement boundary.
