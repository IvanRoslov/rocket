# Choose Model — Task A: Registry Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Registry of launch profiles in SQLite (seeded once), `Effort` in agent adapters, pure policy package `internal/modelpolicy`, and the HTTP API for profiles, default-profile settings and the per-task allowlist.

**Architecture:** Migration `0021_model_profiles.sql` adds `model_profiles` plus policy/snapshot columns on `tasks` and `sessions`. `store` gets CRUD + `SeedModelProfiles`; the daemon seeds on startup. `modelpolicy` is pure (data in, decision out) and is consumed by the API now and by launch paths in Task B. Mutating endpoints are human-only (any caller session → 403 `human_only`).

**Tech Stack:** Go, modernc sqlite, net/http ServeMux patterns.

**Spec:** `docs/superpowers/specs/2026-10-02-choose-model-design.md`; decomposition `docs/superpowers/plans/2026-10-02-choose-model.md` (Task A).

## Global Constraints

- Migration `internal/store/migrations/0021_model_profiles.sql` (next free number — verified: last is 0020).
- Profile name regex `^[a-z0-9][a-z0-9-]{0,39}$`.
- Empty model/effort = agent default, no flag passed.
- claude-code: `--effort <e>`, efforts `low, medium, high, xhigh, max` (verified with `claude --help`).
- codex: `-c model_reasoning_effort=<e>`, efforts `minimal, low, medium, high, xhigh` (codex-cli 0.157.0 binary enum contains none/minimal/low/medium/high/xhigh; `none` deliberately left out, `xhigh` included — documented in task log).
- Error codes verbatim: `profile_not_found`, `profile_not_allowed`, `no_profiles_allowed`, `agent_unavailable`, `bad_effort`, `human_only`, `profile_in_use`.
- Settings keys `default_orchestrator_profile`, `default_worker_profile`, seed marker `model_profiles_seeded`.
- JSON shapes exactly as in the decomposition plan (Task A "HTTP").

## Review Focus

1. Worker caller on `GET /v1/model-profiles/available` resolves to the parent feature task's allowlist — API test.
2. Orchestrator caller widening its choice (`PATCH /v1/tasks/{own}` with `allowed_profiles`, `POST/PATCH/DELETE /v1/model-profiles`, `PUT /v1/settings` with default profile) → 403 `human_only` — API tests.
3. Allowlist naming only deleted profiles → `Allowed` empty → `ResolveWorker` returns `no_profiles_allowed` (not "all") — policy test.
4. Delete-all then re-seed → nothing reappears (marker) — store test.
5. PATCH task with valid status + unknown profile name → 400 and nothing applied (validation before any mutation) — API test.

---

### Task 1: Migration + store (profiles CRUD, seeding, task/session columns)

**Files:**
- Create: `internal/store/migrations/0021_model_profiles.sql`
- Create: `internal/store/model_profiles.go`, `internal/store/model_profiles_test.go`
- Modify: `internal/store/tasks.go` (Task fields, taskColumns, scanTask, `SetTaskAllowedProfiles`, `SetTaskOrchestratorProfile`)
- Modify: `internal/store/sessions.go` (Session fields Profile/Model/Effort in insert/select/update/scan)
- Modify: `internal/store/settings.go` (key constants)

**Produces:**
```go
type ModelProfile struct {
    Name, Agent, Model, Effort, Description string
    Enabled  bool
    Position int
    CreatedAt, UpdatedAt time.Time
}
func (s *Store) ListModelProfiles() ([]ModelProfile, error)       // ORDER BY position, name
func (s *Store) GetModelProfile(name string) (ModelProfile, error) // ErrNotFound
func (s *Store) CreateModelProfile(p ModelProfile) error           // ErrExists on dup
func (s *Store) UpdateModelProfile(p ModelProfile) error           // ErrNotFound
func (s *Store) DeleteModelProfile(name string) error              // ErrNotFound
func (s *Store) SeedModelProfiles(defaultAgent string) error
func (s *Store) SetTaskAllowedProfiles(taskID int64, names []string) error
func (s *Store) SetTaskOrchestratorProfile(taskID int64, name string) error
const SettingDefaultOrchestratorProfile = "default_orchestrator_profile"
const SettingDefaultWorkerProfile       = "default_worker_profile"
const SettingModelProfilesSeeded        = "model_profiles_seeded"
// Task.AllowedProfiles []string (nil when unset), Task.OrchestratorProfile string
// Session.Profile, Session.Model, Session.Effort string
```

