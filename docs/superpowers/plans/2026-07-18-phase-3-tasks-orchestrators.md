# Фаза 3 «Задачи и оркестраторы» — план имплементации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Основной сценарий продукта: задача на канбане → `rocket up` поднимает оркестратора с системным промптом и kickoff → оркестратор спавнит воркеров (только в репо своего проекта, с автоподзадачами), ведёт доки/журнал/Q&A-треды в задаче → heartbeat будит застрявших.

**Architecture:** Слой задач поверх готовых таблиц (`tasks/task_docs/task_log/task_questions/question_messages` — схема с фазы 1). Идентификация агентских вызовов — заголовок `X-Rocket-Session` (клиент ставит из `ROCKET_SESSION_ID`); права проверяет API. Промпты — embedded шаблоны (тексты из docs/prompts/) с плейсхолдерами `{{...}}` и переопределением файлами `~/.rocket/prompts/`. Спавн оркестратора — расширение session manager (kind=orchestrator, ветка `orch/<slug>`, main-репо). Heartbeat — новый цикл демона поверх монитора/очереди.

**Tech Stack:** без новых зависимостей; шаблоны — `text/template` c делимитерами `{{`/`}}` (plain replace недостаточен из-за списков) или простой strings.Replacer — решено: **strings.Replacer** (плейсхолдеры плоские, YAGNI).

## Global Constraints

