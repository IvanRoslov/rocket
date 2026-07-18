# Фаза 1 «Скелет: демон, tmux, worktree» — план имплементации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `rocket` умеет поднять Claude Code в изолированном git worktree через tmux и управлять сессиями руками: `repo add → project create → spawn → attach → kill --cleanup`; демон переживает рестарт (реконсиляция).

**Architecture:** Один Go-модуль, один бинарник. `rocket daemon run` — демон (HTTP+JSON на Unix-сокете `~/.rocket/rocket.sock` и `127.0.0.1:4477`); все остальные команды — тонкие клиенты с автозапуском демона. Состояние — только в SQLite (`~/.rocket/rocket.db`, единственный писатель — демон). Session manager оркестрирует spawn/kill/restore поверх трёх внутренних интерфейсов: Runtime (tmux), Workspace (git worktree), Agent (claude-code).

**Tech Stack:** Go ≥1.24, `github.com/spf13/cobra`, `modernc.org/sqlite` (без cgo), `gopkg.in/yaml.v3`, `gopkg.in/natefinch/lumberjack.v2` (ротация логов), stdlib `net/http`.

## Global Constraints

- Модуль: `github.com/IvanRoslov/rocket`; бинарник `rocket`, точка входа `cmd/rocket/main.go`.
- SQLite-драйвер строго `modernc.org/sqlite` (кросс-компиляция без cgo), режим WAL; миграции — embedded SQL, применяются при старте демона.
- Вся модель данных — в `rocket.db`; `config.yaml` — только настройки демона, опционален.
- Идентификаторы (repo, project, session): `^[a-z0-9-]+$`; tmux-таргеты всегда exact-match (`=name`).
- Все внешние команды — `exec.Command` без shell-интерполяции (кроме явно оговорённых launch-скриптов).
- Worktree: `~/.rocket/worktrees/<repo-id>/<session-id>/`; ветку **никогда не удаляем**.
- Env в сессиях: `ROCKET_SESSION_ID, ROCKET_KIND, ROCKET_PARENT_ID, ROCKET_PROJECT_ID, ROCKET_REPO_ID, ROCKET_FEATURE, ROCKET_SOCKET` + env репозитория.
- Ошибки API: `{"error":{"code":"<machine_code>","message":"..."}}`; коды выхода CLI: 0 успех, 1 ошибка API/валидации, 2 демон недоступен, 3 неверное использование.
- Формат события: `{id, ts, type, session_id?, data{}}`.
- Коммиты частые, по задаче; тесты Go — `go test ./...`; интеграционные тесты tmux/git скипаются, если бинарник недоступен (`testing.Short` не используем — проверяем `exec.LookPath`).

---

### Task 1: Каркас модуля и CLI (cobra)

**Files:**
- Create: `go.mod`, `cmd/rocket/main.go`, `internal/cli/root.go`, `internal/version/version.go`
- Test: `internal/cli/root_test.go`

**Interfaces:**
- Produces: `cli.Execute() int` (код выхода), `cli.NewRootCmd() *cobra.Command`; глобальные флаги `--json`, `--socket`; `version.Version` (var, ldflags-friendly).

- [ ] **Step 1: Инициализировать модуль и зависимости**

```bash
go mod init github.com/IvanRoslov/rocket
go get github.com/spf13/cobra@latest gopkg.in/yaml.v3 modernc.org/sqlite gopkg.in/natefinch/lumberjack.v2
```

- [ ] **Step 2: Написать падающий тест**

```go
// internal/cli/root_test.go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpListsCoreCommands(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"daemon", "--json", "--socket"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing %q", want)
		}
	}
}
```

- [ ] **Step 3: Убедиться, что тест падает** — `go test ./internal/cli/` → ошибка компиляции (нет `NewRootCmd`).

- [ ] **Step 4: Минимальная реализация**

```go
// internal/version/version.go
package version

var Version = "dev"
```

