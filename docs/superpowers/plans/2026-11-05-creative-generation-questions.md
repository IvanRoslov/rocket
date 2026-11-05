# Creative paid-generation questions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement gate298 specv10/planv9 deliverable1: discuss paid Creative generation in existing contextual question threads, then wait for explicit Go on the final scope and cost.

**Architecture:** Update the existing Creative orchestrator and worker instructions and their tool/discussion documentation. Existing ask/reply commands supply the conversation; no runtime enforcement or new proposal artifact is introduced.

**Tech Stack:** Markdown instruction templates, Go embedded prompt rendering and existing question tests.

**Spec:** Accepted bounded design sent to creative-work-orch for task #5999 (gate298 specv10/planv9): single call or clearly enumerated batch; intended outputs, tool/provider/model, all transmitted inputs/upload services, count and price/estimate basis; denial/clarification/reduction do not authorize execution; restate changes and await explicit human Go; clarify unknown price before execution; fresh agreement for extra paid attempts/cost; preserve normal versioned artifacts and history.

## Global Constraints

- Behavioral instruction policy only; no new API, gate engine, spend interception, global MCP settings or ordinary development workflow changes.
- Work only in Rocket; no live Creative-session changes, paid calls, configuration changes or restart.
- No separate proposal artifact solely to obtain permission; results remain normal versioned artifacts.
- Human evaluates real cases; automated rendering/question checks establish integration, not agent obedience.

## Review Focus

- Human reduces a batch: restate final scope/cost and wait for explicit Go.
- Unknown price: clarify before execution, rather than merely label unknown.
- Extra variants/retries after Go: fresh agreement for additional paid attempts/cost.
- Interruption: inspect actual completed calls before retrying.
- Pre-generation question without artifact: body text is enough; retain exact versions when discussing existing results.

These are prose acceptance scenarios reviewed manually. No new string-presence tests: they cannot establish agent behavior. Existing tests cover prompt rendering, contextual question/reply payloads, history and restoration.

### Task 1: Align Creative consent instructions and documentation

**Files:**
- Modify: `internal/prompts/templates/creative-orchestrator.md`
- Modify: `internal/prompts/templates/creative-worker.md`
- Modify: `docs/creative-tools.md`
- Modify: `docs/creative-agents.md`

**Interfaces:** Consumes existing `rocket creative agent ask/reply/questions` and `rocket tools list/show`; produces behavioral instructions for both roles, no changed CLI/API contracts.

- [x] Read current templates, discovery instructions and contextual question CLI; verify isolated worktree and run focused baseline tests.
- [x] Replace the paid-generation paragraph identically in both templates with explicit question-thread proposal, editable batch, updated scope/cost then Go, and unknown-price clarification rules.
- [x] Align creative-tools policy and creative-agents examples; show inline proposal text and existing reply command, with no mandatory proposal file or artifact.
- [x] Review the five scenarios above against both roles and docs; ensure normal publication/history remains intact.
- [x] Run existing focused rendered-prompt and question tests, then `go test ./internal/prompts ./internal/cli ./internal/api` and `git diff --check`; expect PASS/exit 0.
- [ ] Review the complete diff, commit and open one PR referencing creative-work and gate298; report verification limits and PR URL.
