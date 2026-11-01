# Агенты

## Интерфейс

Внутренний Go-интерфейс (не плагинная система — адаптеры компилируются в бинарник):

```go
type Agent interface {
    Name() string                          // "claude-code", "codex", "kimi-code", "pi", "deepseek"
    Available() error                      // бинарник найден, версия ок (для doctor)
    LaunchCommand(spec LaunchSpec) []string
    Env(spec LaunchSpec) map[string]string
    SetupWorkspace(spec LaunchSpec) error  // hooks/настройки в worktree до старта
    Activity(ctx, SessionRef) (ActivityState, error) // агент-специфичная часть каскада
}

type LaunchSpec struct {
    SessionID, Kind, ParentID, ProjectID, RepoID, Feature string
    WorktreePath, SystemPrompt, FirstMessage      string
    Model, Effort, PermissionMode                  string
}
```

Адаптер ещё отдаёт `Efforts() []string` — допустимые уровни усилия: claude-code `low, medium, high, xhigh, max`; codex `minimal, low, medium, high, xhigh, max, ultra`; kimi-code `low, high, max`; pi `off, minimal, low, medium, high, xhigh, max`; deepseek `low, medium, high, xhigh, max`. Пустые `Model`/`Effort` — флаг не передаётся, агент работает со своими умолчаниями. Модель адаптер не проверяет: неверное имя проявится при запуске CLI.

Выбор агента, модели и усилия — через профиль модели (см. «Профили моделей» ниже); `--agent` при spawn/up/start остаётся для обратной совместимости, а запуск без профиля (пустой реестр) берёт `defaults.agent` конфига. Общая часть детекции (жив ли процесс на TTY pane) — в мониторе, не в адаптерах.

## Адаптер claude-code (основной)

- **Launch:** `claude --dangerously-skip-permissions --settings '{"crossSessionInbound":"accept"}' --append-system-prompt "$(cat <prompt-file>)" [--model X] [--effort E] -- "<first message>"` — позиционный аргумент авто-сабмитит первый ход, оставляя интерактивный режим.
- **`--settings`:** инлайновый JSON на сессию. `crossSessionInbound: "accept"` нужен, чтобы входящие cross-session сообщения (через них rocket доставляет очередь) сразу попадали агенту. Без явного `accept` сессия с обойдёнными правами (а это все воркеры) *придерживает* сообщение до подтверждения человеком — и оно молча протухает. Настройка идёт именно флагом, а не через `.claude/settings.local.json` в worktree: repo-уровень может только *ужесточать* `crossSessionInbound`, поэтому `accept` оттуда был бы проигнорирован.
- **Env:** `CLAUDECODE=""` (анти-nesting) + стандартные ROCKET_*. Ни spawn-env, ни командная строка не выставляют `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`, `DISABLE_TELEMETRY`, `DO_NOT_TRACK`, `DISABLE_GROWTHBOOK`: они выключают вычисление feature-флагов, а cross-session messaging живёт за флагом — inbox сессии просто не поднимется (на это есть тесты).
- **SetupWorkspace:** идемпотентный upsert `.claude/settings.json` в worktree — hook-скрипт активности (`SessionStart/Stop/PreToolUse/PostToolUse/Notification/...` → `POST /v1/internal/activity` через `curl --unix-socket`).
- **Скилл шторма оркестратора:** rocket встраивает в бинарь свой скилл `orchestrator-brainstorming` — копию `superpowers:brainstorming` из superpowers 6.4.1 (`internal/prompts/skills/orchestrator-brainstorming/`, источник и лицензия — в его README/LICENSE). Адаптер заявляет это через `agent.BrainstormSkillShipper` (`ShipsBrainstormSkill() bool`; есть только у claude-code — только для него старт задачи и выбирает этот скилл). `SetupWorkspace` для **оркестратора**, чья задача стартовала с этим скиллом (`LaunchSpec.BrainstormSkill` — имя без `@версии`: задача хранит `orchestrator-brainstorming@1.1`, менеджер сессий отрезает версию через `prompts.SkillName`, адаптер на всякий случай сравнивает так же), каждый раз перезаписывает `<worktree>/.claude/skills/orchestrator-brainstorming/` (`scripts/*.sh` — исполняемые) и добавляет путь в `.git/info/exclude`; со штатным скиллом каталог удаляется — у обоих одинаковое описание, и лишняя копия могла бы подхватиться вместо штатного и смешать ряды метрики. Глобальный `~/.claude` не трогается, воркеры скилл не получают, codex — тоже (скиллы у него вырезаются).
- **Superpowers:** промпты rocket ([prompts/](../docs/prompts/)) требуют от агентов навыков плагина Superpowers (brainstorming, writing-plans, TDD, systematic-debugging, verification-before-completion). Для claude-code это предусловие: `rocket doctor` проверяет, что плагин установлен, и предупреждает, если нет. Для агентов без поддержки skills секции про Superpowers из промптов вырезаются адаптером (шаблоны помечают их условным блоком `{{#if skills}}`).
- **Activity:** нативный JSONL-транскрипт `~/.claude/projects/<путь-как-slug>/*.jsonl`: тип последней записи + mtime → active/ready/idle/blocked; push-hooks дают waiting_input.

## Адаптер codex

