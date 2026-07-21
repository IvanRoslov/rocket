# Состояние, конфиг, файловая раскладка

## Файловая раскладка

```
~/.rocket/
├── config.yaml        # опциональные настройки демона (только настройки!)
├── rocket.sock        # Unix-сокет API (создаёт демон, mode 0600)
├── rocketd.pid
├── rocket.db          # SQLite: всё состояние, включая реестр репо и проектов
├── logs/rocketd.log   # ротация по размеру
├── repos/             # чекауты, склонированные самим rocket'ом
│   └── <owner>__<name>/
├── worktrees/
│   └── <repo-id>/<session-id>/
└── attachments/        # картинки, вставленные Ctrl+V в дашборде
    └── <id>.<ext>
```

Принцип: **вся модель данных — в rocket.db, единственный писатель — демон.** Репозитории и проекты тоже живут в базе и управляются через CLI/API/дашборд, а не редактированием файлов. config.yaml — только настройки демона, опционален (без него — дефолты).

## Куда rocket клонирует репо и кладёт worktree

Два источника репозиториев — две судьбы `path`:

- **Локальный чекаут** (`rocket repo add <path>` / `POST {path}`): rocket ничего не копирует, `repos.path` указывает на существующую рабочую копию пользователя. rocket в ней ничего не меняет — она нужна только как источник для `git worktree add` и symlinks.
- **Клон из GitHub** (UI / `POST {github: "owner/name"}`): демон делает `git clone` в `~/.rocket/repos/<owner>__<name>/` (двойное подчёркивание исключает коллизии одинаковых имён у разных owner'ов) и записывает этот путь в `repos.path`. Такой чекаут — служебный: пользователь в нём не работает, демон обновляет его `git fetch` перед созданием worktree.

**Worktree** всегда создаются демоном в `~/.rocket/worktrees/<repo-id>/<session-id>/` — плоско, предсказуемо, вне пользовательских директорий; cleanup сессии удаляет ровно свою папку. Путь виден в карточке сессии (API/`task show`/дашборд).

Обе базовые директории переопределяются в config.yaml (`repos_dir`, `worktrees_dir`) — например, чтобы вынести worktree на быстрый диск.

## Куда rocket кладёт вложения (скриншоты из дашборда)

`POST /v1/attachments` (см. [03-daemon-api.md](03-daemon-api.md)) сохраняет присланные байты под `<attachments_dir>/<id>.<ext>` — `id` это `attachments.id`, `ext` выводится из MIME (`image/png`→`.png`, `image/jpeg`→`.jpg`, `image/webp`→`.webp`). Директория переопределяется в config.yaml (`attachments_dir`, по умолчанию `~/.rocket/attachments`).

## config.yaml (опционально)

```yaml
port: 4477                # localhost-порт для дашборда
heartbeat_interval: 5m
github_poll_interval: 2m
default_agent: claude-code
repos_dir: ~/.rocket/repos          # куда клонировать репо из GitHub
worktrees_dir: ~/.rocket/worktrees  # где создавать worktree сессий
attachments_dir: ~/.rocket/attachments  # куда сохранять вложения (POST /v1/attachments)
```

## Схема SQLite

```sql
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,   -- напр. 'github_token'
  value TEXT NOT NULL
);

CREATE TABLE repos (
  id             TEXT PRIMARY KEY,          -- "api", "web"
  path           TEXT NOT NULL,             -- абсолютный путь к основному чекауту
  default_branch TEXT NOT NULL DEFAULT 'main',
  auto_cleanup   INTEGER NOT NULL DEFAULT 1,
  env            TEXT NOT NULL DEFAULT '{}', -- JSON {K:V}
  symlinks       TEXT NOT NULL DEFAULT '[]', -- JSON [paths]
  post_create    TEXT NOT NULL DEFAULT '[]', -- JSON [commands]
  created_at     INTEGER NOT NULL
);

CREATE TABLE projects (
  id           TEXT PRIMARY KEY,            -- "billing"
  name         TEXT NOT NULL,               -- "Биллинг"
  main_repo    TEXT NOT NULL REFERENCES repos(id),
  linked_repos TEXT NOT NULL DEFAULT '[]',  -- JSON [repo ids]
  created_at   INTEGER NOT NULL
);
```

```sql
CREATE TABLE sessions (  -- продолжение схемы
  id            TEXT PRIMARY KEY,
  kind          TEXT NOT NULL CHECK (kind IN ('orchestrator','worker')),
  project_id    TEXT NOT NULL,             -- проект (верхняя сущность)
  repo_id       TEXT NOT NULL,             -- репозиторий, где worktree
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
  project_id   TEXT NOT NULL,                  -- проект; у подзадачи дополнительно
  repo_id      TEXT,                            -- репо воркера (только подзадачи)
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

CREATE TABLE task_questions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id     INTEGER NOT NULL REFERENCES tasks(id),
  asked_by    TEXT NOT NULL,                 -- session id оркестратора
  body        TEXT NOT NULL,                 -- исходный вопрос
  context     TEXT,                          -- опциональный markdown-контекст
  status      TEXT NOT NULL DEFAULT 'open',  -- open|resolved
  resolution  TEXT,                          -- answered|dismissed (когда resolved)
  asked_at    INTEGER NOT NULL,
  resolved_at INTEGER
);

CREATE TABLE question_messages (             -- тред вопроса
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  question_id INTEGER NOT NULL REFERENCES task_questions(id),
  author      TEXT,                          -- session id оркестратора; NULL = пользователь
  kind        TEXT NOT NULL DEFAULT 'reply', -- reply|answer
  body        TEXT NOT NULL,
  created_at  INTEGER NOT NULL
);
-- чья очередь отвечать — производное от автора последней записи треда

CREATE TABLE attachments (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  mime       TEXT NOT NULL,             -- image/png|image/jpeg|image/webp
  size       INTEGER NOT NULL,          -- байт
  created_at INTEGER NOT NULL
);                                       -- файл лежит на диске: <attachments_dir>/<id>.<ext>

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
CREATE INDEX idx_task_questions ON task_questions(task_id, status);
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
ROCKET_REPO_ID, ROCKET_FEATURE, ROCKET_SOCKET (путь к сокету),
+ env репозитория из конфига
```
