# Creative Agents Implementation Plan

> Use superpowers:executing-plans and test-driven-development task by task.

**Goal:** Launch contextual creative sessions and distribute discoverable shared CLI tools.
**Architecture:** Existing runtime, queue and questions with separate creative associations. Independent YAML tools registry and explicit bundled installation.
**Tech Stack:** Go, SQLite, Cobra, YAML; vendored upstream TypeScript clients.
**Spec:** docs/superpowers/specs/2026-11-04-creative-agents-design.md

## Constraints and review focus

No development lifecycle changes; no paid calls, demos, live tools writes or restart. Test malformed registries, refused overwrite, cross-run context, restore prompt selection, and creative sessions excluded from development PR completion. Immutable shown versions remain pinned despite current-pointer changes.

## 1. Shared tools

Files: internal/toolcatalog/{catalog.go,catalog_test.go,bundle/**}, internal/cli/tools.go, internal/cli/root.go, docs/creative-tools.md.
Interfaces: Entry{Name,Description,Command,Usage,Environment}; Load(home) ([]Entry,error), Register(home,entry,replace) error, Install(home,update) error.
- [ ] Write tests for missing registry, valid YAML registration, invalid entries and explicit replacement; run to observe failure.
- [ ] Implement strict YAML metadata and explicit atomic registration, bundled install without silent overwrite; verify temporary-home tests.
- [ ] Copy only pinned upstream generation sources/tests/manifests, record provenance, adapt distribution paths only where necessary.
- [ ] Expose list/show/register/install commands and document independent human auth/MCP setup. Commit.

## 2. Context and communication

Files: internal/store/creative_sessions.go, migrations/0030_creative_sessions.sql, internal/api/creative_agents.go, internal/cli/creative_agents.go and tests.
Consumes core ResolveCreativeContext/GetCreativeRun and assets ValidateCreativeArtifactVersions.
- [ ] Test session/run association and cross-context rejection, pinned version references and queue/question reuse.
- [ ] Implement durable associations and contextual message/question API using existing participants/delivery; expose CLI.
- [ ] Run focused store/API/CLI tests and commit.

## 3. Launch and restore

Files: internal/session/creative.go, internal/session/manager.go, internal/prompts/templates/creative-*.md and tests; relevant PR polling exclusions.
Consumes existing model resolution, workspace/runtime/session infrastructure and creative associations.
- [ ] Test orchestrator/worker prompts include pipeline/review/context, tools, paid Go rule and no development workflow.
- [ ] Implement launch/restore paths with separate creative prompts, stage/item worker scope and same-project checks.
- [ ] Test no task creation or development cleanup/PR completion; run focused checks and commit.

## 4. Verify and deliver

- [ ] Run Go tests/vet, relevant pure bundled-client tests, inspect diff and documentation/CLI contracts.
- [ ] Publish one feature/creative-work PR, report URL to orchestrator and address review.
