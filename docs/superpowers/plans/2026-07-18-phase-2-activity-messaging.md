# Фаза 2 «Активность и сообщения» — план имплементации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Надёжный `rocket send` (очередь в демоне, доставка когда получатель готов) и честные статусы активности в `rocket ls`; SSE-поток событий.

**Architecture:** Монитор (цикл ~5s) определяет activity каждой живой сессии каскадом: процесс на TTY pane → агент-специфичные источники (JSONL-транскрипт Claude Code); push-канал (hooks агента → `POST /v1/internal/activity`) уточняет и даёт `waiting_input`. Очередь сообщений: таблица `messages` (схема уже есть), пер-получатель FIFO-доставщик в демоне доставляет через `runtime.Inject`, когда activity ∈ {ready, idle, waiting_input}. SSE — fan-out шины событий.

**Tech Stack:** без новых зависимостей (stdlib + существующие). tmux `list-panes`, `ps`; JSONL-парсинг `bufio`+`encoding/json`.

## Global Constraints

- Никакого regex по содержимому терминала для классификации состояния (только для подтверждения доставки — уже в runtime.Inject).
- Состояния activity: `active | ready | idle | waiting_input | blocked | exited` (схема: sessions.activity, activity_ts).
- Пороги: active < 30s от последней работы; ready → idle после 5m; конфигурируемые (config.yaml: `activity_poll_interval` default 5s, `ready_to_idle` default 5m, `queue_timeout` default 30m).
- Конфликт push vs поллинг: свежий timestamp побеждает; `exited` из поллинга всегда приоритетен.
- Доставка: FIFO пер-получатель; следующее сообщение не начинается, пока текущее не delivered/failed. Готовность: ready/idle/waiting_input → доставляем; active → ждём; blocked/exited/терминальная сессия → failed.
- Формат: `[from <id>] <body>` если from_session задан; от человека — без префикса.
- Контракт `runtime.ErrSubmitUnconfirmed`: НЕ переинжектировать вслепую. Ре-инжекция разрешена только если Capture показывает, что черновик (последняя непустая строка текста) всё ещё виден в хвосте экрана; иначе считать доставленным. До 5 циклов с экспоненциальной паузой (1s, 2s, 4s, 8s, 16s) → `failed` + `message.failed`.
- Таймаут в очереди: queued дольше queue_timeout → `failed` (reason timeout).
- События: `session.activity_changed {from,to,source}`, `message.queued|delivered|failed`.
- Событийный формат и error envelope — как в фазе 1; id-валидация `^[a-z0-9-]+$`; никакого shell.
- Ретенция: events и messages старше 30 дней чистятся фоновой задачей (раз в сутки).
- Коммиты частые; `go test ./... && go vet ./... && gofmt -l ./internal ./cmd` перед каждым коммитом.

---

### Task 1: Activity-типы, store, конфиг

**Files:**
- Create: `internal/activity/activity.go` (тип ActivityState + константы + Valid())
- Modify: `internal/store/sessions.go` (+`UpdateSessionActivity(id, state string, ts int64) error`), `internal/config/config.go` (+3 поля с дефолтами)
- Test: `internal/store/store_test.go` (дополнить), `internal/config/config_test.go` (дополнить)

**Interfaces:**
- Produces: `activity.State` (string alias) c константами `Active, Ready, Idle, WaitingInput, Blocked, Exited`; `store.UpdateSessionActivity` (обновляет activity, activity_ts, updated_at; ErrNotFound); config: `ActivityPollInterval time.Duration` (5s), `ReadyToIdle` (5m), `QueueTimeout` (30m), yaml-ключи `activity_poll_interval`, `ready_to_idle`, `queue_timeout`.

- [ ] TDD: store-тест (update + чтение activity/activity_ts; unknown id → ErrNotFound); config-тест (дефолты + переопределение из yaml).
- [ ] Commit: `feat: activity types, store update, config thresholds`.

### Task 2: Agent.Activity — интерфейс + адаптер claude-code (JSONL)

**Files:**
- Modify: `internal/agent/agent.go` (+`Activity(ctx context.Context, ref ActivityRef) (activity.State, time.Time, error)`; `type ActivityRef struct{ SessionID, WorktreePath string }`), фейки в тестах session
- Modify: `internal/agent/claudecode/claudecode.go` + Test: `internal/agent/claudecode/activity_test.go`

