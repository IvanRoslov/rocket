# Статистика расхода агентов (task #5138)

## Коротко

**Что строим.** Rocket сам считает, сколько токенов и времени потратила каждая сессия агента (оркестратор и воркеры, Claude Code и Codex). Считает он из журнала, который агент и так пишет на диск: модель, уровень рассуждения, задача, подзадача, PR, длительность, токены по видам. Запись постоянная и переживает удаление воркера. Для оркестратора снимок делается при переходе задачи в ревью, а итог пересчитывается при его завершении. Один раз задним числом досчитываются все прошлые сессии, чьи журналы ещё лежат на диске. В дашборде появляется экран **Usage**: диапазон дат, расход по каждой модели и по задачам за период, примерная стоимость в $ по прайсу, который вы ведёте в Настройках. В карточке задачи появляется вкладка **Usage** со строкой на каждого агента фичи.

**Чего не делаем.** Агенты ничего не отчитывают сами, их промпты не меняются. Нет графика по дням и встроенных цен (прайс изначально пуст). Не считаем расход живых сессий на лету — кроме снимка оркестратора при ревью. Нет лимитов и бюджетов.

**Главное число** — «токены без чтения кэша»: input + cache write + output. Чтение кэша показывается отдельной колонкой (решение Q4).

## Решения шторма

| # | Решение |
|---|---|
| Q1 | Статистику пишет демон из транскрипта, когда сессия завершается. Агент ничего не отчитывает. |
| Q2 | Бэкфилл всей истории из сохранившихся транскриптов. |
| Q3 | Оркестратор: снимок при переходе задачи в review, финальный пересчёт при завершении сессии. |
| Q4 | Главное число — без cache read, полная разбивка рядом. |
| Q5 | Токены и оценка $ по прайсу, который ведётся в Настройках. |
| Q6 | Экран Usage: таблица по моделям + таблица задач, плюс вкладка Usage в карточке задачи. Без графика. |

Технические решения оркестратора (журнал задачи):

- Учёт идёт по **фактической модели** из транскрипта, а не по профилю. Одна сессия может дать несколько строк — по одной на модель (сабагенты на другой модели, `/model`).
- Токены сабагентов Claude Code входят в расход сессии.
- Прайс изначально пустой. Стоимость считается при чтении по текущему прайсу. Модель без цены показывает «—».

## 1. Данные

### 1.1 Таблица `session_usage` (миграция `0023_session_usage.sql`)

Одна строка на пару (сессия × модель):

```sql
CREATE TABLE session_usage (
  session_id   TEXT    NOT NULL,
  model        TEXT    NOT NULL,          -- фактическая модель из транскрипта
  input        INTEGER NOT NULL DEFAULT 0, -- без кэша
  cache_write  INTEGER NOT NULL DEFAULT 0,
  cache_read   INTEGER NOT NULL DEFAULT 0,
  output       INTEGER NOT NULL DEFAULT 0, -- включая reasoning
  reasoning    INTEGER NOT NULL DEFAULT 0, -- подмножество output (Codex), иначе 0
  messages     INTEGER NOT NULL DEFAULT 0, -- уникальных ответов модели
  PRIMARY KEY (session_id, model)
);
```

### 1.2 Таблица `session_stats` (та же миграция)

Одна строка на сессию — метаданные сбора:

```sql
CREATE TABLE session_stats (
  session_id   TEXT PRIMARY KEY,
  task_id      INTEGER,           -- корневая задача (фича); NULL, если не определилась
  subtask_id   INTEGER,           -- подзадача воркера; NULL у оркестратора и агентов
  status       TEXT NOT NULL,     -- ok | missing | error
  final        INTEGER NOT NULL,  -- 0 = снимок живой сессии, 1 = сессия терминальна
  started_at   INTEGER NOT NULL,  -- sessions.created_at
  ended_at     INTEGER,           -- конец сессии (см. 2.4); у снимка — NULL
  collected_at INTEGER NOT NULL,  -- когда посчитано; период снимка считается по нему
  error        TEXT NOT NULL DEFAULT '',
  attempts     INTEGER NOT NULL DEFAULT 0 -- неудачных попыток подряд (status=error)
);
CREATE INDEX session_stats_ended ON session_stats(ended_at);
CREATE INDEX session_stats_task  ON session_stats(task_id);
```

