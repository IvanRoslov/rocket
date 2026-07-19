# Remote-квиз (AskUserQuestion через API) — план имплементации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox syntax.

**Goal:** pending-квиз AskUserQuestion виден через API/SSE в момент показа (PreToolUse-хук), отвечается через `POST /v1/sessions/{id}/quiz/answer` (демон транслирует в tmux-нажатия), квиз-раунды рендерятся в ленте чата; очередь сообщений не доставляет текст поверх открытого виджета. Спека (binding): docs/superpowers/specs/2026-07-19-remote-quiz-design.md. Эмпирические факты (binding): docs/superpowers/recon/2026-07-19-quiz-recon.md и -quiz-recon2-hooks.md.

**Global Constraints:** только claude-code (codex вне скоупа); глобальный 120-рунный лимит tool-дайджеста НЕ меняется; **Enter никогда не эмитится на строке «Type something» без предварительно отправленного непустого текста** (иначе отмена всего квиза — обязателен юнит-тест инварианта); multi-select-ответы в toolUseResult парсить сверкой с labels, не сплитом по разделителю; тесты — изолированный ROCKET_HOME (короткий mktemp, не t.TempDir) + эфемерные порты (у пользователя живой демон на 4477); никаких git merge/rebase/reset у сабагентов; gofmt/vet/тесты перед каждым коммитом.

### Task 1: Store — pending_quiz

**Files:** internal/store/sessions.go, internal/store/migrations/ (новая миграция по образцу 0002), internal/store/sessions_quiz_test.go (новый).
**Produces:** колонка `sessions.pending_quiz TEXT NULL`; методы `SetPendingQuiz(id string, quizJSON string) error`, `ClearPendingQuiz(id string) error`; поле `PendingQuiz string` в структуре Session (пустая строка = нет). Очистка pending_quiz во всех местах, где сессия переводится в терминальное состояние (killed/errored/done — найти существующие переходы состояний в store/session manager и добавить очистку там же, где сбрасываются другие эфемерные атрибуты).
- [ ] TDD: set → читается в Session; clear → пусто; терминальный переход чистит; миграция применяется на существующей базе.
- [ ] Commit: `feat(store): pending_quiz column + accessors`.

### Task 2: Хук + внутренний эндпоинт + события

**Files:** internal/agent/claudecode/claudecode.go (quiz-hook.sh writer + upsert PreToolUse/PostToolUse matcher "AskUserQuestion" — по образцу activity-hook), internal/api/internal_quiz.go (+тест, новый), internal/api/server.go (маршрут), internal/api/events.go при необходимости.
**Consumes:** Task 1 (Set/ClearPendingQuiz).
**Produces:** `POST /v1/internal/quiz` `{session, phase: "pending"|"resolved", payload: <raw hook stdin JSON>}`; события bus `session.quiz_asked {session_id}` / `session.quiz_resolved {session_id}` (пинги без контента, как chat_updated).
- Скрипт `<worktree>/.rocket/quiz-hook.sh <phase>`: `payload=$(cat)`; curl POST с timeout 3s; **exit 0 всегда**. Порт и session id — тем же механизмом, что в activity-hook.sh (посмотреть writeActivityHookScript).
- pending: из payload взять `.tool_input` (там `questions`), сохранить `{"questions":...,"asked_at":<unix now>}`; resolved: ClearPendingQuiz (payload с `tool_response` может быть is_error — всё равно clear). Идемпотентность: повторный pending перезаписывает; resolved при пустом pending — 200 no-op без события.
- [ ] TDD: оба phase, событие публикуется, 404 неизвестная сессия, 400 битый payload (warn), no-op resolved.
- [ ] Тест адаптера: settings.local.json после SetupWorkspace содержит оба matcher-хука; quiz-hook.sh существует и исполняем.
- [ ] Commit: `feat(quiz): AskUserQuestion hooks + internal quiz endpoint`.

### Task 3: Публичный API — pending_quiz наружу + POST quiz/answer + инъектор

**Files:** internal/api/sessions.go (pending_quiz в карточке), internal/api/chat.go (pending_quiz в session{}), internal/api/quiz.go (+тест, новый), internal/session/ (инъектор-хелпер — рядом с существующей инжекцией сообщений; юнит-тест генерации последовательности), internal/api/server.go.
**Consumes:** Task 1, Task 2.
**Produces:** `pending_quiz` (omitempty) в GET /v1/sessions/{id} и в session{} ответа чата: `{"questions":[{"question","header","multi_select","options":[{"label","description"}]}],"asked_at":N}`. `POST /v1/sessions/{id}/quiz/answer` `{answers:[{question_index, option_indices?[], text?}]}` → 202 `{status:"answering"}`; 409 no_pending_quiz; 400 quiz_answer_invalid (детали в message: индекс вне диапазона / single-select не ровно один / и option_indices и text вместе / пустой text / отвечены не все вопросы).
- Инъектор: чистая функция `quizKeySequence(quiz, answers) ([]keyStep, error)` (keyStep: digit/space/tab/down/literal-text/enter + settle-пауза ~300ms) + исполнение через runtime send-keys. Порядок по спеке §4 (Other — стрелками Down, текст, Enter). После инъекции запустить unconfirmed-таймер 60s (конфигурируемый для теста): если resolved-хук не пришёл — событие `session.quiz_answer_unconfirmed {session_id}` (warn-лог), pending НЕ чистить.
- [ ] TDD API: 202/409/400-варианты; pending_quiz в обоих GET; unconfirmed с коротким таймаутом.
- [ ] TDD инъектор: single-select (одна цифра); multi-select (цифры выбранных + Tab); Other (Down×k, literal, Enter); финальный Enter на Submit; **инвариант: для Other с пустым text — ошибка ещё на валидации, последовательность с Enter-без-текста непредставима** (тест перебором сгенерированных последовательностей: перед Enter на Other-строке всегда есть literal).
- [ ] Commit: `feat(api): pending quiz exposure + remote quiz answer endpoint`.