```go
// internal/cli/root.go
package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/IvanRoslov/rocket/internal/version"
)

type globalFlags struct {
	JSON   bool
	Socket string
}

var flags globalFlags

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "rocket",
		Short:         "Оркестрация AI-кодинг-агентов поверх tmux и git worktree",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&flags.JSON, "json", false, "машинный вывод")
	root.PersistentFlags().StringVar(&flags.Socket, "socket", "", "путь к сокету демона")
	root.AddCommand(newDaemonCmd()) // на этом шаге — пустая заглушка `daemon` c подкомандами-заглушками
	return root
}

// Execute возвращает код выхода процесса (0/1/2/3 — см. Global Constraints).
func Execute() int {
	if err := NewRootCmd().Execute(); err != nil {
		os.Stderr.WriteString("rocket: " + err.Error() + "\n")
		return exitCode(err)
	}
	return 0
}
```

`exitCode(err)` в этом же файле: типизированные ошибки `usageError` → 3, `daemonUnavailableError` → 2, остальное → 1 (типы объявить здесь же, наполняются в следующих задачах). `newDaemonCmd()` пока в `root.go` — `&cobra.Command{Use: "daemon"}` без RunE.

```go
// cmd/rocket/main.go
package main

import (
	"os"

	"github.com/IvanRoslov/rocket/internal/cli"
)

func main() { os.Exit(cli.Execute()) }
```

- [ ] **Step 5: Тест зелёный, бинарник собирается** — `go test ./... && go build ./cmd/rocket && ./rocket --help`.

- [ ] **Step 6: Commit** — `git add -A && git commit -m "feat: go module, cobra CLI skeleton"`.

---

### Task 2: Конфиг и файловая раскладка

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:

```go
type Config struct {
	Port              int           `yaml:"port"`               // 4477
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"` // 5m (фаза 3, поле уже есть)
	GithubPollInterval time.Duration `yaml:"github_poll_interval"`
	DefaultAgent      string        `yaml:"default_agent"`      // "claude-code"
	ReposDir          string        `yaml:"repos_dir"`
	WorktreesDir      string        `yaml:"worktrees_dir"`
	Home              string        `yaml:"-"`                  // ~/.rocket (или $ROCKET_HOME)
}
func Load(home string) (*Config, error)      // home=="" → $ROCKET_HOME или ~/.rocket
func (c *Config) SocketPath() string         // <home>/rocket.sock
func (c *Config) DBPath() string             // <home>/rocket.db
func (c *Config) PidPath() string            // <home>/rocketd.pid
func (c *Config) LogPath() string            // <home>/logs/rocketd.log
```

- [ ] **Step 1: Тест** — `Load` из пустой директории даёт дефолты (port 4477, agent claude-code, ReposDir=`<home>/repos`, WorktreesDir=`<home>/worktrees`); `Load` с `config.yaml` (`port: 9999`, `worktrees_dir: /x`) переопределяет; `~` в путях раскрывается. `$ROCKET_HOME` уважается (нужен для всех тестов демона).
- [ ] **Step 2: Тест падает** — `go test ./internal/config/`.
- [ ] **Step 3: Реализация** — yaml.v3, отсутствующий файл — не ошибка; `Load` создаёт `<home>` и `<home>/logs` (0700).
- [ ] **Step 4: Тест зелёный.**
- [ ] **Step 5: Commit** — `feat: config loading and ~/.rocket layout`.

---

### Task 3: Store — SQLite, миграции, DAO

**Files:**
- Create: `internal/store/store.go`, `internal/store/migrations/0001_init.sql`, `internal/store/repos.go`, `internal/store/projects.go`, `internal/store/sessions.go`, `internal/store/events.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces:

