# Раздел «Брейнштормы»: план реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Собрать брейнштормы, которые ждут человека (storm-вопросы на нём или pending-гейт «Go»), в отдельный раздел в вебе и мобилке и полностью убрать их из инбокса «Вопросы».

**Architecture:** Новый эндпоинт `GET /v1/brainstorms` (только для человека) собирает на сервере открытые storm-треды на человеке и pending-гейты. Задачи в `review`, `done` и `cancelled` в ответ не попадают. Веб и мобилка получают общий список и фильтруют его на клиенте. Обновление живое, через существующие SSE-события. Дыры в событиях смены статуса закрываются публикацией `task.status_changed` из PR-поллера и при отмене подзадачи.

**Tech Stack:** Go (net/http, SQLite store), React + react-query (web), Expo/React Native + react-query (mobile), Vitest (web), Jest (mobile).

**Spec:** `docs/superpowers/specs/2026-10-27-brainstorms-section-design.md`

## Global Constraints

- Скрытые статусы задач: `review`, `done`, `cancelled`. Любой другой (в том числе будущий) статус показывается.
- «Ждёт ответа» — open-вопрос `type = brainstorm`, у которого в `Attention` есть human. «Ждёт Go» — гейт со `status = 'pending'`.
- Порядок: сначала `questions_stale = true`, затем по `waiting_since` по возрастанию.
- `GET /v1/brainstorms` для сессии-агента отвечает 403 `forbidden`.
- Ничего не закрываем и не мутируем: только чтение и скрытие.
- Отвечать и жать Go в списке нельзя, строка ведёт по `stormHref` (milestone без проекта ведёт на `/milestones/:id`).
- Бейдж «Вопросов» (`inboxCount`) брейнштормы больше не считает. Бейдж раздела = длина списка `GET /v1/brainstorms`.

## Review Focus

1. Задачу перевели в Review, пока открыт экран раздела. Строка должна исчезнуть без перезагрузки (Task 3 и invalidation в Task 4/7).
2. Задача с pending-гейтом и без единого треда. Должна попасть в список: права не через `canReadThread` (Task 2, тест `TestBrainstorms_GateWithoutThreads`).
3. Storm-вопрос, где очередь сейчас у агента (человек ответил, агент уточняет). Не должен показываться (Task 2, тест `TestBrainstorms_QuestionOnAgentHidden`).
4. Гейт стал `superseded` (новая версия спеки), а новый не запрошен. Строка уходит из «Ждёт Go» (Task 2, тест `TestBrainstorms_DecidedOrSupersededGateHidden`).
5. Инбокс «Вопросы», где остались только storm-треды. Пустое состояние без упоминания storm, бейдж 0 (Task 6, Task 8).

---

### Task 1: Store — список pending-гейтов

**Files:**
- Modify: `internal/store/gates.go`
- Test: `internal/store/gates_test.go`

**Interfaces:**
- Produces: `func (s *Store) ListPendingGates() ([]TaskGate, error)` — все гейты со `status = 'pending'` по всем задачам, `ORDER BY requested_at, id`. Использовать `gateColumns`/`scanTaskGate`.

- [ ] **Step 1: Write the failing test** `TestListPendingGates`: две задачи со спекой. На первой `RequestTaskGate` и `DecideTaskGate(..., "go", ...)`, на второй `RequestTaskGate`. Ожидаем ровно один гейт, `TaskID` второй задачи, `Status == "pending"`. Ещё один кейс: повторный `RequestTaskGate` на той же задаче, в списке по-прежнему один гейт (старый стал superseded).
- [ ] **Step 2: Run** `go test ./internal/store -run TestListPendingGates`. Ожидаем FAIL (undefined).
- [ ] **Step 3: Implement** `ListPendingGates`.
- [ ] **Step 4: Run** `go test ./internal/store -run TestListPendingGates`. Ожидаем PASS.
- [ ] **Step 5: Commit** `feat: list pending gates across tasks`

### Task 2: API — `GET /v1/brainstorms`

