# Usage Collector Implementation Plan (W3, task #5141)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** демон сам собирает расход токенов сессии при её терминальности, делает снимок оркестратора при переходе корневой задачи в review и в фоне досчитывает всю историю; ручной перезапуск — `POST /v1/stats/usage/collect` / `rocket stats collect`.

**Architecture:** `internal/usage.Collector` — подписчик шины + FIFO-очередь с дедупликацией + один рабочий поток, который между явными заданиями прогоняет свипер пачками по 50. Сбор = `ResolveSessionTask` + `agent.UsageReader.Usage(worktree, since=created_at)` → `store.ReplaceSessionUsage` → `bus.Publish("usage.collected")`. API и демон видят коллектор через узкий интерфейс `api.UsageCollector`.

**Tech Stack:** Go, `modernc.org/sqlite`, `net/http`, cobra.

**Spec:** `docs/superpowers/specs/2026-10-03-agent-task-stats-design.md` §2.2–2.4, план фичи `docs/superpowers/plans/2026-10-03-agent-task-stats.md` пункт W3.

## Global Constraints

- `final` = терминальна ли сессия в момент сбора; снимок живой — `final=0`, `ended_at=NULL`.
- `since` = `sessions.created_at`.
- `ended_at`: время события для живого терминального события; иначе прежний `ended_at` финальной строки, иначе `Usage.LastAt`, иначе `sessions.updated_at`.
- `Found=false` → `missing`, ноль токенов. Ошибка ридера → `error`, `attempts+1` (сбрасывается успехом); частичный результат не сохраняется — прежние строки моделей остаются.
- Терминальные события: `session.killed`; `session.state_changed` с `to ∈ {done, killed, errored}`. Задержка 15 с, инжектируется.
- Свипер: при старте и раз в 10 мин (инжектируется), пачки по 50 через `SessionsNeedingUsage`.
- После каждой записи — `usage.collected{task_id}`.
- Ошибки сбора только логируются; kill и PATCH не ждут сбора.
- `collect` из сессии агента → `403 human_only`.
- Маппинг `agent.Tokens → store.UsageTokens` в одном месте (`toStoreModels`).

## Review Focus

1. Событие пришло, пока сессия уже восстановлена (`rocket restore`) → финальный сбор не пишет `final=1` живой сессии (пишется снимок). Тест в Task 1.
2. Повторный финальный сбор после снимка не складывает цифры → полная перезапись. Тест в Task 1.
3. Ошибка записи в БД посреди свипа → свипер не крутится бесконечно на той же пачке. Тест в Task 2.
4. Шина теряет события (буфер 64) → свипер подбирает пропущенные терминальные сессии. Тест в Task 2 (свипер на сессиях без событий).
5. Неизвестный агент у сессии (нет адаптера/нет `UsageReader`) → `missing`, а не вечный `error`. Тест в Task 1.

---

### Task 1: Collector core — collect one session

**Files:** Create `internal/usage/collector.go`, `internal/usage/collector_test.go`.

**Produces:**
```go
type ReaderFunc func(agentName string) (agent.UsageReader, bool)
type Options struct{ Delay, SweepInterval time.Duration; BatchSize int; Readers ReaderFunc; Now func() time.Time }
func New(st *store.Store, b *bus.Bus, opts Options) *Collector
func (c *Collector) CollectNow(ctx context.Context, sessionID string, eventAt int64) error // synchronous, used by worker and tests
```

Steps (TDD): tests with a real temp store + bus and a fake reader:
- [ ] terminal session + reader returns 2 models → `final=1`, `status=ok`, 2 rows, `ended_at=eventAt`, task keys resolved.
- [ ] live session → `final=0`, `ended_at` nil; then terminal → `final=1`, rows replaced (old model gone).
- [ ] twice final → identical row (idempotent).
- [ ] `Found=false` → `missing`, no rows; unknown agent → `missing`.
- [ ] reader error → `error`, attempts 1→2→3; previous model rows kept; success resets attempts.
- [ ] `usage.collected` published with `task_id`.
- [ ] backfill `ended_at`: no eventAt → `LastAt`, no LastAt → `updated_at`.
- [ ] commit.

### Task 2: Queue, bus subscription, sweeper, Run

**Produces:**
```go
func (c *Collector) Run(ctx context.Context)          // subscribes, sweeps at start and every SweepInterval, one worker
func (c *Collector) Snapshot(sessionID string)        // enqueue now (final if already terminal)
func (c *Collector) Enqueue(sessionID string)         // enqueue now
func (c *Collector) EnqueueAll(retryMissing bool) (int, error)
func (c *Collector) Sweep(ctx context.Context) (batches int, err error)
```
Also `store.TerminalSessionIDs(includeOK, includeMissing bool) ([]string, error)` in `internal/store/usage.go`.

- [ ] Sweep over 120 terminal sessions → 3 batches, all collected; second Sweep → 0 batches; missing never re-picked.
- [ ] Sweep stops on write error (closed store) instead of looping.
- [ ] Run: `session.killed` event → after injected delay (10ms) row `final=1`; `state_changed{to:running}` ignored; ctx cancel stops Run.
- [ ] EnqueueAll: excludes missing unless retryMissing.
- [ ] commit.

### Task 3: API — collect endpoint + review snapshot hook

**Files:** Create `internal/api/usage_collect.go`, `usage_collect_test.go`; modify `internal/api/server.go` (Deps.Usage, register), `internal/api/tasks.go`.

- [ ] `POST /v1/stats/usage/collect` `{session_id}` → 202 `{queued:1}`; unknown → 404 `session_not_found`; `{all:true,retry_missing}` → 202 `{queued:n}`; neither → 400 `bad_request`; agent caller → 403 `human_only`; nil collector → 503 `unavailable`.
- [ ] PATCH root task → review calls `Snapshot(orchID)` once; subtask/other status/blocked → not called.
- [ ] commit.

### Task 4: Daemon wiring, CLI, docs

- [ ] `internal/daemon/daemon.go`: `uc := usage.New(st, b, usage.Options{})`, `go uc.Run(ctx)`, `Deps.Usage = uc`.
- [ ] `internal/cli/stats.go`: `rocket stats collect [--session S | --all] [--retry-missing]` + test with httptest daemon (pattern from stats_test.go).
- [ ] docs: `docs/03-daemon-api.md`, `docs/04-cli.md`, `docs/07-activity.md` (механика сбора).
- [ ] `go test ./...`, `go vet ./...`; live check on a DB copy; PR.