```go
func Open(path string) (*Store, error) // WAL, busy_timeout=5000, миграции
func (s *Store) Close() error

type Repo struct{ ID, Path, DefaultBranch string; AutoCleanup bool; Env map[string]string; Symlinks, PostCreate []string; CreatedAt int64 }
func (s *Store) AddRepo(r Repo) error / GetRepo(id) (Repo, error) / ListRepos() ([]Repo, error) / UpdateRepo(r Repo) error / DeleteRepo(id) error

type Project struct{ ID, Name, MainRepo string; LinkedRepos []string; CreatedAt int64 }
func (s *Store) AddProject / GetProject / ListProjects / UpdateProject / DeleteProject
// Repos возвращает main+linked; DeleteRepo возвращает ErrRepoInUse, если репо входит в проект.

type Session struct{ ID, Kind, ProjectID, RepoID, FeatureSlug, ParentID, Agent, Branch, WorktreePath, TmuxName, State, Activity, Prompt string; ActivityTS, CreatedAt, UpdatedAt int64 /* pr-поля фазы 4 читаем/пишем как NULL */ }
func (s *Store) AddSession / GetSession / ListSessions(f SessionFilter) / UpdateSessionState(id, state string) / UpdateSession(sess Session) error
type SessionFilter struct{ Kind, Project, Feature, State string; All bool } // All=false → только spawning|running

type Event struct{ ID int64; TS int64; Type, SessionID string; Data map[string]any }
func (s *Store) AppendEvent(e Event) (int64, error)
func (s *Store) ListEvents(sinceID int64, limit int, sessionID string) ([]Event, error)
```

- [ ] **Step 1: Миграция 0001** — полная схема из `docs/05-state.md` (таблицы `settings, repos, projects, sessions, messages, tasks, task_docs, task_log, task_questions, question_messages, events` + все индексы) — скопировать SQL дословно; таблица `schema_migrations(version INTEGER PRIMARY KEY)`. Embedded через `//go:embed migrations/*.sql`, применение по возрастанию в транзакции.
- [ ] **Step 2: Тесты** (временный файл БД): open → повторный open идемпотентен; CRUD repo с JSON-полями (env/symlinks/post_create round-trip); DeleteRepo при наличии проекта → `ErrRepoInUse`; проект CRUD + link/unlink через UpdateProject; session add/list c фильтрами (живые по умолчанию); AppendEvent/ListEvents (since, limit, session-фильтр).
- [ ] **Step 3: Тесты падают.**
- [ ] **Step 4: Реализация DAO** — plain `database/sql`; JSON-поля сериализуются `encoding/json`; `UpdatedAt` проставляется в Update-методах; ошибки-сентинелы `ErrNotFound`, `ErrRepoInUse`, `ErrExists`.
- [ ] **Step 5: Тесты зелёные.**
- [ ] **Step 6: Commit** — `feat: sqlite store with embedded migrations and DAOs`.

---

### Task 4: Шина событий

**Files:**
- Create: `internal/bus/bus.go`
- Test: `internal/bus/bus_test.go`

**Interfaces:**
- Consumes: `store.AppendEvent`.
- Produces:

```go
type Bus struct{ ... }
func New(st *store.Store) *Bus
func (b *Bus) Publish(typ, sessionID string, data map[string]any) // append в store + fan-out; ошибки store — в лог, не паника
func (b *Bus) Subscribe() (ch <-chan store.Event, cancel func())  // буфер 64; медленный подписчик теряет события (drop), не блокирует
```

- [ ] **Step 1: Тест** — Publish записывает в store и доставляет двум подписчикам; после cancel доставки нет; переполнение буфера не блокирует Publish.
- [ ] **Step 2: Падает → реализация (mutex + map[int]chan) → зелёный.**
- [ ] **Step 3: Commit** — `feat: event bus with store append and fan-out`.

---

### Task 5: HTTP API-каркас + health/shutdown

**Files:**
- Create: `internal/api/server.go`, `internal/api/errors.go`
- Test: `internal/api/server_test.go`

**Interfaces:**
- Produces:

```go
type Deps struct { Store *store.Store; Bus *bus.Bus; Cfg *config.Config; Shutdown func(); StartedAt time.Time; /* далее по задачам: Manager *session.Manager */ }
func NewHandler(d Deps) http.Handler                 // маршрутизация http.ServeMux (Go 1.22 паттерны "GET /v1/health")
func Serve(ctx context.Context, d Deps) error        // два листенера: unix-сокет (0600, удалить stale-файл) + 127.0.0.1:port
func writeErr(w http.ResponseWriter, status int, code, msg string)
func writeJSON(w http.ResponseWriter, status int, v any)
```