Агент, профиль, модель профиля, усилие, kind, проект, репо и PR не дублируются: они берутся join-ом из `sessions`. Строки `sessions` не удаляются никогда, а снимок `profile/model/effort` уже хранится там.

### 1.3 Привязка сессии к задаче: `sessions.task_id`, `sessions.subtask_id`

Сейчас связь односторонняя: `tasks.session_id`, причём он перезаписывается при респавне. Миграция добавляет в `sessions` колонки `task_id` и `subtask_id` (nullable). `POST /v1/sessions` (воркер) и `SpawnOrchestrator` заполняют их при создании. Для старых сессий бэкфилл восстанавливает связь в таком порядке:

1. `tasks.session_id` — корневая задача (оркестратор) или подзадача (воркер); `task_id` воркера = `parent_id` подзадачи;
2. записи журнала задач `spawned worker <id> for subtask #N` — так находятся воркеры, затёртые респавном;
3. иначе остаётся NULL — на экране это «без задачи».

### 1.4 Прайс `model_prices` (та же миграция)

```sql
CREATE TABLE model_prices (
  model        TEXT PRIMARY KEY,  -- точный id модели из транскрипта
  input        REAL,              -- $ за 1M токенов; NULL = не задано
  cache_write  REAL,
  cache_read   REAL,
  output       REAL,
  updated_at   INTEGER NOT NULL
);
```

Изначально таблица пустая. `reasoning` отдельно не тарифицируется — он уже входит в output.

## 2. Сбор: `internal/usage`

### 2.1 Парсеры (чистые функции, тестируются на фикстурах)

Каждый адаптер получает необязательный интерфейс:

```go
type UsageReader interface {
    // Usage находит все транскрипты сессии с данным worktree и суммирует расход по моделям.
    Usage(ctx context.Context, worktreePath string, since time.Time) (Usage, error)
}
type Usage struct {
    Models   map[string]Tokens // модель → токены
    FirstAt, LastAt time.Time  // первый/последний timestamp в транскриптах
    Found    bool              // нашёлся ли хоть один файл
}
```

**claude-code.**
- Папка — `projectsDir(worktree)`, та же функция, что для activity. Читаются **все** `*.jsonl` папки и все `*/subagents/*.jsonl`.
- Строка учитывается, только если её `cwd` совпадает с worktree (сабагенты — по родительскому файлу).
- Берутся строки `type=="assistant"` с `message.usage`. Дедупликация — по `message.id`: один ответ пишется несколькими строками с одинаковым usage. Модель — `message.model`. Строки `<synthetic>` (модель `<synthetic>`) пропускаются.
- Маппинг: `input_tokens→input`, `cache_creation_input_tokens→cache_write`, `cache_read_input_tokens→cache_read`, `output_tokens→output`.

**codex.**
- Rollout-файлы в `$CODEX_HOME/sessions/YYYY/MM/DD`, у которых `session_meta.payload.cwd == worktree`. Окно поиска — от даты `sessions.created_at` до сегодня, а не 14 дней, как у activity.
- На файл берётся **последнее** событие `token_count` с его `info.total_token_usage` (оно накопительное). Модель — последний `turn_context.model` до этого события. Если модель в файле менялась, разница между соседними `total_token_usage` относится к модели, активной на момент события.
- Маппинг: `input = input_tokens − cached_input_tokens`, `cache_read = cached_input_tokens`, `cache_write = cache_write_input_tokens`, `output = output_tokens`, `reasoning = reasoning_output_tokens`.

Повреждённая строка пропускается. Файл, который не читается, даёт `status=error` с текстом ошибки, а частичный результат не сохраняется.

### 2.2 Collector

`usage.Collector` в демоне: подписчик на шину и рабочая очередь из одного потока.

