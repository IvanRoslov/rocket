# Choose Model — Dashboard (Task C) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The dashboard manages the model-profile registry (Settings → Модели), picks the orchestrator profile and the worker allowlist on Start ▸, edits a feature task's allowlist on the task screen, and shows each session's profile/model/effort.

**Architecture:** Thin React screens over the Task A HTTP API (`/v1/model-profiles`, `/v1/settings` default-profile fields, `/v1/agent-kinds` efforts, `PATCH /v1/tasks/{id}` `allowed_profiles`, session `profile/model/effort`). TanStack Query hooks in `lib/queries.ts`, types in `lib/types.ts`, one shared label helper in `lib/profiles.ts`, MSW handlers with mutable state for tests.

**Tech Stack:** React 19, TanStack Query 5, Vitest 4 + Testing Library + MSW 2.

**Spec:** `docs/superpowers/specs/2026-10-02-choose-model-design.md` (section «Дашборд (web)», «API»); decomposition `docs/superpowers/plans/2026-10-02-choose-model.md` Task C.

## Global Constraints

- JSON exactly as Task A serves it (verified in `internal/api/model_profiles.go`, `settings.go`, `tasks.go`, `sessions.go`, `agent_kinds.go`):
  - `GET /v1/model-profiles` → `{"profiles":[{name,agent,model,effort,description,enabled,position}]}`; `POST` → 201 profile; `PATCH /v1/model-profiles/{name}` partial → 200 profile; `DELETE` → 204; errors `{error:{code,message}}` (`profile_in_use` 409, `bad_effort`, `profile_exists` 409, `human_only` 403).
  - `GET/PUT /v1/settings` carry `default_orchestrator_profile`, `default_worker_profile` (`""` = unset).
  - agent kinds carry `efforts: string[]`.
  - task JSON: `allowed_profiles: string[]` (`[]` = all enabled), `orchestrator_profile: string`; `PATCH /v1/tasks/{id}` takes `allowed_profiles`.
  - session JSON: `profile?`, `model?`, `effort?` (omitempty).
- `POST /v1/tasks/{id}/start` body: `{profile?, allowed_profiles?}` — fields omitted when not chosen. The `profile`/`allowed_profiles` handling lands in Task B; until then the daemon ignores them (PR must say so).
- Empty model/effort mean "agent default" — UI shows `default`, never sends a made-up value.
- Profile name regex `^[a-z0-9][a-z0-9-]{0,39}$` (server validates; client only trims).
- `make test` green before PR.

## Review Focus

1. Empty registry (human deleted all): Start modal falls back to the legacy «Агент» select (sends `{agent}` or nothing); Models section shows an empty state with Add. → StartModal test "empty registry".
2. Deleting a profile that is a default → 409 `profile_in_use` message shown in the section, row stays. → ModelsSection test.
3. Changing a profile's agent in the editor must reset an effort not valid for the new agent (else 400 `bad_effort`). → ModelsSection test.
4. Allowlist naming a profile since deleted: task panel still renders the stale name (marked) and Save can drop it. → TaskModelsPanel test.
5. Sessions without profile (pre-feature, legacy launch): rail/system show only the agent, no empty parentheses. → profiles.ts unit test.

---

### Task 1: API types, hooks, MSW state, label helper

**Files:**
- Modify: `web/src/lib/types.ts` (Session, Task, Settings, AgentKind, new ModelProfile/ModelProfiles)
- Modify: `web/src/lib/queries.ts` (useModelProfiles, useCreateModelProfile, useUpdateModelProfile, useDeleteModelProfile; useUpdateSettings payload; useUpdateTask payload; useStartTask payload)
- Create: `web/src/lib/profiles.ts` + `web/src/lib/profiles.test.ts`
- Modify: `web/src/mocks/fixtures.ts` (modelProfiles, settings defaults, orchestrator session profile snapshot, task 12 allowlist)
- Modify: `web/src/mocks/handlers.ts` (model-profile CRUD with `resetModelProfiles()`, settings default fields, agent-kinds efforts, task PATCH allowed_profiles, start body)