**Files:**
- Create: `internal/api/brainstorms.go`
- Modify: `internal/api/server.go` (рядом с `registerGateRoutes`: `registerBrainstormRoutes(mux, d)`)
- Test: `internal/api/brainstorms_test.go`

**Interfaces:**
- Consumes: `Store.ListPendingGates()` (Task 1), `Store.ListThreads(false)`, `Store.GetTask`, `threadStale(d, q, lastMessage, attention)`, `callerSession`, `store.ParticipantHuman`/`store.IsHuman`, `store.QuestionTypeBrainstorm`.
- Produces: JSON `{"brainstorms": [brainstormEntry...]}`:

```go
type brainstormEntry struct {
	TaskID           int64         `json:"task_id"`
	ProjectID        string        `json:"project_id,omitempty"`
	TaskTitle        string        `json:"task_title"`
	TaskStatus       string        `json:"task_status"`
	QuestionsWaiting int           `json:"questions_waiting"`
	QuestionsStale   bool          `json:"questions_stale"`
	PendingGate      *pendingGate  `json:"pending_gate"` // null when none
	WaitingSince     int64         `json:"waiting_since"`
}
type pendingGate struct {
	ID          int64 `json:"id"`
	SpecVersion int64 `json:"spec_version"`
	RequestedAt int64 `json:"requested_at"`
}
var hiddenBrainstormStatuses = map[string]bool{"review": true, "done": true, "cancelled": true}
```

Логика: caller != nil → 403 `forbidden` "brainstorms are the human's list". Затем по `ListThreads(false)`: `type == brainstorm`, `TaskID != 0`, в `Attention` есть human (`store.IsHuman`). Копим по задаче: `QuestionsWaiting++`, `QuestionsStale ||= threadStale(...)`, `waiting_since = min(updatedAt)` (`updatedAt` = `LastMessage.CreatedAt` или `AskedAt`, как в инбоксе). По `ListPendingGates()` выставляем `PendingGate`, `waiting_since = min(..., RequestedAt)`. Задачи читаются один раз (кэш-мап), `ErrNotFound` пропускается, скрытые статусы отбрасываются. Сортировка по Global Constraints, на равенстве по `task_id`. Пустой результат — `[]`, не `null`.

- [ ] **Step 1: Write failing tests** (фикстура по образцу `newGateFixture` + `askBrainstorm`):
  - `TestBrainstorms_QuestionOnHuman`: storm-вопрос от orch-1 → 1 запись, `questions_waiting == 1`, `pending_gate == nil`, `task_status == "brainstorm"`.
  - `TestBrainstorms_QuestionOnAgentHidden`: storm-вопрос, человек отвечает репликой без закрытия, внимание уходит агенту (или вопрос задан человеком агенту) → 0 записей.
  - `TestBrainstorms_PendingGate`: спека + запрос гейта → `pending_gate.spec_version == 1`, `questions_waiting == 0`.
  - `TestBrainstorms_GateWithoutThreads`: задача без тредов с pending-гейтом → в списке.
  - `TestBrainstorms_InProgressGateShown`: задача `in_progress` с pending-гейтом → в списке.
  - `TestBrainstorms_DecidedOrSupersededGateHidden`: гейт `go` → 0 записей; гейт стал `superseded` после новой спеки → 0 записей.
  - `TestBrainstorms_HiddenStatuses`: table-тест по `review`, `done`, `cancelled` → и с вопросом, и с гейтом 0 записей.
  - `TestBrainstorms_BothOneEntry`: вопрос + гейт на одной задаче → одна запись с обоими полями, `waiting_since` = меньшее из двух.
  - `TestBrainstorms_Order`: stale-задача первой, дальше по `waiting_since` (stale делается через `d.Cfg.QuestionStaleAfter` = 1ns или правкой `asked_at` в БД, как в существующих stale-тестах).
  - `TestBrainstorms_AgentForbidden`: заголовок сессии `orch-1` → 403.