### Task 4: Гейт очереди

**Files:** internal/queue/queue.go (+тест).
**Consumes:** Task 1 (PendingQuiz в Session).
- В точке выбора «можно ли доставлять этому получателю» (рядом с activity-гейтом) — если у сессии-получателя PendingQuiz != "" → пропустить (остаётся queued), без failed и без событий. После clear — доставка возобновляется существующим циклом.
- [ ] TDD: сообщение не доставляется при pending, доставляется после clear; failed-механика не задета.
- [ ] Commit: `feat(queue): hold deliveries while recipient has a pending quiz`.

### Task 5: Квиз в ленте чата

**Files:** internal/agent/agent.go (ChatEntry += `Quiz json.RawMessage`), internal/agent/claudecode/chat.go (+тест; codex chat.go НЕ трогать), internal/api/chat.go (поле quiz в JSON-ответе, omitempty).
**Consumes:** типы Task 1-3 не нужны — независим, но по конвейеру идёт после.
- tool_use с name=="AskUserQuestion": entry как сейчас (role tool, дайджест 120 рун) + `Quiz` = raw `input` JSON.
- user-запись, состоящая только из tool_result: сейчас пропускается всегда; если её `toolUseResult` содержит `answers` (или это is_error-отмена квиза AskUserQuestion) → entry `{Role:"quiz_answer", Text:<построчно «вопрос → выбор»; отмена → "квиз отменён">, Quiz:<raw toolUseResult>}`. Прочие tool_result-only записи — пропуск как раньше (регресс-тест обязателен: существующий корпусный тест не должен измениться).
- [ ] TDD на фикстурах: квиз-tool_use → Quiz-поле; answers → quiz_answer; is_error; регресс прочих tool_result-only.
- [ ] Commit: `feat(chat): quiz entries in session chat feed`.

### Task 6: Доки

**Files:** docs/13-chat.md (раздел «Квизы»), docs/03-daemon-api.md (эндпоинты + 3 события), docs/06-messaging.md (строка про гейт).
- По спеке §4-§8: pending_quiz-схема, POST quiz/answer с JSON-примерами и всеми кодами ошибок, SSE-флоу (asked → GET → answer → resolved; unconfirmed), рендер в ленте (tool-entry.quiz = заданный, quiz_answer = закрытый; pending — интерактивный пузырь из session.pending_quiz), edge-кейсы (терминальный ответ параллельно → 409; Esc; рестарт демона; хук не дозвонился), конвенция клиента: композер заблокирован при pending-квизе. Язык/стиль — как 13-chat.md.
- [ ] Ревью-чек: каждое утверждение соответствует Task 1-5.
- [ ] Commit: `docs: remote quiz contract`.

### Task 7: Live-приёмка

- Изолированный ROCKET_HOME (короткий mktemp, свой порт; живой демон на 4477 НЕ трогать), живой claude-оркестратор: попросить позвать AskUserQuestion (2 вопроса: single+multi) → pending_quiz виден в GET и SSE quiz_asked пришёл ДО ответа; POST quiz/answer → в pane виден выбор, quiz_resolved пришёл, pending снялся; лента чата после ответа показывает квиз-раунд (quiz-поле + quiz_answer); `rocket send` во время квиза остаётся queued до resolve; повторный квиз + ответ в терминале руками → POST получает 409. Other-ответ: отдельный квиз, text-ответ доезжает. Зачистка (tmux/процессы/ROCKET_HOME). Фиксы отдельными коммитами.
- [ ] Commit(ы) по фактам.

## Self-Review (выполнен)

- Спека покрыта: хуки+detection T2, store T1, публичный API+инъектор T3, гейт T4, лента T5, доки T6, приёмка T7. Инвариант Other-Enter — юнит в T3 и live-проверка в T7.
- Типы согласованы: PendingQuiz (store, T1) ← T2 пишет, T3/T4 читают; ChatEntry.Quiz (T5) не пересекается с T3.
- Плейсхолдеров нет; точные значения (тайминги, коды ошибок, форматы) взяты из спеки/разведки.
