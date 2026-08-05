# M2 milestone-quiet: heartbeat-напоминания молчащему агенту

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:test-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Майлстон `in_progress`, у которого дольше `milestone_quiet_after` (default 24h) нет следов работы взявшего его агента, шлёт агенту одно напоминание за 24h, публикует событие шины `milestone.quiet` и отдаётся в API с аддитивным флагом `quiet: true`.

**Architecture:** Правило «майлстон молчит» — чистая экспортируемая функция в `internal/heartbeat` (как `StaleThread`/`InputStalled`), читаемая дважды: heartbeat-свипом (напоминание) и API (производный флаг, как `waiting_terminal`). «След работы» считается одним SQL-агрегатом `Store.MilestoneActivity()` — max по task_log (kind != status), task_docs, questions и question_messages, где автор = `tasks.assigned_role`. Доставка — существующий `remindParticipant` (живому агенту в сессию, спящему в инбокс).

**Tech Stack:** Go, SQLite (modernc), пакеты `internal/{store,heartbeat,config,api}`.

## Global Constraints

- Спека v2 (док #825 задачи #1023), §«Видимость работы» п.2 и §API: конфиг `milestone_quiet_after` (default 24h), анти-спам «раз в 24h на майлстон», аддитивный `quiet: true` в API, событие `milestone.quiet`.
- Обычные задачи не затрагиваются: правило требует `milestone = 1` и непустой `assigned_role`.
- Человеку heartbeat сообщений не шлёт — его канал это флаг `quiet` и событие шины.
- Флаг `quiet` не хранится в базе: он функция активности и часов.
- `go test ./...` зелёный; доки (M4) и web/mobile (M3) — вне скоупа, CLI не трогаем.
- Текст напоминания — английский, как у уведомления о назначении майлстона в M1.

---

### Task 1: Store — последний след агента в майлстоне

**Files:**
- Create: `internal/store/milestones.go`
- Test: `internal/store/milestones_test.go`

**Interfaces:**
- Produces: `func (s *Store) MilestoneActivity() (map[int64]int64, error)` — по id майлстона unix-время последнего следа взявшего его агента; майлстоны без следов в карту не попадают.

- [ ] **Step 1:** тест: майлстон с записью журнала (kind=note, author=cto), доком и сообщением в треде от cto → в карте max из трёх; запись kind=status от cto и запись другого автора не считаются; обычная задача в карту не попадает.
- [ ] **Step 2:** запустить — упадёт (нет метода).
- [ ] **Step 3:** реализовать одним UNION ALL-запросом с `JOIN tasks ... ON a.who = t.assigned_role AND t.milestone = 1`.
- [ ] **Step 4:** `go test ./internal/store/`.
- [ ] **Step 5:** commit.

### Task 2: Config — milestone_quiet_after

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.DefaultMilestoneQuietAfter = 24 * time.Hour`, поле `Config.MilestoneQuietAfter time.Duration` (`yaml:"milestone_quiet_after"`).

- [ ] **Step 1:** тест: default 24h; `milestone_quiet_after: 6h` в yaml читается.
- [ ] **Step 2:** запустить — упадёт.
- [ ] **Step 3:** добавить константу, поле и default.
- [ ] **Step 4:** `go test ./internal/config/`.
- [ ] **Step 5:** commit.

### Task 3: Heartbeat — правило и свип

**Files:**
- Create: `internal/heartbeat/quiet.go`, `internal/heartbeat/quiet_test.go`
- Modify: `internal/heartbeat/heartbeat.go` (вызов свипа в `Tick`), `internal/heartbeat/heartbeat_test.go` (`testConfig`)

**Interfaces:**
- Consumes: `Store.MilestoneActivity`, `Config.MilestoneQuietAfter`, `(*Heartbeat).remindParticipant`, `antiSpamOK`.
- Produces: `func QuietMilestone(task store.Task, lastActivity int64, now time.Time, after time.Duration) (since time.Duration, ok bool)`; `(*Heartbeat).sweepQuietMilestones() error`; событие шины `milestone.quiet` с `{task_id, agent_id, title, since_seconds, reminded}`.

- [ ] **Step 1:** тест правила: не майлстон / не in_progress / без assigned_role / свежая активность → не quiet; молчание 30h → quiet; без активности точкой отсчёта служит `updated_at`; ровно на пороге — ещё не quiet.
- [ ] **Step 2:** тест свипа: `Tick` кладёт ровно одно напоминание в инбокс спящего агента и не повторяет его на втором тике; событие `milestone.quiet` опубликовано.
- [ ] **Step 3:** запустить — упадёт.
- [ ] **Step 4:** реализовать `QuietMilestone` + `sweepQuietMilestones`, вызвать из `Tick` рядом с `sweepStaleThreads`.
- [ ] **Step 5:** `go test ./internal/heartbeat/`.
- [ ] **Step 6:** commit.

### Task 4: API — аддитивный флаг quiet

**Files:**
- Create: `internal/api/quiet.go`
- Modify: `internal/api/tasks.go` (поле `Quiet`, аннотация в list/board/detail)
- Test: `internal/api/tasks_milestones_test.go`

**Interfaces:**
- Consumes: `heartbeat.QuietMilestone`, `Store.MilestoneActivity`.
- Produces: `taskResponse.Quiet bool json:"quiet,omitempty"`; `quietMilestones(d Deps) (map[int64]bool, error)`; `annotateQuiet(tr *taskResponse, quiet map[int64]bool)`.

- [ ] **Step 1:** тест: молчащий майлстон отдаётся с `quiet: true` в GET /v1/tasks/{id} и в списке; свежий — без поля; обычная задача — без поля.
- [ ] **Step 2:** запустить — упадёт.
- [ ] **Step 3:** реализовать.
- [ ] **Step 4:** `go test ./internal/api/`.
- [ ] **Step 5:** commit.

### Task 5: Проверка целиком

- [ ] **Step 1:** `go build ./... && go test ./...`.
- [ ] **Step 2:** PR. Доки (`docs/03-daemon-api.md`, `docs/05-state.md`) — задача M4 (#1034): сообщить оркестратору, что ключ `milestone_quiet_after`, поле `quiet` и событие `milestone.quiet` ждут описания там.
