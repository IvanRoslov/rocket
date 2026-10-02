# Choose Model — Launch Wiring Implementation Plan (task B, subtask #5029)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every launch path (worker spawn, task start, restore) runs with a model profile resolved by `internal/modelpolicy` (or the session snapshot on restore); the CLI exposes `rocket models`, `--profile` flags and `rocket task models`; prompts and docs teach orchestrators to choose a model.

**Architecture:** `session.Manager` gains a `LaunchProfile{Name, Model, Effort}` that it writes into the session snapshot and `agent.LaunchSpec.Model/Effort`; the API handlers resolve the profile through `modelpolicy` before spawning (policy errors → 400 with the policy code). Restore reads the snapshot only. CLI commands are thin HTTP wrappers whose logic lives in functions taking `*client.Client` so they are tested over a unix-socket `httptest` server.

**Tech Stack:** Go, cobra, net/http, sqlite store (all from Task A).

**Spec:** `docs/superpowers/specs/2026-10-02-choose-model-design.md`; decomposition `docs/superpowers/plans/2026-10-02-choose-model.md` (Task B).

## Global Constraints

- Error codes verbatim: `profile_not_found`, `profile_not_allowed`, `no_profiles_allowed`, `agent_unavailable`, `bad_effort`, `human_only`, `profile_in_use`; agent/profile mismatch → `bad_request`.
- `--agent` keeps working wherever it works today.
- Task allowlist changes (incl. `allowed_profiles` on `/start`) are human-only: caller session → 403 `human_only`.
- Restore never consults the policy: it relaunches with `sessions.model/effort`.
- Empty registry: task start launches legacy (no model, `orchestrator_profile` = ""); spawn → 400 `no_profiles_allowed`.
- `docs/prompts/orchestrator.md` byte-identical to `internal/prompts/templates/orchestrator.md`.
- `make test` green; tail pasted in the PR body.

## Review Focus

1. Existing API spawn tests run on an empty registry — they must be given a `fake` profile, otherwise they hit `no_profiles_allowed` (test deps seed one profile).
2. Policy refusal must happen **before** the auto-created subtask is inserted, so a refused spawn leaves no orphan cancelled subtask.
3. `allowed_profiles` passed to `/start` must be stored **before** the orchestrator spawns: its first `rocket models ls` must already see the narrowed list.
4. Restore of a session whose profile was deleted keeps model/effort (snapshot), never re-resolves.
5. `rocket models ls` from an agent session shows `/available` (feature allowlist); from the human — the registry (enabled only, `--all` adds disabled) with default markers.

---

### Task 1: Manager launches with a profile; restore uses the snapshot

**Files:** Modify `internal/session/manager.go`; tests in `internal/session/manager_test.go`; update all `SpawnOrchestrator(` callers (tests + `internal/api/tasks.go`).

**Produces:**
```go
// LaunchProfile is the resolved model profile a launch runs with; zero value = legacy launch.
type LaunchProfile struct{ Name, Model, Effort string }
// SpawnReq gains: Profile LaunchProfile
func (m *Manager) SpawnOrchestrator(ctx context.Context, task store.Task, project store.Project, agentName string, prof LaunchProfile) (store.Session, error)
```

- [ ] Write failing tests: `TestSpawnWithProfileFillsSpecAndSnapshot` (Spawn with `Profile{Name:"p",Model:"m",Effort:"high"}` → `testFakeAgent.launchCalls[0].Model=="m"`, `.Effort=="high"`, stored session Profile/Model/Effort match); `TestSpawnOrchestratorWithProfile` (same for orchestrator); `TestRestoreUsesSessionSnapshot` (spawn with profile, delete any registry row, kill, Restore → second launch call has Model/Effort of snapshot).
- [ ] Run `go test ./internal/session/ -run 'Profile|Snapshot'` → FAIL (compile).
- [ ] Implement: add type + field; in Spawn/SpawnOrchestrator set `sess.Profile/Model/Effort` and `spec.Model/Effort`; in Restore set `spec.Model = sess.Model; spec.Effort = sess.Effort`; sed existing callers to pass `session.LaunchProfile{}`/`LaunchProfile{}`.
- [ ] `go test ./internal/session/ ./internal/api/` → PASS. Commit.

