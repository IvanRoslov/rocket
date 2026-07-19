# Remote-квиз (AskUserQuestion через дашборд/мобилку) — дизайн

Дата: 2026-07-19. Статус: подтверждён пользователем («Полный remote-квиз»; квиз живёт в ленте чата — это ключ к мобильному интерфейсу общения без терминала).

## Зачем

AskUserQuestion — интерактивный TUI-виджет Claude Code (вопросы с опциями, single/multi-select, свободный текст «Other»). В терминале он работает отлично, на тач-скринах терминал непригоден. Клиент (дашборд/мобилка) должен: видеть полный квиз (вопросы+опции) как пузырь в ленте чата, отвечать нажатием кнопок/чекбоксов, и демон транслирует выбор в TUI-нажатия. v1 — только claude-code (у codex аналога нет).

## Эмпирические факты (разведка 2026-07-19, binding)

Отчёты: scratchpad quiz-recon-report.md, quiz-recon2-report.md (Claude Code v2.1.215).

1. **Транскрипт молчит, пока квиз на экране.** tool_use+PreToolUse-attachment+tool_result+PostToolUse-attachment пишутся в JSONL одной пачкой ПОСЛЕ ответа. Детекция pending по транскрипту невозможна структурно.
2. **PreToolUse-хук-скрипт исполняется в момент показа квиза** (не при флаше) и получает на stdin JSON с полным `tool_input` (`{questions:[{question, header, multiSelect, options:[{label, description}]}]}`). PostToolUse исполняется сразу после ответа, stdin содержит `tool_response` с `answers` — map «текст вопроса → выбранный label» (multi-select — склейка labels; разделитель между версиями CLI нестабилен: `","` в v2.1.185, `", "` в v2.1.215 — парсить сверкой с labels, не сплитом).
3. **Нажатия** (tmux send-keys, settle ~0.3–1s между): цифра `N` — выбрать опцию (single-select авто-переходит к следующему вопросу; multi-select — тогглит чекбокс, остаётся); `Space` — тогглит чекбокс под курсором; `Tab` — следующая вкладка, после ответа на все — экран Review/Submit; `Enter` на «1. Submit answers» (дефолт) — подтвердить.
4. **«Other»**: строка «Type something.» = опция N+1; надо перевести курсор на неё и НАБРАТЬ ТЕКСТ (send-keys -l), строка live-обновляется, затем Enter. **КАПКАН: голый Enter на пустой строке «Type something» отменяет весь квиз** (tool_result is_error:true «user declined»). После «Other» есть ещё строка «Chat about this» (N+2) — не используем.
5. `Esc` — отмена квиза (user declined).

## Архитектура

```
claude-code TUI ──(PreToolUse hook: quiz-hook.sh)──▶ POST /v1/internal/quiz {phase:pending, tool_input}
                                                          │ store: session.pending_quiz; bus: session.quiz_asked
клиент ◀── SSE session.quiz_asked ── демон
клиент ── GET chat / GET session (pending_quiz) ──▶ рендер пузыря с кнопками
клиент ── POST /v1/sessions/{id}/quiz/answer ──▶ демон валидирует ↦ tmux-нажатия
claude-code TUI ──(PostToolUse hook)──▶ POST /v1/internal/quiz {phase:resolved, tool_response}
                                          │ store: clear; bus: session.quiz_resolved; очередь размораживается
```

### 1. Хуки (internal/agent/claudecode)

`upsertClaudeSettings` дополняется: `PreToolUse` и `PostToolUse` с matcher `AskUserQuestion` → `sh .rocket/quiz-hook.sh pending|resolved` (отдельный скрипт рядом с activity-hook.sh, тот же механизм записи). Скрипт: читает stdin целиком, POST на `http://127.0.0.1:<port>/v1/internal/quiz` c JSON `{"session":"<id>","phase":"<pending|resolved>","payload":<stdin как есть>}` (curl, timeout 3s, exit 0 всегда — хук не должен блокировать TUI). Порт/сессия — тем же способом, каким activity-hook получает свои значения.

### 2. Store (internal/store)

`sessions` получает колонку `pending_quiz TEXT` (миграция; JSON `{"questions":[...],"asked_at":<unix>}`, NULL — нет квиза). Методы: `SetPendingQuiz(id, json)`, `ClearPendingQuiz(id)`, чтение в составе Session. Очистка также при kill/каскадах (там, где сессия становится терминальной).

### 3. Внутренний эндпоинт (internal/api)

`POST /v1/internal/quiz` `{session, phase, payload}`:
- `phase=pending`: извлечь `tool_input.questions` из payload; `SetPendingQuiz`; событие `session.quiz_asked {session_id}` (данные квиза в событии НЕ дублируем — клиент дёргает GET, как с chat_updated).
- `phase=resolved`: извлечь `tool_response`/`answers` (может быть is_error=отмена); `ClearPendingQuiz`; событие `session.quiz_resolved {session_id}`.
- Неизвестная сессия — 404; кривой payload — 400 с warn-логом (хук всё равно exit 0).
- Идемпотентность: повторный pending перезаписывает (новый квиз), повторный resolved при пустом pending — no-op 200.

### 4. Публичный API

- `GET /v1/sessions/{id}` и `session{}` в ответе чата: `pending_quiz` (omitempty) — `{questions:[{question, header, multi_select, options:[{label, description}]}], asked_at}`.
- `POST /v1/sessions/{id}/quiz/answer`:
  ```json
  {"answers":[{"question_index":0,"option_indices":[1]},
              {"question_index":1,"option_indices":[0,2]},
              {"question_index":2,"text":"свой вариант"}]}
  ```
  По каждому вопросу либо `option_indices` (для single-select ровно один), либо `text` (Other). Валидация против сохранённого pending_quiz: 409 `no_pending_quiz`, 400 `quiz_answer_invalid` (индексы вне диапазона, multi-констрейнты, пустой text, не все вопросы отвечены). Успех → 202 `{status:"answering"}`; фактическое закрытие подтверждает `session.quiz_resolved` (от PostToolUse-хука).