- [ ] **Step 1: Тест** — через `httptest.NewServer(NewHandler(...))`: `GET /v1/health` → `{"status":"ok","version":...,"uptime":...}`; неизвестный путь → 404 с `{"error":{...}}`; `POST /v1/shutdown` вызывает Shutdown-колбэк. Отдельный тест: `Serve` на unix-сокете во временной директории отвечает на health через `http.Client` с кастомным `DialContext` («unix», путь), права сокета 0600.
- [ ] **Step 2: Падает → реализация → зелёный.**
- [ ] **Step 3: Commit** — `feat: http api skeleton on unix socket and localhost`.

---

### Task 6: Демон — lifecycle, pid, логи; CLI-клиент с автозапуском

**Files:**
- Create: `internal/daemon/daemon.go`, `internal/client/client.go`, `internal/cli/daemon.go`, `internal/cli/client_helpers.go`
- Modify: `internal/cli/root.go` (реальный `newDaemonCmd`)
- Test: `internal/daemon/daemon_test.go`, `internal/client/client_test.go`

**Interfaces:**
- Produces:

```go
// daemon
func Run(cfg *config.Config) error // foreground: pid-файл (O_EXCL; stale → перезапись после проверки kill 0), lumberjack-лог, store+bus+api, SIGTERM/SIGINT → graceful stop
// client
type Client struct{ ... }
func New(socketPath string) *Client                    // http.Client через unix DialContext
func (c *Client) Get/Post/Patch/Delete(path string, in, out any) error // разворачивает {"error":{...}} в *APIError{Code, Message}
func Connect(cfg *config.Config, autostart bool) (*Client, error)
// Connect: health-пинг; если не отвечает и autostart — exec самого себя `rocket daemon run` (detach: Setsid, stdout/err в лог), поллинг health до 5s; иначе daemonUnavailableError (exit 2)
```
- CLI: `rocket daemon run` (foreground), `start` (Connect с autostart и печать pid), `stop` (POST /v1/shutdown), `status` (health или «not running»).

- [ ] **Step 1: Тесты** — daemon: `Run` в горутине с `$ROCKET_HOME`-времянкой отвечает на health, второй `Run` падает «already running», SIGTERM останавливает; client: Connect с autostart=false на мёртвый сокет → `daemonUnavailableError`; APIError разворачивается.
- [ ] **Step 2: Падают → реализация → зелёные.**
- [ ] **Step 3: Ручная проверка** — `go build ./cmd/rocket && ./rocket daemon start && ./rocket daemon status && ./rocket daemon stop`.
- [ ] **Step 4: Commit** — `feat: daemon lifecycle with pid file and CLI autostart`.

---

### Task 7: Репозитории — API + CLI

**Files:**
- Create: `internal/api/repos.go`, `internal/cli/repo.go`
- Modify: `internal/api/server.go` (маршруты), `internal/cli/root.go`
- Test: `internal/api/repos_test.go`

**Interfaces:**
- Consumes: `store.AddRepo/...`, `client.Client`.
- Produces: REST `GET/POST /v1/repos`, `PATCH/DELETE /v1/repos/{id}`; CLI `rocket repo add <path> [--id]`, `repo ls`, `repo rm <id>`.

- [ ] **Step 1: Тесты API** — POST `{path}` (id из имени директории, нормализация в `[a-z0-9-]`; путь должен существовать и содержать `.git`, иначе 400 `repo_path_invalid`); дубль id → 409 `repo_exists`; невалидный id → 400 `invalid_id`; GET список; PATCH env/symlinks/post_create/default_branch; DELETE занятого проектом → 409 `repo_in_use`. `default_branch` при добавлении определяется `git symbolic-ref refs/remotes/origin/HEAD` (фолбэк `main`).
- [ ] **Step 2: Падают → реализация хендлеров и CLI (`--json` печатает ответ как есть; таблица — text/tabwriter) → зелёные.**
- [ ] **Step 3: Commit** — `feat: repo registry API and CLI`.

---

### Task 8: Проекты — API + CLI

**Files:**
- Create: `internal/api/projects.go`, `internal/cli/project.go`
- Modify: `internal/api/server.go`, `internal/cli/root.go`
- Test: `internal/api/projects_test.go`

