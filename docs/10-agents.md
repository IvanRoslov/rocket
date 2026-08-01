# Агенты

## Интерфейс

Внутренний Go-интерфейс (не плагинная система — адаптеры компилируются в бинарник):

```go
type Agent interface {
    Name() string                          // "claude-code", "codex"
    Available() error                      // бинарник найден, версия ок (для doctor)
    LaunchCommand(spec LaunchSpec) []string
    Env(spec LaunchSpec) map[string]string
    SetupWorkspace(spec LaunchSpec) error  // hooks/настройки в worktree до старта
    Activity(ctx, SessionRef) (ActivityState, error) // агент-специфичная часть каскада
}

type LaunchSpec struct {
    SessionID, Kind, ParentID, ProjectID, RepoID, Feature string
    WorktreePath, SystemPrompt, FirstMessage      string
    Model, PermissionMode                          string
}
```

Выбор агента: `--agent` при spawn/up → `defaults.agent` конфига. Общая часть детекции (жив ли процесс на TTY pane) — в мониторе, не в адаптерах.

## Адаптер claude-code (основной)

- **Launch:** `claude --dangerously-skip-permissions --append-system-prompt "$(cat <prompt-file>)" [-–model X] -- "<first message>"` — позиционный аргумент авто-сабмитит первый ход, оставляя интерактивный режим.
- **Env:** `CLAUDECODE=""` (анти-nesting) + стандартные ROCKET_*.
- **SetupWorkspace:** идемпотентный upsert `.claude/settings.json` в worktree — hook-скрипт активности (`SessionStart/Stop/PreToolUse/PostToolUse/Notification/...` → `POST /v1/internal/activity` через `curl --unix-socket`).
- **Superpowers:** промпты rocket ([prompts/](../docs/prompts/)) требуют от агентов навыков плагина Superpowers (brainstorming, writing-plans, TDD, systematic-debugging, verification-before-completion). Для claude-code это предусловие: `rocket doctor` проверяет, что плагин установлен, и предупреждает, если нет. Для агентов без поддержки skills секции про Superpowers из промптов вырезаются адаптером (шаблоны помечают их условным блоком `{{#if skills}}`).
- **Activity:** нативный JSONL-транскрипт `~/.claude/projects/<путь-как-slug>/*.jsonl`: тип последней записи + mtime → active/ready/idle/blocked; push-hooks дают waiting_input.

## Адаптер codex

- **Launch:** `codex --sandbox danger-full-access [-m X] "<first message>"`; системный промпт — через `AGENTS.md`-механику или флаг (уточнить при реализации по актуальной версии codex CLI).
- **Activity:** сессионные JSONL codex в `~/.codex/sessions/` (аналогичный принцип: mtime + последняя запись); минимум — процесс жив/мёртв + пороги по mtime.
- Уточнение деталей — задача фазы 5; интерфейс рассчитан на то, что адаптер знает только свои источники.

## Добавление нового агента

Реализовать интерфейс + зарегистрировать в реестре адаптеров. Требования к кандидату: интерактивный TUI, переживающий paste-ввод; какой-либо наблюдаемый след активности (файлы сессий, hooks); неинтерактивный режим прав (авто-подтверждение инструментов), иначе воркер без человека застрянет.

## Роли (постоянные агенты)

Не путать с адаптерами выше: **роль** — это зарегистрированный «дежурный» агент («SRE платформы», «разборщик issues»), к которому обращаются люди и другие агенты. Роль постоянна, процесс — нет: durable живут только реестр, инбокс, досье и файловая память, а сессия поднимается по событию, отрабатывает и умирает. Полная спека — в доках задачи #639.

Слой core (эта часть уже в демоне):

- **Реестр** (`agents`): id роли (`[a-z0-9-]`, он же адрес в очереди), проект, путь к роль-промпту, подписки на GitHub, cron, underlying-агент (адаптер выше), enabled.
- **Инбокс** (`agent_inbox`): очередь событий роли (`message`, `issue_opened`, `issue_comment`, `task_update`, `snooze_expired`, `cron`, `question`, `terminal_opened`) со статусами `queued|delivered|done`.
- **Досье** (`agent_items`): что роль ведёт и в каком состоянии — issue/задача/пинг, `external_ref`, состояние, заметка, связанная задача, `snooze_until`.
- **Домашняя директория роли**: `~/.rocket/agents/<id>/role.md` (роль-промпт с политикой триажа, читается на каждом пробуждении — правки не требуют перезапуска) и `~/.rocket/agents/<id>/memory/MEMORY.md` (индекс файловой памяти).
- **Инстанс роли** — сессия `kind=agent` с id `<role>-run-<n>`. Связь запуска с ролью — только через это имя (отдельной колонки нет): по нему демон проверяет, что `rocket agent state set` пишет в своё досье.

Пробуждение (`POST /v1/agents/{id}/wake`, `rocket agent wake`) на этом слое **только кладёт событие в инбокс**; движок пробуждений, брифинг и жизненный цикл инстансов — следующий слой (runtime).
