# Фаза 4 «GitHub» — план имплементации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Демон видит PR/CI воркеров и закрывает петлю доставки: merge PR → авто-cleanup воркера и подзадача в done; красный CI → сообщение воркеру. Токен пользователя, каталог/клонирование репо из GitHub.

**Architecture:** Внутренний REST-клиент (`internal/github`) с инжектируемым `baseURL` — все тесты и E2E ходят в локальный httptest-стаб GitHub API; живой токен подключает пользователь в финале (шаги задокументированы). Поллер — цикл демона (`github_poll_interval` 2m) поверх клиента: PR по head-ветке, rollup чеков, реакции через очередь, авто-cleanup через session manager. `GH_TOKEN` прокидывается в env сессий.

**Tech Stack:** stdlib net/http (без go-github — YAGNI: нужно 5 эндпоинтов).

## Global Constraints

- Токен в таблице `settings` (key `github_token`); в GET /v1/settings маскируется (`ghp_…last4`). Демон в GitHub только читает.
- `github_api_base` в config.yaml (default `https://api.github.com`) — точка инжекции стаба; `merge_grace` (default 5m); `github_poll_interval` уже есть (2m).
- owner/repo — из `git remote get-url origin` основного чекаута (ssh `git@github.com:o/r.git` и https-формы); нет origin/не-github → воркер поллером пропускается (не ошибка).
- Session: поля `PRNumber int`, `PRState string` (open|merged|closed), `CIState string` (pending|passing|failing) добавляются в store.Session (колонки в схеме есть с фазы 1) + `UpdateSessionPR(id, n, prState, ciState)`.
- События: `pr.opened {number}`, `pr.ci_changed {number, from, to}`, `pr.merged {number}`, `repo.clone_started|clone_done|clone_failed`.
- Реакции только через очередь: CI failing → `[rocket] CI failing on PR #<n>: <сводка>. Investigate and fix.` не чаще раза на head SHA; changes requested → `[rocket] Changes requested on PR #<n>. Address the review comments.` раз на review-state.
- Авто-cleanup: pr_state→merged, grace `merge_grace`, воркер не active → Kill(cleanup=true), state=done, событие workspace.cleanup; выключатель — `repos.auto_cleanup` (в схеме с фазы 1; спека говорит per-project — используем существующее поле репо, это осознанное решение). Оркестраторов авто-cleanup не касается.
- Автопереходы подзадачи воркера: PR открыт → review, merged → done (+task_log kind=status); явный `task move` побеждает (не перезатирать вручную выставленный статус: переход только из in_progress→review и in_progress|review→done).
- Клоны: `<repos_dir>/<owner>__<name>`; клонирование с токеном в URL не логируется (токен не должен попадать в логи/события).
- Rate-limit дружелюбие: ETag conditional requests (If-None-Match, 304 → кэш), backoff при 403/5xx (пропуск тика).
- Прежние инварианты: error envelope, id-валидация, тесты/vet/gofmt перед каждым коммитом; сабагентам запрещены merge/rebase/reset.

---

### Task 1: Settings + `rocket github auth`

