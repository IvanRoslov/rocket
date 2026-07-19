# API демона

HTTP+JSON, префикс `/v1`. Листенеры: Unix-сокет `~/.rocket/rocket.sock`, `http://127.0.0.1:<port>` (по умолчанию 4477) и `https://127.0.0.1:<tls_port>` (по умолчанию 4478, `tls_port: 0` выключает) — все три отдают один и тот же API. Ошибки: `{"error": {"code": "<machine_code>", "message": "..."}}`, HTTP-коды стандартные.

**https-листенер существует ради HTTP/2**: браузеры ограничивают cleartext HTTP/1.1 ~6 соединениями на хост суммарно на весь браузер — долгоживущие SSE-стримы дашборда исчерпывают пул, и загрузки страниц зависают в очереди; HTTP/2 (браузеры говорят его только поверх TLS) мультиплексирует всё в одно соединение. Дашборд и мобильное приложение должны предпочитать `https://…:<tls_port>`. Сертификат — `~/.rocket/tls/{cert,key}.pem`: при первом старте генерируется self-signed (браузер предупредит, пока сертификат не доверен в системе); чтобы предупреждения не было — положить туда пару от mkcert (`mkcert -cert-file cert.pem -key-file key.pem localhost 127.0.0.1 ::1`) и перезапустить демон.

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
| GET | `/v1/sessions/{id}/chat?cursor=&limit=` | Лента чата — зеркало нативного транскрипта агента, см. [13-chat.md](13-chat.md) |
| POST | `/v1/sessions/{id}/quiz/answer` | Удалённый ответ на pending-квиз AskUserQuestion: `{answers:[{question_index, option_indices?[], text?}]}` → `202 {status:"answering"}`; `409 no_pending_quiz|quiz_answer_in_flight`, `400 quiz_answer_invalid`. См. [13-chat.md](13-chat.md), раздел «Квизы» |
| GET | `/v1/sessions/{id}/attach` | `{command: ["tmux","attach","-t","=..."]}` |
| WS | `/v1/sessions/{id}/term` | Живой терминал сессии (см. ниже) |

### WebSocket-терминал

`GET /v1/sessions/{id}/term` с Upgrade на WebSocket. На каждое соединение демон запускает `tmux attach -t =<name>` в собственном PTY (Go: `creack/pty`) и гоняет байты в обе стороны:

- **server → client**: бинарные фреймы — вывод терминала (рендерится xterm.js как есть);
- **client → server**: бинарные фреймы — ввод пользователя; текстовые фреймы — контрол-сообщения JSON: `{type:"resize", cols, rows}` (resize PTY), `{type:"ping"}`.
- `?readonly=true` — ввод игнорируется (режим наблюдения).

Несколько параллельных зрителей — это просто несколько tmux-клиентов одной сессии (штатно для tmux). Закрытие WS убивает только attach-клиент, сессию не трогает. Доступ — как у всего API: localhost/socket, без внешней аутентификации.

#### Размер окна (client-size policy)

tmux рендерит окно ровно в **одном** размере; при одновременных клиентах разных размеров кто-то неизбежно видит обрезанную (и панорамируемую за курсором) картинку — это читается как обрезанные справа строки и «уезжающие» фрагменты при скролле. Политика rocket: **веб-терминал — основная поверхность и всегда рендерится точно и во всю ширину**; деградация, если она неизбежна, достаётся локальным клиентам.

- Пока открыт хотя бы один веб-терминал с правом ввода (не `readonly`), демон на каждый resize-фрейм **пиннит** окно ровно под веб-клиент: `resize-window -x -y` (высота минус строка статуса), что переводит `window-size` в `manual`. Локальный клиент шире окна видит его с пустыми полями, уже — обрезанным; это ожидаемая и допустимая деградация.
- Когда последний такой веб-терминал отключается, демон возвращает `window-size latest` (базовая политика, она же ставится при создании сессии) — окно снова следует за локальными клиентами. Несколько веб-вкладок учитываются по счётчику: закрытие одной не сбрасывает пин другой.
- `readonly`-зрители размер не пиннят (и resize у них игнорируется): при несовпадении размеров именно их картинка может быть обрезана.
- Если демон умер, не сняв пин (крайний случай), окно остаётся `manual` до следующего веб-подключения; вручную лечится `tmux set-option -w -t <сессия> window-size latest`.

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
| GET | `/v1/tasks/{id}/questions` | Вопросы задачи с тредами; `?status=open` |
| POST | `/v1/tasks/{id}/questions` | `{body, context?}` — только оркестратор задачи; событие `task.question_asked` |
| POST | `/v1/questions/{id}/reply` | `{body}` — реплика в тред от любой стороны; вопрос остаётся open. Реплика пользователя доставляется оркестратору через очередь (`[task #N QM reply] ...`); реплика оркестратора поднимает бейдж пользователю. Событие `task.question_replied` |
| POST | `/v1/questions/{id}/answer` | `{body}` или `{dismiss: true}` — только пользователь; финальный ответ закрывает вопрос (`resolved`), уходит оркестратору как `[task #N QM answer] ...`. Событие `task.question_resolved` |

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