- Статусы задач: `backlog | in_progress | review | done | cancelled`; подзадача создаётся `in_progress` при спавне (или `backlog` заранее). Явный `task move` всегда побеждает автоматику. PR-автопереходы подзадач — заглушки (фаза 4).
- Права агентов: оркестратор пишет только в свою задачу и её подзадачи; воркер — только в свою подзадачу; пользователь (без X-Rocket-Session) — куда угодно. Ошибка — 403 `forbidden`.
- Q&A: вопрос — тред до явного resolve; закрывает только пользователь (`answer`/`--dismiss`); реплики доставляются оркестратору через очередь с префиксами `[task #N Q<M> reply] ` / `[task #N Q<M> answer] `; «чья очередь» — производное от автора последней записи треда; в Q&A ходит только оркестратор задачи.
- Slug фичи: из заголовка задачи — lowercase, `[a-z0-9-]`, максимум 4 слова, коллизия → `-2`, `-3`.
- Оркестратор: ветка `orch/<slug>`, worktree в main-репо проекта, имя сессии `<slug>-orch`; `parent_id` NULL. Воркер: имя `<slug>-<task>`, ветка `feature/<slug>/<task>`, `parent_id` = оркестратор, `feature_slug` наследуется.
- `rocket spawn` — ТОЛЬКО из сессии живого оркестратора (иначе 403 `orchestrator_only`); репо ∈ main+linked проекта оркестратора.
- Промпт-тексты — дословно из docs/prompts/*.md (блоки в тройных бэктиках); секции Superpowers остаются (claude-code поддерживает skills).
- Heartbeat: интервал `heartbeat_interval` (5m, конфиг уже есть); только живые оркестраторы с задачей `in_progress`; застрявший воркер = idle/blocked/waiting_input > 15m (конфиг `worker_stall_threshold`), или exited, или (фаза 4: CI failing); анти-спам — не чаще 1 сводки на оркестратора за интервал; сводка через очередь сообщений; событие `orchestrator.heartbeat_sent`; напоминание о вопросах со статусом «ждёт оркестратора» дольше порога.
- События: `task.created|status_changed|question_asked|question_replied|question_resolved`, `orchestrator.heartbeat_sent` — session_id по смыслу (оркестратор задачи).
- Кросс-фазные инварианты прежние: id `^[a-z0-9-]+$`, error envelope, никакого shell вне задокументированных исключений, `go test ./... && go vet ./... && gofmt -l ./internal ./cmd` перед коммитом.

---

### Task 1: Store DAO задач

**Files:** Create `internal/store/tasks.go`; Test в `internal/store/tasks_test.go`.

**Produces:**
```go
type Task struct{ ID int64; ParentID int64; Title, Description, ProjectID, RepoID, Status, FeatureSlug, SessionID, CreatedBy string; CreatedAt, UpdatedAt, CompletedAt int64 }
AddTask(t Task) (int64, error)            // status default backlog, created_by default user
GetTask(id) (Task, error)                  // ErrNotFound
ListTasks(f TaskFilter) ([]Task, error)    // TaskFilter{Project, Status string; Parent int64; ParentSet bool} — ParentSet+Parent==0 → только корневые
UpdateTaskStatus(id int64, status string) error   // + completed_at при done/cancelled, updated_at всегда
UpdateTask(t Task) error                   // title/description/feature_slug/session_id/repo_id
type TaskDoc struct{ ID, TaskID, Version int64; Kind, Title, Body, Author string; CreatedAt int64 }
PutTaskDoc(d TaskDoc) (TaskDoc, error)     // version = max(того же task+kind+title)+1
ListTaskDocs(taskID int64, history bool) ([]TaskDoc, error) // history=false → только последние версии
type TaskLogEntry struct{ ID, TaskID int64; Kind, Body, Author string; CreatedAt int64 }
AddTaskLog(e TaskLogEntry) (int64, error); ListTaskLog(taskID int64, kind string) ([]TaskLogEntry, error)
```
- [ ] TDD: CRUD, фильтры (project/status/parent/корневые), версии доков (тот же kind+title → v2, history/latest), журнал с фильтром kind, статус двигает completed_at.
- [ ] Commit: `feat(store): tasks, docs and log DAO`.

### Task 2: Tasks API + права агентов

**Files:** Create `internal/api/tasks.go`, `internal/api/auth.go`; Modify `internal/client/client.go` (заголовок `X-Rocket-Session` из env ROCKET_SESSION_ID — во ВСЕ запросы), `internal/api/server.go`.

**Produces:**
- `callerSession(r)` в auth.go: пустой заголовок → «пользователь» (полные права); непустой → сессия должна существовать (401 `unknown_session`).
- Права (helper `canWriteTask(caller store.Session, task store.Task) bool`): пользователь — всё; kind=orchestrator — task.SessionID==caller.ID (своя задача) или родитель подзадачи принадлежит caller; kind=worker — task.SessionID==caller.ID (своя подзадача). Нарушение → 403 `forbidden`.
- Маршруты: `GET /v1/tasks?status=&project=&parent=` (+`?board=true` — сгруппировано по статусам), `POST /v1/tasks {title, description?, project, parent_id?}` (валидации: project существует; parent существует и корневой; от агентов — только подзадачи своей задачи), `GET /v1/tasks/{id}` (карточка: задача + подзадачи + сессия (tmux_name, attach cmd) + счётчик открытых вопросов), `PATCH /v1/tasks/{id}` {status?, title?, description?} (status через UpdateTaskStatus + событие `task.status_changed` + task_log kind=status), `POST /v1/tasks/{id}/cancel` (→cancelled + каскадный kill сессий задачи и подзадач через Manager, cleanup), `GET/PUT /v1/tasks/{id}/docs` (`?history=`), `GET/POST /v1/tasks/{id}/log` (`?kind=`).
- События: `task.created`, `task.status_changed` (+запись в task_log kind=status «автор: система/имя»).
- [ ] TDD: CRUD happy + все 400/403/404; права: воркер не может писать в чужую подзадачу, оркестратор — в чужую задачу; cancel каскадно убивает (fake runtime); board-группировка.
- [ ] Commit: `feat(api): tasks CRUD with agent permissions`.

### Task 3: CLI задач (базовый)

**Files:** Create `internal/cli/task.go`; Test `internal/cli/task_test.go` (usage + рендер).

- `rocket task add "<title>" [--project] [--parent <id>] [--desc|--desc-file]` (проект по умолчанию: если ровно один в реестре — он; иначе обязателен); `task ls [--status] [--project]` — канбан в терминале (группы по статусам, `#id title [slug] [сессия] [?N открытых вопросов]`); `task show <id>` — карточка (поля, подзадачи таблицей, доки списком «kind/title vN», журнал хвостом 10, attach-подсказка, открытые вопросы); `task move <id> <status>`; `task cancel <id>`; `task doc put <id> --kind --title --file`; `task log <id> --kind <k> "<текст>"`.
- [ ] TDD рендеры/usage; Commit: `feat(cli): task add/ls/show/move/doc/log/cancel`.

### Task 4: Промпт-шаблоны

**Files:** Create `internal/prompts/prompts.go`, `internal/prompts/templates/orchestrator.md`, `templates/kickoff.md`, `templates/worker.md`; Test `internal/prompts/prompts_test.go`.

- Тексты шаблонов — дословно содержимое code-блоков из docs/prompts/*.md (`{{placeholder}}`-плейсхолдеры). `//go:embed templates/*`.
- `type Vars map[string]string`; `Render(name string, v Vars) (string, error)` — strings.Replacer по `{{key}}`; незаполненный плейсхолдер в результате → error (страховка от дрейфа шаблона); `{{project_rules}}` — опциональный, пустая строка ок.
- Переопределение: файл `~/.rocket/prompts/<name>.md` (путь через config.Home) читается вместо embedded, если существует.
- [ ] TDD: рендер всех трёх с полным набором vars (без остатка `{{`), незаполненный → ошибка, override-файл побеждает.
- [ ] Commit: `feat: embedded prompt templates with overrides`.

### Task 5: Спавн оркестратора — `task start` и `rocket up`

**Files:** Modify `internal/session/manager.go` (SpawnOrchestrator), `internal/api/tasks.go` (`POST /v1/tasks/{id}/start {agent?}`), Create `internal/cli/up.go`; Modify `internal/cli/task.go` (task start).

- Manager.SpawnOrchestrator(ctx, task store.Task, project store.Project, agentName string): slug из title (словослаг: lower, не-алфацифра→«-», максимум 4 слова, обрезка до 40 симв., коллизия по sessions.feature_slug/имени → -2); имя `<slug>-orch`; ветка `orch/<slug>`; репо main; kind=orchestrator; SystemPrompt = Render(orchestrator, vars), FirstMessage = Render(kickoff, vars); события как у обычного спавна.
- API start: задача корневая, status==backlog (409 `task_not_startable` иначе), проекта живого оркестратора на этой задаче нет; спавн → tasks.feature_slug/session_id, status→in_progress (+событие+log) → 201 {task_id, feature_slug, session_id}.
- CLI: `rocket task start <id> [--agent]`; `rocket up "<описание>" [--project] [--agent] [--desc...]` = add+start, печатает TASK/SLUG/SESSION.
- [ ] TDD: слаггер (таблица кейсов), API start happy/409/404, up через httptest; менеджер — фейками (промпты реально рендерятся: проверить, что SystemPrompt содержит slug и таск-id).
- [ ] Commit: `feat: orchestrator spawn via task start and rocket up`.

### Task 6: `rocket spawn` — только оркестратор, автоподзадачи

**Files:** Modify `internal/session/manager.go` (Spawn), `internal/api/sessions.go` (POST /v1/sessions теперь требует caller-оркестратора; поле `subtask_id` в ответе), `internal/cli/spawn.go` (+`--subtask`).

- POST /v1/sessions: caller из X-Rocket-Session ОБЯЗАТЕЛЕН и должен быть живым оркестратором (403 `orchestrator_only`); project = проект оркестратора (флаг --project у CLI-spawn удалить); repo ∈ main+linked; feature = caller.FeatureSlug (наследуется, флаг --feature удалить); parent_id=caller.ID; SystemPrompt = Render(worker, vars), FirstMessage = --prompt.
- Подзадача: `--subtask <id>` → валидация (подзадача существует, parent = задача оркестратора, не занята сессией) и привязка (session_id, repo_id, status→in_progress); без флага → автосоздание подзадачи {title: task-name, parent: задача оркестратора, created_by: orchestrator, status: in_progress, repo_id, session_id}. Событие + task_log kind=status в родительскую задачу («spawned worker X for subtask #N»).
- kill --cascade (CLI+API `?cascade=true`): убить оркестратора и всех его живых воркеров (workers first), cleanup по флагу.
- [ ] TDD: не-оркестратор → 403; чужое репо → 400; автоподзадача создана и связана; --subtask привязывает; занятая подзадача → 409; cascade убивает всех.
- [ ] Commit: `feat: orchestrator-only spawn with auto-subtasks and cascade kill`.

### Task 7: Q&A-треды

**Files:** Create `internal/store/questions.go`, `internal/api/questions.go`; Modify `internal/api/tasks.go` (маршруты), server.go.

**Produces (DAO):** Question{ID, TaskID int64; AskedBy, Body, Context, Status, Resolution string; AskedAt, ResolvedAt int64}; QuestionMessage{ID, QuestionID int64; Author, Kind, Body string; CreatedAt int64}; AddQuestion/GetQuestion/ListQuestions(taskID, openOnly)/ResolveQuestion(id, resolution)/AddQuestionMessage/ListQuestionMessages(questionID); производное WhoseTurn(q, msgs): автор последней записи треда (вопрос — от оркестратора) — оркестратор→«ждёт пользователя», пользователь→«ждёт оркестратора».
**API:**
- `POST /v1/tasks/{id}/questions {body, context?}` — только оркестратор этой задачи (403); событие `task.question_asked`; номер вопроса в задаче = порядковый (Q1, Q2 — счёт по task_id).
- `GET /v1/tasks/{id}/questions?status=open` — вопросы + треды + whose_turn.
- `POST /v1/questions/{id}/reply {body}` — обе стороны (агент — только оркестратор задачи); вопрос остаётся open; реплика пользователя → доставка оркестратору через POST-в-очередь `[task #N Q<M> reply] <body>`; реплика оркестратора — только запись (пользователь увидит в CLI/дашборде); событие `task.question_replied`.
- `POST /v1/questions/{id}/answer {body}` | `{dismiss:true}` — только пользователь (агент → 403); status→resolved (resolution answered|dismissed); answered → доставка `[task #N Q<M> answer] <body>` оркестратору; событие `task.question_resolved`.
- [ ] TDD: полный тред (ask → user reply → orch reply → answer) с проверкой доставки в очередь (заглушённый runtime), whose_turn на каждом шаге, dismiss без доставки, права.
- [ ] Commit: `feat: task Q&A threads with queued delivery`.

### Task 8: CLI Q&A + status + attach по задаче

**Files:** Modify `internal/cli/task.go` (+ask/questions/reply/answer), Create `internal/cli/status.go`, Modify `internal/cli/sessions.go` (attach: числовой аргумент → задача).

- `task ask <id> "<вопрос>" [--context]`; `task questions [<id>] [--open]` (треды с индикатором чьей очереди); `task reply <qid> "<текст>"`; `task answer <qid> "<ответ>"` / `--dismiss`.
- `rocket status <slug>`: оркестратор + его воркеры (activity, возраст, подзадача, PR-прочерки) — по sessions.feature_slug.
- `rocket attach 12` → GET /v1/tasks/12 → session_id → обычный attach-флоу.
- [ ] TDD usage/рендер; Commit: `feat(cli): Q&A, status by slug, attach by task id`.

### Task 9: Heartbeat

**Files:** Create `internal/heartbeat/heartbeat.go`; Modify `internal/daemon/daemon.go`, `internal/config/config.go` (+`worker_stall_threshold` 15m, `question_reminder_threshold` 30m).

- Цикл `heartbeat_interval`: для каждого живого (running) оркестратора с корневой задачей in_progress: собрать воркеров (parent_id, живые), стать: activity+activity_ts из store, застрял = state∈{idle,blocked,waiting_input} и now-activity_ts>threshold, или exited. Открытые вопросы задачи со статусом «ждёт оркестратора» дольше question_reminder_threshold → добавить в сводку напоминание.
- Есть застрявшие/напоминания И оркестратор не active И анти-спам ок (последний heartbeat этому оркестратору > interval назад; in-memory map) → построить текст сводки (формат из 08-orchestrators.md) → в очередь (AddMessage from="" to=orch + Wake) → событие `orchestrator.heartbeat_sent`.
- [ ] TDD (фейковое время — поле nowFunc): застрявший воркер → сводка в очереди; активный оркестратор → пропуск; анти-спам; вопрос «ждёт оркестратора» → напоминание в сводке; review-задача → не трогаем.
- [ ] Commit: `feat: orchestrator heartbeat with stall detection and question reminders`.

### Task 10: E2E приёмка фазы

Сценарий (изолированный ROCKET_HOME, ДВА scratch-репо, проект с main+linked):
- `rocket up "add ping feature" --project demo` → живой оркестратор с системным промптом; в терминале оркестратора видно kickoff.
- Человеком (в терминал через send) попросить оркестратора: разбей на 2 задачи в двух репо и спавнь воркеров с крошечными брифами (e.g. создать файл PING.md с текстом). Проверить: `rocket spawn` из НЕ-оркестратора → 403; подзадачи появились в `task show`; воркеры получили промпт воркера (env/содержимое сессии).
- Воркеры коммитят в свои ветки (PR-шаг без GitHub — проверяем ветку+коммит; gh может отсутствовать — допустимо, что воркер доложит «PR не смог, ветка готова»).
- Оркестратор пишет doc/log через CLI (проверить `task show`), задаёт вопрос (`task ask`), пользователь reply→оркестратор reply→answer: треды в `task questions`, доставка с префиксами видна в терминале оркестратора.
- Heartbeat: убить tmux одного воркера вручную → дождаться/форсировать тик (короткий heartbeat_interval в конфиге) → сводка пришла оркестратору.
- `rocket status <slug>`, `rocket attach <task-id>` (проверить JSON-команду), `task move review`, cancel-каскад на второй задаче.
- [ ] Прогнать, чинить баги отдельными коммитами, полный транскрипт в отчёт.

## Self-Review (выполнен)

- Роадмап фазы 3 покрыт: слой задач T1-T3; up/промпты T4-T5; spawn-ограничения+подзадачи T6; Q&A T7-T8; status/cascade/attach T8/T6; heartbeat T9; doctor-Superpowers — уже сделан в фазе 1; PR-переходы — заглушки (не делаем, фаза 4). Критерий готовности — T10.
- Согласованность: X-Rocket-Session вводится в T2 и используется T6/T7; prompts.Render из T4 нужен T5/T6; WhoseTurn из T7 нужен T8/T9.
- Отложено: PR/CI-механика и авто-cleanup по merge (фаза 4), дашборд (фаза 6).
