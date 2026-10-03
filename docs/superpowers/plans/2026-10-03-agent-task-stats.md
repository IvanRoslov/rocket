# Agent Task Stats — план реализации (task #5138)

> **Для исполнителей:** план выполняют воркеры rocket. Один пункт — один воркер, одна ветка `feature/agent-task-stats/<name>`, один PR. Каждый воркер внутри своего пункта обязательно пишет детальный TDD-план через `superpowers:writing-plans` и ведёт работу через TDD и `superpowers:verification-before-completion`. Шаги ниже — контракт пункта, а не построчный код.

**Цель:** демон сам считает расход токенов и времени каждой сессии из транскриптов агентов и хранит его постоянно; API, CLI и экран Usage показывают его по моделям, задачам и сессиям, с оценкой $ по прайсу.

**Архитектура:** парсеры транскриптов в адаптерах (`agent.UsageReader`) → `internal/usage.Collector` в демоне (события шины + свипер-бэкфилл) → таблицы `session_usage` / `session_stats` / `model_prices` → read-API `/v1/stats/usage`, `/v1/tasks/{id}/usage`, `/v1/stats/prices` → дашборд.

**Стек:** Go (daemon, `modernc.org/sqlite`), React + TS + TanStack Query + vitest/MSW (`web/`).

**Спека:** `docs/superpowers/specs/2026-10-03-agent-task-stats-design.md` — каждый воркер читает её целиком.

## Глобальные ограничения

- Главное число `billable = input + cache_write + output`; `cache_read` никогда не входит в billable.
- `output` включает `reasoning`; `reasoning` отдельно не тарифицируется.
- Учёт — по фактической модели из транскрипта (`message.model` / `turn_context.model`), а не по профилю.
- Claude: дедупликация по `message.id`, учитываются все `*.jsonl` папки и `*/subagents/*.jsonl`, строки с моделью `<synthetic>` пропускаются.
- Codex: на файл берётся последний накопительный `total_token_usage`; `input = input_tokens − cached_input_tokens`.
- Стоимость считается при чтении по текущему `model_prices`. Модель без цены даёт `cost_usd: null`, в агрегатах выставляется `cost_partial: true`.
- Прайс изначально пустой — никаких зашитых цен.
- Миграция — одна, `internal/store/migrations/0023_session_usage.sql`.
- Мутирующие эндпоинты (prices PUT/DELETE, collect) из сессии агента отвечают `403 human_only`.
- Копирайт дашборда английский; документация в `docs/` — русская, в стиле существующих разделов.
- Даты периода `from/to` — локальные даты демона, `to` включительно.

## Фокус ревью

1. **Повторы одного ответа Claude** (несколько строк с одинаковым `message.id`) → токены считаются один раз. Тест — в W1.
2. **Kill воркера сразу после последнего ответа**, когда транскрипт ещё дописывается → сбор после задержки 15 с видит финальные строки; повторный финальный сбор не удваивает цифры (полная перезапись). Тест — в W3.
3. **Сессия без транскрипта** (удалён Claude Code, codex-сессия старше окна поиска) → `missing`, ноль токенов, свипер больше не перебирает её. Демон не падает и не зацикливается. Тест — в W3 (+ поиск по дате `created_at` в W1).
4. **Граница периода**: сессия, завершившаяся в 23:59 локального дня `to`, входит в период; снимок оркестратора попадает в период по `collected_at`. Тест — в W4.
5. **Модель без цены рядом с ценёнными** → итог $ — сумма ценённых, `cost_partial: true`, в UI «—» и пометка partial. Тесты — в W4 (API) и W5 (UI).

---

## Волны

```
Волна 1 (параллельно):  W1 usage-parsers   W2 usage-store   W5 usage-web (на MSW-моках по контракту спеки §3)
Волна 2 (после W2):      W4 usage-api        (после W1+W2): W3 usage-collector
Финал:                   W5 переключается на реальный API после мержа W4; живая проверка (оркестратор)
```

---

### W1: usage-parsers — чтение токенов из транскриптов

**Профиль:** `codex-gpt-6-sol` — чёткий, хорошо специфицированный парсинг на фикстурах.

**Файлы:**
- Modify: `internal/agent/agent.go` — интерфейс `UsageReader` и типы.
- Create: `internal/agent/claudecode/usage.go`, `usage_test.go`, `testdata/usage/...`
- Create: `internal/agent/codex/usage.go`, `usage_test.go`, `testdata/usage/...`
- Modify: `docs/10-agents.md` — подраздел «Расход (UsageReader)».

