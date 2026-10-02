# Choose model v2 — catalog in the dashboard (Task E) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Settings › Модели uses the daemon's model catalog: model picker with groups, per-model efforts, description prefill, source line + warning, and «Добавить профили для всех моделей».

**Architecture:** Two new React Query hooks (`useModelCatalog`, `useImportCatalog`) over the Task D endpoints; MSW handlers + fixtures mirror them. `ModelsSection.tsx` gains a `ModelPicker` (native `<select>` with `<optgroup>`s plus a «Другая…» text input), a `CatalogSource` line and an `ImportCatalog` block. Pure helpers (efforts for agent+model, source label) live in `web/src/lib/catalog.ts`.

**Tech Stack:** React 19, TanStack Query 5, Vitest + Testing Library + MSW 2.

**Spec:** `docs/superpowers/specs/2026-10-02-choose-model-design.md` § «v2: каталог моделей» → «Дашборд»; master plan `docs/superpowers/plans/2026-10-02-choose-model.md` § Task E.

## Global Constraints

- **Spec v3 (#123): dashboard copy is English**, including the v1 screens shipped in #116 (Settings › Models, StartModal, task Overview allowlist panel, `profileErrorText`). The Russian names below are descriptive; the shipped copy is «Main» / «Previous» / «Other…», «Model list for <agent>: from Codex CLI / from Claude Code cache / built-in», «Refresh», «Add profiles for all models», «include previous models», «Created: N (…)» / «No new models — …».

- API: `GET /v1/model-catalog` → `{agents:[{agent, source, fetched_at, warning, models:[{id, name, description, main, efforts, default_effort}]}]}`; `efforts` is never null.
- API: `POST /v1/model-profiles/import-catalog` `{agent?, include_legacy?}` → `{created:[string], skipped:[{model, reason}]}`; 403 `human_only`, 400 `agent_unavailable`.
- Effort list: model found in its agent's catalog (exact id) → that model's `efforts`; empty list → effort select disabled, value `''`. Model not in catalog (alias, custom, empty) → agent's `efforts` from `/v1/agent-kinds`.
- Codex agent-level efforts widened: `minimal, low, medium, high, xhigh, max, ultra` (mock must match).
- Copy: groups «Основные» / «Предыдущие», last item «Другая…»; source «Список моделей: из Codex CLI / из кэша … / встроенный»; button «Добавить профили для всех моделей», checkbox «включая предыдущие». Imported profiles are disabled.
- Description prefill only when the description field is empty.

## Review Focus

1. Switching model clears an effort the new model lacks; Haiku (empty efforts) disables the select. → Task 2 test.
2. Switching agent clears an unsupported effort, and a catalog model of the old agent becomes «Другая…» for the new one. → Task 2 test.
3. Editing a v1 profile with alias `opus` opens with «Другая…» + `opus` in the text box and agent-level efforts, and saves unchanged. → Task 2 test.
4. Catalog request failing must not break the form: picker falls back to the plain text input / «Другая…», efforts from agent. → Task 2 test.
5. Import with nothing new says so instead of an empty list; import error shown in the block. → Task 3 test.

---

### Task 1: API layer — types, hooks, MSW

**Files:**
- Modify: `web/src/lib/types.ts` (add `CatalogModel`, `AgentCatalog`, `ImportCatalogResult`)
- Modify: `web/src/lib/queries.ts` (add `useModelCatalog`, `useImportCatalog`)
- Create: `web/src/lib/catalog.ts` (+ `catalog.test.ts`): `findCatalogModel`, `effortsFor`, `catalogSourceText`
- Modify: `web/src/mocks/fixtures.ts` (`modelCatalog`), `web/src/mocks/handlers.ts` (GET catalog, POST import mutating `profilesState`; codex efforts widened)

**Interfaces (Produces):**
```ts
export interface CatalogModel { id: string; name: string; description: string; main: boolean; efforts: string[]; default_effort: string }
export interface AgentCatalog { agent: string; source: 'cli' | 'cache' | 'builtin' | string; fetched_at: string | null; warning: string; models: CatalogModel[] }
export interface ImportCatalogResult { created: string[]; skipped: { model: string; reason: string }[] }
useModelCatalog(): UseQueryResult<AgentCatalog[]>            // key ['model-catalog']
useImportCatalog(): UseMutationResult<ImportCatalogResult, Error, { include_legacy: boolean }>  // invalidates ['model-profiles']
findCatalogModel(cat: AgentCatalog | undefined, model: string): CatalogModel | undefined
effortsFor(agentEfforts: string[], cat: AgentCatalog | undefined, model: string): { efforts: string[]; disabled: boolean }
catalogSourceText(c: AgentCatalog): string   // e.g. 'из кэша Claude Code'
```

- [ ] Step 1: failing unit tests in `catalog.test.ts` for `effortsFor` (catalog model, Haiku → disabled, alias → agent list, undefined catalog → agent list) and `catalogSourceText` (cli/cache per agent, builtin).
- [ ] Step 2: run `cd web && npx vitest run src/lib/catalog.test.ts` → FAIL.
- [ ] Step 3: implement types, helpers, hooks, fixtures, handlers.
- [ ] Step 4: run → PASS; commit.

### Task 2: Model picker, per-model efforts, description prefill

**Files:** Modify `web/src/screens/settings/ModelsSection.tsx`, `ModelsSection.test.tsx`, `settings.css` if needed.

Behaviour of `ProfileModal`:
- `<select id="profile-model">` labelled «Модель»: option `''` «по умолчанию», `<optgroup label="Основные">` / `<optgroup label="Предыдущие">` with option text `name — description`, value = id; last option «Другая…» (sentinel `__custom__`). When custom, a text input `aria-label="Своя модель"` holds the model.
- Initial mode: model empty → default; model in agent's catalog → that option; otherwise custom.
- Picking a catalog model: set model; drop effort if not in its efforts; if description is empty, fill it with catalog description (fallback name).
- Changing agent: if the current model is not in the new agent's catalog and not empty → custom mode keeping the text; drop unsupported effort.
- Effort select uses `effortsFor`; disabled when the model has no efforts.

Tests (each fails first): groups + «Другая…» options for claude-code; picking Opus 5.5 prefills description and offers its efforts; picking Haiku disables effort and clears `xhigh` (Review Focus 1); switching agent clears effort and goes custom (RF 2); editing `claude-opus` (alias `opus`) shows custom + agent efforts and saves `model: 'opus'` (RF 3); catalog 500 → still can type a custom model and save (RF 4); existing tests adapted (typing model now goes through «Другая…»).

- [ ] Steps: write tests → run (FAIL) → implement → run (PASS) → commit.

### Task 3: Source line and import button

**Files:** same as Task 2.

- Under the table: per agent `Список моделей <agent>: <catalogSourceText>`; warning in `settings-models__warning` next to it; «Обновить» → `useRefreshModelCatalog()` (`GET /v1/model-catalog?refresh=1`, result written into `['model-catalog']`).
- Effort options mark the model's `default_effort` as «<level> — по умолчанию у модели».
- Import block: checkbox «включая предыдущие», button «Добавить профили для всех моделей» (disabled while pending). Result: «Создано: N (a, b). Профили выключены — включите нужные.» or «Новых моделей нет — профили для всех уже есть»; error via `profileErrorText` in `role="alert"`. Profiles list refreshes via invalidation.

Tests: source line + warning rendered from fixture; import sends `{include_legacy:false}` then new rows appear disabled; with checkbox sends `{include_legacy:true}`; second import shows the «нет» message (RF 5); 403 shows an error.

- [ ] Steps: write tests → run (FAIL) → implement → run (PASS) → commit.

### Task 4: Verification

- [ ] `make test` green; `cd web && npx tsc -b` clean.
- [ ] Isolated daemon (`ROCKET_HOME`/`ROCKET_SOCKET` in a tmpdir), dashboard screenshots of the picker, the source line and an import result; attach to the PR body.
