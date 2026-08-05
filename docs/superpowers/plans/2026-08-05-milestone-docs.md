# Milestone docs pass (T6, subtask #1034) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Свести доки под уже смерженную фичу майлстонов (M1 #1031, M2 #1023/M2, M3 #1033): майлстон описан как сущность, у него есть раздел в 12-tasks.md, обязанности агента — в сниппете 10-agents.md, а CLI/API/схема/конфиг/дашборд перечисляют ровно то, что есть в коде.

**Architecture:** Правки только в `docs/*.md`. Источник истины — код на `main`
(миграция 0012, `internal/api/milestones.go`, `internal/api/quiet.go`,
`internal/heartbeat/quiet.go`, `internal/cli/task_milestones.go`,
`web/src/screens/milestones/*`, `mobile/app/(tabs)/milestones.tsx`). Ни одной
строки, которую нельзя показать пальцем в коде.

**Tech Stack:** Markdown.

## Global Constraints

- **Код побеждает спеку и бриф.** Каждое утверждение проверено grep'ом по коду.
- Ничего не выдумывать про то, чего нет: CLI **не** печатает `quiet`, у майлстона
  **нет** отдельного эндпоинта листинга (это `GET /v1/tasks?milestones=true`).
- Язык доков — русский, стиль существующих файлов (плотный, без маркетинга).
- Дублирования не плодить: полная модель — в 12-tasks.md, остальные файлы
  ссылаются на неё.
- Не трогать код, тесты, промпты (`docs/prompts/` — вне этой подзадачи).

### Факты, зафиксированные из кода

| Факт | Где в коде |
|---|---|
| `tasks.milestone INTEGER NOT NULL DEFAULT 0`, `tasks.assigned_role TEXT`, `idx_tasks_milestone` | `internal/store/migrations/0012_milestones.sql` |
| `POST /v1/tasks/{id}/take` — только `kind='agent'`, 403 `agent_only`; чужой → 409 `already_taken`; свой → no-op 200 | `internal/api/milestones.go:74-114` |
| `POST /v1/tasks/{id}/assign` `{agent_id}`\|`{none:true}` — только человек (403 `human_only`), несуществующий агент → 400 `agent_not_found`, назначенному агенту уходит уведомление | `internal/api/milestones.go:124-183` |
| Не майлстон → 403 `not_a_milestone` | `internal/api/milestones.go:24-31` |
| `review` без дока/записи журнала → 422 `milestone_empty`; `done`/`cancelled` — только человек (403 `human_only`) | `internal/api/milestones.go:192-215` |
| `POST /v1/tasks {milestone:true}`: с `project` → 400 `milestone_with_project`, с `parent_id` → 400 `milestone_with_parent` | `internal/api/tasks.go:340-347` |
| `task start` на майлстоне → 403 `milestone_not_startable`; `cancel` агентом → 403 `human_only` | `internal/api/tasks.go:791-793, 891-893` |
| Каждый take/assign: `task_log` kind=status + событие `task.assigned` `{task_id, agent_id, by, verb}` | `internal/api/milestones.go:36-59` |
| `quiet` — производное поле ответа задачи, не хранится | `internal/api/tasks.go:64-68`, `internal/api/quiet.go` |
| Порог `milestone_quiet_after`, дефолт 24h | `internal/config/config.go:22-25, 75-83` |
| Что считается следом: не-`status` запись журнала, док, вопрос/сообщение треда — только за авторством держателя | `internal/store/milestones.go:24-40` |
| Не взятый / не `in_progress` майлстон никогда не quiet; отсчёт от `updated_at`, если следов нет | `internal/heartbeat/quiet.go:31-46` |
| Напоминание держателю не чаще 24h, событие `milestone.quiet` `{task_id, agent_id, title, since_seconds, reminded}` публикуется всегда | `internal/heartbeat/quiet.go:52, 96-113` |
| Человеку heartbeat про quiet не пишет | `internal/heartbeat/quiet.go:64-68` |
| CLI: `task take`, `task assign --none`, `task add --milestone`, `task ls --milestones`; карточка печатает `Milestone/Agent/Attach`, `agent show` — список майлстонов | `internal/cli/task_milestones.go`, `internal/cli/task.go:380,440,1302` |
| `GET /v1/agents/{id}` отдаёт `milestones[] {id,title,status}` | `internal/api/agents.go:30-52` |
| Web: `/milestones` (доска вне проектов), `/milestones/:id` (карточка с AgentRail), пикер assign/unassign, бейджи `not taken`/`◆ holder`/`🤐 quiet`/`⏳ waiting for input`, term/chat/attach; Milestones в шапке | `web/src/screens/milestones/*`, `web/src/routes.tsx:38-41`, `web/src/components/AppShell.tsx:93-96` |
| Mobile: таб Milestones, чипсы по статусам, ActionSheet назначения | `mobile/app/(tabs)/milestones.tsx`, `mobile/app/(tabs)/_layout.tsx:141` |