- **Событие терминальности** (`session.killed`, `session.state_changed` с `to ∈ {done, killed, errored}`) ставит сбор в очередь с задержкой 15 с — агент успевает дописать транскрипт. Результат пишется как `final=1`.
- **Переход корневой задачи в review** (`PATCH /v1/tasks/{id}` → review, успешный): оркестратор задачи получает снимок `final=0`, если он жив. Если сессия к тому времени уже терминальна, это обычный финальный сбор.
- **Финальный сбор** всегда перезаписывает строку целиком: удаляет все `session_usage` сессии и вставляет их заново в одной транзакции. Поэтому повтор безопасен, а снимок заменяется итогом.
- **Свипер** (при старте демона и раз в 10 минут) берёт терминальные сессии без строки `session_stats` или с `final=0` и собирает их пачками по 50. Это и есть **бэкфилл**: при первом старте после миграции он пройдёт всю историю в фоне, не блокируя демон. Сессии со `status=missing` повторно не перебираются.
- Каталог транскриптов не найден → `status=missing`, токены нулевые. Бэкфилл старых сессий с удалёнными журналами — штатный случай.
- Kind `agent` (постоянные агенты) живёт долго, но тоже собирается при терминальности. Его `task_id` — NULL.

### 2.3 Ручной перезапуск

`rocket stats collect [--session <id> | --all] [--retry-missing]` → `POST /v1/stats/usage/collect` (только человек). `--all` ставит в очередь пересбор всех терминальных сессий, `--retry-missing` — также и `missing`.

### 2.4 Время

- `started_at` = `sessions.created_at`.
- `ended_at` = момент терминального события. В бэкфилле — `LastAt` из транскрипта, при отсутствии транскрипта — `sessions.updated_at`.
- **Длительность** = `ended_at − started_at`, настенное время жизни сессии. Активное время не считаем.
- В период сессия попадает по `ended_at`, снимок — по `collected_at`.

## 3. API

| Метод | Путь | Описание |
|---|---|---|
| GET | `/v1/stats/usage` | `?from=YYYY-MM-DD&to=YYYY-MM-DD` (локальная дата демона, включительно; по умолчанию последние 30 дней) `&project=<id>` (необязательно). Любой вызывающий |
| GET | `/v1/tasks/{id}/usage` | Расход фичи: все сессии задачи и её подзадач. Только корневая задача: подзадача → `400 not_root_task`, нет задачи → `404 task_not_found`. Любой вызывающий |
| GET | `/v1/stats/prices` | Прайс + список всех моделей, встреченных в `session_usage` (у неценённых цены `null`) |
| PUT | `/v1/stats/prices/{model}` | `{input, cache_write, cache_read, output}` (числа ≥ 0 или null). Только человек (`403 human_only` из сессии агента) |
| DELETE | `/v1/stats/prices/{model}` | Только человек |
| POST | `/v1/stats/usage/collect` | `{session_id?} | {all: true, retry_missing?: bool}` → `202`. Только человек |

`Tokens` в ответах: `{input, cache_write, cache_read, output, reasoning, billable}`, где `billable = input + cache_write + output` — главное число. `cost_usd` — число или `null`, если у какой-либо из входящих моделей нет полной цены для ненулевого вида токенов. В агрегатах `cost_usd` — сумма по ценённым моделям, флаг `cost_partial: true`, если часть расхода без цены.

`GET /v1/stats/usage` →

```json
{
  "from": "2026-09-04", "to": "2026-10-03",
  "totals": {"sessions": 0, "tokens": {...}, "cost_usd": 0, "cost_partial": false},
  "models": [{"model", "agent", "sessions", "tokens": {...}, "cost_usd"}],
  "tasks":  [{"task_id", "title", "project_id", "status", "sessions", "tokens": {...}, "cost_usd", "cost_partial"}],
  "pending": 0
}
```