- [ ] **Step 2: Run** `go test ./internal/api -run TestBrainstorms`. Ожидаем FAIL.
- [ ] **Step 3: Implement** `brainstorms.go` и регистрацию маршрута.
- [ ] **Step 4: Run** `go test ./internal/api -run TestBrainstorms`, затем `go test ./...`. Ожидаем PASS.
- [ ] **Step 5: Commit** `feat: GET /v1/brainstorms lists storms waiting on the human`

### Task 3: События смены статуса вне API

**Files:**
- Modify: `internal/ghpoller/reactions.go` (`PROpened`, `Merged`)
- Modify: `internal/api/sessions.go:~282` (авто-отмена подзадачи)
- Test: `internal/ghpoller/reactions_test.go`, `internal/api/sessions_test.go`

**Interfaces:**
- Produces: событие `task.status_changed` с payload `{"task_id", "from", "to", "by"}`, тот же формат, что в `applyTaskStatusChange`. `by` = `"github"` для поллера, `callerLabel(caller)` в sessions.go. Session id события `task.SessionID`.

- [ ] **Step 1: Write failing tests:** `TestReactions_PROpenedPublishesStatusChanged` и `TestReactions_MergedPublishesStatusChanged` (подписка на `bus`, ожидание события с `to == "review"` / `"done"`). Для sessions.go добавить проверку события в существующий тест неудачного спавна с авто-созданной подзадачей (`to == "cancelled"`).
- [ ] **Step 2: Run** `go test ./internal/ghpoller ./internal/api -run 'StatusChanged|Spawn'`. Ожидаем FAIL.
- [ ] **Step 3: Implement** публикацию после успешного `UpdateTaskStatus` на всех трёх путях.
- [ ] **Step 4: Run** те же тесты и `go test ./...`. Ожидаем PASS.
- [ ] **Step 5: Commit** `fix: publish task.status_changed from the PR poller and spawn cancel`

### Task 4: Web — данные раздела, бейджи, invalidation

**Files:**
- Modify: `web/src/lib/types.ts` (тип `BrainstormEntry` = wire из Task 2)
- Modify: `web/src/lib/queries.ts` (`useBrainstorms`, `eventQueryKeys`)
- Create: `web/src/lib/brainstorms.ts`, `web/src/lib/brainstorms.test.ts`
- Modify: `web/src/lib/storm.ts` (`inboxCount`), `web/src/lib/storm.test.ts`
- Modify: `web/src/components/AppShell.tsx`, `web/src/components/AppShell.test.tsx`
- Modify: `web/src/mocks/handlers.ts`, `web/src/mocks/fixtures.ts` (мок `GET /v1/brainstorms`)

**Interfaces:**
- Produces:
  - `useBrainstorms(): UseQueryResult<BrainstormEntry[]>`, queryKey `['brainstorms']`.
  - `type BrainstormFilter = 'all' | 'answer' | 'go'`.
  - `filterBrainstorms(list: BrainstormEntry[], f: BrainstormFilter): BrainstormEntry[]` (`answer`: `questions_waiting > 0`; `go`: `pending_gate !== null`).
  - `brainstormCounts(list): { all: number; answer: number; go: number }`.
  - `isMidWorkSpecChange(e: BrainstormEntry): boolean`: `pending_gate !== null && (task_status === 'in_progress' || task_status === 'blocked')`.
  - `brainstormHref(e: BrainstormEntry): string`: та же логика, что `stormHref`.
  - `inboxCount(threads)` теперь возвращает только `threads.filter(t => waitingOnYou(t) && !isStorm(t)).length`.

- [ ] **Step 1: Write failing tests:** `brainstorms.test.ts`: фильтры, счётчики (задача с обоими попадает в оба), `isMidWorkSpecChange` для `in_progress`/`blocked`/`brainstorm`, href для milestone. `storm.test.ts`: `inboxCount` по списку из одного storm-треда на человеке и одного обычного возвращает 1. `AppShell.test.tsx`: у пункта Brainstorm бейдж с числом из мока `/v1/brainstorms` (2), бейдж Questions его не включает.
- [ ] **Step 2: Run** `cd web && npx vitest run src/lib/brainstorms.test.ts src/lib/storm.test.ts src/components/AppShell.test.tsx`. Ожидаем FAIL.
- [ ] **Step 3: Implement.** В `eventQueryKeys` добавить `['brainstorms']` к веткам `task.` и `pr.`. Бейдж Brainstorm того же вида, что у Questions.
- [ ] **Step 4: Run** тесты из Step 1. Ожидаем PASS.
- [ ] **Step 5: Commit** `feat(web): brainstorms query, nav badge, inbox count without storms`

