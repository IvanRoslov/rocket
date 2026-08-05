# T5 web-questions: дашборд и mobile для редизайна вопросных тредов

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development или
> superpowers:executing-plans. Шаги помечены чекбоксами (`- [ ]`).

**Goal:** показать в web (`web/`) и mobile (`mobile/`) новую модель вопросных тредов задачи
#1023: кнопки-варианты, закрывающие тред через `choose`; бейдж stale; единый экран Questions
поверх `GET /v1/threads` с фильтром «ждут меня»; бейджи из attention; fyi без бейджей;
локальные id (`1023/Q2`, `cto/Q1`) как единственный видимый id треда.

**Architecture:** сервер отдаёт почти всё нужное; трёх полей в едином инбоксе не хватает —
они добавляются строго аддитивно (Task 0, скоуп расширен решением оркестратора). Веб: расширяем TS-типы под новые поля ответов, добавляем `options`/`stale`/
`local_ref` в общий `QuestionThreadView` (его переиспользуют треды задач и ролей), переписываем
`QuestionsScreen` на `GET /v1/threads` с ленивой подгрузкой полного треда при раскрытии строки.
Mobile — минимальное зеркало: те же поля в типах, бейджи `local_ref`/`type`/`stale` в карточке
треда и варианты как нажимаемые кнопки.

**Tech Stack:** web — React 19 + TanStack Query + react-router + vitest/@testing-library + msw
(моки в `web/src/mocks/handlers.ts`). mobile — Expo Router + React Native + jest-expo +
@testing-library/react-native.

## Global Constraints

- **В `internal/` трогаем ровно один файл** — `internal/api/thread_inbox.go` (+ его тест),
  ровно три аддитивных поля (Task 0). Всё остальное серверное — вне скоупа.
- **Никакого клиентского пересчёта staleness** — порог `question_stale_after` конфигурируемый,
  клиент его не знает; читаем поле `stale`.
