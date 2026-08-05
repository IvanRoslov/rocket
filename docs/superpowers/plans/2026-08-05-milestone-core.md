# Milestone core (M1, subtask #1031) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:test-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Майлстон — корневая задача без проекта, которую берёт постоянный агент: модель (миграция), права take/assign, мини-гейт review, события, CLI.

**Architecture:** Всё на существующей механике задач. Две новые колонки в `tasks`
(`milestone INTEGER NOT NULL DEFAULT 0`, `assigned_role TEXT`), два новых эндпоинта
(`take`, `assign`), правки прав в существующих хендлерах (`start`, `PATCH`, `cancel`,
`POST /v1/tasks`), новые команды CLI поверх них.

**Tech Stack:** Go 1.x, SQLite (modernc), cobra, net/http ServeMux.

## Global Constraints

- Спека: task #1023 doc «Спека v2: майлстоны для внешних агентов»,
  `docs/superpowers/specs/2026-08-05-milestones-design.md` (ветка orch/task-1023).
- Обычные задачи не меняются: `project_id` для них по-прежнему обязателен.
- Не трогать `internal/api/thread_inbox.go` (T5), `internal/heartbeat` (M2),
  `docs/prompts` (T6). `docs/04-cli.md` — минимальные правки.
- Каждый take/assign/unassign: запись `task_log` (kind=status) + событие `task.assigned`.
- `go test ./...` зелёный; миграция проверена на копии живой базы.

**Явное представление майлстона:** колонка `milestone`, а не «project_id IS NULL ⇒
майлстон». Причина: `project_id` объявлен `NOT NULL` в 0001, у майлстона он `''`
(та же конвенция, что у `agents.project_id`), и признак «майлстон» должен переживать
будущие фичи с необязательным проектом у обычных задач.

---

### Task 1: Миграция и store

**Files:**
- Create: `internal/store/migrations/0012_milestones.sql`
- Modify: `internal/store/tasks.go` (Task, TaskFilter, AddTask, scanTask, ListTasks; новый `SetTaskAssignedRole`)
- Test: `internal/store/tasks_test.go`

**Interfaces:**
- Produces: `store.Task{Milestone bool, AssignedRole string}`,
  `store.TaskFilter{Milestones bool}` (true ⇒ только `milestone = 1`),
  `func (s *Store) SetTaskAssignedRole(id int64, role string) error` («» очищает).

- [ ] **Step 1:** Тест: `AddTask` с `Milestone: true, ProjectID: ""` сохраняется и читается
  обратно (`GetTask` → Milestone=true, AssignedRole=""); обычная задача → Milestone=false.
- [ ] **Step 2:** Тест: `SetTaskAssignedRole(id, "cto")` → `GetTask.AssignedRole == "cto"`;
  `SetTaskAssignedRole(id, "")` → `""`; несуществующий id → `ErrNotFound`.
- [ ] **Step 3:** Тест: `ListTasks(TaskFilter{Milestones: true})` возвращает только майлстоны.
- [ ] **Step 4:** Запустить `go test ./internal/store/ -run Milestone` — FAIL.
- [ ] **Step 5:** Написать миграцию 0012 (`ALTER TABLE tasks ADD COLUMN milestone INTEGER NOT NULL DEFAULT 0;`
  `ALTER TABLE tasks ADD COLUMN assigned_role TEXT;` + индекс по milestone) и код store.
- [ ] **Step 6:** `go test ./internal/store/` — PASS. Commit.

### Task 2: Создание майлстона (API + права)

**Files:**
- Modify: `internal/api/tasks.go` (taskResponse, toTaskResponse, postTaskRequest, handlePostTask, handleListTasks)
- Test: `internal/api/tasks_milestones_test.go`

**Interfaces:**
- Produces: `POST /v1/tasks {milestone:true}`; в ответах задач — `milestone`, `assigned_role`;
  `GET /v1/tasks?milestones=true`.

- [ ] **Step 1:** Тесты: `milestone:true` без проекта → 201, `project_id` пуст, `milestone:true`
  в ответе; `milestone:true` + `project` → 400 `milestone_with_project`;
  создание из сессии оркестратора/воркера → 403; из agent-сессии → 201.
