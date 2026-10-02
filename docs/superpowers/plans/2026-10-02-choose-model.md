# Choose Model — Implementation Plan (decomposition, task #5026)

> **For agentic workers:** this plan decomposes the feature into worker PRs. Each worker MUST write its own detailed TDD plan for its slice (superpowers:writing-plans), then execute it with superpowers:test-driven-development and finish with superpowers:verification-before-completion. Steps use checkbox (`- [ ]`) syntax.

**Goal:** A registry of launch profiles (name, agent, model, effort, description) that the human manages, orchestrators query and pick from when spawning workers, with hard server-side restrictions (global enable flag + per-task allowlist) and a per-task orchestrator profile.

**Architecture:** New SQLite table `model_profiles` + snapshot columns on `sessions` and policy columns on `tasks`. One pure policy package (`internal/modelpolicy`) resolves which profile any launch gets; every launch path (spawn, task start) goes through it, restore uses the session snapshot. Agent adapters gain an `Effort` launch field. HTTP API + `rocket models` CLI + web Settings section expose it.

**Tech Stack:** Go (modernc sqlite, net/http, cobra-style CLI in `internal/cli`), React 19 + TanStack Query + Vitest/MSW in `web/`.

**Spec:** `docs/superpowers/specs/2026-10-02-choose-model-design.md` (Russian; workers read it in full).

## Global Constraints

- Migration file: `internal/store/migrations/0021_model_profiles.sql` (next free number; re-check at PR time).
- Profile name regex: `^[a-z0-9][a-z0-9-]{0,39}$`.
- Empty `model` / `effort` = agent default; no flag passed.
- Effort flags: claude-code `--effort <e>`; codex `-c model_reasoning_effort=<e>`.
- Effort allowlists come from `Agent.Efforts()`; claude-code `low,medium,high,xhigh,max`, codex `minimal,low,medium,high` — verify against installed `claude --help` / `codex --help`.
- Registry mutations and task `allowed_profiles` changes are human-only: any request with a caller session → 403 `human_only`.
- Error codes verbatim: `profile_not_found`, `profile_not_allowed`, `no_profiles_allowed`, `agent_unavailable`, `bad_effort`, `human_only`, `profile_in_use`.
- Settings keys: `default_orchestrator_profile`, `default_worker_profile`, seed marker `model_profiles_seeded`.
- `--agent` keeps working everywhere it works today (back-compat).
- `docs/prompts/orchestrator.md` must stay byte-synced with `internal/prompts/templates/orchestrator.md` (`docs_sync_test.go`).
- No CI exists: `make test` must be green locally; paste the tail of its output into the PR body.

## Review Focus

1. Worker session runs `rocket models ls` — must see its **parent feature task's** allowlist, not an empty/unknown set (worker's own task is a subtask).
2. Orchestrator tries `PATCH /v1/tasks/{own}` with `allowed_profiles` or `POST /v1/model-profiles` to widen its own choice — must get 403 `human_only`. (Task A + Task B tests.)
3. Restore of a session whose profile was since disabled or deleted — must come back with the original model/effort snapshot, not fail and not silently switch. (Task B test.)
4. Empty registry (human deleted everything): task start still works (no model, legacy behaviour); spawn returns `no_profiles_allowed` with a clear message; daemon restart does not re-seed. (Task A seed test + Task B start test.)
5. Allowlist names a profile later deleted — `Allowed` silently drops it; if that empties the intersection, spawn returns `no_profiles_allowed` (not "all allowed"). (Task A policy table test.)

---

### Task A: Registry backend — store, seeding, adapters, policy, profile API

Worker: `registry-backend` (claude-code). Branch `feature/choose-model/registry-backend`.