- **Никаких правок в `docs/*.md`** — доки ведёт T6 (#1029). Этот план в
  `docs/superpowers/plans/` — исключение, он про процесс, а не про продуктовые доки.
- Контрактные поля читаем как отдают (`internal/api/questions.go` `questionResponse`,
  `internal/api/agent_questions.go` `agentQuestionResponse`, `internal/api/thread_inbox.go`
  `threadInboxEntry`); никакого клиентского вывода «чей ход» — только `your_turn`/`attention`.
- Человека в тредах опознаём только через `isHuman()` (`web/src/lib/participants.ts`,
  `mobile/src/lib/threads.ts`) — на проводе он и `""`, и `"human"`.
- `choose` в API — **1-based** индекс варианта (`internal/api/threads.go:281`
  `chooseOptionBody`).
- Тип треда: `decision` (по умолчанию) | `fyi`. fyi приходит уже `status:"resolved"`,
  `resolution:"fyi"`, с пустым attention — бейджей не зажигает и в счётчики не попадает
  (сервер уже исключает: `internal/api/threads.go:150` `threadCounts` считает только открытые).
- Локальный id (`local_ref`) — то, что видит человек. `Q{ordinal}` остаётся фолбэком, если
  демон старый и `local_ref` не прислал.
- Команды проверки: `cd web && npm run build && npm test`; `cd mobile && npm run typecheck && npm test`.

## Разрешённое расхождение

Бриф утверждал, что `GET /v1/threads` отдаёт `stale`. Код на `origin/main` (86e306d) этого не
подтверждает: `threadInboxEntry` (`internal/api/thread_inbox.go:18-49`) поля не имеет, `stale`
живёт только в per-thread ответах (`questions.go:79`, `agent_questions.go:40`). Оркестратор
подтвердил ошибку брифа и расширил скоуп: поля добавляются здесь (Task 0).

---

## Файловая структура

Server (ровно один файл):
- `internal/api/thread_inbox.go` — `Stale`, `ProjectID`, `TaskTitle` в `threadInboxEntry`.
- `internal/api/thread_inbox_test.go` — их тесты.

Web:
- `web/src/lib/types.ts` — новые поля `Question`/`AgentQuestion`, новый тип `ThreadInboxEntry`.
- `web/src/lib/queries.ts` — `useThreads()`; `choose` в `useAnswerQuestion`/`useAnswerAgentQuestion`.
- `web/src/components/QuestionThreadView.tsx` — заголовок (local_ref + stale), кнопки вариантов.
- `web/src/components/questionthread.css` — стили stale-бейджа и кнопок вариантов.
- `web/src/components/QuestionThread.tsx`, `AgentQuestionThread.tsx` — проброс новых пропов.
- `web/src/screens/task/QuestionBanner.tsx` (+ `.css`) — stale-бейдж, кнопки вариантов, local_ref.
- `web/src/screens/task/QuestionsTab.tsx` — fyi-строки в «Resolved» без бейджей.
- `web/src/screens/questions/QuestionsScreen.tsx` (+ `.css`) — единый список поверх `/v1/threads`.
- `web/src/mocks/handlers.ts`, `web/src/mocks/fixtures.ts` — мок `/v1/threads`, `choose`, fyi, stale.
- Тесты: `web/src/components/QuestionThreadView.test.tsx`, `web/src/screens/questions/Questions.test.tsx`,
  `web/src/screens/task/Task.test.tsx`.

Mobile:
- `mobile/src/api/types.ts` — новые поля `Question`/`AgentQuestion`.
- `mobile/src/lib/threads.ts` — `threadRefLabel()`, `threadBadges()` (чистые, тестируемые).
- `mobile/src/lib/threads.test.ts` — их тесты.
- `mobile/src/api/queries.ts` — `choose` в мутациях ответа (task и role).
- `mobile/app/task/[id].tsx`, `mobile/app/agent/[id].tsx` — бейджи и кнопки вариантов.

---

### Task 0: три аддитивных поля в едином инбоксе (server)

**Files:**
- Modify: `internal/api/thread_inbox.go` (структура `threadInboxEntry:18-49`, сборка entry `:129-150`)
- Test: `internal/api/thread_inbox_test.go`

**Interfaces:**
- Consumes: `threadStale(d, q, lastMessage, attention) bool` (`internal/api/threads.go:418`),
  `store.OpenThread{Question, LastMessage, Attention}`.
- Produces: в JSON `GET /v1/threads` появляются `stale`, `project_id`, `task_title` (все
  `omitempty`). Их читают Tasks 1 и 5.

- [ ] **Step 1: написать падающие тесты**

В `internal/api/thread_inbox_test.go` (стиль файла: поднять `srv`, дёрнуть `getThreads(t, srv, "")`):

```go
// TestThreadInboxStale: единый инбокс — единственный экран, где человек видит
// все треды сразу, поэтому «висит больше суток» должно быть видно прямо в нём,
// а не только при открытии треда. Порог конфигурируемый, клиент его не знает —
// признак считает демон.
func TestThreadInboxStale(t *testing.T) {
	// тред задачи, вопрос задан 30 часов назад, attention непустой
	// → threads[0].Stale == true
}

// TestThreadInboxTaskContext: дашборд линкует строку инбокса на страницу задачи
// (/p/<project>/tasks/<id>), а subject — человекочитаемая строка, парсить её
// клиенту нельзя. Поэтому проект и заголовок едут отдельными полями.
func TestThreadInboxTaskContext(t *testing.T) {
	// тред задачи → ProjectID == задачин проект, TaskTitle == её title
	// тред роли   → оба поля пустые
}
```

- [ ] **Step 2: убедиться, что падают**

Run: `go test ./internal/api/ -run TestThreadInbox -v`
Expected: FAIL — полей нет (не компилируется).

- [ ] **Step 3: реализовать**

В `threadInboxEntry` после `ResolvedAt`:

```go
	// Stale mirrors questionResponse.Stale: an open decision thread nobody has
	// moved for longer than question_stale_after. The inbox is the one screen
	// that shows every thread at once, so the badge has to be readable without
	// opening each thread.
	Stale bool `json:"stale,omitempty"`
	// ProjectID and TaskTitle give a task thread the context the per-task
	// endpoints get for free from their URL: the dashboard links a row to
	// /p/<project>/tasks/<id> and must not parse it out of Subject. Both are
	// empty for a role thread, which hangs off no project.
	ProjectID string `json:"project_id,omitempty"`
	TaskTitle string `json:"task_title,omitempty"`
```

В цикле сборки: `title` уже вычислен для тредов задач (`title = task.Title`), рядом завести
`projectID` (из `task.ProjectID`, пусто для роли) и в литерал entry добавить:

```go
			Stale:     threadStale(d, q, th.LastMessage, th.Attention),
			ProjectID: projectID,
			TaskTitle: title,
```

- [ ] **Step 4: тесты зелёные**

Run: `go test ./internal/api/ && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/thread_inbox.go internal/api/thread_inbox_test.go
git commit -m "api: stale, project_id и task_title в едином инбоксе тредов"
```

---

### Task 1: TS-типы новой модели треда (web)

**Files:**
- Modify: `web/src/lib/types.ts` (`Question` ~354-376, `AgentQuestion`)
- Test: типы проверяются компиляцией — отдельного теста нет, гейт `npm run build`

**Interfaces:**
- Produces: `Question.local_ref?: string`, `Question.type?: ThreadType`, `Question.options?: string[]`,
  `Question.stale?: boolean`, `Question.attention?: string[]`; `export type ThreadType = 'decision' | 'fyi'`;
  `export type QuestionResolution = 'answered' | 'dismissed' | 'fyi'`;
  `export interface ThreadInboxEntry` (см. ниже) — их читают Tasks 2-6.

- [ ] **Step 1: расширить `Question` и `AgentQuestion`**

В `web/src/lib/types.ts`, рядом с `QuestionStatus`:

```ts
/** `questions.type` — internal/api/threads.go normalizeThreadType. */
export type ThreadType = 'decision' | 'fyi'
```

В `QuestionResolution` добавить `'fyi'` (fyi-тред создаётся сразу resolved с этой резолюцией).

В `Question` (и точно так же в `AgentQuestion`):

```ts
  /** Локальный id треда — "1023/Q2" у задачи, "cto/Q1" у роли. Единственный id для человека. */
  local_ref?: string
  /** decision (по умолчанию) | fyi. Отсутствует у демона до задачи #1023. */
  type?: ThreadType
  /** Варианты ответа: выбор варианта закрывает тред (choose — 1-based индекс). */
  options?: string[]
  /** Открытый decision-тред без движения дольше question_stale_after. */
  stale?: boolean
  /** Хранимое множество «чей ход»; `waiting_on` — то же самое под старым именем. */
  attention?: string[]
```

- [ ] **Step 2: добавить `ThreadInboxEntry`**

Ниже `GlobalQuestion`:

```ts
/**
 * Одна строка `GET /v1/threads` — `threadInboxEntry` из
 * internal/api/thread_inbox.go. Несёт только текст вопроса, без переписки:
 * полный тред читается per-subject эндпоинтом при раскрытии строки.
 */
export interface ThreadInboxEntry {
  local_ref: string
  kind: 'task' | 'role'
  task_id?: number
  role_id?: string
  /** Человекочитаемый субъект: `task #1023 "Ship it"` или `role cto`. */
  subject: string
  id: number
  ordinal: number
  asked_by: string
  body: string
  status: QuestionStatus
  resolution?: QuestionResolution
  type: ThreadType
  options?: string[]
  participants: string[]
  attention: string[]
  waiting_on: string[]
  your_turn: boolean
  asked_at: number
  updated_at: number
  resolved_at?: number
  /** Открытый decision-тред без движения дольше question_stale_after (Task 0). */
  stale?: boolean
  /** Только у тредов задач: контекст для ссылки на /p/<project>/tasks/<id> (Task 0). */
  project_id?: string
  task_title?: string
}
```

- [ ] **Step 3: проверить сборку**

Run: `cd web && npm run build`
Expected: PASS (типы аддитивные, ничего не ломают).

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/types.ts
git commit -m "web: типы новой модели тредов — local_ref, type, options, stale, attention"
```