- `models` сортируются по `billable` по убыванию.
- `tasks` — только задачи с расходом в периоде, по `billable` по убыванию. Сессии без задачи собраны в строку `task_id: null` («Без задачи»).
- `pending` — число терминальных сессий в периоде, ещё не обработанных свипером. Дашборд показывает «Идёт подсчёт истории… N».

`GET /v1/tasks/{id}/usage` →

```json
{
  "task_id": 5138,
  "totals": {...},
  "sessions": [{"session_id", "role": "orchestrator|worker", "subtask_id", "subtask_title",
                "agent", "profile", "effort", "repo_id", "pr_number", "pr_url", "pr_state",
                "state", "status", "final", "started_at", "ended_at", "duration_s",
                "models": [{"model", "tokens", "cost_usd"}], "tokens", "cost_usd"}]
}
```

`pr_url` строится из GitHub owner/name репо и `pr_number`.

## 4. CLI

```
rocket stats usage [--from D] [--to D] [--project P]   # таблицы по моделям и задачам
rocket stats task <id>                                 # расход фичи по сессиям
rocket stats prices [set <model> --input X --cache-write X --cache-read X --output X | rm <model>]
rocket stats collect [--session S | --all] [--retry-missing]
```

## 5. Дашборд

**Экран `/usage`** — вкладка «Usage» в шапке, рядом с «Brainstorm».
- Сверху: пресеты 7d / 30d / 90d, произвольный диапазон из двух полей даты и дропдаун проекта («All projects»). Выбор сохраняется в query-строке.
- Карточки итогов: Tokens (billable), Cache read, ≈ Cost, Sessions.
- Таблица **By model**: Model, Agent, Sessions, Tokens (billable), Input, Cache write, Cache read, Output, ≈ $. Модель без цены показывает «—» со ссылкой «Set price» в Настройки.
- Таблица **By task**: Task (ссылка на вкладку Usage задачи), Project, Status, Sessions, Tokens, ≈ $.
- Пусто: «No usage in this period». Пока идёт бэкфилл: баннер «Counting history… N sessions left».
- Копирайт дашборда — английский, как во всём UI.

**Вкладка «Usage» в карточке задачи.**
- Итог фичи сверху.
- Таблица сессий: Role, Subtask, PR (ссылка + состояние), Agent / Model (несколько моделей — несколько строк под сессией), Effort, Duration, Tokens, Cache read, ≈ $, Status. Статусы: `live snapshot` для `final=0`, `no transcript` для missing, `error` с подсказкой.
- Живые сессии без снимка показываются строкой «running — counted when finished».

**Настройки → «Prices».** Таблица всех встреченных моделей с четырьмя полями $/1M (input, cache write, cache read, output), сохранение построчно, кнопка «Clear».

Данные обновляются через SSE: демон публикует `usage.collected{session_id, task_id}`, по нему инвалидируются `['usage', …]` и `['taskUsage', id]`.

## 6. Обработка ошибок и граничные случаи

- Транскрипт удалён или не найден → `missing`, ноль токенов, на экране «no transcript». Повторно не ищется без `--retry-missing`.
- Ошибка чтения → `error`, `attempts+1`; свипер повторяет не чаще раза в час, пока `attempts < 3`. Успех сбрасывает `attempts` в 0.
- Один worktree на одну сессию гарантирован схемой пути `<worktrees>/<repo>/<session-id>`. Совпадение `cwd` с чужой сессией невозможно, если только сессию с тем же id не создали заново. Тогда фильтр `since = created_at` отсекает старые файлы.
- `rocket restore` поднимает ту же сессию в том же worktree, и новые транскрипты попадают в ту же сессию. Если сессия уже была `final=1`, а потом её восстановили, повторная терминальность пересчитает всё целиком.
- Сбор не должен тормозить `kill`: он асинхронный, ошибки только логируются.
- Большие транскрипты читаются построчно (bufio, буфер до 16 МБ на строку, как в activity), без загрузки файла целиком.

## 7. Тестирование