- Инъекция (internal/session или runtime-хелпер): последовательность per-вопрос в порядке вкладок: single-select — цифра выбранной опции (авто-переход); multi-select — цифры каждого выбранного, затем `Tab`; Other — переместиться на строку `N+1` **стрелками Down** (не цифрой: в single-select цифра = немедленный выбор, полагаться на её поведение для Other-строки нельзя), затем `send-keys -l <text>`, затем `Enter`. **Инвариант (тест обязателен): Enter никогда не эмитится на строке Other без предварительно отправленного непустого текста.** После всех вопросов: `Tab` до Submit не требуется, если авто-переход уже привёл на Review; завершение — `Enter` на «1. Submit answers». Между нажатиями settle-паузы (константа ~300ms). Реализация не подтверждает успех сама — источником истины остаётся resolved-хук; если он не пришёл за таймаут (60s), событие `session.quiz_answer_unconfirmed` (warn) — клиент показывает «проверьте терминал».
- Race «ответили в терминале параллельно»: PostToolUse-хук закроет pending в любом случае; POST answer после этого получит 409 — клиент перерисует по quiz_resolved.

### 5. Гейт очереди сообщений (internal/queue)

Пока у сессии-получателя `pending_quiz != NULL` — доставка сообщений ей приостанавливается (сообщения остаются queued; инжекция текста сломала бы виджет). После `ClearPendingQuiz` доставка возобновляется штатным циклом. Отражить в docs/06-messaging.md одной строкой.

### 6. Квиз в ленте чата (internal/agent/claudecode/chat.go)

- tool_use `AskUserQuestion` (белый список из одного имени): entry получает аддитивное поле `Quiz` (полный `questions` JSON) помимо обычного 120-рунного дайджеста в `text`. ChatEntry расширяется полем `Quiz json.RawMessage` (omitempty в API-ответе). Глобальный лимит дайджеста НЕ меняется.
- tool_result-only user-запись: сейчас пропускается всегда; для записей, чей `toolUseResult` содержит `answers` (или is_error-отмена квиза) — эмитить entry `role:"quiz_answer"` с `text` = человекочитаемая сводка («Вопрос → выбор» построчно; отмена → «квиз отменён») и `Quiz` = raw `toolUseResult`. Прочие tool_result-only записи — пропускаются как раньше.
- Клиент рендерит: pending-квиз — из `pending_quiz` сессии (интерактивный пузырь); историю — из ленты (tool-entry с Quiz = заданный квиз, quiz_answer = закрытый).

### 7. События

`session.quiz_asked`, `session.quiz_resolved`, `session.quiz_answer_unconfirmed` — в таксономию 03-daemon-api.md. Как у chat_updated: пинги без контента.

### 8. Доки (deliverable)

- docs/13-chat.md: раздел «Квизы» — pending_quiz в session{}, SSE-события, POST quiz/answer c примерами, рендер квиз-пузырей в ленте (tool-entry.quiz / quiz_answer), edge-кейсы (ответ в терминале, отмена Esc, unconfirmed, рестарт демона — pending_quiz в SQLite переживает), гейт композера на время квиза (конвенция клиента: композер блокируется, показывается квиз).
- docs/03-daemon-api.md: строки эндпоинтов + события.
- docs/06-messaging.md: строка про гейт доставки при pending-квизе.

## Ошибки и краевые случаи

- Демон перезапустился при pending-квизе: pending_quiz в SQLite; resolved-хук по приходу закроет. Если сессия умерла с pending — kill/cleanup чистит.
- Хук не смог достучаться до демона (демон лежал): pending не зафиксирован — квиз виден только в терминале; resolved при следующем квизе перезапишет состояние корректно. Деградация приемлема, без ретраев в скрипте.
- Версии CLI: раскладка клавиш зафиксирована на v2.1.215; поведенческие константы (авто-переход, Space, капкан Other) закреплены live-тестами разведки; при регрессии CLI — unconfirmed-событие даст сигнал.
- Битый payload от хука (обрезанный stdin): 400 + warn, состояние не меняется.

## Тестирование

- store: миграция + Set/Clear/чтение pending_quiz; очистка при терминальных переходах.
- api: internal/quiz оба phase, идемпотентность, 404/400; quiz/answer — валидации (409/400), 202, unconfirmed-таймер (с фейковым временем/коротким таймаутом); pending_quiz в GET session и chat session{}.
- queue: доставка приостановлена при pending, возобновляется после clear.
- chat-парсер: tool_use AskUserQuestion → Quiz-поле; toolUseResult с answers → quiz_answer entry; is_error-отмена; прочие tool_result — по-прежнему пропуск (регресс-тест).
- инъектор: юнит на генерацию последовательности нажатий (single/multi/Other/комбинация; инвариант «нет Enter на пустом Other»).
- Live-приёмка: изолированный ROCKET_HOME + живой claude-оркестратор: квиз задан → pending виден в API ≤ пары секунд, SSE-событие пришло; ответ через POST — TUI показал выбор, resolved пришёл, transcript-лента показала квиз-раунд; параллельный терминальный ответ → 409; сообщение в очередь во время квиза не доставляется до resolve.

## Вне скоупа v1

Codex; «Chat about this»-ветка; Esc/отмена через API (только показ, отвечать или в терминале); множественные одновременные квизы (у Claude Code их не бывает — один tool call за раз); пуш-нотификации мобилке.
