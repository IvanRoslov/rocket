# Creative rules prompt implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Both Creative roles consult the current user-owned rules before selecting tools, providers or models.

**Architecture:** Extend the existing shared Creative prompt fragment using its existing `tools_home` variable. Agents read the live file themselves; rendering never loads or creates it.

**Tech Stack:** Embedded Markdown templates, Go rendering tests, Markdown docs.

**Spec:** Task 6065 brief in `.rocket/inbox/msg-82671.md`; design accepted by creative-rules-orch.

## Global constraints

- Rules live at `<ROCKET_HOME>/tools/rules.md`; Rocket ships and creates no content.
- No CLI, validation, history, UI or runtime enforcement changes.
- Direct human instructions take priority; paid Go and stage approval remain required.

## Review focus

- Custom Rocket home, including spaces: the rendered path uses that home.
- Missing/empty rules: agents continue without a blocker.
- Restore and later choices: agents reread current content.
- Unavailable required tool/model or authorization: report or ask, never substitute silently.
- Ambiguous one-off versus permanent instruction: ask before editing; preserve other rules.

### Task 1: Shared policy, docs and rendered contract tests

**Files:** Modify `internal/prompts/templates/creative-rules.md`, `internal/prompts/creative_test.go`, `docs/creative-tools.md`.

**Interfaces:** Consume existing `Render(home, role, Vars)` and `tools_home`; produce shared instructions for both Creative roles, with no new Go interfaces.

- [x] Add a table-driven test for both roles and custom homes, asserting the resolved path, read timing and full policy contract.
- [x] Run `go test ./internal/prompts -run TestCreativePromptsReadLiveRules -count=1`; expect missing-policy failures.
- [x] Add the shared policy and user documentation within the brief's scope.
- [x] Run `go test ./internal/prompts -count=1`, `go test ./...`, `go vet ./...` and `git diff --check`; expect success.
- [ ] Review the full diff against every brief requirement, commit, push and open a single PR against main for creative-rules.
- [ ] Inspect CI, fix any attributable failures, and report the green PR to the orchestrator.

## Verification record

Focused test failed before implementation and passed afterward. Both roles and both custom homes pass. Fresh review found no issues. `go vet ./...` and `git diff --check` passed. The first full Go run hit the existing hook metadata subprocess timeout under concurrent vet; its isolated rerun and the subsequent full `go test ./...` both passed.