**Interfaces:**
- Produces: адаптер возвращает «сырое» состояние источника + timestamp последней работы; пороги применяет монитор. Семантика возврата: `(Active, ts)` если транскрипт свидетельствует о незавершённом ходе; `(Ready, ts)` если последняя запись — штатный конец хода (монитор сам понизит до Idle по порогу); `(Blocked, ts)` при api_error в хвосте; ошибка «транскрипт не найден» — не фатальна: `(Ready, mtime-худший-случай)` c err==nil и признаком low-confidence? Нет — упрощение: не найден → `("", zero, ErrNoSignal)` (экспортированный сентинел agent.ErrNoSignal; монитор тогда опирается только на процесс/TTY).
- claude-code: путь транскриптов `~/.claude/projects/<slug>/*.jsonl`, slug = worktree path c заменой `/` и `.` на `-` (проверить реальную схему слаггирования на этой машине по существующим директориям и закрепить в тесте!); берётся самый свежий .jsonl по mtime; последняя строка файла: JSON с полем `type` — `assistant`/`user`/`system`/`summary` и т.п.: mtime<30s и файл растёт → Active; иначе тип последней записи `assistant` (конец хода) → Ready; запись с `isApiErrorMessage`/type error → Blocked. Чтение хвоста файла — последние ~64KB, не весь файл.

- [ ] TDD на фикстурах: temp-директория с поддельным транскриптом (хелпер пишет строки JSONL, выставляет mtime через os.Chtimes); кейсы: свежий → Active, старый assistant → Ready, api_error → Blocked, нет файла → ErrNoSignal.
- [ ] Commit: `feat: agent activity interface + claude-code JSONL detection`.

### Task 3: Монитор — каскад поллинга

**Files:**
- Create: `internal/monitor/monitor.go`
- Modify: `internal/daemon/daemon.go` (запуск/остановка монитора)
- Test: `internal/monitor/monitor_test.go` (фейки Runtime/Agent, реальный store+bus)

**Interfaces:**
- Produces: `monitor.New(st, b, rt, cfg, resolveAgent func(name string) (agent.Agent, error))`, `Run(ctx)` (цикл ActivityPollInterval), `PushUpdate(sessionID string, state activity.State, ts time.Time)` (для API из Task 4; та же логика слияния).
- Каскад на живую (spawning|running) сессию: (1) tmux pane TTY: `tmux list-panes -t =<name> -F '#{pane_tty}'`; `ps -t <tty> -o comm=` — ищем процесс агента (имя бинарника от адаптера: добавить в Agent метод? НЕТ — YAGNI: если pane жив, но agent.Activity даёт ErrNoSignal и последняя запись старше 60s — считаем Ready/Idle по порогу от activity_ts; настоящий exited определяем проще: pane мёртв (tmux сессии нет) ЛИБО `ps -t <tty>` показывает только shell (comm = базовый шелл: sh/bash/zsh/-zsh и т.п.) → Exited). (2) agent.Activity → сырое состояние. (3) Слияние с последним push (in-memory map[sessionID]{state,ts}): свежий ts побеждает, кроме Exited из каскада — он всегда побеждает. (4) Порог: Ready + (now-ts) > ReadyToIdle → Idle. (5) Если итог отличается от store → UpdateSessionActivity + событие `session.activity_changed {from,to,source:"poll"|"push"}`.
- Exited дополнительно: сессия остаётся running (жизненный цикл не меняем — это фаза-2-политика: kill/cleanup — человек или оркестратор).

- [ ] TDD: фейковый Runtime (alive/tty), фейковый агент; кейсы: активный агент → active; стоп → ready; порог → idle; pane мёртв → exited; push свежее поллинга → push побеждает; exited приоритетен над свежим push.
- [ ] Commit: `feat: activity monitor with poll cascade and push merge`.

### Task 4: Push-канал — internal API + hooks в SetupWorkspace

**Files:**
- Create: `internal/api/internal_activity.go` (`POST /v1/internal/activity {session, state, ts}` → monitor.PushUpdate; валидация state)
- Modify: `internal/agent/claudecode/claudecode.go` (SetupWorkspace: upsert `.claude/settings.json` в worktree + hook-скрипт `.rocket/activity-hook.sh`)
- Test: `internal/api/internal_activity_test.go`, дополнение `claudecode_test.go`