**Interfaces:**
- Produces: REST `GET/POST /v1/projects`, `GET/PATCH/DELETE /v1/projects/{id}`; CLI `project create <id> --main <repo> [--link <repo>]... [--name]`, `project ls`, `project show <id>`, `project link|unlink <project> <repo>`, `project rm <id>`. GET-агрегаты фазы 1: количество живых сессий (задач ещё нет — счётчики задач отдаём нулями).

- [ ] **Step 1: Тесты API** — create c несуществующим main → 400 `repo_not_found`; дубль linked отфильтровывается; link/unlink через PATCH; delete при живых сессиях проекта → 409 `project_busy`; GET show содержит main+linked с путями.
- [ ] **Step 2: Падают → реализация → зелёные.**
- [ ] **Step 3: Commit** — `feat: project registry API and CLI`.

---

### Task 9: Runtime tmux

**Files:**
- Create: `internal/runtime/runtime.go`, `internal/runtime/tmux.go`
- Test: `internal/runtime/tmux_test.go` (интеграционный, skip без tmux)

**Interfaces:**
- Produces (сигнатуры фиксированы — их используют session manager и фаза 2):

```go
type Handle struct{ Name string }
type CreateSpec struct{ Name, Dir, Command string; Env map[string]string }
type Runtime interface {
	Create(ctx context.Context, spec CreateSpec) (Handle, error)
	Inject(ctx context.Context, h Handle, text string) error
	Capture(ctx context.Context, h Handle, lines int) (string, error)
	Alive(ctx context.Context, h Handle) bool
	Destroy(ctx context.Context, h Handle) error
	AttachCommand(h Handle) []string // ["tmux","attach","-t","=name"]
	List(ctx context.Context) ([]string, error) // имена живых tmux-сессий (для реконсиляции)
}
func NewTmux() Runtime
```

Правила реализации (из AO, зафиксированы в 02-architecture.md):
- Имя валидируется `^[a-z0-9-]+$`; все таргеты — `=name` / `=name:`.
- Create: длинную команду писать во временный launch-скрипт `<worktree>/.rocket-launch.sh` (0700), содержимое: `#!/bin/sh` + `exec` команды агента + финальный fallback; сама tmux-команда: `tmux new-session -d -s <name> -c <dir> -e K=V ... 'sh .rocket-launch.sh; exec $SHELL -i'` — keep-alive shell после выхода агента.
- Inject: `send-keys -t =name C-u` → буфер через temp-файл 0600 `load-buffer` + `paste-buffer -d -t =name` → адаптивный submit: до 5 попыток `send-keys Enter` с поллингом `capture-pane` (черновик исчез из последней строки → доставлено), паузы 300ms.
- Alive: `tmux has-session -t =name` (exit 0).
- Capture: `tmux capture-pane -p -t =name: -S -<lines>`.

- [ ] **Step 1: Интеграционный тест** (`exec.LookPath("tmux")` иначе `t.Skip`): Create с `Command: "cat"` и env → Alive true; env виден (`capture` после inject `echo $FOO`); Inject текста в `cat` появляется в Capture; Destroy → Alive false; сессия `test-a` не матчится таргетом `test-a1` (создать обе, убить одну, проверить вторую живой).
- [ ] **Step 2: Падает → реализация → зелёный.**
- [ ] **Step 3: Commit** — `feat: tmux runtime with exact-match targets and adaptive inject`.

---

### Task 10: Workspace git worktree

**Files:**
- Create: `internal/workspace/workspace.go`
- Test: `internal/workspace/workspace_test.go`

**Interfaces:**
- Consumes: `store.Repo` (Path, DefaultBranch, Symlinks, PostCreate).
- Produces:

```go
type CreateResult struct{ Path string; BranchCollision bool }
type Workspace interface {
	Create(ctx context.Context, repo store.Repo, sessionID, branch string) (CreateResult, error)
	Restore(ctx context.Context, repo store.Repo, sessionID, branch string) (string, error)
	Destroy(ctx context.Context, repo store.Repo, sessionID string) error
}
func New(worktreesDir string) Workspace // путь: <worktreesDir>/<repo-id>/<session-id>/
```