**Files:**
- Create: `internal/store/migrations/0021_model_profiles.sql`, `internal/store/model_profiles.go` (+ `_test.go`)
- Create: `internal/modelpolicy/policy.go` (+ `policy_test.go`)
- Create: `internal/api/model_profiles.go` (+ `_test.go`); register routes where other routes are registered
- Modify: `internal/agent/agent.go` (`LaunchSpec.Effort`, `Agent.Efforts()`), `internal/agent/claudecode/claudecode.go`, `internal/agent/codex/codex.go` (+ their tests); any other `Agent` implementers/fakes in tests
- Modify: `internal/api/agent_kinds.go` (add `efforts` per kind), `internal/api/settings.go` + `internal/store/settings.go` (two default-profile keys with existence validation)
- Modify: `internal/store/tasks.go` / sessions store: read/write new columns (`tasks.allowed_profiles` JSON, `tasks.orchestrator_profile`, `sessions.profile/model/effort`)
- Modify: daemon startup (`internal/daemon/daemon.go`) to call seeding once

**Interfaces (Produces):**
```go
// store
type ModelProfile struct {
    Name, Agent, Model, Effort, Description string
    Enabled bool
    Position int
    CreatedAt, UpdatedAt time.Time
}
func (s *Store) ListModelProfiles() ([]ModelProfile, error)       // ordered position, name
func (s *Store) GetModelProfile(name string) (ModelProfile, error) // ErrNotFound
func (s *Store) CreateModelProfile(p ModelProfile) error
func (s *Store) UpdateModelProfile(p ModelProfile) error
func (s *Store) DeleteModelProfile(name string) error
func (s *Store) SeedModelProfiles(defaultAgent string) error      // idempotent via model_profiles_seeded
func (s *Store) SetTaskAllowedProfiles(taskID int64, names []string) error
// Task gets AllowedProfiles []string, OrchestratorProfile string; Session gets Profile, Model, Effort string

// agent
type LaunchSpec struct{ /* existing */; Effort string }
// Agent interface gains: Efforts() []string

// modelpolicy (pure; no store access — callers pass data in)
type Inputs struct {
    Profiles        []store.ModelProfile // full registry
    TaskAllowed     []string             // feature task allowlist; nil/empty = all enabled
    DefaultWorker   string
    DefaultOrch     string
    DefaultAgent    string
}
func Allowed(in Inputs) []store.ModelProfile
func ResolveWorker(in Inputs, profile, agent string) (store.ModelProfile, error)
func ResolveOrchestrator(in Inputs, profile, agent string) (p store.ModelProfile, ok bool, err error) // ok=false → legacy launch without model
type PolicyError struct{ Code string; Allowed []string } // Code ∈ spec error codes; Error() = "code: allowed: a, b"
```
HTTP (exact JSON, consumed by Task B CLI and Task C web):
- `GET /v1/model-profiles` → `{"profiles":[{name,agent,model,effort,description,enabled,position}]}`
- `POST /v1/model-profiles` body = profile fields; `PATCH /v1/model-profiles/{name}` partial; `DELETE` → 204; all 403 `human_only` with caller session; 409 `profile_in_use` when deleting a default.
- `GET /v1/model-profiles/available` → `{"profiles":[{name,agent,model,effort,description}],"default":"<name or empty>"}`; caller session → its feature task (orchestrator: own task; worker: parent of its subtask); no caller → all enabled, default = `default_worker_profile`.
- `GET /v1/agent-kinds` kinds gain `"efforts":[...]`.
- `GET/PUT /v1/settings` gain `default_orchestrator_profile`, `default_worker_profile`.
- `PATCH /v1/tasks/{id}` accepts `allowed_profiles: string[]` (unknown → 400 `profile_not_found`, caller session → 403 `human_only`); task JSON exposes `allowed_profiles`, `orchestrator_profile`.

- [ ] Worker writes detailed TDD plan for this slice (writing-plans), stored in `docs/superpowers/plans/`.
- [ ] Policy table tests cover every rule in the spec section «Разрешение профиля» + Review Focus 5.
- [ ] Seed tests: seeds once; does not re-seed after delete-all; defaults chosen per `default_agent`.
- [ ] Adapter tests: model+effort, model only, neither — both agents.
- [ ] API tests incl. `human_only` (Review Focus 2) and worker-caller `available` resolving parent task (Review Focus 1).
- [ ] `make test` green; PR opened.

### Task B: Wiring — spawn/start/restore, CLI, prompts, docs