---

### Task 2: `choose` в мутациях ответа (web)

**Files:**
- Modify: `web/src/lib/queries.ts:483-505` (`useAnswerQuestion`), `:839-859` (`useAnswerAgentQuestion`)
- Test: `web/src/lib/queries.test.tsx`

**Interfaces:**
- Consumes: типы Task 1.
- Produces: `useAnswerQuestion().mutate({ id, taskId, choose: 2 })` и
  `useAnswerAgentQuestion().mutate({ id, roleId, choose: 2 })` — третий вариант объединения
  рядом с `body` и `dismiss`. Их вызывают Tasks 3 и 4.

- [ ] **Step 1: написать падающий тест**

В `web/src/lib/queries.test.tsx` (следовать стилю существующих тестов файла — msw-хендлер,
`renderHook` с `QueryClientProvider`):

```tsx
it('useAnswerQuestion sends choose as a 1-based index', async () => {
  let sent: unknown
  server.use(
    http.post('http://localhost/v1/questions/7/answer', async ({ request }) => {
      sent = await request.json()
      return HttpResponse.json({ id: 7 })
    }),
  )
  const { result } = renderHook(() => useAnswerQuestion(), { wrapper })
  result.current.mutate({ id: 7, taskId: 1, choose: 2 })
  await waitFor(() => expect(result.current.isSuccess).toBe(true))
  expect(sent).toEqual({ choose: 2 })
})
```

- [ ] **Step 2: убедиться, что тест падает**

Run: `cd web && npx vitest run src/lib/queries.test.tsx -t "choose"`
Expected: FAIL — тип не принимает `choose`, тело запроса не то.

- [ ] **Step 3: реализовать**

`useAnswerQuestion` — расширить объединение и `mutationFn`:

```ts
  { id: number; taskId: number; to?: string[] } & (
    | { body: string; dismiss?: never; choose?: never }
    | { dismiss: true; body?: never; choose?: never }
    | { choose: number; body?: never; dismiss?: never }
  )
```

```ts
    mutationFn: ({ id, body, dismiss, choose, to }) =>
      api.post<Question>(
        `/v1/questions/${id}/answer`,
        // choose — 1-based индекс варианта; сервер сам подставляет текст
        // варианта в резолюцию (internal/api/threads.go chooseOptionBody).
        // Выбор варианта закрывает тред, отвечать больше некому — `to` не шлём.
        dismiss ? { dismiss: true } : choose ? { choose } : withTo({ body }, to),
      ),
```

То же самое в `useAnswerAgentQuestion` для `/v1/agent-questions/${id}/answer`.

- [ ] **Step 4: тесты зелёные**

Run: `cd web && npx vitest run src/lib/queries.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/queries.ts web/src/lib/queries.test.tsx
git commit -m "web: choose в мутациях закрытия треда (задача и роль)"
```

