# API демона

HTTP+JSON, префикс `/v1`. Листенеры: Unix-сокет `~/.rocket/rocket.sock`, `http://127.0.0.1:<port>` (по умолчанию 4477) и `https://127.0.0.1:<tls_port>` (по умолчанию 4478, `tls_port: 0` выключает) — все три отдают один и тот же набор маршрутов, но TCP-листенеры требуют аутентификации (см. [«Аутентификация»](#аутентификация)). Ошибки: `{"error": {"code": "<machine_code>", "message": "..."}}`, HTTP-коды стандартные.

**https-листенер существует ради HTTP/2**: браузеры ограничивают cleartext HTTP/1.1 ~6 соединениями на хост суммарно на весь браузер — долгоживущие SSE-стримы дашборда исчерпывают пул, и загрузки страниц зависают в очереди; HTTP/2 (браузеры говорят его только поверх TLS) мультиплексирует всё в одно соединение. Дашборд и мобильное приложение должны предпочитать `https://…:<tls_port>`. Сертификат — `~/.rocket/tls/{cert,key}.pem`: при первом старте генерируется self-signed (браузер предупредит, пока сертификат не доверен в системе); чтобы предупреждения не было — положить туда пару от mkcert (`mkcert -cert-file cert.pem -key-file key.pem localhost 127.0.0.1 ::1`) и перезапустить демон.

## Служебное

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/health` | `{status, version, uptime}` |
| POST | `/v1/shutdown` | Штатная остановка демона (сессии не трогает) |

## Аутентификация

Каналы доступа:

- **Unix-сокет** (`~/.rocket/rocket.sock`, права 0600) — доверенный, аутентификации нет, поведение не менялось. Только по нему принимаются `X-Rocket-Session` (идентичность агента) и `/v1/internal/*`.
- **TCP** (`port` и `tls_port`) — аутентификация всегда, даже с 127.0.0.1. Любой `/v1/*` требует токен устройства: `Authorization: Bearer <token>` (мобилка/CLI) или cookie `rocket_auth` (браузер).

Без токена по TCP открыты только `POST /v1/auth/pair`, `GET /v1/auth/status` и все пути вне `/v1/` (статика SPA).

Проверки на TCP выполняются в таком порядке:

1. Заголовок `Host` должен быть loopback (`localhost`, `127.0.0.0/8`, `::1`), IP-литералом или хостом из `public_url` — иначе `421 bad_host`.
2. `X-Rocket-Session` по TCP — `401 agent_header_forbidden`.
3. `/v1/internal/*` по TCP — `404 not_found`.
4. Нет валидного токена на закрытом маршруте — `401 unauthorized`.
5. Запрос авторизован cookie и небезопасный (не GET/HEAD/OPTIONS) либо WebSocket-upgrade: `Origin` обязан совпасть с origin самого запроса (схема+хост+порт) или с origin из `public_url` — иначе `403 bad_origin`. Bearer-запросы проверке Origin не подлежат.

Отзыв устройства мгновенно обрывает его живые SSE/WebSocket-соединения.

### Токены и коды

- Токен устройства: `rkt_` + base64url без padding от 32 байт `crypto/rand`. Демон хранит только hex SHA-256; сам токен выдаётся один раз при сопряжении.
- Код сопряжения: 8 символов алфавита Crockford base32 (`0123456789ABCDEFGHJKMNPQRSTVWXYZ`), показывается как `XXXX-XXXX`, живёт 10 минут, одноразовый. При вводе нормализуется: верхний регистр, `-` и пробелы отбрасываются, `O→0`, `I→1`, `L→1`. Хранится только хеш.
- Cookie `rocket_auth`: `HttpOnly; SameSite=Strict; Path=/; Max-Age=31536000`; `Secure` — всегда, кроме обычного http на loopback-хост (иначе браузер её не сохранит).
- `last_seen_at` устройства обновляется не чаще раза в минуту.
- Конфиг: `public_url` (необязателен; абсолютный http/https URL с хостом, хвостовой `/` обрезается) — внешний адрес демона, например `https://mac.tail1.ts.net` за `tailscale serve`. Он попадает в allowlist `Host`/`Origin` и в QR сопряжения.

### Маршруты

| Метод | Путь | Описание |
|---|---|---|
| POST | `/v1/auth/pairing-codes` | Выпустить код сопряжения (обычно вызывает `rocket pair` по сокету) → `{"code":"AB12-CD34","expires_at":<unix>,"url":"<public_url или "">"}` |
| POST | `/v1/auth/pair` | Обменять код на токен. Открыт без токена. Тело `{"code","name","kind"}`, `kind` — `mobile` или `web`; `name` обрезается до 64 символов, пустое = `kind`; тело не больше 4 КиБ |
| GET | `/v1/auth/status` | Открыт без токена. `{"authenticated":false}` или `{"authenticated":true,"device":{...}}` |
| POST | `/v1/auth/logout` | Отозвать текущее устройство, сбросить cookie → `204`. По сокету (нет устройства) — `400 no_device` |
| GET | `/v1/auth/devices` | `[{"id","name","kind","created_at","last_seen_at","current"}]`; `last_seen_at` — unix или `null`; `current` — это устройство вызывающего |
| DELETE | `/v1/auth/devices/{id}` | Отозвать устройство → `204`; нет такого — `404 not_found`; нечисловой id — `400 invalid_request` |

`POST /v1/auth/pair` отвечает по-разному в зависимости от `kind`:

- `mobile` → `200 {"device":{...},"token":"rkt_..."}`; приложение хранит токен и шлёт его как Bearer.
- `web` → `200 {"device":{...}}` и `Set-Cookie: rocket_auth=<token>`; токен в теле не возвращается.

Ограничение перебора: после 5 неудачных попыток (глобально) за последние 60 секунд `pair` отвечает `429 rate_limited`.

### Коды ошибок

| HTTP | code | Когда |
|---|---|---|
| 401 | `unauthorized` | нет/неверный/отозванный токен на закрытом маршруте по TCP |
| 401 | `agent_header_forbidden` | `X-Rocket-Session` по TCP |
| 404 | `not_found` | `/v1/internal/*` по TCP; `DELETE /v1/auth/devices/{id}` с неизвестным id |
| 421 | `bad_host` | `Host` вне allowlist |
| 403 | `bad_origin` | cookie-запрос (небезопасный метод или WebSocket) с чужим `Origin` |
| 400 | `invalid_code` | код неизвестен, просрочен или уже использован (случаи неразличимы) |
| 429 | `rate_limited` | слишком много неудачных попыток `pair` |
| 400 | `invalid_request` | битый JSON, неверный `kind`, нечисловой id |

CLI: `rocket pair [--web]`, `rocket devices ls|revoke <id>` — см. [04-cli.md](04-cli.md). `rocket doctor` проверяет loopback-привязку, `public_url` и `tailscale serve`.

## Настройки и GitHub

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/settings` | `{github_token, orchestrator_brainstorm_custom}`: токен замаскирован, переключатель — bool (по умолчанию `false`) |
| PUT | `/v1/settings` | `{github_token?: "...", orchestrator_brainstorm_custom?: bool}` — применяются только присланные поля (переключатель не трогает токен); пустой токен удаляет его, непустой валидируется запросом к GitHub (до записи переключателя). Пустое тело `{}` — `400 bad_request`. Ответ — обе настройки (+ `login` при валидации токена) |
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
| POST | `/v1/sessions/{id}/quiz/answer` | Удалённый ответ на pending-квиз AskUserQuestion: `{answers:[{question_index, option_indices?[], text?}]}` → `202 {status:"answering"}`; `409 no_pending_quiz|quiz_answer_in_flight`, `400 quiz_answer_invalid`. Для `pending_quiz.source = "permission"` (диалог разрешения Claude Code): ровно один индекс, `-1` = Esc, одно нажатие цифры без Enter; `400 invalid_answer`, `409 prompt_changed` (на панели уже другой диалог — клавиши не отправлены). См. [13-chat.md](13-chat.md), разделы «Квизы» и «Разрешения» |
| GET | `/v1/sessions/{id}/attach` | `{command: ["tmux","attach","-t","=..."]}` |
| WS | `/v1/sessions/{id}/term` | Живой терминал сессии (см. ниже) |

`stale` (в ответах треда, аддитивное поле задачи #1023) — производный флаг «тред завис»: открытый тред типа `decision`, у которого непусто attention-множество и от последней записи прошло больше `question_stale_after` (yaml, по умолчанию 24h). Считается на каждом чтении тем же правилом, что и напоминания хартбита (`heartbeat.StaleThread`), никогда не хранится в базе, `omitempty` — у здоровых тредов поля просто нет. Человеку heartbeat сообщений не шлёт, поэтому для него это единственный канал: из него дашборд рисует бейдж «stale». В единый инбокс (`GET /v1/threads`, `rocket questions`) поле не входит — там то же видно как `updated_at`, то есть возраст треда в строке.

`waiting_terminal` (в ответах сессии и задачи) — производный флаг «висит на интерактивном вводе»: сессия держит незакрытый quiz `AskUserQuestion` либо её `activity = waiting_input`, и так дольше `input_stall_threshold` (yaml, по умолчанию 10 минут). Предикат — `heartbeat.InputStalled`; считается на каждом чтении поверх живых сессий и никогда не хранится в базе (`omitempty` — у здоровых поля просто нет, оно гаснет само, как только квиз закрыт). У задачи флаг берётся с её собственной сессии; задача без сессии не помечается никогда. Рендерится как `⏳ ждёт ответа в терминале` в `rocket task ls`, как `waiting_input ⏳` в колонке ACTIVITY у `rocket status` и бейджем на карточке в дашборде.

`quiet` (в ответах задачи) — производный флаг «майлстон молчит»: майлстон в работе (`brainstorm` или `in_progress`), взятый агентом, у которого дольше `milestone_quiet_after` (yaml, по умолчанию 24h) нет ни одного видимого следа в этом майлстоне — не-`status`-записи журнала, документа, вопроса или сообщения в треде **его** авторства. Считается на каждом чтении тем же правилом, что напоминания хартбита (`heartbeat.QuietMilestone`), в базе не хранится, `omitempty`. Обычная задача, не взятый майлстон и майлстон не в работе не помечаются никогда. Для человека это единственный канал (сообщений про тишину ему не шлют): из флага дашборд рисует бейдж «🤐 quiet». Рядом идёт событие `milestone.quiet`. Поля `milestone` и `assigned_role` в тех же ответах — хранимые: признак майлстона и id держателя.

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
| POST | `/v1/tasks/{id}/start` | Создать оркестратора и назначить на задачу (`{agent?}`); задача → `brainstorm` (в `in_progress` её двигает первый `POST /v1/sessions`). До запуска оркестратора запоминает в задаче `brainstorm_skill` по текущей настройке `orchestrator_brainstorm_custom` (`orchestrator-brainstorming@<версия>`, сейчас `@1.1`, — только если агент оркестратора раскладывает этот скилл, т.е. claude-code; иначе `superpowers:brainstorming`; задачи, стартовавшие на своём скилле до #5027, после миграции 0022 — `@1.0`); в ответах задачи поле есть всегда, `""` — задача стартовала до появления поля. Только человек или постоянный агент; остальным `403 only the human user or a registered agent may start a task`. На майлстоне — `403 milestone_not_startable`: его берут через `/take` |
| POST | `/v1/tasks/{id}/take` | Майлстон: постоянный агент берёт **не взятый** майлстон (id держателя — из сессии вызывающего, тела нет). Только `kind='agent'` — иначе `403 agent_only`; не майлстон — `403 not_a_milestone`; занят другим — `409 already_taken`; свой же — `200` без изменений. Пишет `task_log` (`kind=status`) и публикует `task.assigned` |
| POST | `/v1/tasks/{id}/assign` | Майлстон: человек вручает его агенту (`{agent_id}`) или снимает (`{none: true}`) — ровно одно из двух, иначе `400 bad_request`. Только человек (вызов от сессии — `403 human_only`); не майлстон — `403 not_a_milestone`; неизвестный агент — `400 agent_not_found`. Назначенному агенту уходит уведомление (живому в сессию, иначе в инбокс), плюс `task_log` и `task.assigned` |
| POST | `/v1/tasks/{id}/cancel` | Отмена; каскадно убивает сессии задачи. У майлстона — только человек (`403 human_only`) |
| GET | `/v1/tasks/{id}/docs` | Документы (последние версии; `?history=true` — все) |
| PUT | `/v1/tasks/{id}/docs` | `{kind, title, body}` — создаёт новую версию; `kind` — `problem`&#124;`spec`&#124;`plan`&#124;`report`&#124;`doc`. Событие `task.doc_put` `{task_id, kind, version}`; новая `spec` переводит ожидающий гейт задачи в `superseded` (`task.gate_superseded`) |
| POST | `/v1/tasks/{id}/gates` | Запрос гейта выхода из шторма (тела нет). Право — как на запись в задачу (человек, постоянный агент, оркестратор своей задачи; иначе `403`). Фиксирует последние версии `spec` и `plan`; без спеки — `400 no_spec`. Ожидающий гейт становится `superseded`. `201` + объект гейта; события `task.gate_superseded` (если был), `task.gate_requested` |
| GET | `/v1/tasks/{id}/gates` | `{gates: [...]}` — история гейтов, новые первыми |
| POST | `/v1/gates/{id}/decide` | `{decision: "go"\|"changes", comment?}` — **только человек** (любой `X-Rocket-Session`, включая постоянных агентов, → `403`). Не `pending` → `409 gate_not_pending`; спека задачи новее той, по которой запрошен гейт → гейт становится `superseded`, `409 gate_superseded` (проверка — в той же транзакции, что и решение); не удалось сдвинуть статус задачи → `500`, решение откатывается в `pending`, повтор работает; `changes` без комментария → `400 comment_required`; иное `decision` → `400 bad_request`. `go`: задача в `brainstorm` → `in_progress` (как PATCH status: `task_log` + `task.status_changed`), иначе статус не трогается и пишется `note`. Решение доставляется оркестратору задачи: `[rocket gate] Go по спеке vN (план vM) — начинай реализацию.` / `[rocket gate] Нужны правки по спеке vN: <комментарий>`; нет живого оркестратора — сообщение пропускается, решение остаётся. Событие `task.gate_decided` |
| GET | `/v1/tasks/{id}/brainstorm/stats` | Счётчики шторма задачи — один объект формата `storms[]` из `/v1/stats/brainstorm` (для вкладки «Брейншторм»); у задачи без шторма — нули, `answered_by: []`, `by_answerer: []`, `has_gate: false` и `go_at: null`; неизвестная задача — `404`. Только чтение, любой вызывающий |
| GET | `/v1/tasks/{id}/log` | Журнал; `?kind=` |
| POST | `/v1/tasks/{id}/log` | `{kind, body}` |
| GET | `/v1/questions` | Все открытые вопросы по всем задачам (для глобальной страницы Questions дашборда): элемент = вопрос (та же форма, что и в `/v1/tasks/{id}/questions`) + `task_title`, `project_id`, `project_name`, `orchestrator_name?` |
| GET | `/v1/tasks/{id}/questions` | Вопросы задачи с тредами; `?status=open` |
| GET | `/v1/threads` | Единый инбокс тредов: **и задач, и ролей** одним списком (за ним стоит `rocket questions`). `?waiting_on=<id>` — только треды, ждущие названного участника (фильтр по attention set); `?all=true` — вместе с закрытыми, включая `fyi`. Элемент — «шапка» треда без переписки: `local_ref`, `kind` (`task`&#124;`role`), `task_id`/`role_id`, `subject`, `body` (сам вопрос), `type`, `options`, `participants`, `attention` (оно же `waiting_on`), `your_turn`, `updated_at` (последнее движение). Права применяются к каждому треду отдельно — тем же правилом, что и на чтение одного треда |
| POST | `/v1/tasks/{id}/questions` | `{body, title?, brief?, context?, to?, type?, options?, recommend?}` — только на корневой задаче. `brief` — вопрос простыми словами для человека (проблема → чем отличаются варианты → рекомендация); дашборд показывает его первым, тело сворачивает. Сервер хранит его как есть, обязательность и проверки — в CLI (`rocket task ask`/`agent ask` у агентов). `type` — `decision` (по умолчанию) или `fyi` (тред рождается `resolved` с резолюцией `fyi`, attention пуст); `options` — массив строк-вариантов для последующего `choose`. `type: "brainstorm"` — вопрос шторма: нужны хотя бы два `options` и `recommend` (номер рекомендованного варианта, 1-based, в диапазоне), иначе `400 bad_request`; `recommend` у другого типа — тоже `400`. Открыть тред может человек (без `X-Rocket-Session`), любой постоянный агент и оркестратор самой задачи; воркер получает `403`. Участниками сеются человек, автор и оркестратор задачи (если он есть), плюс `to`; запись доставляется всем им, кроме автора, как `[#N/QM question from <кто>] ...` (+ `context`, если есть); при `options` после тела через пустую строку идёт строка `варианты: 1) A ★ рекомендовано  2) B` — та же, что печатает CLI, рекомендованный вариант шторма помечен `★ рекомендовано`. Хранимое тело вариантов не содержит. Событие `task.question_asked` |
| POST | `/v1/questions/{id}/reply` | `{body, to?, join?, dry_run?, dispute?}` — реплика в тред от **любого участника** (оркестратор задачи допускается и до того, как впервые написал). Не-участник получает `403 not_a_participant` с эхом цели и составом треда; человек и постоянный агент могут повторить с `join: true`, оркестратор и воркер чужой задачи — нет. `dry_run: true` возвращает эхо цели и ничего не пишет. В resolved-вопрос: человек → `409 question_resolved` (кроме `fyi`); любой другой участник → запись ложится в историю, статус и attention не меняются. **Переоткрывает** вопрос только `dispute: true` (оспаривание финального ответа доказательствами; статус снова open, события `task.question_reopened` + `task.question_replied`) |
| POST | `/v1/questions/{id}/answer` | `{body, to?, choose?, join?, dry_run?}` или `{dismiss: true}` — человек и постоянный агент (`kind='agent'`); оркестратору и воркеру `403 forbidden` с текстом «only the human user or a persistent agent may answer; use reply». `choose` — номер варианта из `options` (1-based), его текст и становится телом ответа (с `body` — текст варианта, пустая строка и `body` как комментарий); вне диапазона — `400 invalid_choice`. У `brainstorm`-треда ответ записывает `chosen_option` (`null` без `choose`), `answer_comment=body`, `answer_source=ui` и исход `outcome` (`accepted` / `corrected` / `wrong_turn`, см. docs/12-tasks.md «Вопрос шторма»). Закрывает тред (`resolved`), очищает attention и рассылается участникам как `[#N/QM answer from <кто>] ...`. Событие `task.question_resolved` |
| POST | `/v1/questions/{id}/brainstorm-record` | `{choose?, body?}` (хотя бы одно) — оркестратор задачи записывает ответ, который человек дал в его терминале. Только сессия-оркестратор задачи этого треда (иначе `403 forbidden` — в том числе воркеру, чужому оркестратору, постоянному агенту и человеку), только `type=brainstorm` (`400 not_brainstorm`), только открытый (`409 question_resolved`). Закрывает тред как ответ: запись от `human`, `answer_source=terminal`, `chosen_option`/`answer_comment`/`outcome` — как у `answer`. Рассылается участникам, кроме записавшего оркестратора. Событие `task.question_resolved` (с `answer_source: "terminal"`) |
| PATCH | `/v1/questions/{id}/outcome` | `{outcome}` — `accepted` / `corrected` / `wrong_turn`; только человек (любому агенту `403`), только отвеченный `brainstorm`-тред (другой тип или неизвестный исход — `400`, открытый или закрытый как неактуальный — `409 not_answered`). Ставит `outcome_overridden=true`. Событие `task.question_outcome_set` |

Объект гейта: `{id, task_id, spec_version, plan_version, status, comment, decided_by, requested_by, requested_at, decided_at}`; `status` — `pending`&#124;`go`&#124;`changes`&#124;`superseded`; `plan_version` и `decided_at` — `null`, если плана не было / гейт не решён; время — unix-секунды; `requested_by`/`decided_by` — session id или `user`. Модель — в [12-tasks.md](12-tasks.md), раздел «Гейт выхода из шторма».

Тред — это список участников, а не диалог двух сторон; полная модель — в [12-tasks.md](12-tasks.md), раздел «Вопросы и ответы через задачу». В ответе вопроса поверх прежних полей есть: `local_ref` — единственный пользовательский id треда (`"1023/Q2"`, у ролей `"cto/Q1"`); `participants` — идентификаторы участников (`human`, id постоянного агента, session id); `attention` — от кого ждут хода, и `waiting_on` — то же множество под прежним именем; `your_turn` — входит ли в него вызывающий; `type` (`decision`&#124;`fyi`&#124;`brainstorm`) и `options`; поля вопроса шторма `recommended_option`, `chosen_option` (номера 1-based или `null`), `answer_comment`, `answer_source` (`ui`&#124;`terminal`), `outcome` (`accepted`&#124;`corrected`&#124;`wrong_turn`), `outcome_overridden` (у других типов — `null`/`""`/`false`) и `answered_by` — автор ответа, которым закрыт тред (`human`, id агента; `""`, пока ответа нет) — они же есть в `/v1/threads`; у каждого сообщения `addressed_to` — кому оно адресовано (пусто = всем, кроме автора). Совместимое поле `whose_turn` сохранено и выводится из attention. У пишущих ответов есть `echo` — та же строка подтверждения цели, что печатает CLI; при `dry_run` ответ состоит из `{dry_run: true, echo}`.

Поле `to` в запросах задаёт, **от кого ждут хода**, и добавляет названных в участники; на доставку оно не влияет — каждая запись уходит всем участникам, кроме автора. Начиная с задачи #1023 attention — **хранимое** множество (`question_attention`), а не производное от последней записи: открытие ставит в него адресатов (или всех, кроме автора), каждая следующая запись выводит своего автора и вводит своих адресатов, а опустевшее множество передаёт ход остальным участникам. Прежнее правило «побеждает последняя запись, реплика без `to` возвращает очередь всем» **отменено**: реплика одного из двоих ожидаемых не снимает ход со второго.

Идентификаторы участников на проводе едины: человек всюду — `asked_by`, `messages[].author`, `participants`, `waiting_on`, `addressed_to` — приходит каноническим `human`. Прежняя пустая строка больше не отправляется; клиентам стоит и дальше считать человеком оба варианта, чтобы пережить старые кэши и записи.

Права: вызовы от сессий (определяются по `from`/env сессии) ограничены — постоянный агент (`kind='agent'`) в правах на задачи приравнен к человеку, оркестратор пишет только в свою задачу и её подзадачи, воркер — только в свою подзадачу. Автопереходы статусов (start → задача `brainstorm`, первый spawn → задача `in_progress`, spawn → подзадача `in_progress`, PR open → `review`, merged → `done`) делает демон и записывает в `task_log` с `kind=status`.

### Метрика брейншторма

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/stats/brainstorm` | `?weeks=N` (по умолчанию 12, допустимо 1–520, иначе `400 bad_request`) — понедельная сводка и список штормов. Только чтение, любой вызывающий |

Ответ: `{weeks: [{week, skill, answered_by, answered, accepted, accepted_with_comment, corrected, wrong_turn}], storms: [{task_id, title, project_id, skill, questions, answered, accepted, accepted_with_comment, corrected, wrong_turn, answered_by, by_answerer, spec_changes, first_try_go, has_gate, go_at}]}`, где `by_answerer` — `[{answered_by, answered, accepted, accepted_with_comment, corrected, wrong_turn}]`. Считается на лету (задача #4901, спека §4; задача #5019):

- В счётчики ответов (`answered`, `accepted`, …) идут `brainstorm`-треды, закрытые ответом с исходом; обычные вопросы-решения в метрику не попадают. Каждый ответ считается своему автору — участнику, написавшему последнюю запись `answer` треда: `human` (пустой автор старых записей — тоже `human`) или id постоянного агента (`cto`). Ответ, записанный оркестратором из терминала (`brainstorm-record`), — ответ человека. Исход — хранимый, т.е. с учётом ручного переопределения. `answered = accepted + corrected + wrong_turn`; `accepted_with_comment` — принятые с непустым (не из одних пробелов) `answer_comment`. Переоткрытый и снятый (`dismissed`) тред не отвечен.
- `weeks[]` — строка на тройку (ISO-неделя × скилл × кто отвечал): неделя (`2026-W40`) по времени ответа (`resolved_at`) в локальном времени daemon, скилл задачи (`brainstorm_skill` целиком, с версией: `orchestrator-brainstorming@1.0` и `@1.1` — разные строки; пустой → `unknown`), `answered_by` — `human` или id агента. Ответы человека и агентов никогда не складываются в одну строку. Окно — текущая неделя и `N-1` предыдущих, с понедельника 00:00; недели без ответов не выводятся. Порядок: неделя по возрастанию, затем скилл, затем `human` первым, остальные по id.
- `storms[]` — задачи, у которых есть хотя бы один `brainstorm`-тред или гейт и была активность в окне (задан/отвечен вопрос, запрошен/решён гейт); новые (по последней активности) первыми. Счётчики шторма — за всю его жизнь и по всем участникам вместе: `questions` — все `brainstorm`-треды задачи. `answered_by` — кто штормил: каждый участник один раз, в порядке первого ответа (по `resolved_at`, при равенстве — по id вопроса); `by_answerer` — те же счётчики ответов по каждому участнику в том же порядке (их сумма — счётчики шторма). Оба — всегда массивы, `[]` без ответов.
- Гейт шторма: `go_at` — `decided_at` первого гейта `go`, момент выхода из шторма (unix-секунды, `null` до Go). `spec_changes` — правки до Go: гейты в статусе `changes`, решённые раньше первого Go (`decided_at < go_at`); без Go — все `changes`. `superseded` (гейт снят новой версией спеки без решения человека) и `pending` правкой не считаются, `changes` после Go — тоже. `first_try_go = go_at != null && spec_changes == 0`. `has_gate` — у задачи есть хотя бы один гейт в любом статусе (отличает «гейтов не было» от «гейт ждёт, правок 0»).
- Пустые списки — `[]`.

## Постоянные агенты

Агент — зарегистрированный «дежурный» («SRE платформы», «разборщик issues»), к которому обращаются люди и другие агенты. Rocket им не управляет: регистрация, доставка и инбокс — всё (см. [10-agents.md](10-agents.md) и спеку v4 задачи #639).

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/agent-kinds` | Реализации агентов, которыми демон умеет запускать сессии: `{kinds:[{name, available, error?}], default}`. `available` — доступен ли исполняемый файл на машине демона; `default` — `default_agent` из конфига. Питает выбор оркестратора в UI (Start ▸) |
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
| POST | `/v1/agents/{id}/questions` | `{body, title?, brief?, context?, to?, type?, options?}` → 201. Направление — по вызывающему: человек (без заголовка сессии) спрашивает агента, агент эскалирует человеку. Участниками сеются человек, автор и сама роль, плюс `to`. `type`/`options` — как у тредов задач |
| POST | `/v1/agent-questions/{qid}/reply` | `{body, to?, join?, dry_run?, dispute?}` → 201. Любой участник треда; не-участник — `403 not_a_participant` (человек и постоянный агент могут повторить с `join`). reply в закрытый тред от любого участника, кроме человека, принимается и ложится в историю; переоткрывает его только `dispute: true` (человеку — `409 question_resolved`) |
| POST | `/v1/agent-questions/{qid}/answer` | `{body, to?, choose?, join?, dry_run?}` &#124; `{dismiss:true}` → 200. Закрывает тред; человек и постоянный агент, остальным `403 forbidden` |

Тред роли — та же сущность, что тред задачи, только с заполненным `role_id` (отдельных таблиц `agent_questions` / `agent_question_messages` больше нет). Поля `local_ref`, `brief`, `participants`, `attention`/`waiting_on`, `your_turn`, `type`, `options` и `addressed_to` те же, что у тредов задач (`brief` пуст у старых тредов и у открытых человеком; он же есть в `/v1/threads`); `whose_turn` (`user`&#124;`role`) выводится из attention. **Каждая** запись треда доставляется каждому участнику, кроме её автора, тем же путём, что обычное сообщение, с префиксом `[<id>/Q<n> question|reply|answer from <кто>] ...`. Человеку ничего не инжектится — он читает тред через API/CLI.

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

Формат события: `{id, ts, type, session_id?, data{}}`. Типы: `session.spawned|state_changed|activity_changed|killed|restored|chat_updated`, `message.queued|delivered|failed`, `pr.opened|ci_changed|merged`, `orchestrator.heartbeat_sent`, `workspace.branch_collision|cleanup`, `repo.clone_started|clone_done|clone_failed`, `task.question_asked|question_replied|question_resolved|question_reopened|question_outcome_set` (последнее — человек переопределил исход brainstorm-треда: `data{task_id, question_id, outcome}`), `task.doc_put` (`data{task_id, kind, version}`), `task.gate_requested|gate_decided|gate_superseded` (`data{task_id, gate_id, status}`), `task.assigned` (майлстон сменил держателя: `data{task_id, agent_id, by, verb: take|assign}`; `agent_id` пуст — майлстон сняли), `milestone.quiet` (взятый майлстон дольше `milestone_quiet_after` без следов работы держателя: `data{task_id, agent_id, title, since_seconds, reminded}`; `reminded` — было ли на этом проходе отправлено напоминание, анти-спам режет его до одного раза в 24h), `question.stale` (открытый decision-тред без движения дольше `question_stale_after`: `data{question_id, task_id, role_id, local_ref, since_seconds, attention, reminded}`, см. [08-orchestrators.md](08-orchestrators.md)) и т.д. `session.chat_updated` — пинг о том, что транскрипт сессии изменился; поле `data` у этого события отсутствует целиком, см. [13-chat.md](13-chat.md). `session.quiz_asked|quiz_resolved|quiz_answer_unconfirmed` — квиз-пинги того же формата (без `data`): показан pending-квиз AskUserQuestion / квиз закрыт (отвечен или отменён, в т.ч. в терминале) / удалённый ответ напечатан, но закрытие не подтвердилось за таймаут — см. [13-chat.md](13-chat.md), раздел «Квизы». Те же пинги приходят для диалогов разрешения (`pending_quiz.source = "permission"`), которые монитор читает с панели — см. раздел «Разрешения».

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