---

### Task 1: Сущность и модель

**Files:**
- Modify: `docs/01-concepts.md` (новая секция `## Milestone` после `## Task`)
- Modify: `docs/05-state.md` (схема `tasks`, `config.yaml`)

- [ ] **Step 1:** В `01-concepts.md` после `## Task` добавить `## Milestone`: корневая
  задача вне проектов, которую берёт постоянный агент; те же доки/журнал/треды;
  ссылка на 12-tasks.md.
- [ ] **Step 2:** В `05-state.md` в `CREATE TABLE tasks` добавить строки
  `milestone INTEGER NOT NULL DEFAULT 0` и `assigned_role TEXT` с комментариями,
  в список индексов — `idx_tasks_milestone`, в `created_by` — `agent`.
- [ ] **Step 3:** В пример `config.yaml` добавить `milestone_quiet_after: 24h` с
  комментарием.
- [ ] **Step 4:** Проверка: `grep -n "milestone" docs/01-concepts.md docs/05-state.md`.
- [ ] **Step 5:** Commit `docs: майлстон в 01-concepts и схеме 05-state`.

### Task 2: Раздел «Майлстоны» в 12-tasks.md

**Files:**
- Modify: `docs/12-tasks.md`

- [ ] **Step 1:** В `## Модель` → `### Поля` добавить `milestone` и `assigned_role`.
- [ ] **Step 2:** Новый раздел `## Майлстоны` перед `## Интеграция с терминалом`:
  что это, как создаётся (`--milestone`, взаимоисключения с `--project`/`--parent`),
  take/assign и их права, таблица статусной механики майлстона, гейт review (422)
  и приёмка человеком, тишина (`quiet`, порог, что считается следом, куда идёт
  напоминание), где виден (карточка агента, доска майлстонов), таблица прав.
- [ ] **Step 3:** В `## CLI` 12-tasks добавить `task take`/`task assign`/`--milestone`/`--milestones`.
- [ ] **Step 4:** Проверка: ссылки на другие доки резолвятся (`ls docs/<file>`).
- [ ] **Step 5:** Commit `docs: раздел про майлстоны в 12-tasks`.

### Task 3: Обязанности агента в 10-agents.md

**Files:**
- Modify: `docs/10-agents.md`

- [ ] **Step 1:** В `### Права` добавить абзац про майлстоны (take/write/move review,
  чего агент не может: assign, done/cancelled).
- [ ] **Step 2:** Новый подраздел `### Майлстоны` перед `### Сниппет для CLAUDE.md агента`:
  единица работы агента, одна сессия на все майлстоны, `rocket agent show` и карточка.
- [ ] **Step 3:** В сниппет CLAUDE.md добавить блок `## Майлстоны` — команды
  (`task ls --milestones`, `take`, `log`, `doc put`, `move review`), правило
  «показывай работу» и что значит `[rocket quiet milestone]`.
- [ ] **Step 4:** Проверка: команды в сниппете совпадают с `quietBody`
  (`internal/heartbeat/quiet.go:121-129`).
- [ ] **Step 5:** Commit `docs: обязанности агента по майлстонам в 10-agents`.

### Task 4: CLI, API и дашборд

**Files:**
- Modify: `docs/04-cli.md`, `docs/03-daemon-api.md`, `docs/08-orchestrators.md`, `docs/11-dashboard.md`

- [ ] **Step 1:** `04-cli.md`: в блок майлстонов добавить `task take` (он уже есть
  в «Для агентов» — свести), `--none`, точные коды отказов; в `agent show` — майлстоны.
- [ ] **Step 2:** `03-daemon-api.md`: строки таблицы задач для `take`/`assign`,
  `?milestones=true`, `milestone` в `POST /v1/tasks`, абзац про производное поле
  `quiet` рядом с `stale`/`waiting_terminal`, `milestones[]` у `GET /v1/agents/{id}`,
  события `task.assigned` и `milestone.quiet` в список типов.
- [ ] **Step 3:** `08-orchestrators.md`: подраздел `### Молчащие майлстоны` в `## Heartbeat`.
- [ ] **Step 4:** `11-dashboard.md`: экран Milestones (web) и таб Milestones (mobile),
  бейджи, assign-пикер, AgentRail; майлстоны на карточке агента.
- [ ] **Step 5:** Проверка: `grep -n "milestone" docs/*.md` — нет утверждений вне
  таблицы фактов выше.
- [ ] **Step 6:** Commit `docs: майлстоны в CLI, API, heartbeat и дашборде`.

### Task 5: Верификация и PR

- [ ] **Step 1:** `go build ./... && go test ./internal/...` — доки код не трогают,
  но прогон подтверждает чистое дерево.
- [ ] **Step 2:** Перечитать диф целиком: каждое утверждение → строка кода.
- [ ] **Step 3:** `gh pr create` с описанием и ссылкой на фичу task-1023.