- **Launch:** `codex --no-alt-screen --sandbox danger-full-access --ask-for-approval never --no-daemon -c check_for_update_on_startup=false [-m X] [-c model_reasoning_effort=E] -- "<first message>"`; системный промпт — через `AGENTS.md`-механику или флаг (уточнить при реализации по актуальной версии codex CLI).
- **`--no-alt-screen`:** codex рисует TUI в обычном экране, а не в альтернативном. Иначе его вывод не попадает в историю панели tmux, и в браузерном терминале дашборда сессию codex нечем прокручивать — колесо и свайп вместо прокрутки листают прошлые промпты (задача #5073). Флаг, как и остальные, стоит до `--`, иначе он станет частью первого сообщения. Распознавание экрана (доставка через `Inject`, захват `capture-pane -S -N`) от режима не зависит: оно смотрит на последние строки захвата; проверено вживую на codex-cli 0.160.0 (`#{alternate_on}` = 0, `#{history_size}` растёт).
- **`--no-daemon` и `-c check_for_update_on_startup=false`:** оба флага всегда, до `--`. Codex 0.160+ без них исполняет shell-команды через общий `codex app-server --managed-daemon` со своим окружением — переменные `ROCKET_*` сессии там не видны (фича #5707, Q1). Проверка обновлений на старте открывает интерактивный экран, который перехватывает первое сообщение.
- **Activity:** сессионные JSONL codex в `~/.codex/sessions/`, по ходам. Codex обрамляет каждый ход записями `event_msg` с `payload.type` `task_started` и `task_complete` (при прерывании — `turn_aborted`). Адаптер читает журнал с конца блоками по 64 КБ до первого такого события (`lastTurnEvent`), поэтому начало долгого хода находится и за большим выводом команды.
  - последнее событие хода — `task_started` (ход открыт) → `active` при любом mtime: долгая команда может десятки минут ничего не писать в журнал. Исключение — последняя запись журнала об ошибке → `blocked`;
  - ход закрыт или событий хода нет → прежние правила: mtime моложе 30 с → `active`, запись об ошибке → `blocked`, иначе `ready`.
- **Экраны старта (#5707, 5707/Q3).** До первого хода Codex CLI может встать на экран-вопрос: «Trust this folder?», «Update available», «Background server has incompatible feature settings», а в новой версии — какой-нибудь ещё. Известные экраны rocket старается не допускать флагами запуска, а незнакомый не нажимает, только показывает человеку:
  - Адаптер реализует `agent.StartupProber` (`HasStartedTurn`: есть ли в журнале сессии `event_msg` с `payload.type=task_started`). Пока это `false` (или журнала нет), монитор на каждом цикле снимает панель и ищет экран через `runtime.ParseStartScreen`. Признаки экрана: внизу нумерованный список `1..n` (n ≥ 2) с одним курсором `›`, под ним только короткая подсказка и нет поля ввода `› Ask Codex…`, над ним — заголовок. Заголовок экрана (`Title`) — вопрос, если он есть («Trust this folder?»), иначе первая строка («Update available · …»). Варианты — только пронумерованные строки: недоступную строку без номера Codex тоже не нумерует. Снимки — `internal/runtime/testdata/codex-start-*.pane`: trust и update сняты вживую с codex-cli 0.160.0 (update — с отдельным `CODEX_HOME`, где `version.json` называет 0.165.0), background server — insta-снимки самого TUI на теге `rust-v0.160.0`. Негативные снимки: обычный экран Codex, неблокирующий баннер «Update available!», занятый Codex с блоком очереди `↳`, экраны Claude Code.
  - Проверка привязана к текущему запуску. Момент запуска — время последнего события `session.restored`, а без него `created_at`; монитор передаёт его в `ActivityRef.Since`, и `HasStartedTurn` считает только `task_started` не раньше него. После `restore` новый Codex стартует в том же worktree, и пока он не написал свой журнал, самый свежий журнал там — от прошлого запуска, где ходы уже были. Без этой привязки восстановленная сессия на экране старта не проверялась бы. Кэш «стартовала» в мониторе тоже привязан к моменту запуска и после restore сбрасывается.
  - Найден экран → активность `blocked`, в `pending_quiz` — permission-квиз с флагом `start_screen` (заголовок, варианты, сырой экран). Дашборд показывает его как карточку разрешения. Если строка журнала `permission_prompts` для этого экрана новая, rocket сообщает о нём один раз: живому родителю — системным сообщением `[rocket] session <id> is stuck on a Codex start screen: «<Title>». Options: 1. …; 2. … …`, а сессии без родителя — записью `problem` в лог её задачи. Тот же экран на следующих циклах и после рестарта демона переиспользует строку, поэтому повторной тревоги нет; другой экран — новая тревога.
  - Экран пропал (два цикла подряд) → квиз закрывается, активность `ready`. Начался первый ход → квиз закрывается, проверка для сессии больше не выполняется.
  - Ответ человека идёт через `answerPermission`: rocket перечитывает экран (`ParseStartScreen`, тот же экран — иначе 409 `prompt_changed`) и шлёт цифру варианта. В Codex 0.160 цифра на trust («Trust and continue») и на «Restart with these settings» только двигает курсор, подтверждает Enter. Поэтому через 300 мс экран читается снова: если он тот же и курсор `›` стоит на выбранном варианте, уходит Enter. Если экран сменился, Enter не шлётся, чтобы не нажать его на следующем экране. Esc на экране означает «выйти» (trust) или «пропустить» (update). Сам по себе rocket клавиш не жмёт.
  - Ограничения: экран, не похожий на «вопрос с вариантами» (сплошной текст), детектор не поймает — остаётся сводка heartbeat оркестратору о воркерах, застрявших в idle/blocked дольше порога.
- Уточнение деталей — задача фазы 5; интерфейс рассчитан на то, что адаптер знает только свои источники.

- **Доставка в занятый Codex** (опциональные интерфейсы `internal/agent`, есть только у codex):
  - `BusyInputAccepter` — `AcceptsInputWhileBusy() bool`, у codex `true`: вставка во время хода попадает в собственную очередь Codex и не теряется (см. [06-messaging.md](06-messaging.md), «Очередь Codex»). Хелпер `agent.AcceptsInputWhileBusy(a)` возвращает `false` для агента без интерфейса.
  - `DeliveryConfirmer` — `ConfirmDelivered(ctx, ref, text, since) (found, at, err)`: есть ли в журнале сессии (самый новый `rollout-*.jsonl` с `cwd` воркспейса, тот же, что у Activity) запись `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":…}]}}` с `timestamp` строго после `since`, чей текст после нормализации пробелов (серии пробелов/табов/переводов строк → один пробел) равен `text`. `at` — `timestamp` первой такой записи. Codex склеивает сообщения, вставленные в его очередь подряд, в одну запись через `\n` (проверено на codex-cli 0.160.0), поэтому совпадением считается и непрерывный набор целых строк записи. Для большого тела сравнивается реально вставленный текст — ссылка на inbox-файл. Журнал читается **с конца**, кусками по 64 КиБ, и скан останавливается на первой записи с `timestamp` не позже `since`: очередь зовёт этот метод каждые несколько секунд для каждого `accepted`-сообщения, а журнал Codex бывает в десятки мегабайт, так что читается только хвост, записанный после вставки (записи хронологичны, раньше точки остановки совпадений быть не может). Строки без разборного `timestamp` скан не останавливают; длина строки не ограничена — вывод инструментов бывает в мегабайты. Нет журнала — `agent.ErrNoSignal`.

## Адаптер kimi-code

Третий агент рядом с claude-code и codex: Kimi Code CLI 2.4.0 на оплаченной подписке (после `kimi login`, отдельный API-ключ не нужен). Годится и для воркеров, и для оркестраторов; возможности — как у codex. Спека — `docs/superpowers/specs/2026-10-28-kimi-code-agent-design.md`, живая разведка — `docs/superpowers/kimi-recon-report.md`.

- **Бинарь.** `kimi` в PATH демона → `$KIMI_CODE_HOME/bin/kimi` → `~/.kimi-code/bin/kimi`. `LaunchCommand` использует найденный абсолютный путь, поэтому spawn работает, даже если `kimi` нет в PATH демона. Нет бинаря — `Available()` возвращает ошибку, spawn отклоняется понятным сообщением, `rocket doctor` показывает ❌.
- **Домашний каталог Kimi** — `$KIMI_CODE_HOME`, по умолчанию `~/.kimi-code`. Все сессии используют один общий каталог (там же учётные данные: отдельный каталог на воркера означал бы отдельный логин). Rocket **читает** из него (конфиг, журналы сессий, `plugins/installed.json`) и **никогда не пишет**: ни `config.toml`, ни `tui.toml`, ни хуки, ни плагины.
- **Launch:** `<kimi> --auto [-m <model>]`. `--auto` («Never Ask») — запуск без подтверждений; Kimi встаёт только на утверждении плана в Plan mode (`--plan` не передаётся). Первое сообщение позиционным аргументом передать нельзя (TUI его отвергает, `-p` — только неинтерактивный режим), поэтому адаптер реализует `agent.FirstMessageByInput`: менеджер сессий не отдаёт `FirstMessage` в `LaunchCommand`, а кладёт его первым сообщением в очередь доставки сессии (системное сообщение: пустой отправитель, в списках показывается как `rocket`) — для start, spawn и restore. Очередь вставляет его, когда парсер панели видит пустое поле ввода.
- **Env:** всегда `KIMI_CODE_TRUST_WORKSPACE=1` (нет экрана доверия к папке), `KIMI_CODE_NO_AUTO_UPDATE=1` (нет экрана автообновления), `KIMI_DISABLE_TELEMETRY=1`; `KIMI_MODEL_THINKING_EFFORT=<effort>` — только если у профиля задано усилие; плюс стандартные `ROCKET_*`.
- **Усилие.** `Efforts()` = `low, high, max`. Проверка усилия профиля идёт по каталогу модели (у модели без `support_efforts` усилие должно быть пустым) — общий механизм, см. «Каталог моделей».
- **Системный промпт.** `--agent-file` в интерактивном TUI 2.4.0 молча игнорируется, поэтому `SetupWorkspace` пишет промпт rocket в блок `<!-- rocket:start -->…<!-- rocket:end -->` в `<worktree>/AGENTS.md` — тем же способом, что адаптер codex: идемпотентно, чужое содержимое файла не трогается, неотслеживаемый `AGENTS.md` исключается из git. В отличие от codex, секции `{{#if skills}}` **не вырезаются**: Superpowers у Kimi работает через его родной плагин.
- **Superpowers.** Плагин ставит человек (`~/.kimi-code/plugins/managed/superpowers`); `rocket doctor` проверяет, что он установлен и включён (см. ниже). Скилл шторма оркестратора: адаптер реализует `BrainstormSkillShipper`, для оркестратора со скиллом `orchestrator-brainstorming` `SetupWorkspace` перезаписывает `<worktree>/.kimi-code/skills/orchestrator-brainstorming/` (скрипты исполняемые, путь в `.git/info/exclude`), иначе каталог удаляется. Глобальный каталог Kimi не трогается.
- **Вставка текста.** Kimi принимает ввод только как bracketed paste: обычная быстрая вставка включает его эвристику paste-burst, и Enter в течение 120 мс после неё вставляет перевод строки вместо отправки. Адаптер реализует `agent.BracketedPaster`; очередь выставляет `runtime.InjectOpts.Bracketed`, и рантайм вставляет текст `tmux paste-buffer -p`, после чего жмёт Enter. Для claude-code и codex вставка не меняется.
- **Cache expired после простоя.** Когда Kimi перехватывает доставку диалогом с предложением compact / new / continue / never, `Inject` автоматически выбирает только `Compact and continue`, если курсор уже стоит на нём. Диалог до вставки запускает compact и откладывает сообщение до следующей попытки очереди; диалог после submit подтверждает compact без повторной вставки — Kimi сам отправляет отложенный текст после compact, а журнальный `DeliveryConfirmer` подтверждает доставку. Если человек переместил курсор на другой вариант, rocket клавиш не жмёт; глобальный `tui.toml` и `cache_expiry_hint` не изменяются.
- **Журнал сессии.** Индекса сессий у Kimi 2.4.0 нет: адаптер обходит `sessions/wd_<имя-папки-ворктри>_*/session_*/state.json` (если такого префикса нет — все `wd_*`), берёт самую свежую сессию с `cwd` = физический путь ворктри (`filepath.EvalSymlinks`) и `createdAt` не раньше `ActivityRef.Since`. `since` важен после `restore`: новый процесс Kimi стартует в том же ворктри, и журнал прошлого запуска не должен приниматься за текущий. Папка сессии появляется **только после первого отправленного сообщения**; до этого `Activity` возвращает `ErrNoSignal` (не `ready`), а готовность ввода берётся с панели. `kimi` для поиска не запускается. Журнал `agents/main/wire.jsonl` читается с конца блоками по 64 КиБ, строки любой длины, битые пропускаются.
- **Activity.** Последний `turn.prompt` без последующего `turn.ended` → `active` при любом mtime. `turn.ended` с `reason=failed` (в том числе 429 — исчерпана квота подписки) или `blocked` → `blocked` (статус без поля причины). На каждый такой ход (дедуп по `turnId`, без повтора после рестарта демона) монитор шлёт живому родителю системное сообщение `[rocket] session <id>: Kimi turn failed: <текст ошибки>`, а сессии без родителя — пишет запись `problem` в лог её задачи; текст ошибки виден и в чате воркера; открытый `interaction.request` (без `interaction.resolved` с тем же `id`) → `blocked`. Иначе mtime моложе 30 с → `active`, старше → `ready`. Квота не ретраится и не переключается на другой профиль автоматически — решает оркестратор (kill + respawn); heartbeat сообщит о застрявшем воркере.
- **Доставка в занятый Kimi.** `AcceptsInputWhileBusy() = true`: Enter во время хода ставит сообщение в собственную очередь Kimi (строка `❯ …` над полем ввода). `DeliveryConfirmer`: в журнале после `since` есть пользовательская запись с `origin.kind="user"` — `turn.prompt` в начале нового хода или `turn.steer` при передаче очереди в текущий ход, — чей текст после нормализации пробелов равен вставленному (склейка нескольких строк — как у codex; для большого тела сравнивается ссылка на inbox-файл). Одна такая запись может подтвердить несколько разных полных сообщений. Сообщение в очереди Kimi попадает в журнал только когда Kimi его принимает: отдельным `turn.prompt` при начале хода или `turn.steer` внутри текущего; пока записи нет, статус `accepted` — это не ошибка. Записи `turn.steer` с `origin.kind=skill_activation` (и прочий не-user origin) доставкой не считаются.
- **Парсер панели** (`internal/runtime`, снимки `testdata/kimi-*.pane`): по устойчивым строкам рамки поля ввода `│ > … │` (не по подсказкам и спиннеру) различает пустое поле (TUI готов), черновик (draft guard очереди) и занятый Kimi с очередью. Очередь и draft guard используют его для сессий `kimi-code` так же, как композер Claude/Codex.
- **Экраны старта.** `StartupProber.HasStartedTurn` — есть `turn.prompt` не раньше `Since`. Пока нет, монитор ищет экран-вопрос отдельным распознавателем Kimi рядом с `ParseStartScreen`: заголовок, варианты под курсором `❯` **без номеров** и подсказка `↑↓ navigate · Enter select` (например, «Trust this folder?» с вариантами `Trust and continue` / `Exit`). Флагами не гасятся опрос feedback, «cache expired», «Model migration available», «Kimi Code Update Available» — их ловит детектор. Ответ человека на ненумерованный экран — стрелками до выбранного варианта и Enter, с предварительным перечитыванием экрана (экран сменился — 409 `prompt_changed`). Остальное (квиз `start_screen`, тревога родителю, ответ человека) общее с codex.
- **Расход токенов и чат** — см. «Расход токенов» и [13-chat.md](13-chat.md).
- **`rocket doctor`.** Кроме общего `agent:kimi-code` (бинарь найден), если бинарь есть, выполняются две проверки-предупреждения (⚠️, не ❌): `kimi-code:login` — в `config.toml` есть провайдер `managed:kimi-code` и непустой `credentials/` (иначе «вход не выполнен — выполните `kimi login`»); `kimi-code:superpowers` — плагин `superpowers` есть в `plugins/installed.json` и `enabled`. Проверки только читают `$KIMI_CODE_HOME`.
- **Не делаем:** серверный режим Kimi (`kimi web`/`kimi acp`), глобальные хуки, показ остатка квоты подписки в дашборде.

## Адаптер pi

Pi — мультипровайдерный агент для воркеров и оркестраторов. Профиль по-прежнему хранит только `agent/model/effort`: точный `provider/model` выбирается моделью профиля, отдельной provider-схемы нет.

- **Доступность и запуск.** Требуется `pi` версии 1.0.0+. Команда:
  ```
  pi --tui-mode regular --approve --session-id <rocket-session-id> \
     [--model <provider/model>] [--thinking <effort>] [-- <first message>]
  ```
  `regular` оставляет scrollback в tmux и браузере, `--approve` снимает остановку на доверии к worktree, первый бриф идёт позиционным аргументом. Пустые model/effort не передаются.
- **Env и секреты.** Всегда выставляются `PI_TELEMETRY=0` и стандартные `ROCKET_*`. Из окружения демона в процесс явно передаётся только API key провайдера из `provider/model` (для DeepSeek — `DEEPSEEK_API_KEY`); пустые значения и ключи других провайдеров не передаются. Наследуемые ключи других провайдеров вычищаются. Ключи не попадают в профиль, SQLite, worktree, git или журналы rocket.
- **System prompt и skills.** `SetupWorkspace` идемпотентно обновляет блок rocket в `AGENTS.md`, не вырезая секции skills. Оркестратору с custom skill записывается `.pi/skills/orchestrator-brainstorming/` и путь добавляется в `.git/info/exclude`; в остальных сессиях только эта rocket-копия удаляется. Superpowers ставит человек глобально: `pi install git:github.com/obra/superpowers`. Rocket не меняет глобальные Pi settings, auth или packages.
- **Сессия и restore.** Журнал — `~/.pi/agent/sessions/--<path>--/*_<session-id>.jsonl` (с учётом `PI_CODING_AGENT_DIR`), поэтому restore передаёт тот же `--session-id` и снимок профиля, продолжает прежний JSONL и добавляет restore-бриф новым сообщением. `ActivityRef.Since` отсекает ход прежнего запуска; серверный кэш после паузы может уже истечь, и это не failed restore.
- **Activity, chat, ошибки.** По активной ветке JSONL: незавершённый user/assistant/tool ход → `active` независимо от mtime, закрытый ответ → `ready`, provider/model error → `blocked`; отсутствие журнала → `ErrNoSignal`. Чат идёт по активной ветке с курсором; user/assistant — сообщения, tool records — короткие digest, битая строка не ломает остальной журнал. `TurnFailureReporter` отдаёт стабильный id и текст последней ошибки без повторов.
- **Доставка и pane.** `AcceptsInputWhileBusy() = true`: Enter во время хода ставит steering в собственную очередь Pi. Статус `accepted` сохраняется, пока `DeliveryConfirmer` не найдёт соответствующий user record после `since` именно в этой сессии. Отдельный pane parser различает empty/draft/busy composer и steering-очередь; на неизвестный экран ввод вслепую не идёт, чужой draft не стирается. Режим вставки определяется живой разведкой и покрыт тестами.
- **Native-канал pi-uds-v1 (Pi 1.4.0+).** Новые/восстановленные сессии с поддержанным Extension API получают приватный UDS и встроенное расширение по явному `--extension`; доставка идёт публичным `sendUserMessage`/`followUp` без `setText`/paste/submit и без tmux. Rocket ставит служебную подпись `[rocket #<persisted id>] ` перед подготовленным текстом (DB Body и inbox не меняются). Подтверждение native — только строгое равенство всего объединённого текста одной user-записи подписанному ожидаемому тексту; `holdsLines`/подстрока/цитата чужого envelope не засчитываются, старые неподписанные записи не подтверждаются. Нет истории — timeout/exit/restore даёт `native_delivery_unconfirmed`, без blind resend и без tmux-фолбэка. Ограничения: отложенный async `ui_prompt_start` может допустить ход при открытом окне; Escape/dequeue/custom close Pi могут вернуть очередь и переставить курсор; login/OAuth и часть selector-веток не проверены; отменённый switch оставляет канал закрытым до `/reload`; повреждённое accepted-состояние переводит запуск в fail-closed до нового Rocket launch; возврат в старую историю после non-reload завершения требует restore.
- **Каталог.** `pi --list-models` с таймаутом 10 с отдаёт точные `provider/model` и уровни `off,minimal,low,medium,high,xhigh,max`; при ошибке — встроенные `deepseek/deepseek-flash` и `deepseek/deepseek-v4-pro` с `warning`. Ручной ввод любого другого `provider/model` остаётся доступен. Пустой реестр автоматически не сидируется.
- **`rocket doctor`.** После успешной общей `agent:pi` дополнительная `pi:superpowers` с таймаутом читает `pi list` и ищет `git:github.com/obra/superpowers`. Отсутствие пакета, ошибка или таймаут — ⚠️ с подсказкой `pi install git:github.com/obra/superpowers`; credentials и raw output не печатаются.
- **Кэш и non-goals.** Используются нативные cache warming/compaction и фактические usage. Rocket не задаёт `PI_CACHE_RETENTION`, не обещает TTL/hit-rate и не повторяет запрос из-за обычного cache miss. DSH, прокси, provider-поля в профиле, автоматический fallback и отдельный mobile UI не добавляются.

## Адаптер deepseek

DeepSeek — отдельный адаптер поверх полной реализации Claude Code, использующий официальную Anthropic-совместимую интеграцию. Launch, system prompt, Superpowers/orchestrator skill, activity, chat, delivery, restore и startup behavior наследуются; обычный `claude-code` остаётся на Anthropic и не получает DeepSeek-настройки.

- **Доступность.** Требуются бинарь `claude` и непустой `DEEPSEEK_API_KEY` в окружении демона. Если в окружении его нет, демон при старте читает его из `$ROCKET_HOME/deepseek-api-key` (окружение демона зависит от того, кто его запустил: при `make restart` это может быть CLI-вызов агента). Общая проверка `agent:deepseek` сообщает об отсутствии любого из них; ключ не хранится в профиле/SQLite/git/task docs/log.
- **Env.** К базовому Claude Code env адаптер добавляет только свои значения, которые выигрывают у repo-level env:
  ```
  ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic
  ANTHROPIC_AUTH_TOKEN=<значение DEEPSEEK_API_KEY>
  ANTHROPIC_MODEL=<model профиля, иначе deepseek-flash[1m]>
  ANTHROPIC_DEFAULT_OPUS_MODEL=deepseek-flash[1m]
  ANTHROPIC_DEFAULT_SONNET_MODEL=deepseek-flash[1m]
  ANTHROPIC_DEFAULT_HAIKU_MODEL=deepseek-flash
  CLAUDE_CODE_SUBAGENT_MODEL=deepseek-flash
  CLAUDE_CODE_AUTO_COMPACT_WINDOW=786432
  CLAUDE_CODE_EFFORT_LEVEL=<effort профиля, иначе max>
  ```
  Явные `--model` и `--effort` базового запуска сохраняются. Обычный `claude-code` не наследует endpoint/token DeepSeek.
- **Сессия и каталог.** Restore использует нативный Claude Code журнал и снимок профиля. Встроенный каталог содержит `deepseek-flash[1m]` и `deepseek-v4-pro`, рекомендуемое усилие — `max`; сетевого каталога и автосидинга нет.
- **Кэш и usage.** Disk cache DeepSeek включён самим provider и работает best effort: совпадающий префикс может дать hit без специального параметра. В usage фактический hit → `CacheRead`, miss → `Input`, output → `Output`; `CacheWrite` заполняется только при реальном значении из журнала, не по предположению о записи. Anthropic `cache_control` и TTL не управляют этим кэшем, 100% hit не гарантируется, обычный miss не является ошибкой и не вызывает повторный запрос.
- **Non-goals.** DeepSeek Harness (`dsh`), прокси, сторонние cache-расширения, собственная compaction, принудительный TTL, хранение API key и автоматический переход на другой профиль не используются.

## Расход токенов (UsageReader)

Все адаптеры дополнительно реализуют необязательный интерфейс `agent.UsageReader`; основной `agent.Agent` от него не зависит:

```go
type Tokens struct { Input, CacheWrite, CacheRead, Output, Reasoning, Messages int64 }
type Usage struct {
    Models map[string]Tokens
    FirstAt, LastAt time.Time
    Found bool
}
type UsageReader interface {
    Usage(ctx context.Context, worktreePath string, since time.Time) (Usage, error)
}
```

`Models` индексируется **фактическим id модели из транскрипта**, а не моделью профиля запуска. `FirstAt` и `LastAt` берутся из временных меток записей. `Found=false` без ошибки означает, что транскрипт сессии не найден; ошибка чтения файла возвращается без частичной суммы. Записи до `since` не учитываются; для записей без временной метки берётся mtime файла. Парсеры читают JSONL построчно, пропускают повреждённые строки и поддерживают строки до 16 МиБ.

- **Claude Code:** все `*.jsonl` в `~/.claude/projects/<slug-worktree>/` и `*/subagents/*.jsonl` при наличии подходящего родительского файла. У родителя сверяется `cwd` с worktree; сабагент привязывается через родителя. Из строк `type=assistant` учитываются `message.model` и `message.usage`, кроме модели `<synthetic>`. Повторы `message.id` дают один ответ: `Messages` — число уникальных ответов. `input_tokens` → `Input`, `cache_creation_input_tokens` → `CacheWrite`, `cache_read_input_tokens` → `CacheRead`, `output_tokens` → `Output`.
- **Codex:** все `rollout-*.jsonl` в каталогах `$CODEX_HOME/sessions/YYYY/MM/DD` от даты `since` до сегодня, если `session_meta.payload.cwd` совпадает с worktree. `event_msg` с `payload.type=token_count` содержит накопительный `info.total_token_usage`; разница между соседними событиями относится к последнему `turn_context.model`. `Input = input_tokens − cached_input_tokens`, `CacheRead = cached_input_tokens`, `CacheWrite = cache_write_input_tokens`, `Output = output_tokens`; `Reasoning = reasoning_output_tokens` уже входит в `Output`. `Messages` — число событий `token_count`, у которых накопительный `total_tokens` вырос относительно предыдущего события; повтор без прироста не считается.
- **Kimi Code:** записи `usage.record` из `agents/*/wire.jsonl` всех сессий ворктри (папки сессий, найденные тем же локатором, что и Activity), по фактической `model`: `usage.inputOther` → `Input`, `inputCacheRead` → `CacheRead`, `inputCacheCreation` → `CacheWrite`, `output` → `Output`; `Messages` — число записей. Журналы помощников (`agents/agent-N/wire.jsonl`) включаются, а `subagent.completed.usage` в журнале основного агента **не** прибавляется — он повторяет итог журнала помощника (иначе двойной счёт). Записи до `since` не считаются; несколько сессий в одном ворктри суммируются; нет журнала — `Found=false`.
- **Pi:** по журналу сессии суммируются фактические `usage.input/cacheRead/cacheWrite/output` assistant records, а также отдельные usage cache warming, compaction и branch summary по их фактической модели. Все выполненные ветки включаются, даже заброшенные, но один record учитывается один раз; `since` исключает прежнее использование worktree, повторный поллинг не удваивает сумму. Ошибка чтения не превращается в нулевой расход.
- **DeepSeek:** используется Claude Code UsageReader по фактической модели: DeepSeek cache hit → `CacheRead`, miss → `Input`, output → `Output`; `CacheWrite` берётся только из реального поля журнала. Стоимость по тарифам настраивается человеком через `rocket stats prices`.

## Профили моделей

Профиль — запись реестра `model_profiles` (схема — в [05-state.md](05-state.md)): имя, агент, модель, усилие, описание «для чего подходит», флаг `enabled`. Реестр ведёт **только человек** — дашборд (Настройки → «Модели») или CLI:

```
rocket models ls [--all]          # из сессии агента: разрешённые его фиче (* — профиль по умолчанию);
                                  # человек: реестр с пометкой дефолтов; --all — и выключенные
rocket models add <name> --agent A [--model M] [--effort E] [--description D]
rocket models edit <name> [--model M] [--effort E] [--description D] [--enable|--disable] [--position N]
rocket models rm <name>           # 409 profile_in_use, пока профиль — дефолт
rocket models default --orchestrator <name> | --worker <name>

rocket spawn ... --profile <name>                   # профиль воркера
rocket task start <id> --profile <name> [--allow p1,p2]
rocket up "..." --profile <name>
rocket task models <id> [--allow p1,p2 | --clear]   # посмотреть/сузить allowlist задачи
```

`add/edit/rm/default`, `task models --allow/--clear` и `allowed_profiles` в `task start` из сессии агента получают 403 `human_only`: иначе оркестратор мог бы расширить себе выбор.

**Политика** (`internal/modelpolicy`, чистые функции; через неё проходит каждый запуск, кроме restore):

- `Allowed(task)` — включённые профили ∩ allowlist фичи (`tasks.allowed_profiles`, пусто = все), по `position, name`. Allowlist, все имена которого исчезли из реестра, даёт пустой список, а не «все».
- **Воркер** (`POST /v1/sessions`): allowlist берётся с фичи вызывающего оркестратора. `--profile P` — P должен быть в `Allowed` (иначе `profile_not_allowed: allowed: a, b`; несуществующий — `profile_not_found`); только `--agent A` — первый разрешённый профиль агента A; ничего — `default_worker_profile`, если разрешён, иначе первый разрешённый; пустой `Allowed` — `no_profiles_allowed`; профиль и агент не совпадают — `bad_request`. Политика проверяется до создания подзадачи — отказ ничего не оставляет.
- **Оркестратор** (`POST /v1/tasks/{id}/start`): allowlist на него не распространяется, проверяется только `enabled`. Без выбора — `default_orchestrator_profile`, затем первый включённый профиль агента `defaults.agent`; если нет и такого — запуск как раньше, без модели (старт задачи не ломается на пустом реестре). Выбор пишется в `tasks.orchestrator_profile`; скилл шторма выбирается по **агенту** профиля. `allowed_profiles` из тела старта сохраняется до спавна — первый же `rocket models ls` оркестратора видит суженный список.
- **Снимок.** Сессия хранит `profile/model/effort` на момент запуска; правка или удаление профиля запущенные сессии не трогает. `rocket restore` поднимает сессию из снимка, даже если профиль с тех пор выключен или удалён.

`rocket status` показывает профиль у оркестратора и колонку `PROFILE` у воркеров.

### Каталог моделей

Каждый адаптер умеет `Catalog(ctx)` — список моделей агента с допустимыми уровнями усилия (`internal/agent/*/catalog.go`):

- **codex:** `codex debug models` (таймаут 10 с) → `$CODEX_HOME/models_cache.json` (по умолчанию `~/.codex`) → встроенный список. Модели с `visibility=hide` не показываются; все показанные — основные (`main`).
- **kimi-code:** таблицы `[models."kimi-code/*"]` из `$KIMI_CODE_HOME/config.toml` (по умолчанию `~/.kimi-code`): id = алиас (`kimi-code/k3.1-pro`), имя = `display_name`, усилия = `support_efforts`, по умолчанию = `default_effort`; все модели основные (`main`). Нет файла или TOML битый → встроенный снимок 6 моделей на 2026-10-28 + предупреждение. Источник — `cache`; CLI и дашборд подписывают его «из конфига Kimi Code» (`from Kimi Code config`).
- **claude-code:** самый свежий по mtime `~/.claude/cache/model-catalog/*-cc.json` (формат `version: 2`, модели в `catalog.config.models`) → встроенный список. `section=main` — основные, `overflow` — предыдущие. Усилие по умолчанию — вариант с бейджем `Default`, иначе `Recommended`.
- **pi:** `pi --list-models` с таймаутом 10 с → встроенные `deepseek/deepseek-flash` и `deepseek/deepseek-v4-pro` с `warning`. Источник CLI подписывается «из Pi CLI» (`from Pi CLI`). Модели получают полный набор `off,minimal,low,medium,high,xhigh,max`: Pi сам ограничивает его возможностями конкретной модели.
- **deepseek:** встроенные `deepseek-flash[1m]` и `deepseek-v4-pro` с уровнями Claude Code и рекомендуемым `max`. Источник подписывается `built-in` / `встроенный список`.

Битый или отсутствующий источник — не ошибка: адаптер откатывается дальше, пишет предупреждение в лог демона и возвращает его в `warning`. Пустой реестр новых агентов автоматически не сидируется: профили заводит человек (`rocket models import` или Настройки → «Модели»). Демон держит каталог в памяти 10 минут (`refresh=1` сбрасывает).

```
rocket models catalog [--agent A] [--all] [--refresh]   # агент, id, имя, усилия (* — по умолчанию), описание;
                                                        # --all — с предыдущими; внизу источник и предупреждение
rocket models import [--agent A] [--legacy]             # выключенные профили для моделей без профиля (только человек);
                                                        # A: claude-code | codex | kimi-code | pi | deepseek
```

**Проверка усилия профиля.** Если модель профиля есть в каталоге его агента (точное совпадение id), усилие должно входить в уровни этой модели; у модели без настройки усилия (Haiku) оно должно быть пустым. Иначе (алиас вроде `opus`, своя модель, модель не задана) — проверка по `Efforts()` агента: claude-code `low, medium, high, xhigh, max`, codex `minimal, low, medium, high, xhigh, max, ultra`, kimi-code `low, high, max`, pi `off, minimal, low, medium, high, xhigh, max`, deepseek `low, medium, high, xhigh, max`. Ошибка — 400 `bad_effort` со списком допустимых уровней.

**Импорт.** `import` создаёт профиль для каждой модели каталога (по умолчанию только основные), у которой ещё нет профиля с тем же агентом и моделью (усилие не важно). Профиль выключен, усилие пустое, описание из каталога (или имя модели), позиция — после существующих. Имя: claude-code и deepseek — slug id модели (`deepseek-flash[1m]` → `deepseek-flash-1m`); codex — `codex-` + slug с `.`/`_` → `-` (`codex-gpt-5-6-sol`); kimi-code — `kimi-` + slug id без префикса `kimi-code/`, без двойного `kimi-` (`kimi-code/k3.1-pro` → `kimi-k3-1-pro`, `kimi-code/kimi-for-coding` → `kimi-for-coding`); pi — `pi-` + slug полного `provider/model`, при этом повторный префикс провайдера убирается (`deepseek/deepseek-flash` → `pi-deepseek-flash`, `openai/gpt-6-sol` → `pi-openai-gpt-6-sol`). Занятое имя получает `-2`, `-3`…

## Добавление нового агента

Реализовать интерфейс + зарегистрировать в реестре адаптеров. Требования к кандидату: интерактивный TUI, переживающий paste-ввод; какой-либо наблюдаемый след активности (файлы сессий, hooks); неинтерактивный режим прав (авто-подтверждение инструментов), иначе воркер без человека застрянет.

## Постоянные агенты

Не путать с адаптерами выше: **постоянный агент** — это зарегистрированный
«дежурный» («SRE платформы», «разборщик issues»), к которому обращаются люди и
другие агенты. Полная спека — v4 в доках задачи #639.

Rocket агентом **не управляет** и не знает, как он устроен: что у него внутри,
какая память, как он строит работу — не забота демона. От системы требуется
ровно три вещи:

1. **Регистрация**: агент — запись в базе плюс фиксированное имя tmux-сессии.
   Все видят, что агент есть, и могут слать ему сообщения.
2. **Доставка**: сессия жива → доставить обычной очередью; нет → положить в инбокс.
3. **Разбор по одному**: когда агент появился, он получает одно уведомление
   «у тебя N непрочитанных» и разбирает их сам, по одному.

### Модель

- **Реестр** (`agents`): `id` (`[a-z0-9-]`, он же имя tmux-сессии и адрес в
  очереди сообщений), `description` для людей, необязательный `project_id`
  (только группировка в UI, прав не даёт и не ограничивает), необязательная пара
  лончера `dir`/`command`, `launch_profile` (последний выбранный профиль модели;
  пусто — legacy `command`), `enabled`.
- **Инбокс** (`agent_inbox`): сообщения `{id, agent_id, from, body,
  status: unread|read, created_at, read_at}`. Единственный вид записи —
  сообщение; никаких видов событий, статусов доставки и досье.
- **Сессия агента**: внешняя. Агент (или человек) запускает что угодно в
  tmux-сессии с именем `<id>`; rocket её не создаёт по своей инициативе, не
  убивает и не следит за жизненным циклом. Демон лишь определяет живость и
  заводит под неё строку `sessions` с `kind='agent'` и id, равным id агента.

### Права

Постоянный агент в правах на задачи приравнен к человеку: создаёт задачи
(корневые и подзадачи, в любом проекте), запускает их (`rocket up`,
`rocket task start`) и редактирует. Задачи, созданные агентом, получают
`created_by='agent'`.

Особый случай — **майлстоны** ([12-tasks.md](12-tasks.md), раздел «Майлстоны»):
корневые задачи вне проектов, которые агент берёт себе. Свободный майлстон может
взять любой постоянный агент (`rocket task take <id>`), но писать в него —
журнал, документы, смена статуса — вправе только его держатель и человек. Это
единственное место, где агент **не** приравнен к человеку: чужой майлстон для
него закрыт. Назначать и снимать майлстоны (`rocket task assign`) может только
человек, и только человек ставит `done`/`cancelled` — это приёмка.

Чего агент не может: спавнить воркеров (`POST /v1/sessions` — только живой
оркестратор, у которого есть контекст фичи, ветки и карточки подзадачи).

Идентификатор сессии приходит из `ROCKET_SESSION_ID` и является атрибуцией,
а не аутентификацией: запуск с `env -u ROCKET_SESSION_ID` выполняет команду
от имени человека и теряет авторство. Реальный контроль доступа потребовал бы
подписанных токенов сессий — это отдельная задача.

### Живость и адопция

`internal/agentwatch` раз в `activity_poll_interval` сверяет реестр с живыми
tmux-сессиями:

- сессия `<id>` есть, строки сессии нет (или она мертва) → строка заводится
  (одинаково для `rocket agent start` и для сессии, поднятой руками);
- сессии нет, а строка жива → строка переводится в `done`, чтобы `session_alive`
  и уведомления не врали;
- если под этим именем уже живёт сессия **другого** вида (оркестратор/воркер),
  адопция отклоняется с предупреждением в лог — чужая сессия не трогается.

### Лончер (тонкий, опциональный)

Агента проектируют как обычный проект: директория со своим `CLAUDE.md`, скиллами
и прочим. Rocket может его запустить, но не управляет им:

- `rocket agent start <id> [--profile <name>]` — создаёт tmux-сессию `<id>`
  (cwd = `dir`, env `ROCKET_SESSION_ID=<id>`, `ROCKET_SOCKET`, `ROCKET_HOME`,
  чтобы изнутри работали `rocket inbox|send|task`). Профиль из общего реестра
  запускается через свой адаптер с его model/effort и сохраняется в
  `launch_profile`; следующий `start` без флага повторяет его. В `sessions`
  записываются точные `agent/profile/model/effort` на момент запуска, а
  `SetupWorkspace` и системный промпт не используются. Неизвестный или
  выключенный профиль — `profile_not_found`/`profile_disabled`, без fallback на
  другую конфигурацию.
- **Без сохранённого профиля работает legacy launcher.** Запускается `command`;
  нет `command` — интерактивный shell; нет `dir` — ошибка `agent_no_dir` с
  подсказкой создать сессию вручную (`tmux new -s <id>`). Для таких и поднятых
  вручную сессий адаптер определяется из ведущего бинарника `command`: `kimi` →
  `kimi-code`, `claude`/`claude-code` → `claude-code`, `codex` → `codex`, `pi` →
  `pi`; явный `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic` перед
  `claude` → `deepseek`, а bare `claude` остаётся `claude-code`. Понимаются
  `env`, присваивания, пути и кавычки, но не обёртки `bash -c` — неизвестная
  обёртка возвращает пустое определение,
  с fallback на `defaults.agent`. Доставка, монитор и дашборд ключуются по
  `sessions.agent`: неверный адаптер ломает диалект вставки и подтверждение
  доставки. Adoption живой legacy-сессии пересверяет адаптер с `command`;
  snapshot profile-запуска не перезаписывает.
- `rocket agent attach <id>` — attach (или `switch-client` изнутри tmux);
  `rocket agent stop <id>` — убивает tmux-сессию, регистрация остаётся.
- Никаких рестартов, watchdog'а и системных промптов от rocket: контекст агента —
  его собственный `CLAUDE.md`.

### Доставка и разбор

`rocket send <agent-id> "..."` (и `POST /v1/agents/{id}/messages` из дашборда)
идут одним путём:

- сессия `<id>` жива → обычная доставка через очередь сообщений (`[from ...] ...`),
  ответ `202 {to, status:"queued", live:true}`;
- сессии нет → строка в инбоксе, ответ `202 {to, status:"inbox", live:false}`.

Когда сессия появляется и в инбоксе есть непрочитанные, демон **один раз**
инжектит:

```
[rocket] You have 3 unread messages. Read them one by one with: rocket inbox next
```

Повторное уведомление — только если после последнего появились новые
непрочитанные, и не чаще `agent_notify_interval` (5m по умолчанию). Новая сессия
агента всегда получает уведомление заново, даже если пачка не выросла.

Дальше — pull-модель, изнутри сессии агента:

```
rocket inbox            # счётчик + список непрочитанных (id, from, возраст, первая строка)
rocket inbox next       # взять самое старое непрочитанное целиком и пометить прочитанным
rocket inbox peek <id>  # прочитать конкретное, не помечая
```

Id агента команды берут из `--agent`, иначе из `ROCKET_SESSION_ID`, иначе из
имени tmux-сессии — поэтому они работают и в сессии, поднятой руками.

### Q&A-треды

Механизм тот же самый, что у задач, — буквально один: тред роли отличается от
треда задачи только тем, к чему привязан. Полное описание модели участников,
`--to`, attention set, типов `decision`/`fyi`, вариантов ответа и прав — в
[12-tasks.md](12-tasks.md), раздел «Вопросы и ответы через задачу»; здесь только
специфика роли.

- Тред роли — строка в `questions` с заполненным `role_id` (отдельных таблиц
  `agent_questions` / `agent_question_messages` больше нет, они слиты в общую
  схему). У него, как у любого треда, есть список участников: человек и сама
  роль сеются при создании, остальные вступают через `--to` или написав в тред.
- Локальная ссылка на тред роли — `<id>/Q<n>` (`cto/Q1`), без решётки, в отличие
  от треда задачи (`#1023/Q2`). Это тот же id и в выводе команд, и в рамке
  доставленной записи, и в аргументе `reply`/`close`.
- **Человек → агент**: `rocket agent ask <id> "..."` (или `POST
  /v1/agents/{id}/questions` без заголовка сессии). Текст доезжает агенту тем же
  путём, что обычное сообщение — живому в сессию, мёртвому в инбокс — с
  префиксом `[<id>/Q<n> question|reply|answer from <кто>] ...` (у задач это
  `[#N/Q<n> ... from <кто>]`). Так доставляется **каждая** запись треда
  каждому участнику, кроме её автора, а не только открытие вопроса человеком.
- **Агент → человек**: тот же `rocket agent ask` изнутри сессии агента (заголовок
  `X-Rocket-Session: <id>`): `asked_by` = id сессии, на карточке загорается
  бейдж «ждёт ответа». `--fyi` создаёт статусную заметку, которая никого не ждёт
  и бейджа не зажигает, `--option` — варианты ответа для `close --choose`.
- Закрыть тред (`rocket agent close <тред> "<резолюция>" | --choose <n> |
  --dismiss ["<почему>"]`) может человек **и любой постоянный агент** — в том
  числе сам агент, которому этот тред принадлежит. Оркестратор и воркер закрыть
  тред не могут: `403` с подсказкой использовать `reply`. `rocket agent answer` —
  прежнее имя той же команды, скрытое из help.
- Переоткрыть закрытый тред репликой может любой участник, кроме человека.
- «Чей ход» — хранимое attention-множество (`attention`, оно же `waiting_on` для
  совместимости, + `your_turn` для вызывающего); совместимое поле `whose_turn`
  (`user` | `role`) выводится из него. Счётчики `open_questions` /
  `awaiting_user` отдаются в `GET /v1/agents`.
- `rocket agent ask|reply|close` принимают `--to` — ровно как их аналоги у
  задач (id через запятую, флаг можно повторять), а `reply`/`close` ещё и
  `--dry-run`, `--join` и `--file`.

Постоянный агент — полноправный участник и **чужих** тредов: оркестратор может
адресовать ему вопрос задачи (`rocket task ask <id> "..." --to cto`), и тогда
агент получает уведомление, отвечает через `rocket task reply` и вправе закрыть
тред через `rocket task close`. В тред, куда его не звали, агент тоже может
написать — но только осознанно, с `--join`: без него сервер отвечает
`not_a_participant`, показывая, в какой тред запись чуть не ушла.

Если `decision`-тред не двигается дольше `question_stale_after` (по умолчанию
24h), heartbeat присылает каждому участнику из attention одно напоминание с
локальной ссылкой, цитатой вопроса, временем ожидания и готовыми командами
`reply`/`close` (подробности — [08-orchestrators.md](08-orchestrators.md),
§«Устаревание вопросных тредов»).

### Майлстоны

Майлстон — единица работы постоянного агента: корневая задача вне всех проектов,
которую он берёт себе. Полная модель, права и статусная механика — в
[12-tasks.md](12-tasks.md), раздел «Майлстоны»; здесь только то, что касается
самого агента:

- **Сессия одна на все майлстоны.** У майлстона нет своей сессии — есть сессия
  агента, названная его id. Поэтому карточка майлстона показывает не
  `rocket attach <session>`, а `rocket agent attach <agent>`.
- **Что агент взял, видно в одном месте:** `rocket agent show <agent>` и
  `GET /v1/agents/{id}` отдают список его майлстонов (`id`, `title`, `status`),
  тот же список показывает карточка агента в дашборде и в mobile.
- **Молчание замечают.** Если взятый майлстон в работе (`brainstorm` или `in_progress`) дольше
  `milestone_quiet_after` (по умолчанию 24h) не показывает следов работы
  держателя, heartbeat присылает ему `[rocket quiet milestone] …` с готовыми
  командами, а у человека майлстон помечается флагом `quiet`. Следом считается
  запись журнала (кроме `status`), документ или запись в вопросном треде — и
  только за авторством самого держателя.

### Сниппет для CLAUDE.md агента

Rocket ничего не дописывает агенту в промпт, поэтому команды ниже стоит
скопировать в его собственный `CLAUDE.md`:

```markdown
## Rocket

Ты зарегистрирован в rocket как агент `<id>`; твоя tmux-сессия называется так же.
Люди и другие агенты пишут тебе через rocket.

- `rocket inbox` — сколько сообщений тебя ждёт и от кого.
- `rocket inbox next` — взять самое старое непрочитанное целиком (и пометить
  прочитанным). Разбирай по одному: обработал — бери следующее, пока не
  «inbox is empty». Не откладывай пачку «на потом»: прочитанное больше нигде
  не всплывёт.
- `rocket inbox peek <msg-id>` — перечитать сообщение, не трогая его статус.
- `rocket send <кому> "<текст>" | --file <path>` — написать другому агенту или
  сессии. Markdown с бэктиками шли только через `--file` (`-` = stdin): в
  позиционном аргументе шелл выполнит бэктики и получатель прочитает дыры.
- `rocket task ls|show|add` — задачи; работу веди через них, а не в переписке.

## Майлстоны

Майлстон — твоя единица работы: корневая задача вне проектов, которую берёшь ты
сам. Не фича с оркестратором и воркерами — здесь работаешь ты, и видно её ровно
настолько, насколько ты её показал.

- `rocket task ls --milestones` — доска майлстонов: что свободно, что за кем.
- `rocket task take <id>` — взять свободный. Занятый другим вернёт `409` с именем
  держателя: это не повод повторять, это чужая работа. Обычную проектную задачу
  так брать нельзя — `403`, её запускает человек через `task start`.
- `rocket task log <id> --kind decision|problem|note "<что сделал>"` и
  `rocket task doc put <id> --kind doc --title "…" --file <path>` — **это и есть
  твоя отчётность.** Всё, что не попало в майлстон, для человека не произошло.
- `rocket task move <id> review` — попросить приёмку. Пока в майлстоне нет ни
  дока, ни записи журнала от тебя, демон откажет (`422`): смотреть было бы не на
  что. `done` и `cancelled` ставит только человек — это его приёмка, не твоя.
- Писать в майлстон может только его держатель (и человек). Взял — веди; не
  брал — не трогай.
- Статус «к сведению», который не требует ответа, — это `--fyi`-тред
  (`rocket task ask <id> "…" --fyi`), а не открытый вопрос, который кто-то потом
  должен закрывать.

Сообщение `[rocket quiet milestone] #N "…" has had no visible activity from you
for …` значит буквально одно: взятый тобой майлстон сутки не показывает следов
работы. Лечится тем, что в нём же и написано, — записью в журнал, документом или
вопросом, если ты заблокирован. Само оно не погаснет, а у человека майлстон всё
это время помечен как «молчит».

## Вопросные треды

Вопрос — это тред с участниками, а не письмо. Ты в нём полноправный участник:
тебя втягивают, назвав в `--to`, или ты вступаешь сам, написав в тред. Каждую
новую запись треда ты получаешь как обычное сообщение — живым в сессию, мёртвым
в инбокс: `[#12/Q3 reply from billing-v2-orch] ...` (тред задачи) или
`[<твой-id>/Q2 reply from human] ...` (твой собственный тред роли).

**Ссылка в рамке — это ровно то, что ты набираешь в ответ.** `#12/Q3` → отвечать
`rocket task reply 12/Q3 "..."`; `sre/Q2` → `rocket agent reply sre/Q2 "..."`
(изнутри своей сессии хватит `Q2`). Никаких глобальных номеров искать не надо —
и не надо их придумывать по памяти.

**Отвечай В ТРЕДЕ, а не через `rocket send`.** `rocket send` пишет в чужой
терминал мимо треда: ответ не попадёт в историю вопроса, не снимет очередь и не
дойдёт до остальных участников. Для вопроса это неверный инструмент.

- `rocket questions --waiting-on <твой-id>` — ЕДИНЫЙ список всех тредов, задач и
  ролей, которые ждут именно тебя: локальная ссылка, к чему привязан, вопрос,
  варианты, участники и сколько тред висит. Начинай с него, а не с обхода задач
  циклом — на этом обходе треды и терялись. `rocket questions` без фильтра —
  всё открытое, `--all` — вместе с закрытыми и fyi.
- `rocket task reply <тред> "<текст>" [--to a,b]` — реплика в тред задачи;
  `rocket agent reply <тред> "..."` — в свой тред роли. `--to` передаёт ход
  названным; без `--to` ты просто выходишь из очереди, а остальные, кого ждут,
  в ней остаются.
- `rocket task close <тред> "<резолюция>"` — **закрывает тред** (`--choose <n>` —
  закрыть выбором предложенного варианта, `--dismiss ["<почему>"]` — как
  неактуальный). Тебе это можно: права постоянного агента здесь равны
  человеческим, оркестратор и воркер так не умеют. Твоё закрытие — это ОТВЕТ,
  по которому спросивший оркестратор будет действовать; не оставляй его ждать
  человека, когда решение в твоей зоне ответственности. Не уверен — не закрывай,
  а спроси `reply`. (`rocket task answer` — прежнее имя `close`, всё ещё
  работает.)
- `--file <path>` есть у `ask`, `reply`, `close` и `task log` — длинный markdown
  отправляй только так.
- `--dry-run` у `reply`/`close` печатает, куда уйдёт запись, и ничего не
  отправляет. `reply`/`close` и без него печатают эхо цели
  (`→ 12/Q3 «...» (task #12 "...")`) — прочитай его и убедись, что попал туда,
  куда хотел.
- Пишешь в тред, участником которого ты не значишься, — получишь
  `not_a_participant` с составом треда. Это НЕ повод повторять вслепую: сначала
  посмотри на эхо в ошибке, и только если тред действительно твой, повтори с
  `--join`.
- `rocket agent ask <твой-id> "<вопрос>"` / `rocket task ask <id> "..." --to <кто>` —
  задать вопрос, когда без чужого решения не продвинуться. Предложи варианты
  (`--option "A" --option "B"`) — человеку дешевле нажать кнопку, чем написать
  ответ. Обязательно `--brief`: тот же вопрос простыми словами для того, кто не
  читал твою сессию — проблема, чем отличаются варианты, что рекомендуешь; без
  имён файлов и кода, детали — в теле. Если это не вопрос, а статус «к сведению» — `--fyi`: такой тред
  создаётся закрытым, никого не ждёт и не копится в чьей-то очереди.

Тред, который не двигается сутки, присылает тебе напоминание
`[rocket stale thread] ...` с готовыми командами. Это значит буквально одно:
ответь `reply` или закрой `close`. Молча игнорировать — то, из-за чего у людей
копятся «21 висящий вопрос».

Никогда не запускай rocket-команды с `unset ROCKET_SESSION_ID` и не выдавай
себя за человека: это ломает авторство в треде, и никакой нужды в этом больше
нет — всё, что тебе нужно, разрешено под твоим собственным id.

Сообщение `[rocket] You have N unread messages` — это сигнал разобрать инбокс.
```

### Дашборд и мобильное приложение

Экран Agents (web, `/agents`) и таб Agents (mobile) показывают **всех** агентов,
включая зарегистрированных без проекта: web группирует по проектам с секцией
«No project», mobile фильтрует чипсами All / проект / No project. Список — id,
description, живость сессии, непрочитанные, открытые вопросы; карточка — инбокс,
Q&A-треды, поле «отправить сообщение» и терминал к живой сессии, плюс кнопка
Start, когда сессии нет. Вкладок Dossier/Prompt/Memory/Runs больше нет.

С карточки в списке доступны те же действия, что у сессий задач: терминал
(`/term/<id>`, только web), чат (`/chat/<id>`) и копирование
`rocket agent attach <id>`. Чат с агентом открывается и при мёртвой сессии — то,
что вы отправите, уходит обычным путём «живой-или-инбокс» (`POST /v1/messages`,
см. выше), поэтому композер не блокируется; в mobile это тот же экран чата с
параметром `?agent=1`.