---

### Task 3: кнопки вариантов, local_ref и stale в `QuestionThreadView`

**Files:**
- Modify: `web/src/components/QuestionThreadView.tsx`, `web/src/components/questionthread.css`,
  `web/src/components/QuestionThread.tsx`, `web/src/components/AgentQuestionThread.tsx`
- Test: `web/src/components/QuestionThreadView.test.tsx`

**Interfaces:**
- Consumes: `useAnswerQuestion`/`useAnswerAgentQuestion` с `choose` (Task 2).
- Produces: у `QuestionThreadViewProps` новые необязательные поля
  `localRef?: string`, `options?: string[]`, `stale?: boolean`,
  `onChoose?: (choose: number) => void` — их выставляют `QuestionThread` и `AgentQuestionThread`.

- [ ] **Step 1: написать падающие тесты**

В `web/src/components/QuestionThreadView.test.tsx` (стиль файла: `render(<QuestionThreadView ... />)`
с полным набором обязательных пропов — скопировать существующий хелпер):

```tsx
it('renders options as buttons and closes the thread with a 1-based choose', async () => {
  const onChoose = vi.fn()
  render(<QuestionThreadView {...base} options={['Ship it', 'Wait']} onChoose={onChoose} />)
  await userEvent.click(screen.getByRole('button', { name: 'Wait' }))
  expect(onChoose).toHaveBeenCalledWith(2)
})

it('shows the local ref instead of the bare ordinal', () => {
  render(<QuestionThreadView {...base} localRef="1023/Q2" />)
  expect(screen.getByText('1023/Q2')).toBeInTheDocument()
})

it('falls back to Q<ordinal> when the daemon sends no local ref', () => {
  render(<QuestionThreadView {...base} ordinal={2} />)
  expect(screen.getByText('Q2')).toBeInTheDocument()
})

it('badges a stale thread', () => {
  render(<QuestionThreadView {...base} stale />)
  expect(screen.getByText('stale')).toBeInTheDocument()
})
```

- [ ] **Step 2: убедиться, что падают**

Run: `cd web && npx vitest run src/components/QuestionThreadView.test.tsx`
Expected: FAIL — пропы неизвестны, элементов нет.

- [ ] **Step 3: реализовать**

В `QuestionThreadViewProps` добавить:

```ts
  /** Локальный id треда ("1023/Q2"); без него показываем Q<ordinal>. */
  localRef?: string
  /** Варианты ответа: кнопка = закрытие треда выбором. */
  options?: string[]
  /** Открытый тред без движения дольше порога — жёлтый бейдж «stale». */
  stale?: boolean
  /** Закрыть тред выбором варианта; `choose` — 1-based индекс. */
  onChoose?: (choose: number) => void
```

В шапке заменить тег и добавить бейдж:

```tsx
        <span className="question-thread__tag">{localRef ?? `Q${ordinal}`}</span>
        {stale && (
          <span className="question-thread__stale" title="No movement for over a day">
            stale
          </span>
        )}
```

Над `question-thread__form` (после `__messages`) — ряд вариантов; рендерим только когда есть
и варианты, и обработчик:

```tsx
        {options && options.length > 0 && onChoose && (
          <div className="question-thread__options" aria-label="Answer options">
            {options.map((label, i) => (
              <button
                key={label}
                type="button"
                className="question-thread__option"
                onClick={() => onChoose(i + 1)}
                disabled={busy}
              >
                {label}
              </button>
            ))}
            <span className="question-thread__options-hint">
              Picking an option closes the thread with that answer.
            </span>
          </div>
        )}
```

`aria-label` формы ответа тоже завязать на локальный id:
`aria-label={`Reply to ${localRef ?? `Q${ordinal}`}`}`.

В `questionthread.css` — стили по образцу соседних правил (переменные из
`web/src/styles/tokens.css`): `.question-thread__stale` жёлтым (тон как у `--warn`),
`.question-thread__options` — flex-ряд с отступом, `.question-thread__option` — кнопка
в тон «Answer & close», `.question-thread__options-hint` — мелкий приглушённый текст.

В `QuestionThread.tsx` пробросить:

```tsx
      localRef={question.local_ref}
      options={question.options}
      stale={question.stale}
      onChoose={(choose) => answer.mutate({ id: question.id, choose, taskId })}
```

В `AgentQuestionThread.tsx` — то же, но `roleId` вместо `taskId`.

- [ ] **Step 4: тесты зелёные**