Worker: `launch-wiring` (claude-code). Branch `feature/choose-model/launch-wiring`. **Starts after Task A merges** (rebases on main).

**Files:**
- Modify: `internal/api/sessions.go` (`postSessionRequest.Profile`, resolve via `modelpolicy.ResolveWorker`, PolicyError → 400 with code), `internal/api/tasks.go` (`postTaskStartRequest.Profile`, `AllowedProfiles`; `ResolveOrchestrator`; brainstorm skill by profile's agent; persist `orchestrator_profile`)
- Modify: `internal/session/manager.go` (`SpawnReq` / `SpawnOrchestrator` take resolved profile; fill `LaunchSpec.Model/Effort`; write session snapshot; `Restore` uses snapshot)
- Create: `internal/cli/models.go` (+ test): `rocket models ls [--all] | add | edit | rm | default`
- Modify: `internal/cli/spawn.go` (`--profile`), `internal/cli/task.go` (`task start --profile --allow`, new `task models <id> [--allow|--clear]`), `internal/cli/up.go` (`--profile`), `rocket status` output shows profile
- Modify: `internal/prompts/templates/orchestrator.md` + `docs/prompts/orchestrator.md` (section «Choosing a model» per spec), `internal/prompts/templates/worker.md` only if it mentions agents, `docs/10-agents.md`, `docs/05-state.md`

**Interfaces:** Consumes everything Task A produces. Produces CLI surface exactly as in the spec section «CLI».

- [ ] Worker writes detailed TDD plan for this slice.
- [ ] Tests: spawn with allowed / disallowed / missing profile; `--agent` only; nothing (default); profile+agent mismatch.
- [ ] Tests: task start with profile, without (global default), with empty registry (legacy launch, Review Focus 4); `orchestrator_profile` persisted.
- [ ] Test: restore after profile disabled/deleted keeps snapshot (Review Focus 3).
- [ ] CLI tests for `rocket models *` incl. human_only error message from agent session.
- [ ] Prompt docs sync test passes; `make test` green; PR opened.

### Task C: Dashboard — Models settings, Start modal, task card, session display

Worker: `web-models` (claude-code). Branch `feature/choose-model/web-models`. **Starts after Task A merges** (needs real API; may run in parallel with B).

**Files:**
- Create: `web/src/screens/settings/ModelsSection.tsx` (+ test) — table, add/edit/delete, enable toggle, effort select from `agent-kinds.efforts`, two default selectors
- Modify: `web/src/screens/kanban/StartModal.tsx` (+ test) — Profile select (enabled profiles, description shown, default = `default_orchestrator_profile`) instead of Agent; optional multi-select allowlist; POST `/v1/tasks/{id}/start` with `profile`, `allowed_profiles`
- Modify: task card/screen — show orchestrator profile and allowlist, edit via PATCH
- Modify: `web/src/screens/task/SessionRail.tsx`, `web/src/screens/system/SystemScreen.tsx` — show profile/model next to agent
- Modify: API client/types and MSW handlers

**Interfaces:** Consumes Task A HTTP JSON exactly. Note: `POST /v1/tasks/{id}/start` `profile` field lands in Task B; if C merges first the server ignores unknown fields — acceptable, but C's PR must say so; coordinate merge order B → C if possible.

- [ ] Worker writes detailed TDD plan; Vitest+MSW tests for each screen change; `make test` green; screenshot of Settings → Модели and Start modal in PR body (run the app).

## Merge order

A → (B ∥ C) → final: orchestrator runs `make test` on main, `rocket verify-merge` per subtask, final report.

---

# v2 — Model catalog (spec section «v2: каталог моделей»)

Tasks A–C are merged (#114–#116). v2 adds two more worker PRs. Global Constraints above still apply.

## v2 Global Constraints

- Catalog source order — codex: `codex debug models` (10 s timeout) → `~/.codex/models_cache.json` → builtin; claude-code: newest `~/.claude/cache/model-catalog/*-cc.json` → builtin. Never an error, only `warning`.
- Codex models with `visibility=hide` are excluded. Claude `section=main` → `Main=true`.
- Builtin lists = the exact model sets listed in the spec (snapshot 2026-10-02).
- In-memory cache TTL 10 min; `refresh=1` bypasses it.
- `Agent.Efforts()` widened: codex `minimal, low, medium, high, xhigh, max, ultra`; claude-code unchanged `low, medium, high, xhigh, max`.
- Import names: claude-code → model id; codex → `codex-` + slug with `.`/`_` → `-`; collisions get `-2`, `-3`….
- Imported profiles: `enabled=false`, effort empty, description from catalog (fallback: model name), positioned after existing.

## v2 Review Focus

1. Claude cache file present but with an unknown `version` or a missing `catalog.config.models` — must fall back to builtin with a warning, never 500. (Task D)
2. `codex` binary missing or hanging — 10 s timeout, fall back to cache/builtin; the HTTP request must not hang. (Task D)
3. Profile with alias model `opus` + effort `max` keeps validating (agent-level list); an existing v1 profile must stay editable. (Task D)
4. Import run twice creates nothing the second time; a profile with the same agent+model but different effort counts as existing. (Task D)
5. Switching agent or model in the form clears an effort the new model doesn't support; Haiku disables the effort select. (Task E)

### Task D: Catalog backend + CLI

Worker: `catalog-backend` (claude-code). Branch `feature/choose-model/catalog-backend`.

**Files:**
- Modify: `internal/agent/agent.go` (types `Catalog`, `CatalogModel`; `Catalog(ctx)` in the `Agent` interface; update test fakes)
- Create: `internal/agent/codex/catalog.go` (+ test, fixtures under `testdata/` copied from a real `codex debug models` / `models_cache.json`)
- Create: `internal/agent/claudecode/catalog.go` (+ test, fixture from `~/.claude/cache/model-catalog/*-cc.json`)
- Modify: `internal/agent/codex/codex.go` (`Efforts()` widened)
- Create: `internal/api/model_catalog.go` (+ test): `GET /v1/model-catalog`, `POST /v1/model-profiles/import-catalog`, in-memory TTL cache
- Modify: `internal/api/model_profiles.go`: per-model effort validation via the catalog
- Modify: `internal/cli/models.go` (+ test): `rocket models catalog`, `rocket models import`
- Modify: `docs/10-agents.md`: catalog section

**Interfaces (Produces, consumed by Task E):**
- `GET /v1/model-catalog[?agent=A][&refresh=1]` → `{"agents":[{"agent","source","fetched_at","warning","models":[{"id","name","description","main","efforts","default_effort"}]}]}`
- `POST /v1/model-profiles/import-catalog` with body `{"agent"?: string, "include_legacy"?: bool}` → `{"created":[string],"skipped":[{"model","reason"}]}`; 403 `human_only`; 400 `agent_unavailable`

- [ ] Detailed TDD plan → `docs/superpowers/plans/2026-10-02-choose-model-catalog-backend.md`
- [ ] Tests for v2 Review Focus 1–4 plus everything in the spec's «Тестирование (дополнение)» backend list
- [ ] `make test` green; PR body has the test tail and sample output of `rocket models catalog` from an ISOLATED daemon (`ROCKET_HOME` and `ROCKET_SOCKET` in a tmpdir — never the live socket)

### Task E: Catalog in the dashboard

Worker: `catalog-web` (claude-code). Branch `feature/choose-model/catalog-web`. **Starts after Task D merges.**

**Files:**
- Modify: `web/src/screens/settings/ModelsSection.tsx` (+ test): model combobox («Основные» / «Предыдущие» / «Другая…»), per-model efforts, description prefill, source line + warning, button «Добавить профили для всех моделей» with a «включая предыдущие» checkbox and a result message
- Modify: API client/types/queries + MSW handlers

- [ ] Detailed TDD plan → `docs/superpowers/plans/2026-10-02-choose-model-catalog-web.md`
- [ ] Vitest+MSW tests incl. v2 Review Focus 5
- [ ] `make test` green; screenshots from an isolated daemon in the PR body

## v2 merge order

D → E → final verification on main, report v2.
