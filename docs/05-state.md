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
- **Клон из GitHub** (UI / `POST {github: "owner/name"}`): демон делает `git clone` в `~/.rocket/repos/<owner>__<name>/` (двойное подчёркивание исключает коллизии одинаковых имён у разных owner'ов) и записывает этот путь в `repos.path`. Такой чекаут — служебный: пользователь в нём не работает, демон делает `git fetch` перед созданием worktree и держит его свежим фоново (см. ниже).

### Свежесть зеркал

Служебный клон в `~/.rocket/repos/` — **разделяемое зеркало**: из него растут worktree всех сессий, и агенты читают по этому пути файлы чужих репозиториев. Одного `git fetch` для этого мало: fetch двигает только remote-tracking ref'ы, а рабочее дерево зеркала остаётся на том коммите, на котором его склонировали. Читающий файл по пути получает контент недельной давности при совершенно свежем `origin/main` — источник четырёх подряд неверных выводов агентов (задача #795).

Поэтому демон синхронизирует зеркала фоново, по тикеру (`mirror_sync_interval`, по умолчанию 5m), а не только при спавне:

- `git fetch --prune origin` — remote-tracking ref'ы и удаление исчезнувших веток;
- подтягивание рабочего дерева до `origin/<default_branch>` строго `merge --ff-only`.

Fast-forward выполняется только при трёх условиях: дерево чистое, HEAD на default-ветке, FF возможен. Любое отклонение **не перетирается молча** — демон пишет warn в лог, помечает зеркало `Blocked` в записи свежести и `rocket status` печатает по нему отдельную строку ПРОТУХЛО (см. [04-cli.md](04-cli.md)). Локальных изменений в зеркале быть не должно, но если они есть — они переживут синхронизацию.

Свежесть зеркала (отставание в коммитах, время последнего fetch, признак `Blocked`) — производная величина: она вычисляется из самого git-репозитория, в rocket.db не хранится.

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
mirror_sync_interval: 5m  # как часто демон синхронизирует зеркала в repos_dir (0 — выключить)
attachments_dir: ~/.rocket/attachments  # куда сохранять вложения (POST /v1/attachments)
agent_notify_interval: 5m # не чаще этого агенту повторно сообщают о непрочитанных
input_stall_threshold: 10m # сколько сессия может ждать интерактивного ввода, прежде чем её пометят waiting_terminal
question_stale_after: 24h # сколько открытый decision-тред может висеть без движения до напоминания участникам attention
milestone_quiet_after: 24h # сколько взятый майлстон может не показывать следов работы агента до напоминания ему (и флага quiet)
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
  kind          TEXT NOT NULL CHECK (kind IN ('orchestrator','worker','agent')),  -- agent = сессия постоянного агента
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
  status       TEXT NOT NULL DEFAULT 'backlog', -- backlog|brainstorm|in_progress|review|done|cancelled
  feature_slug TEXT,
  session_id   TEXT REFERENCES sessions(id),   -- задача → оркестратор, подзадача → воркер
  milestone    INTEGER NOT NULL DEFAULT 0,     -- 1 = майлстон: корневая задача вне проектов (project_id = '')
  assigned_role TEXT,                          -- id постоянного агента, взявшего майлстон; NULL/'' — не взят
  created_by   TEXT NOT NULL DEFAULT 'user',   -- user|orchestrator|agent
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  completed_at INTEGER
);
-- Майлстон (миграция 0012) представлен явной колонкой, а не выводится из «project_id
-- пуст»: project_id объявлен NOT NULL ещё в 0001, у майлстона он '' (та же конвенция,
-- что у agents.project_id), а '' от NULL здесь не отличить. Ровно одно из session_id
-- (оркестратор фичи) и assigned_role (агент майлстона) может быть непустым.

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