Run: `cd web && npx vitest run src/components/ && npm run build`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/components/
git commit -m "web: кнопки вариантов, локальный id и stale-бейдж в треде"
```

---

### Task 4: баннер вопроса на карточке задачи — варианты, stale, local_ref

**Files:**
- Modify: `web/src/screens/task/QuestionBanner.tsx`, `web/src/screens/task/QuestionBanner.css`,
  `web/src/screens/task/TaskScreen.tsx` (место вызова баннера)
- Test: `web/src/screens/task/Task.test.tsx`

**Interfaces:**
- Consumes: `useAnswerQuestion` с `choose` (Task 2), типы Task 1.
- Produces: `QuestionBannerProps` получает `taskId: number`; поведение — клик по варианту
  закрывает тред, клик по остальному баннеру ведёт на вкладку Questions.

- [ ] **Step 1: написать падающие тесты**

В `web/src/screens/task/Task.test.tsx` (фикстуры — `web/src/mocks/fixtures.ts`; добавить туда
задачу с открытым тредом, у которого `options: ['Ship it', 'Wait']`, `stale: true`,
`local_ref: '<id>/Q1'`):

```tsx
it('closes the thread from the banner when an option is clicked', async () => {
  renderTask(TASK_WITH_OPTIONS)
  await userEvent.click(await screen.findByRole('button', { name: 'Ship it' }))
  await waitFor(() => expect(screen.queryByText('? awaiting you')).not.toBeInTheDocument())
})

it('badges a stale thread on the banner', async () => {
  renderTask(TASK_WITH_OPTIONS)
  expect(await screen.findByText('stale')).toBeInTheDocument()
})
```

- [ ] **Step 2: убедиться, что падают**

Run: `cd web && npx vitest run src/screens/task/Task.test.tsx`
Expected: FAIL

- [ ] **Step 3: реализовать**

Баннер перестаёт быть одной большой `<button>` (кнопка в кнопке недопустима) — становится
`<div>` с кликабельной областью-кнопкой и отдельным рядом вариантов:

```tsx
export interface QuestionBannerProps {
  taskId: number
  question: Question
  onOpen: () => void
}

export function QuestionBanner({ taskId, question, onOpen }: QuestionBannerProps) {
  const answer = useAnswerQuestion()
  const awaitingUser = question.your_turn === true
  const classes = [
    'question-banner',
    awaitingUser ? 'question-banner--warn' : 'question-banner--neutral',
  ]

  return (
    <div className={classes.join(' ')}>
      <button type="button" className="question-banner__main" onClick={onOpen}>
        <span className="question-banner__tag">
          {awaitingUser ? '? awaiting you' : '? awaiting others'}
        </span>
        <span className="question-banner__ordinal">
          {question.local_ref ?? `Q${question.ordinal}`}
        </span>
        {question.stale && <span className="question-banner__stale">stale</span>}
        <span className="question-banner__text">{question.body}</span>
        <span className="question-banner__cta">Open thread →</span>
      </button>
      {(question.options ?? []).length > 0 && (
        <div className="question-banner__options" aria-label="Answer options">
          {question.options!.map((label, i) => (
            <button
              key={label}
              type="button"
              className="question-banner__option"
              disabled={answer.isPending}
              onClick={() => answer.mutate({ id: question.id, choose: i + 1, taskId })}
            >
              {label}
            </button>
          ))}
        </div>
      )}
      {question.stale && (
        <button type="button" className="question-banner__close" onClick={onOpen}>
          Close with a resolution →
        </button>
      )}
    </div>
  )
}
```

(«Close with a resolution» ведёт в тред, где composer с «Answer &amp; close» — закрытие с
резолюцией требует текста, придумывать его за человека нельзя.)

В `TaskScreen.tsx` добавить в вызов `taskId={task.id}`.
В `QuestionBanner.css` перенести существующее оформление кнопки на `.question-banner__main`,
добавить `.question-banner__options` / `.question-banner__option` / `.question-banner__stale` /
`.question-banner__close`.

- [ ] **Step 4: тесты зелёные**

Run: `cd web && npx vitest run src/screens/task/ && npm run build`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/screens/task/ web/src/mocks/
git commit -m "web: баннер вопроса — кнопки вариантов, stale и локальный id"
```

---

### Task 5: экран Questions поверх `GET /v1/threads`

**Files:**
- Modify: `web/src/lib/queries.ts` (новый `useThreads`),
  `web/src/screens/questions/QuestionsScreen.tsx`, `web/src/screens/questions/QuestionsScreen.css`,
  `web/src/mocks/handlers.ts` (хендлер `GET /v1/threads`)
- Test: `web/src/screens/questions/Questions.test.tsx`

**Interfaces:**
- Consumes: `ThreadInboxEntry` (Task 1), `QuestionThread`/`AgentQuestionThread` (Task 3).
- Produces: `useThreads(opts?: { all?: boolean }): UseQueryResult<ThreadInboxEntry[]>`.

- [ ] **Step 1: написать падающие тесты**

В `web/src/screens/questions/Questions.test.tsx`:

```tsx
it('lists task and role threads in one list with their local refs', async () => {
  renderQuestions()
  expect(await screen.findByText('1023/Q2')).toBeInTheDocument()
  expect(await screen.findByText('cto/Q1')).toBeInTheDocument()
})

it('"waiting on me" hides threads waiting on somebody else', async () => {
  renderQuestions()
  await screen.findByText('cto/Q1')
  await userEvent.click(screen.getByLabelText('Waiting on me'))
  expect(screen.queryByText('cto/Q1')).not.toBeInTheDocument()
  expect(screen.getByText('1023/Q2')).toBeInTheDocument()
})

it('badges a stale thread', async () => {
  renderQuestions()
  expect(await screen.findByText('stale')).toBeInTheDocument()
})

it('shows fyi threads only in history and without a turn badge', async () => {
  renderQuestions()
  await userEvent.click(screen.getByLabelText('Show resolved'))
  const row = (await screen.findByText('cto/Q3')).closest('.questions-screen__row')!
  expect(within(row as HTMLElement).getByText('fyi')).toBeInTheDocument()
  expect(within(row as HTMLElement).queryByText(/awaiting/)).not.toBeInTheDocument()
})

it('opens the full thread when a row is expanded', async () => {
  renderQuestions()
  await userEvent.click(await screen.findByRole('button', { name: /1023\/Q2/ }))
  expect(await screen.findByLabelText('Discussion')).toBeInTheDocument()
})
```

Фикстуры в `web/src/mocks/handlers.ts`: `GET /v1/threads` возвращает
`{threads: [...]}` — минимум четыре записи: тред задачи `your_turn: true` со `stale: true`,
тред роли `your_turn: false`, fyi-тред роли (`status: 'resolved'`, `type: 'fyi'`,
`attention: []`), и тред задачи с `options`. Хендлер уважает `?all=true` (без него не отдаёт
resolved) и `?waiting_on=`.

- [ ] **Step 2: убедиться, что падают**

Run: `cd web && npx vitest run src/screens/questions/`
Expected: FAIL

- [ ] **Step 3: реализовать `useThreads`**

В `web/src/lib/queries.ts` рядом с `useOpenQuestions`:

```ts
/**
 * `GET /v1/threads` (internal/api/thread_inbox.go): единый инбокс тредов —
 * задач и ролей вместе, свежие субъекты первыми. Несёт только текст вопроса;
 * переписка догружается per-subject эндпоинтом при раскрытии строки.
 * `all` включает resolved (в том числе fyi).
 */
export function useThreads(opts?: { all?: boolean }): UseQueryResult<ThreadInboxEntry[]> {
  const all = opts?.all ?? false
  return useQuery({
    queryKey: ['threads', { all }],
    queryFn: async () => {
      const res = await api.get<{ threads: ThreadInboxEntry[] }>(
        all ? '/v1/threads?all=true' : '/v1/threads',
      )
      return res.threads
    },
  })
}
```

Фильтр «ждут меня» делаем на клиенте по `your_turn` (caller-relative поле, уже посчитанное
демоном) — так переключатель не гоняет новый запрос и не расходится с бейджами.

Инвалидация: в `onSuccess` мутаций ответа/реплики (`useAnswerQuestion`, `useReplyQuestion`,
`useAnswerAgentQuestion`, `useReplyAgentQuestion`) добавить
`queryClient.invalidateQueries({ queryKey: ['threads'] })`, иначе закрытый из списка тред
останется на экране.

- [ ] **Step 4: переписать `QuestionsScreen`**

Экран — список строк; строка раскрывается в полный тред:

```tsx
export function QuestionsScreen() {
  const [onlyMine, setOnlyMine] = useState(false)
  const [showResolved, setShowResolved] = useState(false)
  const { data: threads, isLoading } = useThreads({ all: showResolved })

  const rows = (threads ?? []).filter((t) => (onlyMine ? t.your_turn : true))
  const open = rows.filter((t) => t.status === 'open')
  const history = rows.filter((t) => t.status !== 'open')
  const awaitingYou = open.filter((t) => t.your_turn)
  const awaitingOthers = open.filter((t) => !t.your_turn)
  ...
}
```

Строка (`ThreadRow`) показывает: `local_ref`, `subject`, первую строку `body`, бейджи —
`stale` (жёлтый, только если `entry.stale` или подмешанный stale, см. ниже), `fyi` (нейтральный,
только для `type === 'fyi'`), «awaiting you» / «awaiting <ids>» (для fyi и resolved — не
показываем вовсе), возраст `timeAgo(entry.updated_at)`. Раскрытие (`aria-expanded`) рендерит
полный тред:

```tsx
{expanded && entry.kind === 'task' && entry.task_id !== undefined && (
  <TaskThreadDetail taskId={entry.task_id} questionId={entry.id} />
)}
{expanded && entry.kind === 'role' && entry.role_id && (
  <RoleThreadDetail roleId={entry.role_id} questionId={entry.id} />
)}
```

`TaskThreadDetail` берёт `useTaskQuestions(taskId)` и находит вопрос по `id`, рендерит
`<QuestionThread taskId={taskId} question={q} />`; `RoleThreadDetail` — то же через
`useAgentQuestions(roleId)` и `<AgentQuestionThread roleId={roleId} question={q} />`.
Пока грузится — «Loading…». Так экран получает переписку, `context` и `stale` без новых
серверных полей.

Stale в строке — прямо из `entry.stale` (Task 0 его добавил). Никакого клиентского пересчёта
и никакого подмешивания из `GET /v1/questions`.