- [ ] Step 1: tests — CRUD round trip + ordering (position, name); dup create → ErrExists; get/update/delete missing → ErrNotFound; seed on empty creates `claude-opus, claude-sonnet, codex` with defaults `claude-opus`/`claude-opus` for `claude-code`; seed with `codex` → both defaults `codex`; seed with unknown agent → no defaults set; second seed after deleting all → still empty; seed on non-empty table without marker → no inserts, marker set; task allowlist round trip (nil → `''`, `[]` → `''`, names → JSON); orchestrator_profile round trip; session profile/model/effort round trip through AddSession/GetSession/UpdateSession.
- [ ] Step 2: run `go test ./internal/store/ -run 'ModelProfile|Seed|AllowedProfiles|OrchestratorProfile|SessionProfile'` → FAIL (undefined).
- [ ] Step 3: implement migration (SQL from spec verbatim) and store code. Seeding: in one transaction: if marker exists → return; if table empty → insert three seeds (positions 0,1,2), pick defaults (`claude-opus` if defaultAgent=="claude-code", else first seed with Agent==defaultAgent), set both default keys when found; always set marker `"true"`. Allowlist stored as JSON array or `''` when empty.
- [ ] Step 4: run store tests → PASS; `go test ./internal/store/` whole package PASS.
- [ ] Step 5: commit `feat(store): model_profiles registry, seeding, task/session profile columns`.

### Task 2: Agents — Effort

**Files:** `internal/agent/agent.go`, `claudecode/claudecode.go`, `codex/codex.go` (+ tests); fakes in `internal/api/sessions_test.go`, `internal/api/tasks_brainstorm_skill_test.go`, `internal/session/manager_test.go`, `internal/monitor/monitor_test.go`.

**Produces:** `LaunchSpec.Effort string`; `Agent.Efforts() []string`.

- [ ] Step 1: tests — claudecode: model+effort → `--model opus --effort high` before `--`; model only → no `--effort`; neither → no `--model`/`--effort`. codex: model+effort → `-m gpt-5 -c model_reasoning_effort=high` before `--`; same three cases. `Efforts()` returns exact lists.
- [ ] Step 2: run → FAIL.
- [ ] Step 3: implement; append effort right after model flags. Add `Efforts() []string { return nil }` to every fake.
- [ ] Step 4: `go build ./... && go vet ./...`; `go test ./internal/agent/... ./internal/monitor/ ./internal/session/` → PASS.
- [ ] Step 5: commit `feat(agent): Effort launch field and per-agent effort lists`.

### Task 3: `internal/modelpolicy`

**Files:** create `internal/modelpolicy/policy.go`, `policy_test.go`.

**Produces:**
```go
type Inputs struct {
    Profiles      []store.ModelProfile
    TaskAllowed   []string
    DefaultWorker string
    DefaultOrch   string
    DefaultAgent  string
}
const (CodeNotFound="profile_not_found"; CodeNotAllowed="profile_not_allowed"; CodeNoneAllowed="no_profiles_allowed"; CodeBadRequest="bad_request")
type PolicyError struct{ Code string; Allowed []string; Msg string }
func (e *PolicyError) Error() string // "code" | "code: allowed: a, b" | "code: msg"
func Allowed(in Inputs) []store.ModelProfile
func DefaultWorkerName(in Inputs) string // what ResolveWorker(in,"","") picks, "" if none
func ResolveWorker(in Inputs, profile, agent string) (store.ModelProfile, error)
func ResolveOrchestrator(in Inputs, profile, agent string) (store.ModelProfile, bool, error)
```

