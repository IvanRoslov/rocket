# Spawn-time mirror sync + mirror freshness in `rocket task show` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Синхронизировать зеркало репозитория синхронно (с таймаутом, не блокируя спавн при ошибке) перед созданием workspace в `internal/session`, и показывать свежесть зеркал в `rocket task show` — строкой в карточке и ключом `mirrors` в `--json`.

**Architecture:** Вся механика уже есть: `internal/mirror` умеет `Sync` (fetch + строгий `merge --ff-only`, никогда не клоббер) и `Check` (`Freshness` без сети), а `internal/cli/mirror.go` умеет `mirrorFreshness`/`mirrorLine` — их уже печатает `rocket status`. Поэтому: (1) `Manager` получает инъектируемое поле `syncMirror` (дефолт `mirror.Sync`), вызываемое под `context.WithTimeout(cfg.MirrorSyncTimeout)` перед `ws.Create`/`ws.Restore`, только для путей под `cfg.ReposDir`; (2) `task show` переиспользует `mirrorFreshness` и `mirrorLine` — те же формулировки, что в `status`, плюс новый JSON-тип `mirrorJSON`.

**Tech Stack:** Go, cobra, `testing` stdlib, `git` через `exec.CommandContext`.

**Spec:** бриф задачи #3578 (feature `rocket-rocket-repos-origin`); нормативные документы — `docs/04-cli.md`, `docs/05-state.md`.

## Global Constraints

- Никакого клоббера: только `mirror.Sync`, в котором `fetch` + `merge --ff-only`. Ни `reset --hard`, ни `checkout -f`, ни `clean`.
- Синк перед workspace — **non-blocking**: таймаут или ошибка = `slog.Warn` и спавн продолжается; ошибка не возвращается наружу и не переводит сессию в `errored`.
- Синкаются только зеркала демона под `cfg.ReposDir`. Рабочая копия человека, зарегистрированная через `rocket repo add <path>`, не трогается никогда (`docs/05-state.md`).
- Формулировки строк свежести заморожены (`mirrorLine` в `internal/cli/mirror.go`) — в `task show` переиспользуются дословно, не переписываются.
- `--json` может только **приобретать** ключи, никогда не двигать существующие. `mirrors` всегда массив, никогда `null`.
- Строго TDD: сначала падающий тест, потом минимальная реализация.

---

### Task 1: `mirror.IsMirror` + `config.MirrorSyncTimeout`