Поведение (02-architecture.md): Create = `git -C <repo.Path> fetch origin` (ошибка не фатальна: offline-фолбэк) → база `origin/<default_branch>`, фолбэк локальная ветка → `git worktree add -b <branch> <path> <base>`; коллизия ветки → `worktree add <path> <branch>` без `-b`, `BranchCollision=true`; затем symlinks (относительные пути из основного чекаута; отсутствующий источник — пропуск с warning) и `post_create` (каждая команда `sh -c` в worktree, ошибка — фатальна для Create). Destroy = `git worktree remove --force`, фолбэк `os.RemoveAll` + `git worktree prune`; ветка остаётся. Restore = `worktree prune` → `fetch` → если каталога нет, `worktree add <path> <branch>` (существующая ветка, коммиты сохраняются).

- [ ] **Step 1: Тесты** — на временном git-репо (`git init` + коммит; helper в тесте): Create делает каталог и ветку от default_branch; повторный Create той же ветки после Destroy → BranchCollision=true и переиспользование ветки; symlink на `node_modules` создан; post_create (`touch marker`) выполнен; Destroy удаляет каталог, ветка остаётся (`git branch --list`); Restore после ручного `rm -rf` возвращает worktree с той же веткой.
- [ ] **Step 2: Падают → реализация → зелёные.**
- [ ] **Step 3: Commit** — `feat: git worktree workspace with symlinks and post_create`.

---

### Task 11: Интерфейс Agent + адаптер claude-code

**Files:**
- Create: `internal/agent/agent.go`, `internal/agent/claudecode/claudecode.go`, `internal/agent/registry.go`
- Test: `internal/agent/claudecode/claudecode_test.go`

**Interfaces:**
- Produces (сигнатуры из 10-agents.md; Activity — заглушка до фазы 2):

```go
type LaunchSpec struct {
	SessionID, Kind, ParentID, ProjectID, RepoID, Feature string
	WorktreePath, SystemPrompt, FirstMessage, Model, PermissionMode string
	SocketPath string
}
type Agent interface {
	Name() string
	Available() error                       // exec.LookPath + минимальная проверка
	LaunchCommand(spec LaunchSpec) []string
	Env(spec LaunchSpec) map[string]string
	SetupWorkspace(spec LaunchSpec) error   // фаза 1: пишет prompt-файл, hooks не ставит
}
func Registry() map[string]Agent            // {"claude-code": ...}
func Get(name string) (Agent, error)
```

claude-code:
- `SetupWorkspace`: если `SystemPrompt != ""` — записать в `<worktree>/.rocket-prompt.md` (0600).
- `LaunchCommand`: `["claude", "--dangerously-skip-permissions"]` + (`--append-system-prompt "$(cat .rocket-prompt.md)"` реализуем без shell: читаем файл в Go и передаём содержимое аргументом) + `--model X` если задан + `["--", FirstMessage]` если задан.
- `Env`: `CLAUDECODE=""` + `ROCKET_*` из spec + `ROCKET_SOCKET`.

- [ ] **Step 1: Тесты** — LaunchCommand без model/first-message минимален; с ними — полный; Env содержит все ROCKET_* и `CLAUDECODE=""`; SetupWorkspace пишет prompt-файл; Registry/Get («нет такого агента» → ошибка).
- [ ] **Step 2: Падают → реализация → зелёные.**
- [ ] **Step 3: Commit** — `feat: agent interface and claude-code adapter`.

---

### Task 12: Session manager + spawn (API и CLI)

**Files:**
- Create: `internal/session/manager.go`, `internal/api/sessions.go`, `internal/cli/spawn.go`
- Modify: `internal/api/server.go`, `internal/daemon/daemon.go` (wiring Manager в Deps), `internal/cli/root.go`
- Test: `internal/session/manager_test.go` (fake Runtime/Workspace/Agent), `internal/api/sessions_test.go`

**Interfaces:**
- Consumes: Runtime, Workspace, Agent, Store, Bus.
- Produces:

