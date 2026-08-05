# T1 core-attention Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans / test-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** заменить вычисляемый `waiting_on` вопросных тредов хранимым attention set, добавить `questions.type` (decision|fyi) и `questions.options`, guard от записи не-участника.

**Architecture:** новая таблица `question_attention` (миграция 0011) + слой правил в `internal/store/attention.go`; `internal/api` вызывает правила на каждой мутации треда (ask/reply/answer) и читает `waiting_on` из attention вместо `waitingOn()`; `dry_run`/`join` — параметры записывающих ручек.

**Tech Stack:** Go 1.2x, SQLite (modernc.org/sqlite), net/http ServeMux, встроенные миграции через `embed`.

## Global Constraints

- Миграция называется `internal/store/migrations/0011_attention.sql`; её версия = позиция в отсортированном списке файлов (11).
- Обратная совместимость API: клиенты, не шлющие `type`/`options`/`join`/`dry_run`, ведут себя как раньше; форма `waiting_on`/`your_turn`/`whose_turn` сохраняется.
- Префиксы доставки (`[task #N Qk reply from X]`) НЕ меняются (вне скоупа T1).
- CLI, heartbeat, web — вне скоупа.
- Все комментарии в коде — на английском (как в существующем коде), сообщения коммитов — как в репозитории (русский допустим).
- `go test ./...` зелёный.

---

## File Structure

- Create: `internal/store/migrations/0011_attention.sql` — схема + бэкфилл.
- Create: `internal/store/attention.go` — DAO и автоправила attention.
- Create: `internal/store/attention_test.go` — тесты правил и бэкфилла.
- Modify: `internal/store/questions.go` — поля `Type`, `Options` в `Question`, чтение/запись колонок.
- Modify: `internal/store/agent_questions.go` — те же поля в фасаде роли.
- Modify: `internal/store/questions_reopen.go` — reopen нормализует `type` в `decision`.
- Modify: `internal/store/open_threads.go` — `OpenThread.Attention`.
- Modify: `internal/api/threads.go` — attention вместо `waitingOn()` в counters, guard-хелперы, эхо цели.
- Modify: `internal/api/questions.go`, `internal/api/agent_questions.go` — `type`/`options`/`join`/`dry_run`/`attention`/`local_ref`, применение правил.

---

### Task 1: миграция 0011 + колонки type/options в store

**Files:**
- Create: `internal/store/migrations/0011_attention.sql`
- Modify: `internal/store/questions.go`, `internal/store/agent_questions.go`
- Test: `internal/store/attention_test.go`

**Interfaces:**
- Produces: `store.Question.Type string`, `store.Question.Options []string`,
  `store.AgentQuestion.Type/Options`, таблица `question_attention`.

- [ ] Step 1: тест `TestQuestionTypeAndOptionsRoundTrip` — AddQuestion с Type="fyi", Options=[]string{"A","B"} → GetQuestion возвращает те же значения; Question без Type → "decision".
- [ ] Step 2: `go test ./internal/store -run TypeAndOptions` → FAIL (нет полей).
- [ ] Step 3: миграция 0011 (таблица + 2 ALTER + индекс + бэкфилл), поля и SQL в questions.go/agent_questions.go.
- [ ] Step 4: тест зелёный.
- [ ] Step 5: коммит.

### Task 2: DAO attention + автоправила

**Files:**
- Create: `internal/store/attention.go`
- Test: `internal/store/attention_test.go`

**Interfaces (Produces):**
```go
func (s *Store) ListAttention(questionID int64) ([]string, error)          // sorted
func (s *Store) SetAttention(questionID int64, ids []string) error         // replace whole set
func (s *Store) ClearAttention(questionID int64) error
func (s *Store) AttentionOnOpen(questionID int64, author string, addressedTo, participants []string) error
func (s *Store) AttentionOnEntry(questionID int64, author string, addressedTo, participants []string) error
func (s *Store) AttentionOfOpenThreads() (map[int64][]string, error)
```

- [ ] Step 1: тесты правил: open с `--to` → только адресаты; open без `--to` → все, кроме автора; entry автора удаляет автора; entry с `--to` добавляет адресатов; опустевший набор → все, кроме автора записи; Clear очищает.
- [ ] Step 2: прогон → FAIL.
- [ ] Step 3: реализация.
- [ ] Step 4: зелёный.
- [ ] Step 5: коммит.

### Task 3: бэкфилл открытых тредов проверен

**Files:** Test: `internal/store/attention_test.go` (+ при необходимости правка миграции)

- [ ] Step 1: тест на «сырой» БД: создать схему до 0010 не получится (миграции применяются разом), поэтому проверяем эквивалентность иначе — тест `TestBackfillMatchesLegacyWaitingOn` строит треды через store, вручную удаляет строки attention, повторно применяет бэкфилл-SQL (вынесен константой) и сравнивает с ожидаемым набором.
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 4: OpenThread.Attention + counters по attention

**Files:** Modify `internal/store/open_threads.go`, `internal/api/threads.go`
- [ ] Step 1: тест api: тред, адресованный cto, не даёт `awaiting_user`; после реплики cto — даёт.
- [ ] Step 2..5: FAIL → реализация (`threadCounts` читает `th.Attention`) → PASS → коммит.

### Task 5: API — attention/type/options/local_ref в ответах, правила на мутациях

**Files:** Modify `internal/api/questions.go`, `internal/api/agent_questions.go`
- [ ] Step 1: тесты: ask `--to cto` → `attention:["cto"]`, `waiting_on` = attention; reply от cto без to → attention = все, кроме cto; answer → attention пуст; `local_ref` = `"1023/Q2"` / `"cto/Q1"`.
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 6: fyi-треды

**Files:** Modify `internal/api/questions.go`, `internal/api/agent_questions.go`, `internal/store/questions_reopen.go`
- [ ] Step 1: тесты: POST questions с `type:"fyi"` → 201, status=resolved, resolution="fyi", attention пуст, счётчики не растут; reply в fyi (в т.ч. от человека) → 201, status=open, type="decision", attention по правилу.
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 7: guard не-участника + join + dry_run

**Files:** Modify `internal/api/threads.go`, `internal/api/questions.go`, `internal/api/agent_questions.go`
- [ ] Step 1: тесты: постоянный агент-не-участник reply → 403 `not_a_participant` с телом треда в message; тот же запрос с `join:true` → 201 и он в participants; тот же guard на answer; `dry_run:true` → 200, ничего не записано, есть `echo`.
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 8: верификация и PR

- [ ] `go build ./... && go vet ./... && go test ./...`
- [ ] проверка миграции на копии реальной БД (`ROCKET_MIGTEST_DB`)
- [ ] `gh pr create`