### Task 5: Web — экран `/brainstorm`

**Files:**
- Create: `web/src/screens/brainstorm/BrainstormScreen.tsx`, `web/src/screens/brainstorm/BrainstormList.tsx`, `web/src/screens/brainstorm/brainstorms.css`, `web/src/screens/brainstorm/BrainstormList.test.tsx`
- Modify: `web/src/routes.tsx` (`/brainstorm` → `BrainstormScreen`)
- Keep: `BrainstormMetricsScreen.tsx`, рендерится вкладкой без изменений.

**Interfaces:**
- Consumes: всё из Task 4.
- `BrainstormScreen`: вкладки «Ждут меня» (по умолчанию) и «Метрики», выбор в `?tab=metrics` через `useSearchParams`.
- `BrainstormList`: переключатель «Всё / Ждёт ответа (N) / Ждёт Go (N)», строки как `<Link to={brainstormHref(e)}>`.

Строка: `#id title`, проект, статус задачи; «N вопрос(а/ов) ждут ответа» + тег `STALE` при `questions_stale`; «Ждёт Go · спека vN» и при `isMidWorkSpecChange` — «смена спеки в работе»; относительное время от `waiting_since` (существующий хелпер из `lib/format.ts`). Пустое состояние: «Брейнштормы тебя не ждут». Язык и стиль подписей — как на соседних экранах.

- [ ] **Step 1: Write failing tests** `BrainstormList.test.tsx` (на моках): три записи (только вопросы, только гейт в `in_progress`, оба) → по умолчанию 3 строки, «Ждёт ответа» → 2, «Ждёт Go» → 2. У гейт-строки текст «смена спеки в работе». Ссылка ведёт на `/p/<proj>/tasks/<id>?tab=brainstorm`. Пустой список → «Брейнштормы тебя не ждут». В `BrainstormMetrics.test.tsx` или новом тесте экрана: `?tab=metrics` рендерит метрики.
- [ ] **Step 2: Run** `cd web && npx vitest run src/screens/brainstorm`. Ожидаем FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `cd web && npx vitest run src/screens/brainstorm`. Ожидаем PASS.
- [ ] **Step 5: Commit** `feat(web): Brainstorms screen with waiting list and metrics tab`

### Task 6: Web — убрать storm из «Вопросов»

**Files:**
- Delete: `web/src/screens/questions/StormRows.tsx`
- Modify: `web/src/screens/questions/QuestionsScreen.tsx`, `BrowseMode.tsx`, `FocusMode.tsx`, `Questions.test.tsx`
- Modify: `web/src/lib/storm.ts`: удалить `stormGroups`, `stormLabel`, `stormMatches`, если после правок их никто не использует. Оставить `isStorm`, `stormsByTask`, `stormHref`, `inboxCount`.

Что убрать: проп `storms`/`currentStormId`, `stormId`-курсор и ветку storm в J/K и Enter/цифрах, `StormCard`, группу «Storms» в Browse, вклад storm в счётчики фильтров и `onYou`, текст «Only storm questions are left…». Фильтр `!isStorm(t)` в списке тредов остаётся.

- [ ] **Step 1: Update tests** в `Questions.test.tsx`: заменить проверки storm-строк на «storm-тред на человеке не даёт ни строки, ни карточки, ни счётчика». Если на человеке только storm-треды, показывается обычное пустое состояние инбокса.
- [ ] **Step 2: Run** `cd web && npx vitest run src/screens/questions`. Ожидаем FAIL.
- [ ] **Step 3: Implement** удаление.
- [ ] **Step 4: Run** `cd web && npx vitest run && npx tsc --noEmit`, затем lint проекта (`npm run lint`, если есть). Ожидаем PASS.
- [ ] **Step 5: Commit** `feat(web): drop storm rows from the Questions inbox`