**Интерфейсы (производит):**

```go
// internal/agent
type Tokens struct{ Input, CacheWrite, CacheRead, Output, Reasoning, Messages int64 }
type Usage struct {
    Models          map[string]Tokens
    FirstAt, LastAt time.Time
    Found           bool
}
type UsageReader interface {
    Usage(ctx context.Context, worktreePath string, since time.Time) (Usage, error)
}
```

Оба адаптера реализуют `UsageReader`. Claude переиспользует `projectsDir` из `activity.go`. Codex ищет rollout-файлы по каталогам `YYYY/MM/DD` от `since` до сегодня, совпадение — по `session_meta.payload.cwd`.

**Обязательные тесты:**
- Claude:
  - три строки с одним `message.id` → считаются один раз;
  - две модели в одном файле → две записи в `Models`;
  - сабагент в `<uuid>/subagents/agent-x.jsonl` на другой модели → учтён;
  - несколько `.jsonl` в папке → суммируются;
  - строка с чужим `cwd` → пропущена;
  - модель `<synthetic>` → пропущена;
  - битая строка JSON → пропущена, остальное посчитано;
  - папки нет → `Found=false`, ошибки нет.
- Codex:
  - несколько `token_count` → берётся последний `total_token_usage`;
  - `input` = `input_tokens − cached_input_tokens`;
  - смена `turn_context.model` посреди файла → разница относится к модели, активной на момент события;
  - два rollout-файла с тем же `cwd` → суммируются;
  - файл с чужим `cwd` → игнорируется;
  - файл старше `since` (в каталоге даты до `since`) → не сканируется.

**Готово, когда:** `go test ./internal/agent/...` зелёный, CI зелёный, PR смержен.

---

### W2: usage-store — схема, store-методы, привязка сессии к задаче

**Профиль:** `codex-gpt-6-sol` — миграция и CRUD по точной схеме из спеки.

**Файлы:**
- Create: `internal/store/migrations/0023_session_usage.sql` — таблицы `session_usage`, `session_stats` (включая `attempts`), `model_prices`, индексы; `ALTER TABLE sessions ADD COLUMN task_id INTEGER`, `subtask_id INTEGER`.
- Create: `internal/store/usage.go`, `usage_test.go`.
- Modify: `internal/store/sessions.go` — поля `TaskID, SubtaskID int64` (0 = NULL) в `Session`, чтение и запись.
- Modify: `internal/api/sessions.go` — при спавне воркера записывать `task_id` (корень) и `subtask_id`.
- Modify: `internal/session/manager.go` (`SpawnOrchestrator`) — записывать `task_id`.
- Modify: `docs/05-state.md`.

**Интерфейсы (производит)** — методы `*store.Store`:

```go
type SessionStats struct {
    SessionID string; TaskID, SubtaskID int64; Status string // ok|missing|error
    Final bool; StartedAt int64; EndedAt *int64; CollectedAt int64; Error string; Attempts int
}
type UsageTokens struct{ Input, CacheWrite, CacheRead, Output, Reasoning, Messages int64 } // свой тип: store не импортирует agent
type ModelUsage struct{ Model string; Tokens UsageTokens }
type ModelPrice struct{ Model string; Input, CacheWrite, CacheRead, Output *float64; UpdatedAt int64 }

func (s *Store) ReplaceSessionUsage(st SessionStats, models []ModelUsage) error // одна транзакция: delete usage + insert + upsert stats
func (s *Store) GetSessionStats(sessionID string) (SessionStats, []ModelUsage, error)
func (s *Store) SessionsNeedingUsage(limit int, now int64) ([]Session, error) // терминальные: без stats, final=0, или error с attempts<3 и collected_at<now-3600
func (s *Store) ResolveSessionTask(sessionID string) (taskID, subtaskID int64, err error) // sessions.task_id → tasks.session_id → журнал "spawned worker <id> for subtask #N"
func (s *Store) UsageRows(from, to int64, projectID string) ([]UsageRow, error) // join session_stats+session_usage+sessions(+tasks) для агрегатов W4
func (s *Store) TaskUsageRows(taskID int64) ([]UsageRow, error)
func (s *Store) CountPendingUsage(from, to int64) (int, error)
func (s *Store) ListModelPrices() ([]ModelPrice, error); UpsertModelPrice(ModelPrice) error; DeleteModelPrice(model string) error
func (s *Store) UsageModels() ([]string, error) // distinct model из session_usage
```

