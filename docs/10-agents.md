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
- **Activity:** нативный JSONL-транскрипт `~/.claude/projects/<путь-как-slug>/*.jsonl`: тип последней записи + mtime → active/ready/idle/blocked; push-hooks дают waiting_input.

## Адаптер codex

- **Launch:** `codex --sandbox danger-full-access [-m X] "<first message>"`; системный промпт — через `AGENTS.md`-механику или флаг (уточнить при реализации по актуальной версии codex CLI).
- **Activity:** сессионные JSONL codex в `~/.codex/sessions/` (аналогичный принцип: mtime + последняя запись); минимум — процесс жив/мёртв + пороги по mtime.
- Уточнение деталей — задача фазы 5; интерфейс рассчитан на то, что адаптер знает только свои источники.

## Добавление нового агента

Реализовать интерфейс + зарегистрировать в реестре адаптеров. Требования к кандидату: интерактивный TUI, переживающий paste-ввод; какой-либо наблюдаемый след активности (файлы сессий, hooks); неинтерактивный режим прав (авто-подтверждение инструментов), иначе воркер без человека застрянет.
