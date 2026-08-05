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
| GET | `/v1/github/issues` | Issues репозитория (PR отфильтрованы) — для создания тасков из issue, см. `docs/09-github.md` |

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
| GET | `/v1/sessions` | Список; фильтры `?kind=&project=&feature=&state=`; у элемента `waiting_terminal` (см. ниже) |
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

`stale` (в ответах треда, аддитивное поле задачи #1023) — производный флаг «тред завис»: открытый тред типа `decision`, у которого непусто attention-множество и от последней записи прошло больше `question_stale_after` (yaml, по умолчанию 24h). Считается на каждом чтении тем же правилом, что и напоминания хартбита (`heartbeat.StaleThread`), никогда не хранится в базе, `omitempty` — у здоровых тредов поля просто нет. Человеку heartbeat сообщений не шлёт, поэтому для него это единственный канал: из него дашборд рисует бейдж «stale». В единый инбокс (`GET /v1/threads`, `rocket questions`) поле не входит — там то же видно как `updated_at`, то есть возраст треда в строке.

`waiting_terminal` (в ответах сессии и задачи) — производный флаг «висит на интерактивном вводе»: сессия держит незакрытый quiz `AskUserQuestion` либо её `activity = waiting_input`, и так дольше `input_stall_threshold` (yaml, по умолчанию 10 минут). Тот же предикат, что у эскалации хартбита (`heartbeat.InputStalled`), тот же порог; считается на каждом чтении поверх живых сессий и никогда не хранится в базе (`omitempty` — у здоровых поля просто нет, оно гаснет само, как только квиз закрыт). У задачи флаг берётся с её собственной сессии; задача без сессии не помечается никогда. Рендерится как `⏳ ждёт ответа в терминале` в `rocket task ls`, как `waiting_input ⏳` в колонке ACTIVITY у `rocket status` и бейджем на карточке в дашборде.

`quiet` (в ответах задачи) — производный флаг «майлстон молчит»: майлстон в `in_progress`, взятый агентом, у которого дольше `milestone_quiet_after` (yaml, по умолчанию 24h) нет ни одного видимого следа в этом майлстоне — не-`status`-записи журнала, документа, вопроса или сообщения в треде **его** авторства. Считается на каждом чтении тем же правилом, что напоминания хартбита (`heartbeat.QuietMilestone`), в базе не хранится, `omitempty`. Обычная задача, не взятый майлстон и майлстон не в `in_progress` не помечаются никогда. Для человека это единственный канал (сообщений про тишину ему не шлют): из флага дашборд рисует бейдж «🤐 quiet». Рядом идёт событие `milestone.quiet`. Поля `milestone` и `assigned_role` в тех же ответах — хранимые: признак майлстона и id держателя.

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
| GET | `/v1/tasks` | Список; фильтры `?status=&project=&parent=`; `?milestones=true` — только майлстоны; `?board=true` — сгруппировано по колонкам; у элемента `waiting_terminal` (см. «Сессии») и `quiet` (см. ниже) |
| POST | `/v1/tasks` | `{title, description?, project, parent_id?, milestone?}`. Человек и постоянный агент (`kind='agent'`) создают любые задачи (`created_by` = `user`/`agent`); оркестратор — только подзадачи своей задачи (иначе `403 agents may only create subtasks` / `parent task does not belong to caller`); воркер — `403 workers may not create tasks`. `milestone: true` создаёт майлстон — корневую задачу вне проектов: вместе с `project` это `400 milestone_with_project`, вместе с `parent_id` — `400 milestone_with_parent` |
| GET | `/v1/tasks/{id}` | Карточка: поля + подзадачи + привязанная сессия (с `tmux_name` и attach-командой) |
| PATCH | `/v1/tasks/{id}` | `{status?, title?, description?}` — ручной move и правки. В майлстон пишут только человек и его держатель; `status=review` требует дока или не-`status`-записи журнала от держателя (иначе `422 milestone_empty`), `done`/`cancelled` доступны только человеку (`403 human_only`) |
| POST | `/v1/tasks/{id}/start` | Создать оркестратора и назначить на задачу (`{agent?}`); задача → `in_progress`. Только человек или постоянный агент; остальным `403 only the human user or a registered agent may start a task`. На майлстоне — `403 milestone_not_startable`: его берут через `/take` |
| POST | `/v1/tasks/{id}/take` | Майлстон: постоянный агент берёт **не взятый** майлстон (id держателя — из сессии вызывающего, тела нет). Только `kind='agent'` — иначе `403 agent_only`; не майлстон — `403 not_a_milestone`; занят другим — `409 already_taken`; свой же — `200` без изменений. Пишет `task_log` (`kind=status`) и публикует `task.assigned` |
| POST | `/v1/tasks/{id}/assign` | Майлстон: человек вручает его агенту (`{agent_id}`) или снимает (`{none: true}`) — ровно одно из двух, иначе `400 bad_request`. Только человек (вызов от сессии — `403 human_only`); не майлстон — `403 not_a_milestone`; неизвестный агент — `400 agent_not_found`. Назначенному агенту уходит уведомление (живому в сессию, иначе в инбокс), плюс `task_log` и `task.assigned` |
| POST | `/v1/tasks/{id}/cancel` | Отмена; каскадно убивает сессии задачи. У майлстона — только человек (`403 human_only`) |
| GET | `/v1/tasks/{id}/docs` | Документы (последние версии; `?history=true` — все) |
| PUT | `/v1/tasks/{id}/docs` | `{kind, title, body}` — создаёт новую версию |
| GET | `/v1/tasks/{id}/log` | Журнал; `?kind=` |
| POST | `/v1/tasks/{id}/log` | `{kind, body}` |
| GET | `/v1/questions` | Все открытые вопросы по всем задачам (для глобальной страницы Questions дашборда): элемент = вопрос (та же форма, что и в `/v1/tasks/{id}/questions`) + `task_title`, `project_id`, `project_name`, `orchestrator_name?` |
| GET | `/v1/tasks/{id}/questions` | Вопросы задачи с тредами; `?status=open` |
| GET | `/v1/threads` | Единый инбокс тредов: **и задач, и ролей** одним списком (за ним стоит `rocket questions`). `?waiting_on=<id>` — только треды, ждущие названного участника (фильтр по attention set); `?all=true` — вместе с закрытыми, включая `fyi`. Элемент — «шапка» треда без переписки: `local_ref`, `kind` (`task`&#124;`role`), `task_id`/`role_id`, `subject`, `body` (сам вопрос), `type`, `options`, `participants`, `attention` (оно же `waiting_on`), `your_turn`, `updated_at` (последнее движение). Права применяются к каждому треду отдельно — тем же правилом, что и на чтение одного треда |
| POST | `/v1/tasks/{id}/questions` | `{body, context?, to?, type?, options?}` — только на корневой задаче. `type` — `decision` (по умолчанию) или `fyi` (тред рождается `resolved` с резолюцией `fyi`, attention пуст); `options` — массив строк-вариантов для последующего `choose`. Открыть тред может человек (без `X-Rocket-Session`), любой постоянный агент и оркестратор самой задачи; воркер получает `403`. Участниками сеются человек, автор и оркестратор задачи (если он есть), плюс `to`; запись доставляется всем им, кроме автора, как `[#N/QM question from <кто>] ...` (+ `context`, если есть). Событие `task.question_asked` |
| POST | `/v1/questions/{id}/reply` | `{body, to?, join?, dry_run?}` — реплика в тред от **любого участника** (оркестратор задачи допускается и до того, как впервые написал). Не-участник получает `403 not_a_participant` с эхом цели и составом треда; человек и постоянный агент могут повторить с `join: true`, оркестратор и воркер чужой задачи — нет. `dry_run: true` возвращает эхо цели и ничего не пишет. В resolved-вопрос: человек → `409 question_resolved`; любой другой участник → **переоткрывает** вопрос (оспаривание финального ответа доказательствами; статус снова open, события `task.question_reopened` + `task.question_replied`) |
| POST | `/v1/questions/{id}/answer` | `{body, to?, choose?, join?, dry_run?}` или `{dismiss: true}` — человек и постоянный агент (`kind='agent'`); оркестратору и воркеру `403 forbidden` с текстом «only the human user or a persistent agent may answer; use reply». `choose` — номер варианта из `options` (1-based), его текст и становится телом ответа; вне диапазона — `400 invalid_choice`. Закрывает тред (`resolved`), очищает attention и рассылается участникам как `[#N/QM answer from <кто>] ...`. Событие `task.question_resolved` |

Тред — это список участников, а не диалог двух сторон; полная модель — в [12-tasks.md](12-tasks.md), раздел «Вопросы и ответы через задачу». В ответе вопроса поверх прежних полей есть: `local_ref` — единственный пользовательский id треда (`"1023/Q2"`, у ролей `"cto/Q1"`); `participants` — идентификаторы участников (`human`, id постоянного агента, session id); `attention` — от кого ждут хода, и `waiting_on` — то же множество под прежним именем; `your_turn` — входит ли в него вызывающий; `type` (`decision`&#124;`fyi`) и `options`; у каждого сообщения `addressed_to` — кому оно адресовано (пусто = всем, кроме автора). Совместимое поле `whose_turn` сохранено и выводится из attention. У пишущих ответов есть `echo` — та же строка подтверждения цели, что печатает CLI; при `dry_run` ответ состоит из `{dry_run: true, echo}`.

Поле `to` в запросах задаёт, **от кого ждут хода**, и добавляет названных в участники; на доставку оно не влияет — каждая запись уходит всем участникам, кроме автора. Начиная с задачи #1023 attention — **хранимое** множество (`question_attention`), а не производное от последней записи: открытие ставит в него адресатов (или всех, кроме автора), каждая следующая запись выводит своего автора и вводит своих адресатов, а опустевшее множество передаёт ход остальным участникам. Прежнее правило «побеждает последняя запись, реплика без `to` возвращает очередь всем» **отменено**: реплика одного из двоих ожидаемых не снимает ход со второго.

Идентификаторы участников на проводе едины: человек всюду — `asked_by`, `messages[].author`, `participants`, `waiting_on`, `addressed_to` — приходит каноническим `human`. Прежняя пустая строка больше не отправляется; клиентам стоит и дальше считать человеком оба варианта, чтобы пережить старые кэши и записи.

Права: вызовы от сессий (определяются по `from`/env сессии) ограничены — постоянный агент (`kind='agent'`) в правах на задачи приравнен к человеку, оркестратор пишет только в свою задачу и её подзадачи, воркер — только в свою подзадачу. Автопереходы статусов (spawn → подзадача `in_progress`, PR open → `review`, merged → `done`) делает демон и записывает в `task_log` с `kind=status`.

## Постоянные агенты

Агент — зарегистрированный «дежурный» («SRE платформы», «разборщик issues»), к которому обращаются люди и другие агенты. Rocket им не управляет: регистрация, доставка и инбокс — всё (см. [10-agents.md](10-agents.md) и спеку v4 задачи #639).

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/agents` | Список агентов; фильтр `?project=`; у элемента `session_alive` (жива ли tmux-сессия `<id>`), `unread` (непрочитанных в инбоксе), `open_questions` и `awaiting_user` (открытые треды и из них ждущие человека) |
| POST | `/v1/agents` | `{id, description?, project?, dir?, command?}` → 201. `id` — `^[a-z0-9-]+$`, он же имя tmux-сессии; `project` проверяется, только если непустой |
| GET | `/v1/agents/{id}` | Карточка агента; `milestones` — майлстоны, которые он держит (`{id, title, status}`, старые первыми), см. [12-tasks.md](12-tasks.md) |
| PATCH | `/v1/agents/{id}` | `{description?, project?, dir?, command?, enabled?}` |
| DELETE | `/v1/agents/{id}` | Удаляет агента вместе с инбоксом и тредами; файлы на диске остаются |
| POST | `/v1/agents/{id}/enable`&#124;`disable` | Включить/выключить агента |
| POST | `/v1/agents/{id}/messages` | `{body, from?}` → `202 {to, status:"queued"&#124;"inbox", live}`. Живой сессии — доставка очередью, иначе строка в инбоксе |
| GET | `/v1/agents/{id}/inbox` | Сообщения инбокса, старые первыми; фильтр `?status=unread&#124;read` |
| POST | `/v1/agents/{id}/inbox/next` | Отдаёт самое старое непрочитанное и помечает его прочитанным → `200 {id, from, body, status, created_at, read_at}`; пустой инбокс → `204` |
| GET | `/v1/agents/{id}/inbox/{msg}` | Одно сообщение целиком, статус не меняется (peek); чужое сообщение — `404` |
| POST | `/v1/agents/{id}/start` | Лончер: tmux-сессия `<id>` (cwd `dir`, `command`, env `ROCKET_*`) → `200 {id, status:"running", dir}`. Нет `dir` → `400 agent_no_dir`, сессия уже жива → `409 agent_live` |
| POST | `/v1/agents/{id}/stop` | Убивает tmux-сессию; регистрация остаётся → `200 {id, status:"stopped"}` |
| GET | `/v1/agents/{id}/questions` | Q&A-треды агента; фильтр `?status=open` |
| POST | `/v1/agents/{id}/questions` | `{body, context?, to?, type?, options?}` → 201. Направление — по вызывающему: человек (без заголовка сессии) спрашивает агента, агент эскалирует человеку. Участниками сеются человек, автор и сама роль, плюс `to`. `type`/`options` — как у тредов задач |
| POST | `/v1/agent-questions/{qid}/reply` | `{body, to?, join?, dry_run?}` → 201. Любой участник треда; не-участник — `403 not_a_participant` (человек и постоянный агент могут повторить с `join`). reply в закрытый тред от любого участника, кроме человека, переоткрывает его (человеку — `409 question_resolved`) |
| POST | `/v1/agent-questions/{qid}/answer` | `{body, to?, choose?, join?, dry_run?}` &#124; `{dismiss:true}` → 200. Закрывает тред; человек и постоянный агент, остальным `403 forbidden` |

Тред роли — та же сущность, что тред задачи, только с заполненным `role_id` (отдельных таблиц `agent_questions` / `agent_question_messages` больше нет). Поля `local_ref`, `participants`, `attention`/`waiting_on`, `your_turn`, `type`, `options` и `addressed_to` те же, что у тредов задач; `whose_turn` (`user`&#124;`role`) выводится из attention. **Каждая** запись треда доставляется каждому участнику, кроме её автора, тем же путём, что обычное сообщение, с префиксом `[<id>/Q<n> question|reply|answer from <кто>] ...`. Человеку ничего не инжектится — он читает тред через API/CLI.

`POST /v1/messages` с `to`, совпадающим с id агента, идёт тем же путём: живой сессии — обычная доставка, иначе инбокс, ответ `202 {to, status, live}`. Мёртвая сессия агента не даёт `409 recipient_terminal` — сообщение просто ложится в инбокс.

Права на треды (общие для тредов задач и ролей, `internal/api/threads.go`): человек и постоянный агент (`kind='agent'`) читают любой тред, а пишут в чужой — только с `join: true` (без него `403 not_a_participant` с эхом цели и составом треда); оркестратор и воркер — только там, где они участники, плюс треды своей задачи, причём «своя задача» для воркера — **корневая** задача над его подзадачей (вопросы живут только на корневых задачах), и `join` им не поможет. Закрыть тред (`answer`/`choose`/`dismiss`) могут человек и постоянный агент; оркестратор и воркер получают `403` с подсказкой использовать `reply`.

## Сообщения

| Метод | Путь | Описание |
|---|---|---|
| POST | `/v1/messages` | `{from?, to, body}` → ставит в очередь, ответ `{id, status:"queued"}` |
| GET | `/v1/messages?session={id}&limit=N` | История сообщений сессии (в обе стороны) |
| GET | `/v1/messages/{id}` | Статус конкретного сообщения |

## Вложения

| Метод | Путь | Описание |
|---|---|---|
| POST | `/v1/attachments` | Тело запроса — сырые байты картинки (не multipart), тип берётся из `Content-Type`: `image/png`, `image/jpeg` или `image/webp` (иначе `415`); лимит 10 MiB (иначе `413`) → `201 {id, url}` |
| GET | `/v1/attachments/{id}` | Отдаёт файл с исходным `Content-Type` и агрессивным `Cache-Control` (immutable — id никогда не переписывается) |

Вставляются в markdown как `![...](/v1/attachments/{id})` (так их вставляет `usePasteImage` при Ctrl+V в дашборде). При постановке сообщения в очередь агенту (`POST /v1/messages`, доставка вопроса) такие ссылки переписываются в `[screenshot: <абсолютный путь к файлу>]`, чтобы агент мог открыть картинку через Read — см. [05-state.md](05-state.md) и `internal/api/attachments.go`.

## События

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/events?since=<id>&limit=N&session=` | Журнал |
| GET | `/v1/events/stream` | SSE; `?session=` — фильтр |

Формат события: `{id, ts, type, session_id?, data{}}`. Типы: `session.spawned|state_changed|activity_changed|killed|restored|chat_updated`, `message.queued|delivered|failed`, `pr.opened|ci_changed|merged`, `orchestrator.heartbeat_sent`, `orchestrator.input_stalled` (оркестратор дольше `input_stall_threshold` ждёт интерактивного ввода: `data{task_id, session_id, since_seconds, kind: quiz|prompt}`, см. [08-orchestrators.md](08-orchestrators.md)), `workspace.branch_collision|cleanup`, `repo.clone_started|clone_done|clone_failed`, `task.question_asked|question_replied|question_resolved|question_reopened`, `task.assigned` (майлстон сменил держателя: `data{task_id, agent_id, by, verb: take|assign}`; `agent_id` пуст — майлстон сняли), `milestone.quiet` (взятый майлстон дольше `milestone_quiet_after` без следов работы держателя: `data{task_id, agent_id, title, since_seconds, reminded}`; `reminded` — было ли на этом проходе отправлено напоминание, анти-спам режет его до одного раза в 24h), `question.stale` (открытый decision-тред без движения дольше `question_stale_after`: `data{question_id, task_id, role_id, local_ref, since_seconds, attention, reminded}`, см. [08-orchestrators.md](08-orchestrators.md)) и т.д. `session.chat_updated` — пинг о том, что транскрипт сессии изменился; поле `data` у этого события отсутствует целиком, см. [13-chat.md](13-chat.md). `session.quiz_asked|quiz_resolved|quiz_answer_unconfirmed` — квиз-пинги того же формата (без `data`): показан pending-квиз AskUserQuestion / квиз закрыт (отвечен или отменён, в т.ч. в терминале) / удалённый ответ напечатан, но закрытие не подтвердилось за таймаут — см. [13-chat.md](13-chat.md), раздел «Квизы».

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