Ссылка на задачу у тредов задач: `entry.project_id` и `entry.task_id` дают
`/p/${entry.project_id}/tasks/${entry.task_id}`, подпись — `entry.task_title`. У тредов роли
ссылка ведёт на `/agents/${entry.role_id}` (сверить фактический маршрут по
`web/src/routes.tsx`). `subject` не парсим.

Заголовок экрана и подписи: «Open questions», чекбоксы «Waiting on me» и «Show resolved»,
секции «Awaiting you» / «Awaiting others» / «History». Стили — в
`QuestionsScreen.css` по образцу существующих правил файла.

- [ ] **Step 5: тесты зелёные**

Run: `cd web && npx vitest run && npm run build`
Expected: PASS (весь web-набор, не только новый файл).

- [ ] **Step 6: Commit**

```bash
git add web/src/screens/questions/ web/src/lib/queries.ts web/src/mocks/
git commit -m "web: единый экран Questions поверх GET /v1/threads с фильтром «ждут меня»"
```

---

### Task 6: fyi в истории задачи без бейджей + ревизия счётчиков

**Files:**
- Modify: `web/src/screens/task/QuestionsTab.tsx`
- Test: `web/src/screens/task/Task.test.tsx`

**Interfaces:**
- Consumes: `Question.type`, `Question.resolution` (Task 1).
- Produces: `resolutionLabel()` возвращает `'fyi'` для fyi-тредов.

- [ ] **Step 1: написать падающий тест**

```tsx
it('shows an fyi thread in history labelled fyi and never as an open question', async () => {
  renderTask(TASK_WITH_FYI)   // фикстура: status resolved, type 'fyi', resolution 'fyi'
  await userEvent.click(screen.getByRole('button', { name: /Questions/ }))
  expect(await screen.findByText('fyi')).toBeInTheDocument()
  expect(screen.queryByText('? awaiting you')).not.toBeInTheDocument()
})
```

- [ ] **Step 2: убедиться, что падает**

Run: `cd web && npx vitest run src/screens/task/Task.test.tsx -t "fyi"`
Expected: FAIL — рендерится «resolved».

- [ ] **Step 3: реализовать**

В `QuestionsTab.tsx`:

```tsx
function resolutionLabel(question: Question): string {
  // fyi-тред создаётся сразу закрытым и никого не ждёт — в истории он
  // подписан своим словом, а не «resolved» (спека v1 §«Тип треда»).
  if (question.type === 'fyi' || question.resolution === 'fyi') return 'fyi'
  if (question.resolution === 'dismissed') return 'dismissed'
  return 'resolved'
}
```

Локальный id в строке истории: `{question.local_ref ?? `Q${question.ordinal}`}`.

- [ ] **Step 4: ревизия счётчиков (проверка, не переписывание)**

Убедиться и зафиксировать в сообщении коммита, что бейджи читают серверные поля, а не выводят
ход на клиенте:
- `web/src/screens/kanban/TaskCard.tsx:130-136` — `task.questions_awaiting_user` / `task.open_questions`
  (сервер: `internal/api/threads.go:150` `threadCounts` — считает по attention и только открытые);
- `web/src/screens/projects/ProjectCard.tsx:37` — `agent.awaiting_user`;
- `web/src/components/QuestionThread.tsx` / `AgentQuestionThread.tsx` — `your_turn`, `whose_turn`
  только как фолбэк для старого демона.

Run: `cd web && grep -rn "whose_turn" src/ | grep -v types.ts`
Expected: только фолбэк-ветки в `QuestionThread.tsx`/`AgentQuestionThread.tsx` и моки.
Если найдётся вывод хода из `messages` — переписать на `your_turn`/`attention`.

- [ ] **Step 5: тесты зелёные**

Run: `cd web && npx vitest run && npm run build`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add web/src/screens/task/ web/src/mocks/
git commit -m "web: fyi-треды в истории без бейджей; счётчики читают attention с сервера"
```

---

### Task 7: mobile — типы, бейджи треда и кнопки вариантов

**Files:**
- Modify: `mobile/src/api/types.ts`, `mobile/src/lib/threads.ts`, `mobile/src/api/queries.ts`,
  `mobile/app/task/[id].tsx`, `mobile/app/agent/[id].tsx`
- Test: `mobile/src/lib/threads.test.ts`

**Interfaces:**
- Produces: `threadRefLabel(t): string`, `threadBadges(t): { label: string }[]` —
  чистые функции в `mobile/src/lib/threads.ts`, их зовут оба экрана.

- [ ] **Step 1: написать падающие тесты**

В `mobile/src/lib/threads.test.ts`:

```ts
describe('threadRefLabel', () => {
  it('prefers the local ref', () => {
    expect(threadRefLabel({ ordinal: 2, local_ref: '1023/Q2' })).toBe('1023/Q2')
  })
  it('falls back to Q<ordinal> on an old daemon', () => {
    expect(threadRefLabel({ ordinal: 2 })).toBe('Q2')
  })
})

