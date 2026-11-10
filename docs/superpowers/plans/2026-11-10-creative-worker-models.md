# Creative worker model allowlist Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Humans control the worker profiles available to each creative run.
**Architecture:** Persist allowed_profiles on runs. Feed that list into existing modelpolicy for spawns and availability; write it before orchestrator startup.
**Tech Stack:** Go, SQLite migrations, HTTP, Cobra.
**Spec:** docs/superpowers/specs/2026-11-10-creative-worker-models-design.md

## Global Constraints

Empty = unrestricted; humans alone edit. Orchestrator profile and restore unchanged. No dashboard or pipeline defaults.

## Review Focus

- Existing runs return [] and survive migration.
- Unknown names never partially update lists.
- Both creative session kinds see the run policy, independent of dev tasks.
- Omitted launch list preserves existing restrictions.
- Disabled/deleted allowed profiles cannot launch workers.

### Task 1: Store, API and enforcement

**Files:** internal/store/creative{,_types}.go; new migration 0032; internal/api/creative_models.go; creative_agents.go; model_profiles.go; new creative_models_test.go.
**Interfaces:** SetCreativeRunAllowedProfiles(runID int64, names []string) error; CreativeRun.AllowedProfiles []string; PUT /runs/{run}/models as in spec.

- [ ] Write HTTP integration tests for replace/read/clear, malformed and unknown inputs, agent denial, launch persistence, spawn resolution/refusal and models availability for orchestrator/worker.
- [ ] Run go test ./internal/api -run 'TestCreative.*(Models|Profiles)' -count=1; expect failures for missing API/field/policy.
- [ ] Add migration, storage field/read/set, human replacement route, launch validation/save and run policy inputs on worker spawn and available-model lookup.
- [ ] Run go test ./internal/store ./internal/api ./internal/modelpolicy; expect PASS.
- [ ] Commit backend changes.

### Task 2: CLI, prompt and docs

**Files:** internal/cli/creative{,_agents}.go and tests; internal/prompts/templates/creative-orchestrator.md; docs/creative-agents.md; docs/creative-core-api.md.
**Interfaces:** rocket creative models RUN [--allow a,b | --clear]; creative agent launch RUN --allow a,b.

- [ ] Add CLI request tests for view/set/clear, launch allow and invalid flag combinations; run tests to see missing commands fail.
- [ ] Implement CLI commands and launch flags; tell orchestrator to inspect rocket models ls before choosing worker profiles; document exact API and CLI semantics.
- [ ] Run go test ./internal/cli ./internal/prompts; expect PASS.
- [ ] Commit changes, run go test ./... and repository CI checks, review whole diff, create one PR to main and report URL.