**Produces:**
```ts
export interface ModelProfile { name: string; agent: string; model: string; effort: string; description: string; enabled: boolean; position: number }
export function useModelProfiles(): UseQueryResult<ModelProfile[]>            // key ['model-profiles']
export function useCreateModelProfile(): UseMutationResult<ModelProfile, Error, ModelProfileInput>
export function useUpdateModelProfile(): UseMutationResult<ModelProfile, Error, { name: string } & Partial<ModelProfileInput & { enabled: boolean }>>
export function useDeleteModelProfile(): UseMutationResult<void, Error, string>
export type ModelProfileInput = { name: string; agent: string; model: string; effort: string; description: string }
export function sessionModelLabel(s: Pick<Session,'profile'|'model'|'effort'>): string   // '' when nothing
export function profileSummary(p: Pick<ModelProfile,'agent'|'model'|'effort'>): string    // "claude-code · opus · high"
```

- [ ] Step 1: failing test `lib/profiles.test.ts`:
```ts
import { expect, test } from 'vitest'
import { profileSummary, sessionModelLabel } from './profiles'
test('sessionModelLabel', () => {
  expect(sessionModelLabel({})).toBe('')
  expect(sessionModelLabel({ profile: 'claude-opus', model: 'opus', effort: 'high' })).toBe('claude-opus (opus, high)')
  expect(sessionModelLabel({ profile: 'codex' })).toBe('codex')
  expect(sessionModelLabel({ model: 'opus' })).toBe('opus')
})
test('profileSummary', () => {
  expect(profileSummary({ agent: 'codex', model: '', effort: '' })).toBe('codex · default model')
  expect(profileSummary({ agent: 'claude-code', model: 'opus', effort: 'high' })).toBe('claude-code · opus · high')
})
```
- [ ] Step 2: run `npx vitest run src/lib/profiles.test.ts` → FAIL (module missing).
- [ ] Step 3: implement `profiles.ts`:
```ts
export function sessionModelLabel(s: { profile?: string; model?: string; effort?: string }): string {
  const detail = [s.model, s.effort].filter(Boolean).join(', ')
  if (!s.profile) return detail
  return detail ? `${s.profile} (${detail})` : s.profile
}
export function profileSummary(p: { agent: string; model: string; effort: string }): string {
  return [p.agent, p.model || 'default model', p.effort].filter(Boolean).join(' · ')
}
```
- [ ] Step 4: types + hooks + fixtures + handlers (no behaviour test of its own beyond tsc; exercised by Tasks 2–4). Hooks: create/update/delete invalidate `['model-profiles']`; delete uses `api.delete`. `useUpdateSettings` payload gains the two default fields. `useUpdateTask` payload gains `allowed_profiles?: string[]`. `useStartTask` variables `{ id; profile?; allowed_profiles? }`, body built with only set fields, `undefined` when empty.
- [ ] Step 5: `npx vitest run src/lib && npx tsc -b` → pass; commit `feat(web): model profile types, hooks and mocks (task-5030)`.

### Task 2: Settings → Модели section

**Files:** Create `web/src/screens/settings/ModelsSection.tsx`, `ModelsSection.test.tsx`; modify `SettingsScreen.tsx` (nav item `{ key: 'models', label: 'Модели' }`), `settings.css` (table styles).

Behaviour:
- Table rows (registry order): name (mono), agent, model (`default` muted when empty), effort (`default`), description, «Включён» checkbox (PATCH `{enabled}`), Edit, Delete (window.confirm → DELETE; error line under table, e.g. profile_in_use message).
- «Добавить профиль» opens `ProfileModal` (name input, agent select over agent-kinds, model input, effort select = `['', ...efforts of chosen agent]` with `''` labelled «по умолчанию», description textarea). Edit opens same modal with name read-only. Changing agent resets effort to `''` if not in the new agent's efforts. Save → POST / PATCH; server error shown in modal.
- Two selects «Профиль оркестратора по умолчанию» / «Профиль воркера по умолчанию» over all profiles (disabled ones labelled `— выключен`), plus `''` «не задан»; change → PUT settings with that single field.
- Empty registry → «Профилей нет — добавьте первый.»

