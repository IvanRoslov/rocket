# Архитектура

## Общая схема

```
┌─────────────┐   Unix socket (HTTP+JSON)   ┌──────────────────────────────┐
│ rocket CLI  │ ───────────────────────────▶│           rocketd            │
└─────────────┘                             │                              │
┌─────────────┐   localhost TCP (HTTP+SSE)  │  api ── session manager      │
│  dashboard  │ ───────────────────────────▶│         │    │     │         │
└─────────────┘                             │   runtime  workspace  agents │
                                            │   (tmux)   (worktree) (CC,   │
агент в tmux ──── `rocket send/...` ───────▶│                        codex)│
                                            │  monitor  queue  heartbeat   │
                                            │  github   events  store(SQLite)
                                            └──────────────────────────────┘
```

Один Go-модуль, один бинарник. `rocket daemon run` — точка входа демона; все остальные команды — клиенты. Если сокет не отвечает, CLI автозапускает демон (fork + detach) и ждёт готовности.

## Компоненты демона

### api
HTTP+JSON. Два листенера с одним роутером: Unix-сокет `~/.rocket/rocket.sock` (mode 0600) и `127.0.0.1:<port>` для дашборда. SSE-эндпоинт `/v1/events/stream`. Никакой аутентификации в v1 — доступ ограничен правами на сокет и localhost.

### store
SQLite (`~/.rocket/rocket.db`, драйвер без cgo — `modernc.org/sqlite`). Единственный писатель — демон. Таблицы: `sessions`, `tasks`, `task_docs`, `task_log`, `messages`, `events`. Схема — [05-state.md](05-state.md). Миграции — embedded SQL, применяются при старте.

### session manager
Оркестрирует спавн/kill/restore: резервирует имя, создаёт worktree (workspace), собирает команду запуска (agents), создаёт tmux-сессию (runtime), пишет запись в store, публикует события. Все переходы состояний — только через него.

### runtime (tmux)
Интерфейс:

```go
type Runtime interface {
    Create(ctx, CreateSpec) (Handle, error) // new-session -d -s <name> -c <dir> -e K=V... <cmd>
    Inject(ctx, Handle, text string) error  // paste-buffer + адаптивный Enter
    Capture(ctx, Handle, lines int) (string, error)
    Alive(ctx, Handle) bool
    Destroy(ctx, Handle) error
    AttachCommand(Handle) []string          // ["tmux","attach","-t","=name"]
}
```

Правила, унаследованные из AO (проверенные боем):
- таргеты только exact-match: `=name` / `=name:` — иначе tmux префиксно матчит и `app-8` попадает в `app-81`;
- после команды агента — keep-alive shell (`exec $SHELL -i`), чтобы pane переживал выход агента;
- инжекция: `send-keys C-u` → `load-buffer`/`paste-buffer -d` через temp-файл 0600 → адаптивный submit (поллинг `capture-pane`, ретраи Enter, пока черновик не ушёл);
- длинные launch-команды — через временный launch-скрипт;
- всё через `exec.Command` без shell-интерполяции; валидация имён `^[a-z0-9-]+$`.

### workspace (git worktree)
Интерфейс `Workspace`: `Create`, `Restore`, `Destroy`. Раскладка: `~/.rocket/worktrees/<project>/<session>/`.

- Create: `git fetch origin` → база `origin/<default_branch>` (фолбэк на локальную) → `git worktree add -b <branch> <path> <base>`; затем symlinks и `post_create` из конфига проекта.
- Коллизия ветки: если ветка уже существует — переиспользовать (`worktree add` без `-b`), событие `workspace.branch_collision`.
- Destroy: `git worktree remove --force`; **ветку не удаляем никогда**. Фолбэк — `rm -rf` + `git worktree prune`.
- Restore (после ребута/рестарта демона): `worktree prune` → `fetch` → переприкрепить существующую ветку без потери коммитов.

### agents
Интерфейс `Agent` и адаптеры `claude-code`, `codex` — см. [10-agents.md](10-agents.md).

### monitor
Цикл (каждые ~5s) опрашивает активность каждой живой сессии через её агент-адаптер, обновляет `activity` в store, публикует события при смене. См. [07-activity.md](07-activity.md).

### queue
Доставка сообщений: пер-получатель FIFO, доставка при `activity ∈ {ready, idle, waiting_input}` через `runtime.Inject`. См. [06-messaging.md](06-messaging.md).

### heartbeat
Цикл (каждые ~5m): для каждого живого оркестратора с задачей в `in_progress` собирает сводку по его воркерам; если есть застрявшие — кладёт сводку оркестратору в очередь. См. [08-orchestrators.md](08-orchestrators.md).

### tasks
Канбан-слой: CRUD задач, автосоздание подзадач при спавне воркеров, автопереходы статусов по PR-циклу, документы и журнал. См. [12-tasks.md](12-tasks.md).

### github
Цикл (каждые ~2m) поллит `gh` по PR воркеров: `pr_state`, `ci_state`; реакции (сообщение воркеру о красном CI) и авто-cleanup после merge. См. [09-github.md](09-github.md).

### events
Внутренняя шина (Go-каналы) + append в таблицу `events` + fan-out в SSE-подписчиков.

## Потоки данных (примеры)

**`rocket up "billing v2"`**: CLI → POST /v1/orchestrators → session manager: slug, worktree в хабе, launch-команда агента с kickoff, tmux create → запись в store → ответ CLI с `session_id`.

**Оркестратор спавнит воркера**: агент выполняет `rocket spawn --task backend --project api --prompt "..."` в своём терминале → CLI (env `ROCKET_SESSION_ID` определяет вызывающего) → POST /v1/workers → проверка: вызывающий — оркестратор, целевой проект ∈ {хаб, links} → спавн с `parent_id`.

**Воркер спрашивает оркестратора**: `rocket send billing-v2-orch "вопрос"` → POST /v1/messages → очередь → монитор видит, что оркестратор idle → инжекция `[from billing-v2-backend] вопрос`.

**Merge PR**: github-поллер видит `merged` → grace-период → kill tmux, destroy worktree, `state=done` → событие → heartbeat при следующем тике сообщит оркестратору.

## Восстановление после сбоев

- Демон при старте: сверяет store с реальностью (`tmux ls`, живость процессов, наличие worktree), помечает мёртвое, публикует события. Сессии не перезапускает автоматически — `rocket restore <session>` делает это явно.
- PID-файл `~/.rocket/rocketd.pid` + проверка живости; повторный запуск демона невозможен (bind сокета — естественный лок).
- Все записи в store транзакционны; журнал событий позволяет восстановить хронологию.
