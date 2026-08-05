# Milestones в дашборде и mobile (M3, подзадача #1033) — план

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:test-driven-development. Шаги отмечаются чекбоксами.

**Goal:** человек видит майлстоны внешних агентов отдельной страницей-канбаном в web и списком в mobile, может назначить агента, провалиться в его терминал/чат и увидеть майлстоны на карточке агента.

**Architecture:** чистый фронт поверх готового API волны M1 — `GET /v1/tasks?milestones=true[&board=true]`, `POST /v1/tasks/{id}/assign`, `agent.milestones`, `POST /v1/tasks {milestone:true}`. Web: новый экран `/milestones` (канбан по образцу `KanbanScreen`) + карточка `MilestoneCard` + деталь через существующий `TaskScreen` на роуте `/milestones/:taskId` (projectId становится необязательным). Mobile: новый таб `milestones` по образцу `app/(tabs)/kanban.tsx` (ChipTabs по статусам), провал в существующие `/task/:id` и `/chat/:agentId`.

**Tech Stack:** React 19 + react-router + @tanstack/react-query + msw + vitest (web); Expo Router + react-query + jest/@testing-library/react-native (mobile).

## Global Constraints

- Бэкенд не трогаем: поле `quiet` появится в M2 (#1032) — рисуем бейдж по **опциональному** `task.quiet`, отсутствие поля = бейджа нет.
- Майлстон — задача без проекта: нигде не строить путей `/p/:projectId/...` для майлстона.
- `take` — только агент из своей сессии; в UI только `assign`/`--none` (человек).
- Сессия агента одна на все майлстоны, её id === id агента (`docs/10-agents.md`), поэтому `/term/:id` и `/chat/:id` принимают id агента как есть.
- Живость: `task.*` уже инвалидирует ключ `['tasks']`, под который попадает и доска майлстонов; в `wireInvalidation` добавить ветку `milestone.` (событие M2) → `['tasks']` + `['agents']`.
- T5 (web-questions) держит файлы вопросных тредов и `web/src/vitest.setup.ts` — их не трогаем.
- Тесты: `cd web && npm test -- --run`, `cd mobile && npm test`, `go test ./...` (должен остаться зелёным — Go не меняем, кроме возможного `web/dist`, который не коммитим).

---

### Task 1: web — типы, queries, моки

**Files:**
- Modify: `web/src/lib/types.ts` (интерфейс `Task`, интерфейс `Agent`)
- Modify: `web/src/lib/queries.ts` (`useMilestonesBoard`, `useAssignMilestone`, `useCreateMilestone`)
- Modify: `web/src/mocks/handlers.ts` (`GET /v1/tasks?milestones=true`, `POST /v1/tasks/:id/assign`, `milestone` в `POST /v1/tasks`)
- Modify: `web/src/mocks/fixtures.ts` (майлстоны, `milestones` у агентов)
- Test: `web/src/lib/queries.test.tsx`

**Interfaces:**
- Produces: `Task.milestone?: boolean`, `Task.assigned_role?: string`, `Task.quiet?: boolean`;
  `Agent.milestones: AgentMilestoneRef[]`, `interface AgentMilestoneRef { id: number; title: string; status: TaskStatus }`;
  `useMilestonesBoard(): UseQueryResult<TaskBoard>` (ключ `['tasks','milestones','board']`);
  `useAssignMilestone(): UseMutationResult<Task, Error, { id: number; agentId: string | null }>` (null → `{none:true}`);
  `useCreateMilestone(): UseMutationResult<Task, Error, { title: string; description?: string }>`.

- [ ] **Шаг 1: тест на useMilestonesBoard и useAssignMilestone** в `queries.test.tsx` (msw отдаёт майлстоны только при `milestones=true`; assign меняет `assigned_role`).
- [ ] **Шаг 2:** прогнать — падает.
- [ ] **Шаг 3:** типы + хуки + хендлеры msw + фикстуры.
- [ ] **Шаг 4:** `npm test -- --run` зелёный.
- [ ] **Шаг 5:** коммит `web: milestones API bindings (types, queries, mocks)`.

### Task 2: web — экран `/milestones` (канбан) и `MilestoneCard`

**Files:**
- Create: `web/src/screens/milestones/MilestonesScreen.tsx`, `MilestoneCard.tsx`, `AssignModal.tsx`, `NewMilestoneModal.tsx`, `milestones.css`
- Modify: `web/src/routes.tsx`, `web/src/components/AppShell.tsx` (пункт меню Milestones)
- Test: `web/src/screens/milestones/Milestones.test.tsx`

**Карточка:** `#id title` (ссылка на `/milestones/:id`), бейдж агента (`◆ cto`) либо `not taken`, бейджи `quiet`, `⏳ waiting for input`, счётчики вопросов (как в `TaskCard`), действия `▣ term` / `💬 chat` (по образцу `AgentCard`, ссылки `termPagePath(assigned_role)` / `chatPagePath(assigned_role)`), `⧉ attach` (копирует `rocket agent attach <id>`), кнопка `Assign`.

**Экран:** те же 4 колонки + Cancelled по чекбоксу, drag-n-drop через `useMoveTask` с откатом и показом ошибки (гейт review отдаёт 422), поиск, `+` в Backlog → `NewMilestoneModal`.

- [ ] **Шаг 1: тесты** — рендер канбана из msw (карточка в нужной колонке, бейдж агента/`not taken`), Assign меняет бейдж, ошибка move показывается и колонка откатывается.
- [ ] **Шаг 2:** прогнать — падает.
- [ ] **Шаг 3:** реализация экрана/карточки/модалок/CSS + роут `/milestones` + пункт меню.
- [ ] **Шаг 4:** тесты зелёные.
- [ ] **Шаг 5:** коммит `web: Milestones kanban page`.

### Task 3: web — деталь майлстона на `/milestones/:taskId`

**Files:**
- Modify: `web/src/screens/task/TaskScreen.tsx` (projectId необязателен: back-ссылка на `/milestones`, ссылки на подзадачи/родителя строятся хелпером), `web/src/routes.tsx`
- Test: `web/src/screens/task/Task.test.tsx`

- [ ] **Шаг 1: тест** — `/milestones/:id` рендерит доки/журнал/треды майлстона и ведёт назад на `/milestones`.
- [ ] **Шаг 2:** прогнать — падает.
- [ ] **Шаг 3:** реализация (никаких `/p/undefined/...`).
- [ ] **Шаг 4:** тесты зелёные.
- [ ] **Шаг 5:** коммит `web: milestone detail route`.

### Task 4: web — майлстоны на карточке агента

**Files:**
- Modify: `web/src/screens/agents/AgentScreen.tsx` (секция «Milestones» со ссылками на `/milestones/:id` и статусами)
- Test: `web/src/screens/agents/AgentScreen.test.tsx`

- [ ] **Шаг 1: тест** — карточка агента показывает его майлстоны и ссылку на каждый; пустой список — короткая заглушка.
- [ ] **Шаг 2–5:** прогнать (падает) → реализовать → зелёные → коммит `web: agent card lists its milestones`.

### Task 5: mobile — типы, queries, таб Milestones

**Files:**
- Modify: `mobile/src/api/types.ts` (`Task.milestone/assigned_role/quiet`, `Agent.milestones`), `mobile/src/api/queries.ts` (`useMilestones`, `useAssignMilestone`), `mobile/app/(tabs)/_layout.tsx`
- Create: `mobile/app/(tabs)/milestones.tsx`, `mobile/__tests__/milestones/milestones.test.tsx`

**Экран:** ChipTabs по статусам (как в `kanban.tsx`), карточка: `#id title`, агент/`not taken`, бейджи quiet/вопросов, тап → `/task/:id`, кнопка `Chat` → `/chat/:agentId`, long-press → ActionSheet с назначением агента и снятием.

- [ ] **Шаг 1: тест** — экран рендерит майлстоны из замоканного fetch, показывает агента, ведёт в чат агента.
- [ ] **Шаг 2–5:** прогнать (падает) → реализовать → `npm test` зелёный → коммит `mobile: Milestones tab`.

### Task 6: mobile — майлстоны на экране агента

**Files:**
- Modify: `mobile/app/agent/[id].tsx` (секция майлстонов с переходом в `/task/:id`)
- Test: `mobile/__tests__/agent/agent-screens.test.tsx`

- [ ] **Шаг 1: тест** → **Шаг 2:** падает → **Шаг 3:** реализация → **Шаг 4:** зелёные → **Шаг 5:** коммит `mobile: agent screen lists milestones`.

### Task 7: verification + PR

- [ ] `go test ./...`, `cd web && npm test -- --run`, `cd mobile && npm test`, `cd web && npx tsc -b`, `npm run build`.
- [ ] Прогнать экран вживую против локального демона (`make build` не коммитим), скриншот-проверка канбана.
- [ ] `gh pr create` с описанием и ссылкой на фичу task-1023.