Формат события: `{id, ts, type, session_id?, data{}}`. Типы: `session.spawned|state_changed|activity_changed|killed|restored|chat_updated`, `message.queued|delivered|failed`, `pr.opened|ci_changed|merged`, `orchestrator.heartbeat_sent`, `workspace.branch_collision|cleanup`, `repo.clone_started|clone_done|clone_failed`, `task.question_asked|question_replied|question_resolved` и т.д. `session.chat_updated` — пинг о том, что транскрипт сессии изменился; поле `data` у этого события отсутствует целиком, см. [13-chat.md](13-chat.md). `session.quiz_asked|quiz_resolved|quiz_answer_unconfirmed` — квиз-пинги того же формата (без `data`): показан pending-квиз AskUserQuestion / квиз закрыт (отвечен или отменён, в т.ч. в терминале) / удалённый ответ напечатан, но закрытие не подтвердилось за таймаут — см. [13-chat.md](13-chat.md), раздел «Квизы».

## Система

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/system` | Обзор для экрана System дашборда: даемон, очередь, tmux, worktree'ы, хвост лога |
| POST | `/v1/system/cleanup` | Убивает осиротевшие tmux-сессии и удаляет осиротевшие worktree-директории |

`GET /v1/system` → `{"daemon":{"version","uptime_s","port","socket","db_path","config_path"},"queue":{"queued":N,"failed":N},"tmux":[{"name","session_id?","state?","orphan":bool}],"worktrees":[{"path","session_id?","size_bytes","state?","orphan":bool}],"log_tail":["..."]}`.

- `queue.queued`/`queue.failed` — число сообщений в очереди сообщений (`internal/store`) в соответствующем статусе.
- `tmux[]` — все живые tmux-сессии с именем, похожим на сессию rocket (`^[a-z0-9-]+$`); `orphan: true`, только если **вообще ни одна** запись в сторе (в любом состоянии — `spawning`/`running`/`killed`/`errored`/`done`) не ссылается на это имя (`session_id`/`state` в этом случае отсутствуют). Если запись есть, но сессия уже `killed`/`errored`/`done` — это не orphan: `session_id` и `state` заполнены, чтобы дашборд мог показать такие «хвосты» отдельно.
- `worktrees[]` — все директории ворктри на диске (`<worktrees_dir>/<repo-id>/<session-id>/`) с их размером на диске; правило `orphan`/`state` то же самое, что и для `tmux[]` — только запись в сторе (в любом состоянии) снимает статус orphan.
- `log_tail` — последние строки `rocketd.log`, не более 200 строк и не более 64 КиБ с конца файла.

`POST /v1/system/cleanup` → `{"killed_tmux":[names], "removed_worktrees":[paths]}` — удаляет **только** ресурсы без какой-либо записи в сторе (истинные orphan'ы, см. выше); ветку при этом никогда не удаляет — только рабочую копию. Ресурсы `killed`/`errored`/`done`-сессий не трогает — их убирает `kill --cleanup` или `restore`.

## Внутренние (для hook-скриптов агентов)

| Метод | Путь | Описание |
|---|---|---|
| POST | `/v1/internal/activity` | `{session, state, ts}` — hook агента репортит активность (push-канал, дополняющий поллинг) |
| POST | `/v1/internal/quiz` | `{session, phase: "pending"\|"resolved", payload}` — PreToolUse/PostToolUse-хуки AskUserQuestion репортят показ/закрытие квиза; payload — сырой stdin хука. Пишет/чистит `pending_quiz` сессии и публикует `session.quiz_asked`/`quiz_resolved` |
