# Механизм брейншторма оркестратора — план разбиения

> **Для воркеров:** каждый воркер ведёт свою подзадачу по своему Superpowers-процессу (brainstorming → writing-plans → TDD → verification). Этот документ — декомпозиция и **контракты между подзадачами**; детальный пошаговый план каждый воркер пишет у себя.

**Цель:** шторм оркестратора как сущность задачи (вопросы с рекомендацией и исходом, документ «Проблема», гейт Go по версии спеки), свой скилл `orchestrator-brainstorming` с переключателем, метрика качества (вкладка, экран, CLI).

**Архитектура:** расширяем существующие треды вопросов (тип `brainstorm` + поля исхода), добавляем таблицу гейтов и вид документа `problem`, вычисляем метрику на лету из SQLite. Скилл встраивается в бинарь и раскладывается в worktree оркестратора; имя скилла в промпте выбирается настройкой и запоминается в задаче.

**Стек:** Go (daemon, CLI, SQLite modernc), React/TS (web), Expo/React Native (mobile).

**Спека:** `docs/superpowers/specs/2026-10-01-orchestrator-brainstorm-design.md` (spec v1 в задаче #4901).

## Глобальные ограничения

- Миграции: A → `0018_brainstorm_questions.sql`, B → `0019_gates.sql`, C → `0020_task_brainstorm_skill.sql`. Номера закреплены, чтобы параллельные ветки не конфликтовали.
- Имя скилла: `orchestrator-brainstorming`; штатный — `superpowers:brainstorming`.
- Ключ настройки: `orchestrator_brainstorm_custom` (`"true"`/`"false"`, по умолчанию false).
- Тип треда: `brainstorm`. Исходы: `accepted` | `corrected` | `wrong_turn`. Источник ответа: `ui` | `terminal`.
- Статусы гейта: `pending` | `go` | `changes` | `superseded`.
- Человек = запрос без агентской идентичности (`callerSession == nil`). Решения гейта и переопределение исхода — только человек (persistent-агенты тоже 403).
- Промпт воркера не меняется. Docs-копии промптов (`docs/prompts/*`) синхронны шаблонам (тест `docs_sync_test.go`).
- Номера вариантов 1-based везде (API, CLI, UI).

## Контракты API (общие для всех подзадач)

### Вопросы (A)

Объект вопроса (в `GET /v1/tasks/{id}/questions`, `GET /v1/threads`, `GET /v1/questions`) получает поля:

```json
{
  "type": "brainstorm",
  "recommended_option": 2,
  "chosen_option": 2,
  "answer_comment": "текст человека",
  "answer_source": "ui",
  "outcome": "accepted",
  "outcome_overridden": false
}
```
`recommended_option`/`chosen_option` — `null`, если нет; `answer_comment`/`answer_source`/`outcome` — `""`, если нет.

- `POST /v1/tasks/{id}/questions` — `{"type":"brainstorm", "options":[...], "recommend": N, ...}`. Для `brainstorm` с вариантами `recommend` обязателен и в диапазоне → иначе 400 `bad_request`.
- `POST /v1/questions/{id}/answer` — `{"choose": N, "body": "..."}`. Для `brainstorm`: `choose>0` → `chosen_option=N`, `answer_comment=body`, тело сообщения-ответа = текст варианта + (если есть) комментарий; `choose=0` → `chosen_option=null`, `answer_comment=body`. `answer_source="ui"`. Исход: chosen==recommended → `accepted`; chosen≠recommended → `corrected`; chosen=null → `wrong_turn`. Для остальных типов поведение не меняется.
- `POST /v1/questions/{id}/brainstorm-record` — `{"choose": N, "body": "текст человека"}`. Только сессия-оркестратор задачи этого треда; только `type=brainstorm`, только `status=open`. Закрывает тред как ответ, `answer_source="terminal"`, автор ответа — человек (`human`) с пометкой источника; исход — по тем же правилам. Ошибки: 403 (не оркестратор задачи), 400 (не brainstorm), 409 (уже закрыт).
- `PATCH /v1/questions/{id}/outcome` — `{"outcome":"corrected"}`. Только человек; только закрытый `brainstorm`-тред → `outcome_overridden=true`.
- Переоткрытие brainstorm-треда обнуляет `chosen_option/answer_comment/answer_source/outcome/outcome_overridden`.
- CLI: `rocket task ask <task> --brainstorm --recommend N --option ... [--title --brief --file --to]`; `rocket task brainstorm record <task>/Q<n> [--choose N] "<текст>"` (+ `--file`); `rocket task close ... --choose N "<комментарий>"` для brainstorm работает как ответ с комментарием.

### Гейт и «Проблема» (B)

- Вид документа `problem` добавлен в допустимые (`rocket task doc put --kind problem`).
- Объект гейта:
```json
{"id":1,"task_id":4901,"spec_version":1,"plan_version":1,"status":"pending",
 "comment":"","decided_by":"","requested_by":"task-4901-orch",
 "requested_at":"2026-10-01T12:00:00Z","decided_at":null}
```
- `POST /v1/tasks/{id}/gates` — запрос гейта (агент с правом записи в задачу или человек). Берёт последние версии `spec` и `plan` (план может отсутствовать → `plan_version: null`); без спеки → 400 `no_spec`. Если уже есть `pending` — он становится `superseded`.
- `GET /v1/tasks/{id}/gates` — история, новые первыми.
- `POST /v1/gates/{id}/decide` — `{"decision":"go"|"changes","comment":"..."}`. Только человек (иначе 403). Не `pending` → 409. `changes` без комментария → 400. `go`: если задача в `brainstorm` → `in_progress` через тот же путь, что `rocket task move` (журнал + событие); оркестратору задачи через `deliverToSession` — `[rocket gate] Go по спеке v<N> (план v<M>) — начинай реализацию.`; `changes` — `[rocket gate] Нужны правки по спеке v<N>: <комментарий>`.
- Запись новой версии `spec` переводит `pending`-гейт задачи в `superseded`.
- События шины: `task.doc_put` `{task_id, kind, version}`, `task.gate_requested|task.gate_decided|task.gate_superseded` `{task_id, gate_id, status}`. Web/mobile инвалидация уже ловит `task.*`.
- CLI: `rocket task gate request <task>`, `rocket task gate ls <task>`, `rocket task gate go <gate-id>` / `rocket task gate changes <gate-id> "<комментарий>"` (для человека из своего терминала).

### Скилл и настройка (C)

- `GET /v1/settings` возвращает `orchestrator_brainstorm_custom: bool`; `PUT /v1/settings` принимает его (не ломая `github_token`).
- Колонка `tasks.brainstorm_skill TEXT` (`""` для старых задач) — заполняется при старте задачи (`POST /v1/tasks/{id}/start`) по текущей настройке; отдаётся в объекте задачи как `brainstorm_skill`.
- Шаблоны получают переменную `{{brainstorm_skill}}`; restore использует значение из задачи.

### Метрика (D)

- `GET /v1/stats/brainstorm?weeks=N` (по умолчанию 12):
```json
{"weeks":[{"week":"2026-W40","skill":"orchestrator-brainstorming",
  "answered":10,"accepted":6,"accepted_with_comment":3,"corrected":3,"wrong_turn":1}],
 "storms":[{"task_id":4901,"title":"...","project_id":"rocket","skill":"orchestrator-brainstorming",
  "questions":5,"answered":5,"accepted":3,"accepted_with_comment":2,"corrected":1,"wrong_turn":1,
  "spec_changes":0,"go_at":"2026-10-01T15:00:00Z"}]}
```
- `GET /v1/tasks/{id}/brainstorm/stats` — одна запись формата `storms[]` для вкладки.
- Неделя — ISO по времени ответа (последнее `answer`-сообщение), локальное время daemon. Пустой `skill` → `"unknown"`.
- CLI: `rocket stats brainstorm [--weeks N] [--json]`.

## Подзадачи

### Волна 1 (параллельно)

**A. `brainstorm-questions`** — тип `brainstorm`, рекомендованный/выбранный вариант, комментарий, источник, исход и его переопределение; `brainstorm-record`; CLI-флаги и подкоманда. Файлы: `internal/store/migrations/0018_brainstorm_questions.sql`, `internal/store/questions.go`, `internal/api/questions.go`, `internal/api/threads.go`, `internal/cli/task.go`, `internal/cli/thread_write.go`, тесты рядом. Проверить ветки `fyi`, где `brainstorm` должен вести себя как `decision` (`normalizeThreadType`, `replyReopens`, `heartbeat/stale.go`). Обновить `docs/12-tasks.md`/`docs/04-cli.md`.

**B. `brainstorm-gate`** — вид документа `problem`, таблица и API гейтов, supersede по новой спеке, Go → in_progress + доставка оркестратору, события `task.doc_put` и `task.gate_*`, CLI `rocket task gate`. Файлы: `0019_gates.sql`, `internal/store/gates.go`, `internal/api/gates.go`, `internal/api/tasks.go` (виды доков, событие doc_put, supersede), `internal/cli/task.go` (gate). Обновить docs.

**C. `brainstorm-skill`** — каталог скилла `internal/prompts/skills/orchestrator-brainstorming/` (копия superpowers 6.4.1 `skills/brainstorming/*` с переименованием в frontmatter и путях; README с версией-источником), embed, раскладка в `<worktree>/.claude/skills/orchestrator-brainstorming/` в `claudecode.SetupWorkspace` для оркестратора (с исключением из git), настройка в settings API, `0020_task_brainstorm_skill.sql`, запоминание при старте, `{{brainstorm_skill}}` в orchestrator.md/kickoff.md, **переписывание раздела шторма в промптах** под механизм (Проблема первым шагом; `ask --brainstorm --recommend`; `brainstorm record`; `gate request` вместо Confirm-spec вопроса; Go двигает задачу механизмом). Синхронизировать `docs/prompts/*`, `docs/08-orchestrators.md`, `docs/10-agents.md`. **Мержится последним в волне 1** — промпты ссылаются на CLI из A и B.

### Волна 2 (после мержа A, B, C; параллельно)

**D. `brainstorm-stats`** — `GET /v1/stats/brainstorm`, `GET /v1/tasks/{id}/brainstorm/stats`, `rocket stats brainstorm`. Файлы: `internal/store/brainstorm_stats.go`, `internal/api/stats.go`, `internal/cli/stats.go`.

**E. `brainstorm-web`** — вкладка «Брейншторм» в `web/src/screens/task/` (счётчики, Проблема, вопросы с ★, вариант+комментарий, свой текст, исход с переключением, «из терминала», блок гейта с историей; по умолчанию в статусе brainstorm), поддержка рекомендации/комментария в общем `QuestionThreadView` (вкладка «Вопросы», входящие), экран «Метрика брейншторма» (новый маршрут + пункт навигации; ряды по скиллу), переключатель в `SettingsScreen`. Может начинаться параллельно с D по контракту; мержится после D.

**F. `brainstorm-mobile`** — вкладка «Брейншторм» в `mobile/app/task/[id].tsx` (то же содержание, что в вебе, без экрана метрики), рекомендация/комментарий в `QuestionCard`/`ThreadCard`, Go/«Нужны правки». Мержится после D (счётчики берёт из `/brainstorm/stats`).

### Финал (оркестратор)

Сквозная проверка: включить настройку → стартовать тестовую задачу → оркестратор получает `orchestrator-brainstorming`, скилл лежит в worktree; вопрос `--brainstorm` виден во вкладке; ответ с комментарием → `accepted`; `brainstorm record` → «из терминала»; новая спека снимает гейт; Go → in_progress и сообщение агенту; `rocket stats brainstorm` показывает шторм.

## Фокус ревью

1. Ответ из терминала, записанный не тем агентом (воркер / другой оркестратор) — должен быть 403 (A).
2. Persistent-агент (cto) пытается нажать Go или переопределить исход — 403 (B, A).
3. Спека обновлена после запроса гейта, а человек жмёт Go на старый — 409, задача не двигается (B).
4. Go, когда задача уже не в brainstorm (воркер заспавнен раньше) — гейт `go`, статус не трогается, сообщение агенту всё равно уходит (B).
5. Старые треды без рекомендации и задачи без `brainstorm_skill` не ломают экраны и метрику (`unknown`, нули) (D, E, F).