**Interfaces:**
- Hook-скрипт (0700, в `<worktree>/.rocket/`): `#!/bin/sh` + `curl -s --unix-socket "$ROCKET_SOCKET" -X POST -H 'Content-Type: application/json' -d "{\"session\":\"$ROCKET_SESSION_ID\",\"state\":\"$1\",\"ts\":$(date +%s)}" http://rocket/v1/internal/activity >/dev/null 2>&1 || true` — не ломает работу агента при недоступном демоне.
- settings.json upsert (идемпотентный merge существующего JSON, не перетирать чужие ключи): hooks: `PreToolUse`/`PostToolUse` → `activity-hook.sh active`; `Stop` → `ready`; `Notification` → `waiting_input`; `SessionEnd` → `exited`. Точный формат hooks Claude Code проверить по документации/локальному `~/.claude/settings.json` при имплементации; тест фиксирует структуру.
- API-хендлер: session должна существовать (404), state валиден (400 invalid_state), ts default now; отвечает 204.

- [ ] TDD: API (валидные/невалидные запросы, вызов PushUpdate — через интерфейс в Deps); SetupWorkspace-тест: свежий worktree → settings.json создан с hooks; существующий settings.json с другими ключами → ключи сохранены, hooks добавлены; повторный вызов идемпотентен.
- [ ] Commit: `feat: push activity channel (hooks + internal API)`.

### Task 5: Очередь доставки сообщений

**Files:**
- Create: `internal/queue/queue.go`
- Modify: `internal/store/messages.go` (Create: DAO messages), `internal/daemon/daemon.go` (запуск очереди)
- Test: `internal/store/store_test.go` (DAO), `internal/queue/queue_test.go` (фейк Runtime, реальный store+bus)

**Interfaces:**
- store DAO: `AddMessage(m Message) (int64, error)`, `GetMessage(id)`, `ListMessages(sessionID string, limit int) ([]Message, error)` (обе стороны, DESC по id, потом reverse), `NextQueuedMessage(to string) (Message, bool, error)` (минимальный id со status=queued), `UpdateMessageStatus(id int64, status string, attempts int, deliveredAt int64) error`, `ExpireQueuedBefore(ts int64) ([]Message, error)` (queued старше ts → failed, вернуть кого затронуло), `ListQueuedRecipients() ([]string, error)`.
- `queue.New(st, b, rt, cfg, getActivity func(sessionID string) (activity.State, bool), getSession func(id) (store.Session, error))`; `Run(ctx)`; `Wake(to string)` (пинок от API при новой постановке).
- Логика на получателя (одна горутина на активного получателя, запускается лениво по Wake/при старте по ListQueuedRecipients; завершение — когда очередь пуста): взять NextQueuedMessage → проверки: получатель существует и state==running, иначе failed(reason recipient_gone); activity ∈ {ready,idle,waiting_input} → доставка; active → подождать (поллинг 2s или подписка на bus activity_changed — берём подписку на bus с фолбэком-тикером); blocked/exited → failed(reason recipient_unavailable). Доставка: status=delivering → `runtime.Inject(=to, prefix+body)`; успех → delivered + `message.delivered`; `ErrSubmitUnconfirmed` → Capture(=to, 5): если последняя непустая строка текста ещё видна в хвосте → ре-инжекция разрешена (следующая попытка), иначе считать delivered; другие ошибки → следующая попытка; попытки: до 5, паузы 1/2/4/8/16s, каждая инкрементит attempts в store; исчерпаны → failed + `message.failed {reason}`.
- Фоновая ретенция + таймаут: тикер раз в минуту — ExpireQueuedBefore(now-QueueTimeout) → события message.failed(reason timeout); раз в сутки — удаление messages и events старше 30 дней (store методы `PurgeOld(before int64)`).

- [ ] TDD DAO отдельно; queue-тесты: FIFO двух сообщений; получатель active → ждёт, стал ready (через getActivity-фейк + Wake) → доставилось; ErrSubmitUnconfirmed + маркер исчез → delivered без ре-инжекции; ErrSubmitUnconfirmed + маркер виден → ретрай; 5 неудач → failed; получатель killed → failed сразу; таймаут очереди → failed.
- [ ] Commit: `feat: per-recipient FIFO message delivery queue`.

### Task 6: API сообщений

**Files:**
- Create: `internal/api/messages.go`
- Modify: `internal/api/server.go` (маршруты), Deps (+Queue, +Monitor)
- Test: `internal/api/messages_test.go`