### Task 7: Mobile — вкладка «Storms»

**Files:**
- Modify: `mobile/src/api/types.ts` (`BrainstormEntry`), `mobile/src/api/queries.ts` (`useBrainstorms`, queryKey `[baseUrl, 'brainstorms']`, `refetchInterval: usePoll(10000)` как у `useThreads`)
- Modify: `mobile/src/api/events.ts`: `'brainstorms'` в ветки `task` и `pr` у `parseEventType`; `mobile/src/api/events.test.tsx`
- Create: `mobile/src/lib/brainstorms.ts` + `brainstorms.test.ts` (те же функции и сигнатуры, что в Task 4, mobile-версия `stormHref`)
- Create: `mobile/app/(tabs)/storms.tsx`
- Modify: `mobile/app/(tabs)/_layout.tsx`: `Tabs.Screen name="storms"` сразу после `questions`, заголовок «Storms», иконка на View (молния или облачко), бейдж = `useBrainstorms().data?.length` тем же способом, что у questions.

**Interfaces:**
- Consumes: wire из Task 2.

Экран: фильтр-чипы «Всё / Ждёт ответа (N) / Ждёт Go (N)», карточки (`Card`) с тем же содержанием, что веб-строка. Тап → `router.navigate(brainstormHref(e))`. Пустое состояние «Брейнштормы тебя не ждут». Pull-to-refresh как в `questions.tsx`.

- [ ] **Step 1: Write failing tests:** `brainstorms.test.ts` (как в Task 4). `events.test.tsx`: `parseEventType('task.gate_requested')` и `parseEventType('pr.merged')` содержат `'brainstorms'`. Тест экрана в `mobile/__tests__/` по образцу существующих тестов вкладок: фильтр «Ждёт Go» оставляет только записи с гейтом.
- [ ] **Step 2: Run** `cd mobile && npx jest brainstorms events storms`. Ожидаем FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `cd mobile && npx jest && npx tsc --noEmit`. Ожидаем PASS.
- [ ] **Step 5: Commit** `feat(mobile): Storms tab with brainstorms waiting on you`

### Task 8: Mobile — убрать storm из «Вопросов»

**Files:**
- Modify: `mobile/app/(tabs)/questions.tsx` (убрать `storms`, их строки, стили `stormRow`/`stormText`/`stormHint`, вклад в `visible`)
- Modify: `mobile/src/lib/storm.ts` (`inboxCount` без storm-групп; удалить `stormGroups`/`stormLabel`, если больше не используются), `mobile/src/lib/storm.test.ts`
- Modify: `mobile/app/(tabs)/_layout.tsx` (комментарий у бейджа questions)

- [ ] **Step 1: Update tests:** `storm.test.ts`: `inboxCount` не считает storm-треды. Тест вкладки «Вопросы» (если есть): storm-тред на человеке не рендерит строку.
- [ ] **Step 2: Run** `cd mobile && npx jest storm questions`. Ожидаем FAIL.
- [ ] **Step 3: Implement** удаление.
- [ ] **Step 4: Run** `cd mobile && npx jest && npx tsc --noEmit`. Ожидаем PASS.
- [ ] **Step 5: Commit** `feat(mobile): drop storm rows from the Questions tab`

### Task 9: Финальная проверка

- [ ] **Step 1:** `go test ./... && (cd web && npx vitest run && npx tsc --noEmit) && (cd mobile && npx jest && npx tsc --noEmit)`. Ожидаем, что всё зелёное.
- [ ] **Step 2:** Запустить демон и веб (скилл `run`), создать задачу со storm-вопросом и гейтом. Проверить, что она в разделе и её нет в «Вопросах». Перевести задачу в `review`: строка исчезает без перезагрузки.
