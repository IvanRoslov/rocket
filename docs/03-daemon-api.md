# API демона

HTTP+JSON, префикс `/v1`. Листенеры: Unix-сокет `~/.rocket/rocket.sock` и `127.0.0.1:<port>` (порт в конфиге, по умолчанию 4477). Ошибки: `{"error": {"code": "<machine_code>", "message": "..."}}`, HTTP-коды стандартные.

## Служебное

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/health` | `{status, version, uptime}` |
| POST | `/v1/shutdown` | Штатная остановка демона (сессии не трогает) |

## Проекты

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/projects` | Список проектов реестра |
| POST | `/v1/projects` | Регистрация: `{id?, path}` — валидирует git-репо, пишет в config.yaml |
| PATCH | `/v1/projects/{id}` | Изменение полей (в т.ч. `links`) |
| DELETE | `/v1/projects/{id}` | Удаление из реестра (сессии должны быть закрыты) |

## Сессии

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/sessions` | Список; фильтры `?kind=&project=&feature=&state=` |
| GET | `/v1/sessions/{id}` | Полная карточка сессии |
| POST | `/v1/orchestrators` | `{description, project, agent?}` → спавн оркестратора; ответ `{id, feature_slug}` |
| POST | `/v1/workers` | `{caller, task, project, prompt, agent?}` → спавн воркера; caller обязан быть живым оркестратором, project ∈ {хаб caller, links хаба} |
| POST | `/v1/sessions/{id}/kill` | Убить сессию: tmux destroy + `state=killed`; `?cleanup=true` — ещё и worktree |
| POST | `/v1/sessions/{id}/restore` | Восстановить упавшую сессию (worktree restore + перезапуск агента) |
| GET | `/v1/sessions/{id}/output?lines=N` | capture-pane |
| GET | `/v1/sessions/{id}/attach` | `{command: ["tmux","attach","-t","=..."]}` |

Спавн-эндпоинты отвечают сразу после резервирования (`state=spawning`); завершение спавна видно по событиям/GET.

## Задачи

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/tasks` | Список; фильтры `?status=&project=&parent=`; `?board=true` — сгруппировано по колонкам |
| POST | `/v1/tasks` | `{title, description?, project, parent_id?}` |
| GET | `/v1/tasks/{id}` | Карточка: поля + подзадачи + привязанная сессия (с `tmux_name` и attach-командой) |
| PATCH | `/v1/tasks/{id}` | `{status?, title?, description?}` — ручной move и правки |
| POST | `/v1/tasks/{id}/start` | Создать оркестратора и назначить на задачу (`{agent?}`); задача → `in_progress` |
| POST | `/v1/tasks/{id}/cancel` | Отмена; каскадно убивает сессии задачи |
| GET | `/v1/tasks/{id}/docs` | Документы (последние версии; `?history=true` — все) |
| PUT | `/v1/tasks/{id}/docs` | `{kind, title, body}` — создаёт новую версию |
| GET | `/v1/tasks/{id}/log` | Журнал; `?kind=` |
| POST | `/v1/tasks/{id}/log` | `{kind, body}` |

Права: вызовы от агентов (определяются по `from`/env сессии) ограничены — оркестратор пишет только в свою задачу и её подзадачи, воркер — только в свою подзадачу. Автопереходы статусов (spawn → подзадача `in_progress`, PR open → `review`, merged → `done`) делает демон и записывает в `task_log` с `kind=status`.

## Сообщения

| Метод | Путь | Описание |
|---|---|---|
| POST | `/v1/messages` | `{from?, to, body}` → ставит в очередь, ответ `{id, status:"queued"}` |
| GET | `/v1/messages?session={id}&limit=N` | История сообщений сессии (в обе стороны) |
| GET | `/v1/messages/{id}` | Статус конкретного сообщения |

## События

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/events?since=<id>&limit=N&session=` | Журнал |
| GET | `/v1/events/stream` | SSE; `?session=` — фильтр |

Формат события: `{id, ts, type, session_id?, data{}}`. Типы: `session.spawned|state_changed|activity_changed|killed|restored`, `message.queued|delivered|failed`, `pr.opened|ci_changed|merged`, `orchestrator.heartbeat_sent`, `workspace.branch_collision|cleanup` и т.д.

## Внутренние (для hook-скриптов агентов)

| Метод | Путь | Описание |
|---|---|---|
| POST | `/v1/internal/activity` | `{session, state, ts}` — hook агента репортит активность (push-канал, дополняющий поллинг) |
