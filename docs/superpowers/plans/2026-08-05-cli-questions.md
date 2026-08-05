# T3 cli-questions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans / test-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** пользовательский CLI редизайна вопросных тредов поверх модели attention из T1: top-level `rocket questions` (единый инбокс тредов задач и ролей), глагол `close` с `--dismiss`/`--choose`, `ask --option/--fyi`, локальные id тредов во всех выводах и в префиксах доставки, эхо цели, `--dry-run`, `--join`.

**Architecture:** снизу вверх. В `internal/store` обобщаем `ListOpenThreads` до `ListThreads(includeResolved)`, чтобы `--all` видел закрытые треды. В `internal/api` добавляем одну read-ручку `GET /v1/threads`, отдающую треды задач И ролей одним списком с `local_ref`/`attention`/`type`/`options`/субъектом, и переводим `threadPrefix` на локальные id. В `internal/cli` появляется `questions.go` с top-level командой, а `task`/`agent` получают глагол `close` (старый `answer` — скрытый алиас) и новые флаги записи. Рендер тредов один на всех — `renderThread`.

**Tech Stack:** Go 1.2x, SQLite (modernc.org/sqlite), net/http ServeMux, spf13/cobra, тесты — стандартный `testing`, httptest-фейк демона (`internal/cli/client_helpers_test.go`), store-хелперы (`internal/store`).

## Global Constraints

