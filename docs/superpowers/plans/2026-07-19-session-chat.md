# Чат с сессиями (зеркало транскрипта) — план имплементации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox syntax.

**Goal:** `GET /v1/sessions/{id}/chat?cursor=` отдаёт диалог сессии из нативного JSONL-транскрипта агента (user/assistant/tool-записи), SSE-событие `session.chat_updated` пингует об обновлениях; публичный контракт `docs/13-chat.md` для дашборда и мобильного приложения. Спека (binding): docs/superpowers/specs/2026-07-19-session-chat-design.md.

**Architecture/Constraints:** см. спеку — она binding целиком: ChatEntry{Role,Text,ToolName,TS}, `TranscriptTail(ctx, ref, cursor) ([]ChatEntry, string, error)` в интерфейсе Agent, курсор `<path>:<offset>` непрозрачный, «best effort»-парсинг, только полные строки, cursor=""→хвост из limit записей, воркеры read-only на клиенте (сервер не различает), SSE-пинг из мониторного цикла при смене mtime/size. Прежние инварианты (тесты/vet/gofmt, никаких merge/rebase/reset у сабагентов) в силе.

### Task 1: TranscriptTail — интерфейс + claude-code

**Files:** internal/agent/agent.go (+ChatEntry, +метод в Agent iface), internal/agent/claudecode/chat.go (+тест), фейки в session/api-тестах (минимальная заглушка: возвращает ErrNoSignal).

- claude-code парсер: файл выбирается как в activity (newest .jsonl по cwd-матчу); cursor "" → прочитать весь файл, отдать последние limit? — лимит применяет API-слой; TranscriptTail отдаёт ВСЁ от курсора (API режет хвост при cursor==""). Записи: type user → {user, текст content (string или блоки text)}, type assistant → text-блоки конкатенацией {assistant}, tool_use-блоки → {tool, ToolName, дайджест input однострочный ≤120}; thinking/summary/system/прочие — пропуск; незавершённая последняя строка не читается (курсор не двигается за неё); непарсибельная полная строка — пропуск с продвижением курсора.
- Курсор: `<abs-path>:<offset>`; если файл из курсора исчез или offset > size (truncate) — начать с текущего newest-файла с 0; если появился более новый файл — дочитать старый до EOF, следующий вызов вернёт курсор на новый файл с 0 (проще: если старый дочитан (offset==size) и есть более новый — сразу перескочить в этом же вызове).
- [ ] TDD на фикстурах (методика activity_test): user/assistant/tool разбор, дайджест-обрезка, курсорная инкрементальность (два вызова), незавершённая строка, мусорная строка, ротация на новый файл, ErrNoSignal без транскрипта.
- [ ] Commit: `feat(agent): TranscriptTail interface + claude-code chat parser`.

### Task 2: TranscriptTail — codex

**Files:** internal/agent/codex/chat.go (+тест).

- По той же схеме поверх codex-таксономии (session_meta пропуск; response_item с текстом пользователя/агента → user/assistant — точные поля взять из существующих activity-фикстур и отчёта разведки P5 Task 2; command/tool event_msg → tool с дайджестом). Файл — как в codex activity (cwd-матч + 14-дневное окно).
- [ ] TDD фикстуры аналогично Task 1.
- [ ] Commit: `feat(codex): chat transcript parser`.

### Task 3: API + SSE-пинг

**Files:** internal/api/chat.go (+тест), internal/monitor/monitor.go (вотчер), server.go маршрут.

- `GET /v1/sessions/{id}/chat?cursor=&limit=` (default 200, max 1000): сессия из store (404), agent.Get(sess.Agent) → TranscriptTail(ref, cursor); ErrNoSignal → 200 {entries:[], next_cursor:""}; cursor=="" → отдать последние limit записей (обрезка на API-слое), иначе все от курсора (до limit; если больше — next_cursor промежуточный: TranscriptTail отдаёт всё, API режет и пересчитывает курсор? НЕТ — упростить: TranscriptTail получает опц. maxEntries... Решение (binding): TranscriptTail без лимита (транскрипты — мегабайты максимум, ок), API режет: cursor=="" → tail limit; cursor!="" → первые limit из полученных, next_cursor = курсор после последней ОТДАННОЙ записи — для этого TranscriptTail возвращает per-entry offsets? Упрощение (binding, зафиксировано): API отдаёт ВСЕ записи от курсора без обрезки (limit применяется только к хвосту при cursor==""); инкременты маленькие по определению. Документировать в 13-chat.md.
- Ответ включает session {id, kind, state, activity}.
- Монитор: в sweep для каждой live-сессии сравнить (mtime,size) транскрипта с прошлым тиком (map в мониторе; источник пути — новый лёгкий метод адаптера? НЕТ: переиспользовать TranscriptTail нельзя (дорого). Решение: adapter-метод не нужен — монитор уже трогает транскрипты через Activity(): расширить возвращаемое? НЕТ. Простейшее (binding): вотчер в мониторе вызывает новый дешёвый метод интерфейса `TranscriptStat(ctx, ref) (mtime int64, size int64, err error)` (оба адаптера: newest-файл, os.Stat; ErrNoSignal — пропуск) — 2 syscall'а на сессию на тик, дёшево. При изменении → bus `session.chat_updated {session_id}`.
- [ ] TDD API (хвост/инкремент/404/ErrNoSignal/limit clamp); монитор-вотчер (изменение → одно событие; без изменений — тишина; терминальные сессии не вотчатся).
- [ ] Commit: `feat(api): session chat endpoint + chat_updated SSE ping`.

### Task 4: Публичные доки

**Files:** docs/13-chat.md (новый), docs/03-daemon-api.md (+строка в «Сессии», +событие), docs/11-dashboard.md (+абзац Chat-режима), docs/00-overview.md (строка в таблицу документов).

- 13-chat.md по спеке §4: контракт, примеры curl с реальными формами ответов, жизненный цикл экрана, статусы отправки через /v1/messages, ограничения (read-only воркеры — конвенция клиента; отставание ~тик монитора; best-effort парсинг; пагинации назад нет). Язык — как остальные доки (русский с англ. JSON-примерами).
- [ ] Ревью-чек: каждое утверждение в 13-chat.md соответствует реализации Task 1-3 (это проверит ревьюер).
- [ ] Commit: `docs: session chat public contract (13-chat.md)`.

### Task 5: Live-приёмка

- Изолированный ROCKET_HOME (живой демон пользователя НЕ трогать; свой порт), живой claude-оркестратор: короткий диалог через `rocket send` → `curl /v1/sessions/<id>/chat` показывает user+assistant записи, совпадающие с терминалом; tool-строки присутствуют при работе агента; SSE `session.chat_updated` приходит при активности; инкрементальный курсор работает (второй curl с next_cursor отдаёт только новое); codex-сессия — то же самое кратко (spawn codex-воркера от оркестратора). Зачистка. Фиксы отдельными коммитами.
- [ ] Commit(ы) по фактам.

## Self-Review (выполнен)

- Спека покрыта: интерфейс+адаптеры T1-T2, API+SSE T3, доки T4, приёмка T5. Открытый вопрос лимита при инкременте разрешён в T3 (без обрезки инкрементов) — отражить в 13-chat.md (T4).
- TranscriptStat добавлен как отдельный дешёвый метод — интерфейс Agent расширяется вторым методом; фейки в тестах обновить (T1 отмечает).