**Files:** Create `internal/store/settings.go`, `internal/api/settings.go`, `internal/cli/github.go`; Modify server.go, root.go, config.go (+`GithubAPIBase` yaml:"github_api_base" default https://api.github.com, `MergeGrace` yaml:"merge_grace" default 5m).

- store: `SetSetting(key, value)` (upsert), `GetSetting(key) (string, error)` (ErrNotFound), `DeleteSetting(key)`.
- API: `GET /v1/settings` → `{github_token: "ghp_…abcd"|""}` (маска: первые 4 + … + последние 4, короткий → "set"); `PUT /v1/settings {github_token}` — валидация через GET `<base>/user` с токеном (401 → 400 `invalid_token`; сетевая ошибка → 502 `github_unreachable`); пустая строка → удаление токена.
- CLI: `rocket github auth <token>` → PUT; печатает подтверждение с login из ответа валидации (вернуть `{login}` в PUT-ответе).
- [ ] TDD: store round-trip; API против httptest-стаба `/user` (валидный/401/сетевая); маскирование; CLI usage.
- [ ] Commit: `feat: github token settings and auth command`.

### Task 2: GitHub REST-клиент

**Files:** Create `internal/github/client.go`, `internal/github/client_test.go`.

- `New(baseURL, token string) *Client` (http.Client timeout 15s):
```go
GetUser(ctx) (Login string, err error)
ListRepos(ctx) ([]Repo, error)            // GET /user/repos?per_page=100 + pagination по Link; Repo{FullName, Private bool, DefaultBranch}
FindPRByBranch(ctx, owner, repo, branch string) (*PR, error) // GET /repos/o/r/pulls?head=owner:branch&state=all; nil если нет
GetPR(ctx, owner, repo string, number int) (*PR, error)      // PR{Number, State, Merged bool, HeadSHA, ReviewDecision} — reviewDecision через GET /repos/o/r/pulls/{n}/reviews (последний state per reviewer; CHANGES_REQUESTED если есть)
CheckRollup(ctx, owner, repo, sha string) (string, error)    // GET /repos/o/r/commits/{sha}/check-runs → pending|passing|failing (нет чеков → passing)
```
- ETag-кэш: map[url]{etag, body} внутри клиента; 304 → кэшированное тело. Ошибки 403 (rate limit) и 5xx → типизированная `ErrBackoff`.
- Sentinel `ErrNoToken` если token=="".
- [ ] TDD против httptest: pagination, 304-кэш, rollup-агрегация (все success → passing; любой failure/cancelled/timed_out → failing; queued/in_progress → pending), reviews-агрегация, 403 → ErrBackoff.
- [ ] Commit: `feat: minimal github REST client with etag cache`.

### Task 3: Каталог и клонирование репо

**Files:** Create `internal/api/github_catalog.go`; Modify `internal/api/repos.go` (POST {github}), `internal/cli/repo.go` (+`--github owner/name`), daemon wiring (клиент в Deps: `GH func() *github.Client` — фабрика, читающая токен из settings на каждый вызов).

- `GET /v1/github/repos?q=` — ListRepos с кэшем 5m (in-memory, инвалидация по времени), фильтр substring по FullName; нет токена → 400 `no_token`.
- `POST /v1/repos {github: "owner/name", id?}`: валидация формата; событие clone_started → `git clone https://x-access-token:<token>@github.com/owner/name.git <repos_dir>/<owner>__<name>` (exec, токен не в логах; при github_api_base-стабе клонирpermission из локального пути — для тестов принять также `{github_clone_url_override}`? НЕТ — тестируем клонирование с file:// URL: если настройка `github_clone_base` задана (default `https://x-access-token:TOKEN@github.com/`), использовать её; тесты ставят `file:///tmp/.../` — документировать как тестовую) → register repo (default_branch из клона) → clone_done; ошибка → clone_failed + 502.
- CLI: `rocket repo add --github owner/name [--id]`.
- [ ] TDD: каталог (кэш: второй запрос не бьёт стаб), клон из file://-бары (создать bare-репо хелпером), событие-цепочка, no_token.
- [ ] Commit: `feat: github repo catalog and clone`.

### Task 4: GH_TOKEN в env сессий + origin-парсер

**Files:** Modify `internal/session/manager.go` (env), Create `internal/github/remote.go` (+тест).

- `github.ParseRemote(url string) (owner, repo string, ok bool)` — ssh/https/с .git и без.
- Manager: при spawn/restore, если токен задан (передать в Manager геттер `getToken func() string` через конструктор из daemon) — добавить `GH_TOKEN` в env сессии (после merge, не перекрывается repo.Env).
- [ ] TDD: парсер-таблица; env содержит GH_TOKEN при токене и не содержит без.
- [ ] Commit: `feat: GH_TOKEN in session env, origin remote parser`.

### Task 5: Store PR-поля + поллер

**Files:** Modify `internal/store/sessions.go` (+PR-поля в Session, scans, `UpdateSessionPR`), Create `internal/ghpoller/poller.go` (+тест), daemon wiring.

- Поллер `New(st, b, gh func() *github.Client, cfg, notify PRNotifier)` (notify — интерфейс Task 6), `Run(ctx)` тикер + экспортируемый `Tick(ctx)`.
- Tick: live воркеры (kind=worker) → группировка по repo_id → owner/repo из origin репо (кэш на тик); без PR → FindPRByBranch → UpdateSessionPR + pr.opened + notify.PROpened(sess, pr); с PR → GetPR + CheckRollup: изменения → UpdateSessionPR + события pr.ci_changed/pr.merged + notify.CIFailing(sess, pr, rollup)/notify.ChangesRequested/notify.Merged. ErrBackoff/ErrNoToken → тихий пропуск тика (лог debug).
- [ ] TDD против htttest-стаба: открытие PR, смена CI, merge; ErrNoToken → no-op.
- [ ] Commit: `feat: PR/CI poller`.

### Task 6: Реакции + авто-cleanup + автопереходы подзадач

**Files:** Create `internal/ghpoller/reactions.go` (реализация PRNotifier; +тест), Modify daemon.

- PROpened: подзадача воркера (GetTaskBySessionID) in_progress → review (+log). CIFailing: раз на head SHA (map) → очередь воркеру `[rocket] CI failing on PR #<n>: <fail-чеки>. Investigate and fix.`. ChangesRequested: раз на состояние → сообщение воркеру. Merged: подзадача → done (из in_progress|review) + запуск grace-таймера (time.AfterFunc merge_grace, отменяемый при shutdown): по истечении — воркер жив и activity != active и repo.AutoCleanup → Manager.Kill(cleanup=true) + state=done (Kill ставит killed — нужен отдельный терминальный путь: Manager метод `Complete(id)` = Kill+state done; ввести) + workspace.cleanup событие.
- [ ] TDD: переходы подзадач (ручной move review→в done не ломается), анти-спам per-SHA, cleanup после grace (короткий grace в тесте) с уважением AutoCleanup=false и active-воркера (перенос ещё на grace).
- [ ] Commit: `feat: CI reactions, auto-cleanup on merge, subtask transitions`.

### Task 7: PR/CI в ls/status/task show

**Files:** Modify `internal/api/sessions.go` (DTO +pr/ci), `internal/cli/sessions.go` (ls: колонки PR, CI), `internal/cli/status.go`, `internal/cli/task.go` (подзадачи: PR/CI), тесты рендеров.

- ls: колонки PR (`#12`/`-`), CI (`passing`/…/`-`); status и task show аналогично; убрать «no PR»-текст хартбита? — хартбит теперь может честно писать `PR #n CI failing` (обновить heartbeat строку: если PRNumber>0 — включить PR/CI, иначе «no PR» остаётся честным).
- [ ] TDD рендеры; Commit: `feat: PR/CI columns in CLI views`.

### Task 8: E2E на стабе + инструкция для live-теста

- Полный сценарий с httptest-стабом GitHub (запущенным как отдельный процесс/горутина теста НЕ выйдет для живого демона — стаб поднять в E2E-скрипте как маленький Go-файл в scratch: `go run stub.go` слушает localhost:PORT; config.yaml demon'а: github_api_base: http://127.0.0.1:PORT, github_clone_base: file:///...): токен-«auth» через стаб, repo add --github (клон из file:// bare), spawn фейкового «воркера» (обычный spawn с prompt «wait»), стаб отдаёт PR open → poller пишет pr_number, подзадача → review; стаб: CI failing → воркер получает `[rocket] CI failing…` (проверить в терминале); стаб: merged → grace (5s в конфиге) → воркер убит+cleanup, подзадача done, ветка живa. `rocket ls` показывает PR/CI.
- Документ `docs/testing/phase-4-live-github.md`: пошаговая инструкция для пользователя (реальный токен: `rocket github auth`, `repo add --github`, реальная фича с PR — что проверять).
- [ ] Прогнать, чинить отдельными коммитами, транскрипт в отчёт.

## Self-Review (выполнен)

- Роадмап фазы 4 покрыт: токен/клиент/GH_TOKEN — T1/T2/T4; каталог+клон — T3; поллер — T5; реакции — T6; авто-cleanup+переходы — T6; колонки — T7; критерий «merge → зачистка; красный CI → воркер в работе» — T8 (стаб) + live-инструкция.
- Стыки: PRNotifier между T5/T6; Session.PR* между T5/T7; github_api_base между T1/T2/T8.
- Отложено: rate-limit точный (Retry-After) — базовый backoff есть; webhooks — за горизонтом.