- Пишущие ручки API уже готовы (T1/PR #43): `type`/`options` у открытия треда, `choose`/`dismiss`/`join`/`dry_run` у `reply`/`answer`, ответы несут `attention`/`type`/`options`/`local_ref`. Новых ПИШУЩИХ ручек не добавляем — только одна читающая (`GET /v1/threads`).
- Формат `local_ref` в API — `1023/Q2` / `cto/Q1` (уже смёржено, web/mobile его читают). Не меняем. Префикс доставки по брифу — `[#1023/Q2 reply from cto]` / `[cto/Q1 reply from human]`.
- Обратная совместимость CLI: `rocket task answer` и `rocket agent answer` работают как прежде (скрытые алиасы `close`); старые формы ссылок (`372`, `--task 799 Q1`, `799/Q1`, `cto/Q1`) принимаются.
- Гигиена ввода-вывода из #1025 сохраняется: человекочитаемое — в stdout через `cmd.Print*`, `--json` покрывает все поля нового вида, текст — через `textBody` (`--file`, `-` = stdin).
- Не трогаем `internal/heartbeat`, `internal/config`, `internal/monitor` — там параллельно работает T4.
- Комментарии в коде — на английском, сообщения коммитов — русские.
- `go build ./... && go vet ./... && go test ./...` зелёный.

---

## File Structure

- Modify: `internal/store/open_threads.go` — `ListThreads(includeResolved bool)`; `ListOpenThreads()` становится обёрткой.
- Modify: `internal/api/threads.go` — `threadPrefix` на локальных id.
- Create: `internal/api/thread_inbox.go` — `GET /v1/threads` (единый список).
- Create: `internal/api/thread_inbox_test.go`.
- Modify: `internal/api/server.go` — регистрация роутов инбокса.
- Create: `internal/cli/questions.go` — `rocket questions`, общий рендер тредов, общий разбор флагов записи.
- Create: `internal/cli/questions_test.go`.
- Modify: `internal/cli/question_ref.go` — `resolveThreadRef`: одна ссылка на оба вида тредов.
- Modify: `internal/cli/task.go` — поля `Type/Options/Attention/LocalRef` в `questionRow`; `ask --option/--fyi`; `close` + скрытый `answer`; `--choose/--dry-run/--join`; рендер через `renderThread`.
- Modify: `internal/cli/agent_questions.go` — то же для role-тредов.
- Modify: `internal/cli/root.go` — регистрация `newQuestionsCmd()`.
- Modify: `docs/04-cli.md` — минимальное описание изменённых команд.

---

### Task 1: store — треды вместе с закрытыми

**Files:** Modify `internal/store/open_threads.go`; Test `internal/store/open_threads_test.go`

**Interfaces (Produces):**
```go
func (s *Store) ListThreads(includeResolved bool) ([]OpenThread, error)
func (s *Store) ListOpenThreads() ([]OpenThread, error) // = ListThreads(false)
```
`OpenThread.Question` уже несёт `Type`/`Options`/`Status`/`Resolution`.

- [ ] Step 1: тест `TestListThreadsIncludesResolved` — два треда задачи, один закрыт: `ListOpenThreads()` даёт 1, `ListThreads(true)` — 2, у закрытого `Attention` пуст, `Participants` заполнены.
- [ ] Step 2: `go test ./internal/store -run ListThreads` → FAIL.
- [ ] Step 3: параметризовать три запроса (`questions`, `question_participants`, attention) по `includeResolved`.
- [ ] Step 4: PASS. Step 5: коммит.

### Task 2: API — `GET /v1/threads`

**Files:** Create `internal/api/thread_inbox.go`, `internal/api/thread_inbox_test.go`; Modify `internal/api/server.go`

**Interfaces (Produces):** ответ `{"threads":[{local_ref, kind:"task"|"role", task_id, role_id, subject, title, body, status, type, options, participants, attention, your_turn, asked_at, updated_at}]}`; query `?waiting_on=<id>&all=true`. Права чтения — существующий `canReadThread`; чужие треды просто выпадают из списка.

- [ ] Step 1: тесты: два открытых треда (задачи и роли) + один resolved → по умолчанию 2, `?all=true` → 3, `?waiting_on=human` → только те, где human в attention; `local_ref` = `1023/Q2` и `cto/Q1`; воркер чужой задачи её тред не видит.
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 3: локальные id в префиксах доставки

**Files:** Modify `internal/api/threads.go`; Test `internal/api/threads_test.go` (+ правка строк в остальных тестах/доках, где закреплён старый формат)

- [ ] Step 1: тест `TestThreadPrefixUsesLocalRef`: task-тред → `[#1023/Q2 reply from cto]`, role-тред → `[cto/Q2 reply from human]`.
- [ ] Step 2: `go test ./internal/api` → FAIL (и покажет остальные места со старым форматом).
- [ ] Step 3: переписать `threadPrefix`; поправить пины старого формата в тестах и в `docs/prompts/*`, `docs/12-tasks.md`, `docs/10-agents.md`.
- [ ] Step 4: PASS. Step 5: коммит.

### Task 4: локальные id и тип/опции в модели и рендере CLI

**Files:** Modify `internal/cli/task.go`, `internal/cli/agent_questions.go`; Test `internal/cli/task_test.go`

**Interfaces (Produces):** поля `questionRow.Type/Options/Attention/LocalRef` (и те же в `agentQuestionRow`); `func renderThread(sb *strings.Builder, q questionRow)`.

- [ ] Step 1: тест `TestRenderQuestionsPrintsLocalRef`: тред с `local_ref:"799/Q1"`, `options:["A","B"]` рендерится заголовком `799/Q1 [open] → ждут: cto` и строкой `  варианты: 1) A  2) B`; глобальный `#id` в человекочитаемом выводе не появляется.
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 5: единая ссылка на тред

**Files:** Modify `internal/cli/question_ref.go`; Test `internal/cli/question_ref_test.go`

**Interfaces (Produces):** `func resolveThreadRef(arg string, taskFlag int64) (kind string, globalID string, err error)`, kind — `"task"` | `"role"`: числовой scope → task, нечисловой → role, голый номер → task-глобальный, голый `Q1` → роль вызывающей сессии.

- [ ] Step 1: тесты на все шесть форм, включая usage-ошибку для `Q1` вне агентской сессии.
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 6: `rocket questions`

**Files:** Create `internal/cli/questions.go`, `internal/cli/questions_test.go`; Modify `internal/cli/root.go`

- [ ] Step 1: тест на фейковом демоне: `GET /v1/threads` отдаёт task- и role-тред → вывод содержит `1023/Q2`, `cto/Q1`, субъект, первую строку вопроса, участников, «ждут: human», возраст; `--waiting-on human` уходит в query; `--all` уходит в query; `--json` печатает массив как есть.
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 7: `close` (+ скрытый `answer`), `--choose`, `--dismiss`

**Files:** Modify `internal/cli/task.go`, `internal/cli/agent_questions.go`; Test соответствующие `_test.go`

- [ ] Step 1: тесты: `task close 799/Q1 "ok"` → POST `/v1/questions/{id}/answer` `{"body":"ok"}`; `--choose 2` → `{"choose":2}`; `--dismiss "почему"` → `{"dismiss":true,"body":"почему"}`; `--choose` вместе с телом → usage-ошибка; `agent close cto/Q1` → `/v1/agent-questions/{id}/answer`; `task answer` продолжает работать и скрыт из help (`Hidden: true`).
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 8: `ask --option` / `--fyi`

**Files:** Modify `internal/cli/task.go`, `internal/cli/agent_questions.go`

- [ ] Step 1: тесты: `--option A --option B` → `{"options":["A","B"]}`; `--fyi` → `{"type":"fyi"}`; `--fyi` вместе с `--option` → usage-ошибка; без новых флагов тело запроса прежнее (ключей `type`/`options` нет).
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 9: эхо цели, `--dry-run`, `--join`

**Files:** Modify `internal/cli/task.go`, `internal/cli/agent_questions.go`

- [ ] Step 1: тесты: `--dry-run` → `{"dry_run":true}` и вывод = строка `echo` сервера; `--join` → `{"join":true}`; при 403 `not_a_participant` печатается текст сервера как есть; после обычной записи печатается `echo`, если он пришёл.
- [ ] Step 2..5: FAIL → реализация → PASS → коммит.

### Task 10: документация и верификация

- [ ] `docs/04-cli.md`: `rocket questions`, `close`, `--choose/--dismiss/--option/--fyi/--dry-run/--join`, формы ссылок, новый префикс доставки.
- [ ] `go build ./... && go vet ./... && go test ./...`
- [ ] ручной прогон приёмки: `task ask 1 "?" --option A --option B --to cto` → `questions --waiting-on cto` → `task close 1/Q1 --choose 2`; `ask --fyi` виден только в `--all`.
- [ ] `gh pr create`