Rules (spec «Разрешение профиля»):
- Allowed = enabled ∩ TaskAllowed (if non-empty), sorted (position, name). Unknown names in TaskAllowed are dropped.
- ResolveWorker: profile+agent mismatch → `bad_request`; profile not in registry → `profile_not_found`; not in Allowed → `profile_not_allowed` with names; agent only → first Allowed with that agent else `profile_not_allowed`; nothing → DefaultWorker if in Allowed, else first; Allowed empty → `no_profiles_allowed` (checked first for agent-only/nothing paths; for explicit profile, existence first).
- ResolveOrchestrator: explicit profile → must exist (`profile_not_found`) and be enabled (`profile_not_allowed` with enabled names), agent mismatch → `bad_request`; agent only → first enabled with that agent, none → `ok=false` (legacy launch with that agent); nothing → DefaultOrch if enabled, else first enabled with DefaultAgent, else `ok=false`. TaskAllowed ignored.

- [ ] Step 1: table tests for every rule incl. Review Focus 3 and disabled profile in allowlist.
- [ ] Step 2: run → FAIL. Step 3: implement. Step 4: PASS. Step 5: commit `feat(modelpolicy): pure profile resolution`.

### Task 4: HTTP API

**Files:** create `internal/api/model_profiles.go`, `model_profiles_test.go`; modify `server.go` (register), `agent_kinds.go` (+test), `settings.go` (+test), `tasks.go` (+test), `sessions.go` (response fields); `internal/daemon/daemon.go` (seed call).

Endpoints:
- `GET /v1/model-profiles` → `{"profiles":[{name,agent,model,effort,description,enabled,position}]}`.
- `POST /v1/model-profiles` `{name,agent,model?,effort?,description?,enabled?(default true),position?(default max+1)}` → 201 profile. Errors: caller session → 403 `human_only`; bad name → 400 `bad_request`; unknown agent → 400 `agent_unavailable`; effort not in `Efforts()` → 400 `bad_effort`; dup → 409 `profile_exists`.
- `PATCH /v1/model-profiles/{name}` partial (agent, model, effort, description, enabled, position); effort revalidated against (possibly new) agent; missing → 404 `profile_not_found`.
- `DELETE /v1/model-profiles/{name}` → 204; default of either key → 409 `profile_in_use`; missing → 404 `profile_not_found`.
- `GET /v1/model-profiles/available` → `{"profiles":[{name,agent,model,effort,description}],"default":"..."}`; feature task: orchestrator → its own task (GetTaskBySessionID); worker → that task's parent (if subtask), else itself; no task found / agent / human → TaskAllowed nil. `default` = `modelpolicy.DefaultWorkerName`.
- `GET /v1/agent-kinds` kinds gain `"efforts":[...]` (never null).
- `GET/PUT /v1/settings` gain `default_orchestrator_profile`, `default_worker_profile` (strings; GET returns "" when unset). PUT: caller session → 403 `human_only` when setting either; non-empty must exist else 400 `profile_not_found`; "" deletes the key. Validation of all fields before any write.
- `PATCH /v1/tasks/{id}` accepts `allowed_profiles: string[]`; caller session → 403 `human_only`; unknown names → 400 `profile_not_found` listing them; validated before status/title changes are applied; `[]` clears. Task JSON gains `allowed_profiles` (always array) and `orchestrator_profile`.
- Session JSON gains `profile`, `model`, `effort` (omitempty).
- Daemon: after `store.Open`, `st.SeedModelProfiles(cfg.DefaultAgent)`; error logged, non-fatal.

- [ ] Step 1: tests per endpoint, including: orchestrator header → 403 on POST/PATCH/DELETE profiles, PUT settings defaults, PATCH task allowlist (Review Focus 2); worker caller `available` sees parent's allowlist (Review Focus 1); delete default → 409; PATCH task status+unknown profile → 400 and status unchanged (Review Focus 5); agent-kinds efforts; settings round trip.
- [ ] Step 2: run → FAIL. Step 3: implement. Step 4: `go test ./internal/api/` PASS.
- [ ] Step 5: commit `feat(api): model profile registry, available, defaults, task allowlist`.

### Task 5: Verification + PR

- [ ] `make test` green (go + web); paste tail in PR body.
- [ ] Manual smoke: build binary, run daemon against temp `ROCKET_HOME`? Not needed if API tests cover handlers via `NewHandler`; run `go vet ./...`.
- [ ] `gh pr create` referencing feature choose-model / task #5028.
