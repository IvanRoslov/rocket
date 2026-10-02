# Choose Model v2 — Catalog Backend + CLI Implementation Plan (Task D, task #5026)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rocket discovers each agent's model catalog (codex CLI → codex cache → builtin; Claude Code cache → builtin), serves it at `GET /v1/model-catalog`, validates profile effort per model, bulk-imports catalog models as disabled profiles, and exposes `rocket models catalog|import`.

**Architecture:** `agent.Agent` gains `Catalog(ctx)`; each adapter owns a `catalog.go` with its parsers, fallback chain and builtin snapshot. The API keeps a per-handler in-memory TTL cache (`catalogCache`, 10 min) in front of the adapters; profile validation and import read through it. CLI is a thin client.

**Tech Stack:** Go 1.25, net/http ServeMux, cobra, modernc sqlite store (existing).

**Spec:** `docs/superpowers/specs/2026-10-02-choose-model-design.md` §«v2: каталог моделей»; decomposition `docs/superpowers/plans/2026-10-02-choose-model.md` §Task D (its Interfaces block is the contract).

## Global Constraints

- Source order — codex: `codex debug models` (10 s timeout, `WaitDelay` so a child holding stdout cannot hang it) → `$CODEX_HOME|~/.codex/models_cache.json` → builtin; claude-code: newest-by-mtime `~/.claude/cache/model-catalog/*-cc.json` → builtin. `Catalog` never returns an error for a bad source, only `Warning`.
- Codex `visibility=hide` dropped; all shown codex models `Main=true`. Claude `section=main` → `Main=true`.
- Claude cache: `version` must be `2` and `catalog.config.models` non-empty, else builtin + warning.
- Claude DefaultEffort = effort option whose `badge.message == "Recommended"`; codex DefaultEffort = `default_reasoning_level`.
- Builtin lists = spec snapshot 2026-10-02 (codex 7 visible; claude 4 main + 7 overflow), efforts copied from the real fixtures.
- `Agent.Efforts()` codex widened to `minimal, low, medium, high, xhigh, max, ultra`; claude-code unchanged.
- TTL 10 min; `refresh=1` bypasses and refills.
- Effort validation: model found (exact id) in its agent's catalog → effort ∈ model.Efforts (empty Efforts ⇒ effort must be empty); else agent-level `Efforts()`. Catalog consulted only when both model and effort are non-empty.
- Import names: claude-code → id; codex → `codex-` + slug with `.`/`_` → `-`; collision → `-2`, `-3`…; enabled=false, effort "", description = catalog description or model name, positions after existing max.
- Errors: unknown agent → 400 `agent_unavailable`; import from agent session → 403 `human_only`.
- Tests never touch real `~/.claude`, `~/.codex` or the real `codex` binary: adapters read `$HOME`/`$CODEX_HOME` (tests `t.Setenv`), codex command runner is a package var; the api package's `TestMain` swaps the catalog fetcher for a fake.
- E2E only with `ROCKET_HOME=<tmp>` and `ROCKET_SOCKET=<tmp>/rocket.sock`.

## Review Focus

1. Claude cache with unknown `version` / no `catalog.config.models` / broken JSON → builtin + warning, no 500 (Task 2 tests).
2. `codex` missing or hanging → timeout, falls back to cache/builtin; HTTP does not hang (Task 1 test with a hanging runner + ctx deadline).
3. Alias model `opus` + effort `max` still valid; v1 profile still PATCHable (Task 4 tests).
4. Import twice → second run creates nothing; same agent+model with different effort counts as existing (Task 5 tests).
5. Concurrent requests during a slow catalog fetch do not deadlock other agents' lookups (cache does not hold its lock while fetching — Task 3 design + test).

---

### Task 1: agent types + codex catalog

**Files:**
- Modify: `internal/agent/agent.go` — add `Catalog`, `CatalogModel`, `Catalog(ctx)` to `Agent`
- Modify fakes: `internal/monitor/monitor_test.go`, `internal/api/sessions_test.go`, `internal/session/manager_test.go` (add `Catalog` returning zero value)
- Create: `internal/agent/codex/catalog.go`, `internal/agent/codex/catalog_test.go`; fixtures `testdata/debug_models.json`, `testdata/models_cache.json` (real, slimmed, no identity/etag)
- Modify: `internal/agent/codex/codex.go` `Efforts()`; `effort_test.go`

**Interfaces — Produces:**
```go
type Catalog struct {
    Source    string    // "cli" | "cache" | "builtin"
    FetchedAt time.Time
    Warning   string
    Models    []CatalogModel
}
type CatalogModel struct {
    ID, Name, Description string
    Main          bool
    Efforts       []string
    DefaultEffort string
}
// codex package
var runDebugModels = func(ctx context.Context) ([]byte, error) // exec codex debug models
const catalogTimeout = 10 * time.Second
func parseCodexModels(b []byte) ([]agent.CatalogModel, error) // drops hide; error if no models
func (c *Codex) Catalog(ctx context.Context) (agent.Catalog, error)
```