describe('threadBadges', () => {
  it('badges a stale decision thread', () => {
    expect(threadBadges({ status: 'open', stale: true }).map((b) => b.label)).toContain('stale')
  })
  it('badges an fyi thread as fyi and never as stale', () => {
    const labels = threadBadges({ status: 'resolved', type: 'fyi', stale: true }).map((b) => b.label)
    expect(labels).toEqual(['fyi'])
  })
  it('gives a plain open thread no badges', () => {
    expect(threadBadges({ status: 'open' })).toEqual([])
  })
})
```

- [ ] **Step 2: убедиться, что падают**

Run: `cd mobile && npx jest src/lib/threads.test.ts`
Expected: FAIL — функций нет.

- [ ] **Step 3: реализовать**

`mobile/src/api/types.ts` — в `Question` и `AgentQuestion` добавить те же поля, что в web
(Task 1): `local_ref?: string`, `type?: 'decision' | 'fyi'`, `options?: string[]`,
`stale?: boolean`, `attention?: string[]`; в `resolution` добавить `'fyi'`.

`mobile/src/lib/threads.ts`:

```ts
/** Локальный id треда — то, что видит человек. Q<ordinal> — фолбэк старого демона. */
export function threadRefLabel(t: { ordinal: number; local_ref?: string }): string {
  return t.local_ref ?? `Q${t.ordinal}`
}

/**
 * Бейджи состояния треда. fyi — статусная запись: она закрыта, никого не ждёт
 * и stale быть не может, поэтому у неё ровно один бейдж (спека v1 §«Тип треда»).
 */
export function threadBadges(t: {
  status: string
  type?: string
  stale?: boolean
}): { label: string }[] {
  if (t.type === 'fyi') return [{ label: 'fyi' }]
  if (t.status === 'open' && t.stale) return [{ label: 'stale' }]
  return []
}
```

`mobile/src/api/queries.ts` — в мутациях ответа (строки ~288 для задачи и ~542 для роли)
разрешить `choose`:

```ts
    mutationFn: (p: { id: number; body?: string; to?: string[]; choose?: number }) =>
      api.post(
        baseUrl,
        `/v1/questions/${p.id}/answer`,
        // choose — 1-based индекс варианта; выбор закрывает тред, адресатов не нужно.
        p.choose ? { choose: p.choose } : { body: p.body, ...addresseePayload(p.to ?? []) },
      ),
```

`mobile/app/task/[id].tsx` (карточка треда, ~строка 113) и `mobile/app/agent/[id].tsx`
(~строка 111): `Badge label={threadRefLabel(q)}` вместо `` `Q${q.ordinal}` ``; после него —
бейджи из `threadBadges(q)` (stale — `colors.amberDeep`/`colors.amberBg`, fyi —
`colors.textDim`/нейтральный фон); бейдж «awaiting …» рендерить только при
`q.status === 'open' && q.type !== 'fyi'`. Под текстом вопроса — ряд `Pressable`-кнопок
вариантов, когда `(q.options ?? []).length > 0 && q.status === 'open'`:

```tsx
{(q.options ?? []).map((label, i) => (
  <Pressable
    key={label}
    style={styles.optionBtn}
    onPress={() => answer.mutate({ id: q.id, choose: i + 1 }, { onError: onErr })}
  >
    <Text style={styles.optionText}>{label}</Text>
  </Pressable>
))}
```

- [ ] **Step 4: тесты и типы зелёные**

Run: `cd mobile && npm run typecheck && npm test`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add mobile/
git commit -m "mobile: локальный id, бейджи type/stale и кнопки вариантов в тредах"
```

---

### Task 8: приёмка

- [ ] **Step 1: полный прогон**

```bash
go test ./internal/api/
cd web && npm run build && npm test
cd ../mobile && npm run typecheck && npm test
```

Expected: обе связки зелёные, вывод приложить в PR.

- [ ] **Step 2: сверить с критериями приёмки спеки**

- Task 0: `stale`/`project_id`/`task_title` в `GET /v1/threads`, тесты в `internal/api`.
- Критерий 4: `ask --option A --option B` → в дашборде кнопки, клик закрывает через `choose`
  (Tasks 3, 4, 7).
- Критерий 5: `--fyi` → resolved-запись, бейджей нет (Tasks 5, 6, 7).
- Бриф п.2: stale-бейдж виден (Tasks 3, 4, 5, 7).
- Бриф п.3: единый список + «ждут меня» (Task 5).
- Бриф п.4: бейджи из attention/awaiting_user (Task 6 Step 4).
- Бриф п.6: `local_ref` везде (Tasks 3, 4, 6, 7).

- [ ] **Step 3: PR**

```bash
git push -u origin feature/task-1023/web-questions
gh pr create --title "web+mobile: редизайн вопросных тредов — варианты, stale, единый инбокс (#1028)" --body "..."
```

- [ ] **Step 4: сообщить оркестратору**

```bash
rocket send task-1023-orch "#1028: PR <url>, сборки и тесты зелёные."
```