`UsageRow` — плоская строка: `session_id, kind, agent, profile, effort, repo_id, project_id, pr_number, pr_state, state, task_id, subtask_id, status, final, started_at, ended_at, collected_at, model, tokens`. Точный набор полей W2 фиксирует в PR-описании — W3 и W4 опираются на него.

**Обязательные тесты:**
- Миграция применяется на БД с данными.
- `ReplaceSessionUsage` дважды → вторая запись полностью заменяет первую, строки старой модели исчезают.
- `ResolveSessionTask` закрывает три случая: колонка задана; только `tasks.session_id`; только запись журнала (воркер затёрт респавном).
- `SessionsNeedingUsage` не возвращает `missing` и `ok final=1`; возвращает `final=0` и `error` с `attempts<3` старше часа.
- `UsageRows`: граница `to` включительно; снимок попадает в период по `collected_at`.
- Спавн воркера через API сохраняет `task_id`/`subtask_id`.

**Готово, когда:** `go test ./...` зелёный, CI зелёный, PR смержен.

---

### W3: usage-collector — сбор в демоне и бэкфилл

**Профиль:** `claude-opus-5-5` — асинхронность, события шины, идемпотентность и граничные случаи жизненного цикла.

**Зависит от:** W1, W2 (стартует после их мержа).

**Файлы:**
- Create: `internal/usage/collector.go`, `collector_test.go`.
- Modify: `internal/daemon/...` — запуск Collector вместе с демоном (по образцу ghpoller/heartbeat).
- Modify: `internal/api/tasks.go` — после успешного перехода корневой задачи в review вызывать `collector.Snapshot(orchSessionID)`.
- Create: `internal/api/usage_collect.go` — `POST /v1/stats/usage/collect` (`human_only`).
- Modify: `internal/cli/stats.go` — `rocket stats collect [--session S | --all] [--retry-missing]`.
- Modify: `docs/03-daemon-api.md` (collect), `docs/04-cli.md`, `docs/07-activity.md` или новый подраздел — механика сбора.

**Поведение (спека §2.2–2.4):**
- Подписка на шину: `session.killed` и `session.state_changed{to∈done,killed,errored}` → в очередь с задержкой 15 с (задержка настраивается для тестов).
- Один рабочий поток. Сбор = `ResolveSessionTask` + `UsageReader.Usage(worktree, since=created_at)` → `ReplaceSessionUsage`; `final` = терминальна ли сессия сейчас.
- `Found=false` → `missing`. Ошибка → `error`, `attempts+1`.
- `ended_at`: время события; в бэкфилле — `LastAt`, иначе `sessions.updated_at`.
- `Snapshot(id)` у живой сессии пишет `final=0`, `ended_at=NULL`.
- Свипер: при старте и раз в 10 мин, пачками по 50 через `SessionsNeedingUsage`.
- После каждой записи — `bus.Publish("usage.collected", id, {task_id})`.
- Ошибки сбора только логируются; `kill` не ждёт сбора.

**Обязательные тесты:**
- Терминальное событие → через задержку строка `final=1`.
- Review → снимок `final=0` → терминальность → `final=1`, цифры заменены, а не сложены.
- Повторный финальный сбор той же сессии даёт ту же строку (идемпотентность).
- `Found=false` → `missing`, свипер её больше не берёт.
- Ошибка ридера трижды → `attempts=3`, дальше не повторяется.
- Свипер пачкой обрабатывает 120 сессий (три пачки).
- `collect` из сессии агента → 403 `human_only`.
- `usage.collected` публикуется.

**Готово, когда:** `go test ./...` зелёный, CI зелёный, PR смержен.

---

### W4: usage-api — чтение, прайс, CLI

**Профиль:** `codex-gpt-6-sol` — эндпоинты и агрегаты по контракту спеки §3.

**Зависит от:** W2.

**Файлы:**
- Create: `internal/api/usage.go`, `usage_test.go` — `GET /v1/stats/usage`, `GET /v1/tasks/{id}/usage`, `GET /v1/stats/prices`, `PUT/DELETE /v1/stats/prices/{model}`.
- Create: `internal/usage/aggregate.go`, `aggregate_test.go` — чистые функции: строки + прайс → `totals`, `models`, `tasks`, `sessions`, `billable`, `cost_usd`, `cost_partial`.
- Modify: `internal/cli/stats.go` — `rocket stats usage`, `rocket stats task <id>`, `rocket stats prices [set|rm]`.
- Modify: `docs/03-daemon-api.md` («Расход агентов»), `docs/04-cli.md`.