```go
type Manager struct{ ... }
func NewManager(st *store.Store, b *bus.Bus, rt runtime.Runtime, ws workspace.Workspace, cfg *config.Config) *Manager
type SpawnReq struct{ Project, Repo, Task, Feature, Prompt, AgentName, Kind string } // Kind по умолчанию "worker"
func (m *Manager) Spawn(ctx context.Context, req SpawnReq) (store.Session, error)
func (m *Manager) Kill(ctx context.Context, id string, cleanup bool) error
func (m *Manager) Restore(ctx context.Context, id string) error
```

Spawn-поток (02-architecture.md): валидация (project существует, repo ∈ main+linked проекта, task/feature матчат `^[a-z0-9-]+$`, агент есть в реестре и `Available()`); feature по умолчанию = task; имя `=feature-task`, при коллизии в store/tmux — суффикс `-2`, `-3`; ветка `feature/<feature>/<task>`; запись `state=spawning` → событие `session.spawned` → **синхронно в фазе 1**: workspace.Create (событие `workspace.branch_collision` при коллизии) → agent.SetupWorkspace → runtime.Create (env: агентские + repo.Env + ROCKET_*) → `state=running` + событие `session.state_changed`; любая ошибка → `state=errored` + событие, workspace при ошибке не удаляем (для дебага). Kill: runtime.Destroy → `state=killed` → если cleanup — workspace.Destroy + событие `workspace.cleanup`. Restore: допустим только из `errored`/`killed` или когда tmux мёртв; workspace.Restore → runtime.Create заново (без FirstMessage) → `state=running`, событие `session.restored`.

API: `POST /v1/sessions {project, repo, task, feature?, prompt?, agent?}` (фаза-1 спавн без проверки «только оркестратор»; `/v1/workers` с caller-валидацией — фаза 3) → `201 {id, feature_slug, branch, worktree_path}`; `GET /v1/sessions` (+фильтры), `GET /v1/sessions/{id}`, `POST /v1/sessions/{id}/kill?cleanup=`, `POST /v1/sessions/{id}/restore`, `GET /v1/sessions/{id}/output?lines=`, `GET /v1/sessions/{id}/attach`.

CLI: `rocket spawn --project <id> --repo <id> --task <name> [--feature <slug>] [--prompt <text>] [--agent <name>]` — печатает id, branch, worktree, attach-подсказку.

- [ ] **Step 1: Юнит-тесты Manager на фейках** — happy path со всеми переходами state и событиями; коллизия имени → суффикс `-2`; ошибка workspace.Create → `errored`; Kill с cleanup зовёт workspace.Destroy; Restore из errored перезапускает runtime.
- [ ] **Step 2: Тесты API** — spawn с несуществующим repo/project → 400; repo не в проекте → 400 `repo_not_in_project`; kill несуществующей → 404.
- [ ] **Step 3: Падают → реализация → зелёные.**
- [ ] **Step 4: Commit** — `feat: session manager with spawn/kill/restore`.

---

### Task 13: CLI сессий — ls, attach, kill, restore

**Files:**
- Create: `internal/cli/sessions.go`
- Modify: `internal/cli/root.go`
- Test: ручная проверка + юнит на форматирование таблицы (`internal/cli/sessions_test.go`)

**Interfaces:**
- Consumes: session-эндпоинты Task 12.
- Produces: `rocket ls [--project] [--feature] [--all]` (колонки: SESSION, KIND, PROJECT, REPO, STATE, ACTIVITY, AGE; PR/CI — прочерки до фазы 4); `rocket attach <session>` (`GET .../attach` → `syscall.Exec` tmux; внутри tmux (`$TMUX`) — `tmux switch-client -t =name`); `rocket kill <session> [--cleanup]`; `rocket restore <session>`.

- [ ] **Step 1: Юнит на рендер таблицы** (функция `renderSessions([]store.Session) string`): колонки и относительный возраст (`5m`, `2h`).
- [ ] **Step 2: Реализация команд.**
- [ ] **Step 3: Ручная сквозная проверка** (критерий фазы, повторить после Task 15):