- [ ] Step 1: failing tests (MSW): lists seeded profiles; toggle enabled sends `{enabled:false}`; add flow sends `{name,agent,model,effort,description}` and the row appears; effort options follow agent (claude-code → low…max; switching agent to codex resets effort); delete of default shows `profile_in_use` message; default worker select sends `{default_worker_profile:'claude-sonnet'}`.
- [ ] Step 2: run → FAIL.
- [ ] Step 3: implement.
- [ ] Step 4: run → PASS; commit `feat(web): Settings › Модели — profile registry and defaults (task-5030)`.

### Task 3: Start modal — profile picker + allowlist

**Files:** Modify `web/src/screens/kanban/StartModal.tsx`, `StartModal.test.tsx`, `kanban.css` (checkbox list).

Behaviour:
- «Профиль» select: first option `''` = `По умолчанию (<default_orchestrator_profile>)` (or `По умолчанию` when unset); then enabled profiles; a profile whose agent is unavailable (agent-kinds `available:false`) is disabled with the error as title. Under the select: description of the effective profile (selected, else the default) + `profileSummary`.
- «Разрешённые профили для воркеров» checkbox list over enabled profiles; hint «Ничего не отмечено — все включённые». 
- Submit sends `{profile?, allowed_profiles?}` (only set fields; no body when neither).
- Empty registry (no enabled profiles): fall back to the legacy «Агент» select over agent-kinds (sends `{agent}` or no body), hint «Реестр профилей пуст — выбор агента».

- [ ] Step 1: rewrite tests: lists enabled profiles only, default label names global default, disabled profile absent; shows description; sends `{profile:'claude-sonnet', allowed_profiles:['claude-sonnet','codex']}`; sends empty body on defaults; empty registry shows the agent select and sends `{agent:'claude-code'}`.
- [ ] Step 2: run → FAIL. Step 3: implement. Step 4: PASS; commit `feat(web): Start modal picks orchestrator profile and worker allowlist (task-5030)`.

### Task 4: Task screen allowlist + session profile display

**Files:** Create `web/src/screens/task/TaskModelsPanel.tsx` (+ styles in `OverviewTab.css`), test in `web/src/screens/task/TaskModelsPanel.test.tsx`; modify `OverviewTab.tsx` (render panel for root, non-milestone tasks), `SessionRail.tsx` (+ test), `SystemScreen.tsx` (+ test).

Behaviour:
- Panel «Модели»: «Оркестратор: <orchestrator_profile or —>»; «Воркерам разрешены: все включённые» or chips of names (a name missing from the registry gets `title="профиль удалён"` and a muted chip). «Изменить» → checkbox list over all registry profiles plus stale names (checked); Save → PATCH `{allowed_profiles:[...]}` (`[]` when none checked); error shown.
- SessionRail: next to agent, `sessionModelLabel(s)` in a muted span (`session-rail__model`) when non-empty — orchestrator and workers. SystemScreen: same in row2 (`session-card__model`).

- [ ] Step 1: failing tests: panel shows profile + chips; edit+save PATCH body; clearing all sends `[]`; stale name shown and droppable; rail shows `claude-opus (opus, high)` for fixture orchestrator; system row shows it.
- [ ] Step 2: FAIL. Step 3: implement. Step 4: PASS; commit `feat(web): task allowlist panel, profile/model shown on sessions (task-5030)`.

### Task 5: Verification and PR

- [ ] `cd web && npx tsc -b && npx vitest run`; `make test` at repo root.
- [ ] Run the app (`make` build + daemon, or `vite` against the running daemon) and screenshot Settings → Модели and the Start modal for the PR body.
- [ ] `gh pr create` referencing feature choose-model; note the B-dependency for `/start` fields.
