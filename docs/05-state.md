# Состояние, конфиг, файловая раскладка

## Файловая раскладка

```
~/.rocket/
├── config.yaml        # реестр проектов и настройки (редактируется руками и CLI)
├── rocket.sock        # Unix-сокет API (создаёт демон, mode 0600)
├── rocketd.pid
├── rocket.db          # SQLite: sessions, messages, events
├── logs/rocketd.log   # ротация по размеру
└── worktrees/
    └── <project>/<session>/
```

Принцип: **config.yaml — декларативный ввод человека; rocket.db — рабочее состояние демона.** CLI никогда не пишет в rocket.db напрямую.

## config.yaml

```yaml
daemon:
  port: 4477              # localhost-порт для дашборда
  heartbeat_interval: 5m
  github_poll_interval: 2m

defaults:
  agent: claude-code      # агент по умолчанию

projects:
  api:
    path: ~/projects/api
    default_branch: main
    links: [web, infra]   # где оркестраторы api могут спавнить воркеров
    auto_cleanup: true    # авто-зачистка воркеров после merge PR (по умолчанию true)
    env:
      FOO: bar
    symlinks: [node_modules, .env]
    post_create:
      - pnpm install
  web:
    path: ~/projects/web
    default_branch: main
```

Демон перечитывает конфиг по SIGHUP и при изменении через API (`project add` и т.п. пишут файл через демон, с сохранением комментариев не заморачиваемся — v1 перезаписывает).

## Схема SQLite

```sql
CREATE TABLE sessions (
  id            TEXT PRIMARY KEY,
  kind          TEXT NOT NULL CHECK (kind IN ('orchestrator','worker')),
  project_id    TEXT NOT NULL,
  feature_slug  TEXT NOT NULL,
  parent_id     TEXT REFERENCES sessions(id),
  agent         TEXT NOT NULL,
  branch        TEXT NOT NULL,
  worktree_path TEXT NOT NULL,
  tmux_name     TEXT NOT NULL,
  state         TEXT NOT NULL,             -- spawning|running|done|killed|errored
  activity      TEXT,                      -- active|ready|idle|waiting_input|blocked|exited
  activity_ts   INTEGER,
  pr_number     INTEGER,
  pr_state      TEXT,                      -- open|merged|closed
  ci_state      TEXT,                      -- pending|passing|failing
  prompt        TEXT,                      -- исходный бриф
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

CREATE TABLE messages (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  from_session TEXT,                       -- NULL = человек/система
  to_session   TEXT NOT NULL,
  body         TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'queued',  -- queued|delivering|delivered|failed
  attempts     INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  delivered_at INTEGER
);

CREATE TABLE tasks (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id    INTEGER REFERENCES tasks(id),   -- NULL = задача, иначе подзадача
  title        TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  project_id   TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'backlog', -- backlog|in_progress|review|done|cancelled
  feature_slug TEXT,
  session_id   TEXT REFERENCES sessions(id),   -- задача → оркестратор, подзадача → воркер
  created_by   TEXT NOT NULL DEFAULT 'user',   -- user|orchestrator
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  completed_at INTEGER
);

CREATE TABLE task_docs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id    INTEGER NOT NULL REFERENCES tasks(id),
  kind       TEXT NOT NULL,                    -- spec|plan|report|doc
  title      TEXT NOT NULL,
  body       TEXT NOT NULL,                    -- markdown
  version    INTEGER NOT NULL DEFAULT 1,      -- put того же (task,kind,title) — новая версия
  author     TEXT,                             -- session id или NULL (пользователь)
  created_at INTEGER NOT NULL
);

CREATE TABLE task_log (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id    INTEGER NOT NULL REFERENCES tasks(id),
  kind       TEXT NOT NULL,                    -- decision|problem|note|status
  body       TEXT NOT NULL,
  author     TEXT,                             -- session id или NULL
  created_at INTEGER NOT NULL
);

CREATE TABLE events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  ts         INTEGER NOT NULL,
  type       TEXT NOT NULL,
  session_id TEXT,
  data       TEXT NOT NULL DEFAULT '{}'    -- JSON
);

CREATE INDEX idx_tasks_status ON tasks(status, parent_id);
CREATE INDEX idx_task_docs ON task_docs(task_id, kind);
CREATE INDEX idx_task_log ON task_log(task_id, id);
CREATE INDEX idx_sessions_state ON sessions(state);
CREATE INDEX idx_sessions_feature ON sessions(feature_slug);
CREATE INDEX idx_messages_to ON messages(to_session, status);
CREATE INDEX idx_events_session ON events(session_id, id);
```

Драйвер: `modernc.org/sqlite` (без cgo, кросс-компиляция сохраняется). Режим WAL. Ретенция: events и messages чистятся фоновой задачей (по умолчанию 30 дней).

## Env-переменные сессий

При создании tmux-сессии демон прокидывает:

```
ROCKET_SESSION_ID, ROCKET_KIND, ROCKET_PARENT_ID, ROCKET_PROJECT_ID,
ROCKET_FEATURE, ROCKET_SOCKET (путь к сокету), + env проекта из конфига
```