**Контракт ответов** — дословно спека §3: поля `from, to, totals, models[], tasks[], pending`; `sessions[]` задачи с `role, subtask_id, subtask_title, agent, profile, effort, repo_id, pr_number, pr_url, pr_state, state, status, final, started_at, ended_at, duration_s, models[], tokens, cost_usd`. Сессии без задачи — строка `task_id: null`. `pr_url` строится из GitHub owner/name репо.

**Обязательные тесты:**
- Период по умолчанию — 30 дней; `from > to` или кривая дата → 400 `bad_request`.
- Граница `to` включительно в локальной зоне.
- Фильтр `project`.
- `billable` не включает `cache_read`.
- `cost_usd`: цена задана частично (null для ненулевого вида) → у модели `null`; итог — сумма ценённых, `cost_partial: true`.
- Сортировка `models` и `tasks` по `billable` по убыванию.
- `prices` возвращает и неценённые модели из `UsageModels()`.
- PUT с отрицательной ценой → 400.
- PUT/DELETE из сессии агента → 403.
- `/v1/tasks/{id}/usage` у подзадачи → 400 `not_root_task`; несуществующая задача → 404 `task_not_found`.

**Готово, когда:** `go test ./...` зелёный, CI зелёный, PR смержен.

---

### W5: usage-web — экран Usage, вкладка задачи, Prices

**Профиль:** `claude-opus-5-5` — UI-решения и вёрстка в стиле существующих экранов.

**Зависит от:** контракт спеки §3. Стартует сразу на MSW-моках (`web/src/mocks/handlers.ts`, `fixtures.ts`); перед мержем — проверка на реальном API после мержа W4.

**Файлы:**
- Create: `web/src/screens/usage/UsageScreen.tsx`, `UsageScreen.test.tsx`, `usage.css`.
- Create: `web/src/screens/task/UsageTab.tsx`, `UsageTab.test.tsx`.
- Create: `web/src/screens/settings/PricesSection.tsx`, `PricesSection.test.tsx`.
- Modify: `web/src/routes.tsx` (`/usage`), `web/src/components/AppShell.tsx` (NavLink «Usage» после «Brainstorm»), вкладки TaskScreen, `SettingsScreen.tsx`.
- Modify: `web/src/lib/queries.ts` — `useUsageStats({from,to,project})`, `useTaskUsage(id)`, `usePrices()`, `useSetPrice()`, `useDeletePrice()`.
- Modify: `web/src/lib/sse.ts` — по `usage.collected` инвалидировать `['usage']`, `['taskUsage', task_id]`.
- Modify: `web/src/mocks/*`, `docs/11-dashboard.md` («Экран 2e: Usage», вкладка Usage, Prices).

**Поведение (спека §5):**
- Пресеты 7d/30d/90d и произвольный диапазон; состояние в query-строке; дропдаун проекта.
- Карточки итогов, таблицы By model и By task.
- «—» и «Set price» у модели без цены; «(partial)» у итога.
- Баннер «Counting history… N sessions left» при `pending > 0`.
- Пустое состояние «No usage in this period».
- Вкладка Usage: сессии, несколько моделей — подстроки; метки `live snapshot`, `no transcript`, `error`, «running — counted when finished».
- Prices: построчное редактирование, Clear.

**Обязательные тесты (vitest + MSW):**
- Смена пресета меняет `from/to` в запросе и в URL.
- Модель без цены → «—» и ссылка; итог с «(partial)».
- `pending > 0` → баннер.
- Пустой ответ → пустое состояние.
- Вкладка задачи рендерит оркестратора и воркера с PR-ссылкой и меткой снимка.
- Prices: сохранение вызывает PUT с числами, пустое поле уходит как `null`.

**Готово, когда:** `npm test` и `npm run build` зелёные, CI зелёный, экран проверен вживую против демона с реальными данными (скриншот в PR), PR смержен.

---

## Финальная проверка (оркестратор)

1. Все пять PR смержены, `rocket verify-merge` по каждой подзадаче.
2. `make build`, демон перезапущен на свежем бинаре, бэкфилл прошёл (`pending` → 0).
3. Расход этой фичи на вкладке Usage задачи #5138 сверен с ручным подсчётом по транскрипту одного воркера (`jq` по `message.usage` с дедупликацией по `message.id`).
4. Экран Usage за 30 дней показывает модели `claude-opus-5-5`, `gpt-6-sol` и другие с ненулевыми цифрами.
5. Отчёт — в задачу (`--kind report`), задача — в review.