- [ ] Tests (fail first): parse fixture → 7 models in order `gpt-6-astra, gpt-6-sol, gpt-6-luna, gpt-5.6-sol, gpt-5.6-terra, gpt-5.6-luna, gpt-5.5`, `gpt-6-astra` efforts `low…ultra`, default `medium`, `gpt-5.5` efforts `low,medium,high,xhigh`; CLI ok → source `cli`; CLI error + cache → `cache` with FetchedAt from `fetched_at` and warning mentioning `codex debug models`; CLI error + broken cache → `builtin` + warning; CLI hanging (runner blocks on ctx) returns within the timeout (timeout var lowered in test); builtin list equals spec set; `Efforts()` widened.
- [ ] Implement; `go test ./internal/agent/...`; commit.

### Task 2: claude-code catalog

**Files:** Create `internal/agent/claudecode/catalog.go`, `catalog_test.go`, `testdata/catalog-cc.json` (real, `state` stripped).

**Interfaces — Produces:** `func (c *ClaudeCode) Catalog(ctx) (agent.Catalog, error)`; `func parseClaudeCatalog(b []byte) ([]agent.CatalogModel, time.Time, error)`.

- [ ] Tests: fixture → 11 models, 4 main; Haiku `Efforts` empty; `claude-sonnet-4-6` has no `xhigh`; opus-5-5 default `medium`; overflow description empty; source `cache`, FetchedAt = fetchedAt ms. Newest of two files by mtime wins. version 3 → builtin+warning; missing models → builtin+warning; broken JSON → builtin+warning; no dir → builtin + warning. Builtin set equals spec.
- [ ] Implement; test; commit.

### Task 3: GET /v1/model-catalog + cache

**Files:** Create `internal/api/model_catalog.go`, `model_catalog_test.go`, `main_test.go` (TestMain fake fetcher). Modify `internal/api/server.go` (Deps.Catalogs, NewHandler default, register routes).

**Interfaces — Produces:**
```go
type catalogFetcher func(ctx context.Context, agentName string) (agent.Catalog, error)
var fetchAgentCatalog catalogFetcher // default: agent.Get(name).Catalog(ctx)
type CatalogCache struct { /* mu, ttl, now, fetch, entries */ }
func NewCatalogCache(fetch catalogFetcher) *CatalogCache
func (c *CatalogCache) Get(ctx context.Context, agentName string, refresh bool) agent.Catalog
// JSON
type catalogModelJSON struct{ ID, Name, Description string; Main bool; Efforts []string; DefaultEffort string } // json: id,name,description,main,efforts([] never null),default_effort
type agentCatalogJSON struct{ Agent, Source string; FetchedAt *time.Time `json:"fetched_at"` (null for zero); Warning string; Models []catalogModelJSON }
// GET /v1/model-catalog[?agent=A][&refresh=1] → {"agents":[agentCatalogJSON sorted by agent]}
```
- [ ] Tests: all agents sorted; `?agent=codex` only codex; unknown → 400 agent_unavailable; second call within TTL does not refetch, `refresh=1` does, after TTL refetches (injected `now`); fetch error → entry with `source:"builtin"`, warning, empty models; efforts `[]` not null; agent session may read.
- [ ] Implement; test; commit.

### Task 4: per-model effort validation

**Files:** Modify `internal/api/model_profiles.go` (`validateProfileAgent(w, r, d, p)`), `model_profiles_test.go`.

- [ ] Tests (fake catalog: claude-code has `claude-opus-5-5` [low..max], `claude-haiku-4-5-20251001` []; `claude-sonnet-4-6` [low,medium,high,max]): POST opus-5-5+xhigh ok; sonnet-4-6+xhigh → 400 bad_effort listing `low, medium, high, max`; haiku+low → 400 bad_effort "has no effort"; haiku+"" ok; alias `opus`+max ok; custom `my-model`+ultra for claude-code → bad_effort (agent-level); codex no model + `max` ok (widened); PATCH v1 seeded profile description only → 200.
- [ ] Implement; test; commit.

### Task 5: POST /v1/model-profiles/import-catalog

**Files:** `internal/api/model_catalog.go`, test.

**Produces:** body `{"agent"?: string, "include_legacy"?: bool}` (empty body allowed) → 200 `{"created":[names],"skipped":[{"model","reason"}]}`; `importProfileName(agent, id string) string`.

- [ ] Tests: creates disabled profiles named per rule, empty effort, description fallback to name, positions after max; second run created=[] and all skipped; existing profile with same agent+model but different effort → skipped; name collision → `-2`; `include_legacy` adds overflow; agent filter; unknown agent 400; agent session 403 human_only.
- [ ] Implement; test; commit.

### Task 6: CLI `rocket models catalog|import` + docs

**Files:** `internal/cli/models.go`, `internal/cli/models_test.go` (fakeDaemon records RawQuery), `docs/10-agents.md`, `docs/03-daemon-api.md` (if it lists model-profile routes), `docs/04-cli.md` (same).

- [ ] Tests: `catalog` hits `/v1/model-catalog` with `agent=`/`refresh=1` query; hides non-main without `--all`; efforts printed with default marked `*`; footer shows source per agent and warning; `--json` raw. `import` posts `{agent, include_legacy}`, prints created and skipped, maps human_only through `explainHumanOnly`.
- [ ] Implement; docs; `make test`; commit.

### Task 7: verification

- [ ] `make test` green; isolated daemon (`ROCKET_HOME`, `ROCKET_SOCKET` in tmp) → `rocket models catalog --all`, `rocket models import`, capture output; PR.