- [ ] **Step 2:** Тест: `GET /v1/tasks?milestones=true` возвращает только майлстоны.
- [ ] **Step 3:** Запустить — FAIL. Реализовать. PASS. Commit.

### Task 3: take / assign

**Files:**
- Create: `internal/api/milestones.go` (хендлеры take/assign + общие проверки)
- Modify: `internal/api/tasks.go` (registerTaskRoutes)
- Test: `internal/api/tasks_milestones_test.go`

**Interfaces:**
- Produces: `POST /v1/tasks/{id}/take`, `POST /v1/tasks/{id}/assign {agent_id|none}`.

- [ ] **Step 1:** Тесты take: agent-сессия берёт свободный майлстон → 200, `assigned_role`;
  человек → 403; оркестратор → 403; обычная задача → 403 `not_a_milestone`;
  занятый другим агентом → 409 с именем держателя; повторный своим агентом → 200 (идемпотентно).
- [ ] **Step 2:** Тесты assign: человек назначает зарегистрированного агента → 200 + запись
  журнала + событие `task.assigned` + доставка сообщения агенту
  (`[rocket] You have been assigned milestone #N "<title>". Start with: rocket task show N`);
  `{none:true}` снимает; агент-вызыватель → 403; неизвестный agent_id → 400;
  обычная задача → 403.
- [ ] **Step 3:** Запустить — FAIL. Реализовать. PASS. Commit.

### Task 4: Гейты жизненного цикла

**Files:**
- Modify: `internal/api/tasks.go` (handlePostTaskStart, handlePatchTask, handlePostTaskCancel),
  `internal/api/auth.go` (canWriteTask)
- Test: `internal/api/tasks_milestones_test.go`

- [ ] **Step 1:** Тесты: `POST /start` на майлстоне → 403 «milestones are taken by persistent agents»;
  на обычной задаче поведение не изменилось (существующие тесты зелёные).
- [ ] **Step 2:** Тесты гейта review: майлстон без доков и без записей агента → 422
  `milestone_empty`; с доком → 200; с записью журнала автора-агента (kind != status) → 200.
- [ ] **Step 3:** Тесты: `done` и `cancelled` на майлстоне из agent-сессии → 403; от человека → 200.
- [ ] **Step 4:** Тест: писать в майлстон (log/doc/move) может человек и назначенный агент;
  посторонний агент → 403.
- [ ] **Step 5:** Запустить — FAIL. Реализовать. PASS. Commit.

### Task 5: CLI

**Files:**
- Modify: `internal/cli/task.go` (add --milestone, ls --milestones, show, новые take/assign),
  `internal/cli/agent.go` (agent show — список майлстонов)
- Test: `internal/cli/task_test.go`, `internal/cli/agent_test.go`

- [ ] **Step 1:** Тесты: `task add --milestone --project X` → usage error; `--milestone`
  шлёт `{"milestone":true}` без резолва проекта по умолчанию.
- [ ] **Step 2:** Тесты: `task take <id>` → POST take; `task assign <id> <agent>` → POST assign
  `{agent_id}`; `task assign <id> --none` → `{none:true}`; `assign` без агента и без `--none` → usage error.
- [ ] **Step 3:** Тест: `task ls --milestones` добавляет `milestones=true` в query.
- [ ] **Step 4:** Тест рендера карточки: у майлстона печатается `Milestone: yes`,
  `Agent: <id>` (или `не взят`) и `Attach: rocket agent attach <id>`; у обычной задачи — нет.
- [ ] **Step 5:** Тест: `agent show <id>` печатает секцию `milestones:` со списком.
- [ ] **Step 6:** Запустить — FAIL. Реализовать. PASS. Commit.

### Task 6: Миграция на копии живой базы + докиCLI

**Files:**
- Modify: `docs/04-cli.md`
- Test: `internal/store/migrate_realdb_test.go` (существующий механизм)

- [ ] **Step 1:** Прогнать существующий real-db тест миграций; отдельно — руками на копии
  `~/.rocket/rocket.db`: применить миграции, убедиться, что обычные задачи не изменились
  (`milestone=0`, `assigned_role IS NULL`, счётчики строк совпадают).
- [ ] **Step 2:** Дописать в `docs/04-cli.md` новые команды (add --milestone, take, assign, ls --milestones).
- [ ] **Step 3:** `go test ./...`, `gofmt`, `go vet`. Commit + PR.