```bash
./rocket repo add ~/projects/rocket --id rocket-src
./rocket project create rocket --main rocket-src
./rocket spawn --project rocket --repo rocket-src --task probe --prompt "скажи привет и жди"
./rocket ls
./rocket attach probe-probe        # живой Claude Code в worktree
./rocket kill probe-probe --cleanup
```

- [ ] **Step 4: Commit** — `feat: session CLI (ls/attach/kill/restore)`.

---

### Task 14: Реконсиляция при старте демона

**Files:**
- Create: `internal/session/reconcile.go`
- Modify: `internal/daemon/daemon.go` (вызов после открытия store)
- Test: `internal/session/reconcile_test.go` (fake Runtime)

**Interfaces:**
- Produces: `func (m *Manager) Reconcile(ctx context.Context) error`.

Логика: для каждой сессии store в `spawning|running`: tmux-сессии нет → `state=errored` + событие `session.state_changed` (данные `{"reason":"tmux_missing"}`); tmux есть, а worktree-каталога нет → тоже `errored` (`"reason":"worktree_missing"`), tmux при этом не убиваем. Tmux-сессии с валидным именем, которых нет в store, — только событие-warning (чужие tmux не трогаем). Автоперезапуска нет — только явный `rocket restore`.

- [ ] **Step 1: Тесты на фейках** — три случая выше.
- [ ] **Step 2: Падают → реализация → зелёные.**
- [ ] **Step 3: Commit** — `feat: reconcile store vs tmux/worktrees on daemon start`.

---

### Task 15: events, logs, doctor

**Files:**
- Create: `internal/api/events.go` (`GET /v1/events`), `internal/cli/events.go`, `internal/cli/logs.go`, `internal/cli/doctor.go`
- Modify: `internal/api/server.go`, `internal/cli/root.go`
- Test: `internal/api/events_test.go`

**Interfaces:**
- Produces: `rocket events [--follow] [--session <id>]` (follow в фазе 1 — поллинг `?since=<lastID>` раз в 2s; SSE — фаза 2); `rocket logs [--follow]` (`tail` файла `logs/rocketd.log` средствами Go); `rocket doctor` — проверки: tmux в PATH (+версия ≥3.0), git в PATH, демон отвечает, каждый агент из реестра `Available()`, для claude-code — предупреждение (не ошибка), если плагин Superpowers не найден (`~/.claude/plugins/cache/*/superpowers` glob); вывод — список ✅/⚠️/❌, код выхода 1 при ❌.

- [ ] **Step 1: Тест API events** — since/limit/session-фильтры (поверх Task 3 DAO — тонкий хендлер).
- [ ] **Step 2: Реализация трёх команд.**
- [ ] **Step 3: Финальная сквозная проверка критерия фазы** — сценарий из Task 13 Step 3 целиком, плюс: `rocket daemon stop && rocket daemon start && rocket ls` (сессия жива после рестарта демона, реконсиляция ничего не сломала); `kill --cleanup` сносит tmux и worktree, ветка остаётся.
- [ ] **Step 4: Commit** — `feat: events/logs/doctor commands` и, если нужно, фиксы по итогам сквозной проверки отдельными коммитами.

---

## Self-Review (выполнен)

- **Покрытие роадмапа фазы 1:** каркас CLI/демона — T1/T6; автозапуск, PID/сокет, `daemon *` — T6; SQLite+миграции — T3; журнал событий + `rocket events` — T3/T4/T15; `repo *` — T7; `project *` — T8; runtime tmux — T9; workspace — T10; адаптер claude-code — T11; `spawn` — T12; `ls/attach/kill/restore` — T13; реконсиляция — T14; `doctor`/`logs` — T15. Пробелов нет.
- **Типы согласованы:** `store.Repo` используется workspace (T10), `runtime.Handle/CreateSpec` — manager (T12), `agent.LaunchSpec` — manager; фильтры `SessionFilter` — API и CLI.
- **Сознательно отложено по роадмапу:** activity/hooks/`send`/SSE — фаза 2; `/v1/workers` с caller, задачи, `up` — фаза 3; PR/CI — фаза 4; codex — фаза 5.