CREATE TABLE questions (                     -- тред задачи ИЛИ роли (миграции 0009-0011)
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id      INTEGER REFERENCES tasks(id),  -- NULL — тред не привязан к задаче
  role_id      TEXT REFERENCES agents(id),    -- NULL — тред не привязан к роли
  asked_by     TEXT NOT NULL DEFAULT '',      -- session id автора; '' = человек
  body         TEXT NOT NULL,                 -- исходный вопрос
  context      TEXT,                          -- опциональный markdown-контекст
  status       TEXT NOT NULL DEFAULT 'open',  -- open|resolved
  resolution   TEXT,                          -- answered|dismissed|fyi (когда resolved)
  addressed_to TEXT NOT NULL DEFAULT '',      -- CSV адресатов самого вопроса
  type         TEXT NOT NULL DEFAULT 'decision', -- decision|fyi
  options      TEXT NOT NULL DEFAULT '',      -- JSON-массив вариантов ответа; '' — вариантов нет
  asked_at     INTEGER NOT NULL,
  resolved_at  INTEGER
);

CREATE TABLE question_messages (             -- записи треда
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  question_id  INTEGER NOT NULL REFERENCES questions(id),
  author       TEXT NOT NULL DEFAULT 'human', -- id участника; 'human' = человек
  kind         TEXT NOT NULL DEFAULT 'reply', -- reply|answer
  body         TEXT NOT NULL,
  addressed_to TEXT NOT NULL DEFAULT '',      -- CSV участников; '' = всем, кроме автора
  created_at   INTEGER NOT NULL
);

CREATE TABLE question_participants (         -- кто в треде
  question_id    INTEGER NOT NULL REFERENCES questions(id),
  participant_id TEXT NOT NULL,              -- 'human' | id агента | session id
  added_at       INTEGER NOT NULL,
  UNIQUE (question_id, participant_id)
);

CREATE TABLE question_attention (            -- чей ход: ХРАНИМОЕ множество, не производное
  question_id    INTEGER NOT NULL REFERENCES questions(id),
  participant_id TEXT NOT NULL,
  added_at       INTEGER NOT NULL,
  UNIQUE (question_id, participant_id)
);
-- Правила ведения attention (открытие/запись/закрытие) — internal/store/attention.go
-- и docs/12-tasks.md. Флаг «тред завис» (stale) в базе не хранится: он считается
-- на каждом чтении из времени последней записи и question_stale_after.

CREATE TABLE attachments (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  mime       TEXT NOT NULL,             -- image/png|image/jpeg|image/webp
  size       INTEGER NOT NULL,          -- байт
  created_at INTEGER NOT NULL
);                                       -- файл лежит на диске: <attachments_dir>/<id>.<ext>

CREATE TABLE agents (                          -- постоянный агент
  id          TEXT PRIMARY KEY,              -- "sre", [a-z0-9-]; оно же имя tmux-сессии
  description TEXT NOT NULL DEFAULT '',
  project_id  TEXT NOT NULL DEFAULT '',      -- только группировка в UI; '' — вне проекта
  dir         TEXT NOT NULL DEFAULT '',      -- рабочая директория для agent start
  command     TEXT NOT NULL DEFAULT '',      -- команда запуска; '' — интерактивный shell
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

CREATE TABLE agent_inbox (                     -- сообщения, ждущие разбора агентом
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id   TEXT NOT NULL REFERENCES agents(id),
  from_id    TEXT NOT NULL DEFAULT '',        -- id сессии отправителя; '' — человек/UI
  body       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'unread',  -- unread|read
  created_at INTEGER NOT NULL,
  read_at    INTEGER
);

CREATE TABLE events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  ts         INTEGER NOT NULL,
  type       TEXT NOT NULL,
  session_id TEXT,
  data       TEXT NOT NULL DEFAULT '{}'    -- JSON
);

CREATE INDEX idx_tasks_status ON tasks(status, parent_id);
CREATE INDEX idx_tasks_milestone ON tasks(milestone, assigned_role);
CREATE INDEX idx_task_docs ON task_docs(task_id, kind);
CREATE INDEX idx_task_log ON task_log(task_id, id);
CREATE INDEX idx_questions_task ON questions(task_id, status);
CREATE INDEX idx_questions_role ON questions(role_id, status);
CREATE INDEX idx_question_messages ON question_messages(question_id, id);
CREATE INDEX idx_question_participants_participant ON question_participants(participant_id, question_id);
CREATE INDEX idx_question_attention_participant ON question_attention(participant_id, question_id);
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
