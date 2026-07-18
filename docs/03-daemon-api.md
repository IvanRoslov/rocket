# API демона

HTTP+JSON, префикс `/v1`. Листенеры: Unix-сокет `~/.rocket/rocket.sock` и `127.0.0.1:<port>` (порт в конфиге, по умолчанию 4477). Ошибки: `{"error": {"code": "<machine_code>", "message": "..."}}`, HTTP-коды стандартные.

## Служебное

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/health` | `{status, version, uptime}` |
| POST | `/v1/shutdown` | Штатная остановка демона (сессии не трогает) |

## Настройки и GitHub

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/settings` | Настройки (секреты замаскированы) |
| PUT | `/v1/settings` | `{github_token?: "..."}` — валидирует токен запросом к GitHub |
| GET | `/v1/github/repos?q=` | Репозитории, доступные токену (для UI выбора), с кэшем |

## Репозитории

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/repos` | Список зарегистрированных репозиториев |
| POST | `/v1/repos` | Регистрация: `{id?, path}` — локальный чекаут, либо `{github: "owner/name"}` — демон клонирует в `~/.rocket/repos/` и регистрирует |
| PATCH | `/v1/repos/{id}` | Изменение полей (env, symlinks, post_create, …) |
| DELETE | `/v1/repos/{id}` | Удаление из реестра (не должен входить в проекты) |

## Проекты

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/projects` | Список проектов (+ агрегаты: задачи по статусам, живые сессии) |
| POST | `/v1/projects` | `{id?, name, main, linked?}` — main/linked это id репозиториев |
| GET | `/v1/projects/{id}` | Карточка проекта: репозитории, счётчики |
| PATCH | `/v1/projects/{id}` | Изменение `name`, `main`, `linked` |
| DELETE | `/v1/projects/{id}` | Удаление (задачи должны быть закрыты/отменены) |

Для UI создания проекта: POST `/v1/repos` принимает и путь, введённый вручную — так дашборд регистрирует репо и сразу добавляет его в проект.

## Сессии

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/sessions` | Список; фильтры `?kind=&project=&feature=&state=` |
| GET | `/v1/sessions/{id}` | Полная карточка сессии |
| POST | `/v1/orchestrators` | `{description, project, agent?}` → спавн оркестратора; ответ `{id, feature_slug}` |
| POST | `/v1/workers` | `{caller, task, repo, prompt, agent?}` → спавн воркера; caller обязан быть живым оркестратором, repo ∈ репозитории проекта caller (main + linked) |
| POST | `/v1/sessions/{id}/kill` | Убить сессию: tmux destroy + `state=killed`; `?cleanup=true` — ещё и worktree |
| POST | `/v1/sessions/{id}/restore` | Восстановить упавшую сессию (worktree restore + перезапуск агента) |
| GET | `/v1/sessions/{id}/output?lines=N` | capture-pane (одноразовый снимок) |
| GET | `/v1/sessions/{id}/attach` | `{command: ["tmux","attach","-t","=..."]}` |
| WS | `/v1/sessions/{id}/term` | Живой терминал сессии (см. ниже) |

### WebSocket-терминал

`GET /v1/sessions/{id}/term` с Upgrade на WebSocket. На каждое соединение демон запускает `tmux attach -t =<name>` в собственном PTY (Go: `creack/pty`) и гоняет байты в обе стороны:

- **server → client**: бинарные фреймы — вывод терминала (рендерится xterm.js как есть);
- **client → server**: бинарные фреймы — ввод пользователя; текстовые фреймы — контрол-сообщения JSON: `{type:"resize", cols, rows}` (resize PTY), `{type:"ping"}`.
- `?readonly=true` — ввод игнорируется (режим наблюдения).

Несколько параллельных зрителей — это просто несколько tmux-клиентов одной сессии (штатно для tmux). Закрытие WS убивает только attach-клиент, сессию не трогает. Доступ — как у всего API: localhost/socket, без внешней аутентификации.

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
| GET | `/v1/tasks/{id}/questions` | Вопросы задачи; `?status=open` |
| POST | `/v1/tasks/{id}/questions` | `{body, context?}` — только оркестратор задачи; событие `task.question_asked` |
| POST | `/v1/questions/{id}/answer` | `{body}` — сохраняет ответ и ставит его в очередь сообщений оркестратору (`[task #N answer QM] ...`); событие `task.question_answered` |

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

Формат события: `{id, ts, type, session_id?, data{}}`. Типы: `session.spawned|state_changed|activity_changed|killed|restored`, `message.queued|delivered|failed`, `pr.opened|ci_changed|merged`, `orchestrator.heartbeat_sent`, `workspace.branch_collision|cleanup`, `repo.clone_started|clone_done|clone_failed`, `task.question_asked|question_answered` и т.д.

## Внутренние (для hook-скриптов агентов)

| Метод | Путь | Описание |
|---|---|---|
| POST | `/v1/internal/activity` | `{session, state, ts}` — hook агента репортит активность (push-канал, дополняющий поллинг) |