### Task 2: API — spawn and start resolve through modelpolicy

**Files:** Modify `internal/api/sessions.go`, `internal/api/tasks.go`; tests `internal/api/sessions_profile_test.go`, `internal/api/tasks_start_profile_test.go`; `sessionsTestDeps` seeds profile `fake` (agent fake).

- [ ] Failing tests, spawn (`POST /v1/sessions` as orchestrator): allowed profile → 201, session snapshot = profile; disallowed (task allowlist excludes) → 400 `profile_not_allowed` with allowed names in message and no new subtask; missing → 400 `profile_not_found`; `agent` only → first allowed of agent; nothing → `default_worker_profile`; profile+agent mismatch → 400 `bad_request`; empty registry → 400 `no_profiles_allowed`.
- [ ] Failing tests, start: `profile` given → session snapshot + `tasks.orchestrator_profile`; none → `default_orchestrator_profile`; empty registry → 201, no model, `orchestrator_profile` ""; `allowed_profiles` stored; unknown name → 400 `profile_not_found`; `allowed_profiles` from an agent caller → 403 `human_only`; disabled profile → 400 `profile_not_allowed`.
- [ ] Implement spawn: after `findRootTaskForSession`, `in, _ := policyInputs(d, &root)`; `p, err := modelpolicy.ResolveWorker(in, req.Profile, req.Agent)`; on `*modelpolicy.PolicyError` → `writeErr(400, pe.Code, pe.Error())`; pass `AgentName: p.Agent, Profile: session.LaunchProfile{p.Name, p.Model, p.Effort}`. Shared helper `writePolicyErr(w, err) bool`.
- [ ] Implement start: request gains `Profile string`, `AllowedProfiles *[]string`; validate allowlist (human-only, known names) and store via `SetTaskAllowedProfiles` before spawning; `ResolveOrchestrator(in, req.Profile, req.Agent)`; ok → agentName = p.Agent; brainstorm skill by that agent; after spawn `SetTaskOrchestratorProfile(task.ID, p.Name)`.
- [ ] `go test ./internal/api/` → PASS. Commit.

### Task 3: CLI — `rocket models`, `--profile`, `task models`, status

**Files:** Create `internal/cli/models.go` (+ `models_test.go`); modify `spawn.go`, `task.go`, `up.go`, `status.go`, `sessions.go` (`sessionRow.Profile`), `root.go`.

- [ ] Failing tests over `newUnixSocketTestServer`: `runModelsLs` agent-mode (calls `/available`, prints NAME AGENT MODEL EFFORT DESCRIPTION with `*` default marker); human-mode filters disabled unless `all`; `runModelsAdd/Edit/Rm/Default` send the right method/path/body; a `human_only` APIError is rewritten to a clear message; `taskModels` GET shows allowlist and PATCHes `--allow a,b` / `--clear` (`[]`); flag presence of `--profile` on spawn/up/task start and `--allow` on task start; `renderStatus` shows profile.
- [ ] Implement; `go test ./internal/cli/` → PASS. Commit.

### Task 4: Prompts and docs

**Files:** `internal/prompts/templates/orchestrator.md`, `docs/prompts/orchestrator.md`, `docs/10-agents.md`, `docs/05-state.md`.

- [ ] Replace the «Which agent a worker runs on…» bullet with a `## Choosing a model` section (run `rocket models ls`; pick by description; spawn with `--profile`; record profile in subtask log; honour human requests; `profile_not_allowed` is a human restriction, not to work around). Copy byte-for-byte to docs. Document registry/policy/CLI in 10-agents.md, tables/columns/settings in 05-state.md.
- [ ] `go test ./internal/prompts/ ./...` (docs_sync_test) → PASS; `make test` → PASS. Commit.