**Interfaces:**
- `POST /v1/messages {from?, to, body}` → валидация: to существует (404 session_not_found) и не терминальна (409 recipient_terminal), body непуст (400 empty_body), from если задан — существующая сессия (400 from_unknown); INSERT queued + событие `message.queued` + queue.Wake(to) → 201 `{id, status:"queued"}`.
- `GET /v1/messages?session=<id>&limit=N` (default 50) → история в обе стороны; `GET /v1/messages/{id}` → статус.

- [ ] TDD: happy, все ошибки, история фильтруется по обеим сторонам.
- [ ] Commit: `feat: messages API`.

### Task 7: CLI `rocket send`

**Files:**
- Create: `internal/cli/send.go`
- Test: `internal/cli/send_test.go` (usage-кейсы)

**Interfaces:**
- `rocket send <session> "<текст>"` | `rocket send <session> --file <path>` (ровно один источник тела, иначе usageError); `--wait` — поллинг GET /v1/messages/{id} каждые 2s до delivered (exit 0) / failed (exit 1, печать reason); from — из `ROCKET_SESSION_ID` если выставлен; печатает `message <id> queued` или JSON.

- [ ] TDD usage-валидация; реализация; ручная проверка в E2E Task 9.
- [ ] Commit: `feat: rocket send with --file and --wait`.

### Task 8: SSE `/v1/events/stream`

**Files:**
- Create: `internal/api/sse.go`
- Modify: `internal/api/server.go`, `internal/cli/events.go` (--follow переключить на SSE с фолбэком на поллинг при ошибке подключения)
- Test: `internal/api/sse_test.go`

**Interfaces:**
- `GET /v1/events/stream?session=&since=`: заголовки SSE (`text/event-stream`, no-cache), сначала catch-up из store (since или Last-Event-ID), затем live из bus.Subscribe(); формат: `id: <id>\nevent: <type>\ndata: <json>\n\n`; heartbeat-комментарий `: ping` каждые 15s; клиент отваливается → отписка. Дедупликация catch-up/live по id (live-события с id ≤ последнего отданного пропускаются).

- [ ] TDD: httptest + бегущий стрим (читать через bufio с дедлайном): catch-up отдаёт сохранённые, live-публикация доезжает, фильтр session работает.
- [ ] Commit: `feat: SSE event stream + CLI follow via SSE`.

### Task 9: Приёмка фазы — E2E «двое переписываются»

**Files:**
- Modify: при необходимости фиксы; отчёт.

Сценарий (изолированный ROCKET_HOME, scratch-репо, два спавна claude):
- A и B запущены; `rocket ls` показывает активность (active при работе, ready после ответа — сверить с реальностью по output).
- От имени A (`ROCKET_SESSION_ID=<A> rocket send <B> "вопрос..."`): B получает `[from <A>] вопрос...` (видно в output B), отвечает.
- Кейс «получатель занят»: дать B длинную задачу (`send` с просьбой посчитать что-то долгое/поспать), сразу отправить второе сообщение → оно queued и доставляется только после того как B освободился (проверить по событиям и таймстампам; 10-минутный кейс эмулируется занятостью в минуты — критерий устойчивости, не буквальных 10 минут).
- `rocket send <B> --wait` возвращается по delivered; `rocket events --follow` (SSE) показывает activity_changed/message.* живьём; сообщение мёртвому получателю → failed.
- waiting_input: проверить хук Notification — если у B permission prompt (спровоцировать сложно с bypass permissions — допустимо проверить хук вручную вызовом скрипта).
- Зачистка: kill --cleanup обоих, daemon stop.

- [ ] Прогнать, зафиксировать транскрипт, побочные баги чинить отдельными коммитами.
- [ ] Commit: фиксы по итогам E2E.

## Self-Review (выполнен)

- Роадмап фазы 2 покрыт: монитор (T2-T3), push (T4), очередь (T5), send/--file/--wait + GET /v1/messages (T6-T7), SSE (T8), критерий готовности (T9). Ретенция 30 дней (05-state.md) — в T5.
- Типы согласованы: activity.State используется монитором/очередью/API; queue берёт getActivity из монитора (слабая связка через функцию — без импорта monitor из queue).
- Отложено сознательно: heartbeat, задачи, `rocket status` — фаза 3; MCP-транспорт — за горизонтом.