- Парсеры: фикстуры jsonl. Claude — повторы по `message.id`, сабагенты, чужой `cwd`, `<synthetic>`, битая строка. Codex — накопительный `token_count`, смена модели, несколько rollout-файлов.
- Collector: фейковая шина и фейковый ридер. Терминальное событие → строка `final=1`. Review → снимок `final=0`, затем терминальность → `final=1` с полной перезаписью. Свипер обрабатывает пачками и не трогает `missing`.
- Store/API: агрегаты по периоду (граница `to` включительно в локальной зоне), фильтр проекта, `cost_partial`, `pending`, `human_only` на прайсе и collect.
- Web: vitest — экран Usage (пресеты, пусто, баннер бэкфилла, «—» без цены), вкладка Usage задачи, раздел Prices.
- Живая проверка: на реальной `~/.rocket/rocket.db` (копии) бэкфилл проходит, и сумма по этой фиче совпадает с ручным подсчётом по транскрипту оркестратора.

## 8. Документация

- `docs/05-state.md` — новые таблицы и колонки.
- `docs/03-daemon-api.md` — раздел «Расход агентов».
- `docs/04-cli.md` — `rocket stats …`.
- `docs/11-dashboard.md` — «Экран 2e: Usage», вкладка Usage в карточке задачи, Prices в Настройках.
- `docs/10-agents.md` — интерфейс `UsageReader` в адаптерах.

## 9. Сценарии

**С1. Воркер на Sonnet сливает PR.**
1. Оркестратор спавнит воркера с профилем `claude-sonnet-5-5`. В сессии записываются `task_id=5138`, `subtask_id=5140`.
2. ghpoller видит мерж и вызывает `Manager.Complete` → `session.state_changed{to: done}`, worktree удаляется. Транскрипт в `~/.claude/projects/` остаётся.
3. Через 15 с Collector читает папку транскриптов. Основной файл даёт `claude-sonnet-5-5`, сабагент Explore — `claude-haiku-4-5`. Получаются две строки `session_usage` и `session_stats{status: ok, final: 1}`.
4. Публикуется `usage.collected`, вкладка Usage задачи обновляется: строка воркера с PR #130 merged, 42 мин, токены по двум моделям.

Пробел: если оркестратор убьёт воркера `kill --cleanup` раньше мержа, это тоже терминальность — сбор пройдёт так же. Пробела нет.

**С2. Оркестратор уходит в review, потом правки, потом done.**
1. `rocket task move 5138 review` → снимок `final=0`: 3,1M billable. Экран Usage показывает его с пометкой `live snapshot`, в периоде — по `collected_at`.
2. Человек просит правку, оркестратор работает ещё час. Снимок не меняется (живые сессии не пересчитываются).
3. Человек переводит задачу в done → `cascadeOrchestratorCleanup` убивает оркестратора → финальный сбор перезаписывает строку целиком: 3,6M, `final=1`, `ended_at` = момент смерти.

Пробел (принят): между review и done цифра оркестратора отстаёт от реальности. Это осознанный выбор Q3. Повторный переход в review снимок обновит.

**С3. Первый старт после выкатки (бэкфилл).**
1. Миграция создаёт пустые таблицы. Свипер находит ~4900 терминальных сессий без статистики.
2. Пачками по 50 он восстанавливает `task_id` (`tasks.session_id`, затем журнал задач) и читает транскрипты. Codex-сессии старше нескольких месяцев ищутся по датам от `created_at` — перебор каталогов по дням, а не всех файлов.
3. Сессии, чьи журналы Claude Code уже удалил, получают `missing`. Экран показывает баннер «Counting history… N», пока `pending > 0`.

Пробел: стоимость бэкфилла — до нескольких тысяч файлов на сотни МБ. Пачки и один рабочий поток не дают ему забить демон, но первый проход может занять минуты. Это приемлемо, прогресс виден в баннере.

**С4. Модель без цены.** Codex `gpt-6-sol` без прайса: в By model у неё «—» и «Set price», в итоге `≈ $ 12.40 (partial)`. После ввода цены в Настройках цифры пересчитываются сразу, потому что стоимость считается при чтении.
