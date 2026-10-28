# Kimi Code Adapter Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task by task. Steps use checkbox syntax for tracking.

**Goal:** Add the Kimi Code launch adapter, session journal locator, and first-message queue path for Task 2 of the Kimi Code agent plan.

**Architecture:** The adapter uses the existing agent registry and workspace setup patterns. A shared AGENTS.md helper preserves Codex's skill stripping while allowing Kimi to keep skill instructions. The session manager queues the initial prompt only for agents declaring `FirstMessageByInput`.

**Tech Stack:** Go 1.25, SQLite store, tmux runtime.

**Spec:** `docs/superpowers/specs/2026-10-28-kimi-code-agent-design.md`; parent plan `docs/superpowers/plans/2026-10-28-kimi-code-agent.md` Task 2.

## Global Constraints

- Registry name `kimi-code`, package `internal/agent/kimicode`.
- CLI 2.4.0: absolute binary path, `--auto [-m model]`, three fixed environment variables and optional effort.
- Workspace uses a Rocket block in AGENTS.md without skill stripping; no writes to global Kimi files.
- Journal discovery uses native `sessions/wd_*/session_*/state.json` and `createdAt` at or after `since`.
- Existing agent behavior remains unchanged; `make test` must pass.

## Review Focus

- No Kimi journal exists before the first prompt, so first-message queue insertion must precede runtime launch and preserve FIFO.
- Restore must exclude prior sessions by `state.json.createdAt` and the new process's `Since` bound.
- Binary lookup must work with an empty PATH and a home-local executable.
- Corrupt config or journal lines must not prevent fallback or tail reading.
- Codex AGENTS.md content and skill stripping must remain identical after helper extraction.

---

### Task 1: Optional capabilities and initial queue

**Files:** `internal/agent/agent.go`, `internal/session/manager.go`, `internal/session/manager_test.go`.

**Interfaces:** `FirstMessageByInput`, `TakesFirstMessageByInput`, `BracketedPaster`, `WantsBracketedPaste` with the parent plan's exact signatures.

- [ ] Write capability and Start/Spawn/Restore tests, including unchanged legacy launches and first queued message from `rocket`.
- [ ] Run targeted tests and observe expected failures.
- [ ] Implement optional interfaces and session-manager queue path.
- [ ] Run targeted tests green; commit.

### Task 2: Launch and workspace

**Files:** `internal/agent/kimicode/kimicode.go`, tests; shared AGENTS.md helper in `internal/agent`; `internal/agent/codex/codex.go`; registry imports.

- [ ] Write launch, env, binary fallback, AGENTS.md, skill install/removal tests.
- [ ] Run targeted tests and observe expected failures.
- [ ] Extract the Codex AGENTS.md helper with a strip-skills option, implement Kimi launch and workspace setup, register adapter.
- [ ] Run Kimi and Codex tests green; commit.

### Task 3: Journal locator and catalog

**Files:** `internal/agent/kimicode/wire.go`, `catalog.go`, tests.

- [ ] Write `findSession`, `mainWire`, `agentWires`, `readTail`, catalog fixture and fallback tests.
- [ ] Run targeted tests and observe expected failures.
- [ ] Implement native journal discovery, tail reader, TOML model catalog, builtin fallback; keep activity/chat stubs.
- [ ] Run targeted tests green; commit.

### Task 4: Whole-branch verification and delivery

- [ ] Run `make test`, inspect the diff and use `superpowers:requesting-code-review`.
- [ ] Fix important findings with red/green tests, rerun `make test`.
- [ ] Open PR with `feat(kimi-code):` title and test output; send URL to orchestrator.