**Files:**
- Modify: `internal/mirror/syncer.go` (переименовать `isMirror` → `IsMirror`, добавить doc-комментарий об экспорте)
- Modify: `internal/config/config.go` (поле `MirrorSyncTimeout time.Duration \`yaml:"mirror_sync_timeout"\`` + дефолт)
- Test: `internal/mirror/syncer_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: —
- Produces: `mirror.IsMirror(path, reposDir string) bool`; `config.Config.MirrorSyncTimeout` (дефолт `30 * time.Second`, `DefaultMirrorSyncTimeout`).

- [ ] **Step 1:** Тест `TestIsMirrorExported` в `internal/mirror/syncer_test.go`: `IsMirror("/repos/a", "/repos")` == true, `IsMirror("/home/me/proj", "/repos")` == false, `IsMirror("/repos/a", "")` == false.
- [ ] **Step 2:** Тест `TestLoadDefaultsMirrorSyncTimeout` в `internal/config/config_test.go`: `Load(t.TempDir())` даёт `MirrorSyncTimeout == 30*time.Second`; и что `mirror_sync_timeout: 5s` из `config.yaml` читается.
- [ ] **Step 3:** `go test ./internal/mirror/ ./internal/config/` — FAIL (undefined: IsMirror / MirrorSyncTimeout).
- [ ] **Step 4:** Реализовать: переименовать `isMirror`→`IsMirror` (и вызов в `SyncOnce`), добавить поле+дефолт в config.
- [ ] **Step 5:** `go test ./internal/mirror/ ./internal/config/` — PASS.
- [ ] **Step 6:** Commit `mirror+config: экспортировать IsMirror и добавить mirror_sync_timeout`.

---

### Task 2: синхронный синк зеркала перед созданием workspace

**Files:**
- Modify: `internal/session/manager.go` (поле `syncMirror`, `SetMirrorSyncer`, метод `syncMirrorBeforeWorkspace`, три точки вызова)
- Test: `internal/session/manager_test.go`

**Interfaces:**
- Consumes: `mirror.IsMirror`, `config.Config.MirrorSyncTimeout` (Task 1).
- Produces: `(*Manager).SetMirrorSyncer(func(context.Context, store.Repo) error)`; приватный `(*Manager).syncMirrorBeforeWorkspace(ctx context.Context, repo store.Repo)`.

- [ ] **Step 1:** Падающие тесты в `internal/session/manager_test.go`:
  - `TestSpawnSyncsMirrorBeforeWorkspace`: репозиторий с путём внутри `cfg.ReposDir`; подменённый синкер записывает порядок вызовов; проверяем, что синк вызван **до** `ws.Create` и с тем же `repo.ID`.
  - `TestSpawnSkipsSyncForNonMirrorRepo`: путь вне `ReposDir` → синкер не вызван, спавн успешен.
  - `TestSpawnProceedsWhenMirrorSyncFails`: синкер возвращает ошибку → `Spawn` возвращает `nil` ошибку, сессия в состоянии `running`.
  - `TestSpawnMirrorSyncRespectsTimeout`: `cfg.MirrorSyncTimeout = 10 * time.Millisecond`, синкер блокируется на `ctx.Done()` → `Spawn` завершается успешно, а синкер увидел `context.DeadlineExceeded`.
- [ ] **Step 2:** `go test ./internal/session/ -run Mirror` — FAIL (undefined: SetMirrorSyncer).
- [ ] **Step 3:** Реализация в `manager.go`:
  ```go
  // syncMirror refreshes a repo mirror; nil means mirror.Sync. Injected so
  // tests do not shell out to git.
  syncMirror func(context.Context, store.Repo) error
  ```
  дефолт в `NewManager`: `syncMirror: mirror.Sync`; `SetMirrorSyncer` для тестов; и метод:
  ```go
  // syncMirrorBeforeWorkspace fast-forwards repo's mirror so the worktree
  // branches off current origin instead of whatever the mirror was cloned
  // at. It is deliberately best-effort: a failed or timed-out sync is
  // logged and the spawn proceeds — a stale mirror is a worse worktree,
  // but a blocked spawn is a stopped feature.
  func (m *Manager) syncMirrorBeforeWorkspace(ctx context.Context, repo store.Repo) {
      if m.syncMirror == nil || !mirror.IsMirror(repo.Path, m.cfg.ReposDir) {
          return
      }
      timeout := m.cfg.MirrorSyncTimeout
      if timeout <= 0 {
          timeout = config.DefaultMirrorSyncTimeout
      }
      ctx, cancel := context.WithTimeout(ctx, timeout)
      defer cancel()
      if err := m.syncMirror(ctx, repo); err != nil {
          slog.Warn("session: mirror sync before workspace failed, continuing",
              "repo", repo.ID, "path", repo.Path, "error", err)
      }
  }
  ```
- [ ] **Step 4:** Вставить `m.syncMirrorBeforeWorkspace(ctx, repo)` непосредственно перед `m.ws.Create(...)` в `Spawn` и в `SpawnOrchestrator`, и перед `m.ws.Restore(...)` в `Restore`.
- [ ] **Step 5:** `go test ./internal/session/ ./internal/api/ ./internal/daemon/` — PASS.
- [ ] **Step 6:** Commit `session: синхронно синкать зеркало перед созданием workspace`.

---

### Task 3: свежесть зеркал в `rocket task show` (карточка + `--json`)

**Files:**
- Modify: `internal/cli/mirror.go` (новый тип `mirrorJSON` + конвертер `mirrorJSONRows`)
- Modify: `internal/cli/task.go` (`taskShowJSON.Mirrors`, вызов `mirrorFreshness`, секция в `renderTaskCard`)
- Test: `internal/cli/mirror_test.go`, `internal/cli/task_test.go`

**Interfaces:**
- Consumes: `mirrorRow`, `mirrorLine`, `mirrorFreshness` (существующие).
- Produces:
  ```go
  type mirrorJSON struct {
      RepoID        string `json:"repo_id"`
      Stale         bool   `json:"stale"`
      BehindCommits int    `json:"behind_commits"`
      LastFetch     int64  `json:"last_fetch"`
      Blocked       string `json:"blocked"`
      Error         string `json:"error"`
  }
  func mirrorJSONRows(rows []mirrorRow) []mirrorJSON // никогда не nil
  ```
  и `renderTaskCard(task, docs, logs, questions, mirrors []mirrorRow, w, now)`.

- [ ] **Step 1:** Тест `TestMirrorJSONRows` в `internal/cli/mirror_test.go`: пустой вход → `[]mirrorJSON{}` (маршалится в `[]`, не `null`); строка с `Err` → `error` заполнен, `stale` == true; строка с `Blocked`/`BehindCommits`/`LastFetch` → поля скопированы, `last_fetch` == unix-секунды, а нулевое время → `0`.
- [ ] **Step 2:** Тест `TestTaskShowCardRendersMirrors` в `internal/cli/task_test.go`: `renderTaskCard` с одной протухшей строкой печатает секцию `## Mirrors` и ровно текст `mirrorLine(row, now)`; с пустым срезом — секции нет вовсе.
- [ ] **Step 3:** Тест `TestTaskShowJSONHasMirrorsKey`: в `taskShowJSON` замаршаленном без зеркал есть ключ `mirrors` со значением `[]`.
- [ ] **Step 4:** `go test ./internal/cli/ -run 'Mirror|TaskShow'` — FAIL.
- [ ] **Step 5:** Реализация: `mirrorJSON`+`mirrorJSONRows` в `mirror.go`; в `newTaskShowCmd` заменить `c, _, err := connect(true)` на `c, cfg, err := connect(true)` и вызвать `mirrors := mirrorFreshness(cmd.Context(), c, cfg, now)`; добавить `Mirrors []mirrorJSON \`json:"mirrors"\`` в `taskShowJSON`; в `renderTaskCard` после секций добавить:
  ```go
  if len(mirrors) > 0 {
      fmt.Fprintf(w, "## Mirrors\n")
      renderMirrors(mirrors, w, now)
      fmt.Fprintf(w, "\n")
  }
  ```
- [ ] **Step 6:** Обновить все существующие вызовы `renderTaskCard` в `internal/cli/task_test.go` под новую сигнатуру.
- [ ] **Step 7:** `go test ./internal/cli/` — PASS.
- [ ] **Step 8:** Commit `cli: показывать свежесть зеркал в rocket task show и в --json`.

---

### Task 4: документация и финальная верификация

**Files:**
- Modify: `docs/04-cli.md` (секция `rocket task show`), `docs/05-state.md` (про синк перед workspace), `docs/03-config.md` или где описан config (ключ `mirror_sync_timeout`)

- [ ] **Step 1:** Найти места: `grep -rn "mirror_sync_interval\|task show" docs/`.
- [ ] **Step 2:** Описать `mirror_sync_timeout` рядом с `mirror_sync_interval`; описать строку свежести и ключ `mirrors` в описании `rocket task show`; описать синхронный синк перед созданием worktree.
- [ ] **Step 3:** `go build ./... && go vet ./... && go test ./...` — всё зелёное.
- [ ] **Step 4:** Commit `docs: описать mirror_sync_timeout и свежесть зеркал в task show`.
