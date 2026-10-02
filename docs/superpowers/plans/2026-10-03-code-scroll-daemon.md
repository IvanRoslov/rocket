# Code scroll daemon Implementation Plan

> **Для исполняющего агента:** выполнять задачи по порядку с `superpowers:executing-plans` и `superpowers:test-driven-development`; каждый шаг с тестом проходит RED → GREEN.

**Цель:** принимать кадр `{"type":"scroll","lines":N}` в WebSocket-терминале и прокручивать историю tmux без потери следующего ввода.

**Архитектура:** API разбирает и ограничивает кадр, менеджер находит tmux-сессию, а runtime управляет copy-mode конкретной панели. Состояние прокрутки общее для веб-клиентов одной сессии: перед первым следующим бинарным вводом любого клиента API выводит панель из copy-mode.

**Стек:** Go, `coder/websocket`, `creack/pty`, tmux.

**Спецификация:** утверждённый бриф задачи #5073, часть A; контракт и ограничения изложены ниже.

## Общие ограничения

- `lines < 0` — к старому выводу; `lines > 0` — к новому; ноль и отсутствующее поле игнорируются.
- Модуль значения ограничен до 1000 строк; `readonly` игнорирует прокрутку.
- tmux `mouse` и глобальные настройки не меняются; ошибки tmux не закрывают WebSocket.
- Изменения браузера и запуска codex выполняют другие задачи.

## Особое внимание при проверке

- Первый ввод после прокрутки, включая большую вставку, попадает в PTY после выхода из copy-mode.
- Ввод из другой вкладки и после переподключения также выходит из общего для панели copy-mode.
- Прокрутка вниз вне copy-mode не входит в режим просмотра.
- Нулевые кадры и readonly не меняют режим панели.
- Значения за пределом 1000 ограничиваются перед вызовом runtime.
- Ошибка runtime не закрывает соединение и не мешает следующему вводу.

---

### Task 1: Разбор кадра

**Файлы:** `internal/api/term.go`, `internal/api/term_test.go`.

**Интерфейс:** `termControl.Lines int`; `clampScroll(int) int`; `maxScrollLines = 1000`.

- [ ] Добавить тесты разбора `scroll` с `lines=-3`, нулевого и отсутствующего `lines`; тест ограничения `±5000` до `±1000`.
- [ ] Запустить `go test ./internal/api -run 'TestParseControlScroll|TestClampScroll'` и проверить падение из-за отсутствующего поведения.
- [ ] Реализовать поле, тип кадра и ограничение.
- [ ] Запустить тот же тест и проверить успех; сделать коммит `feat(api): parse scroll control frame`.

### Task 2: Управление историей tmux

**Файлы:** `internal/runtime/runtime.go`, `internal/runtime/tmux.go`, `internal/runtime/tmux_scroll_test.go`, `internal/session/manager.go`, фейки интерфейса Runtime в тестах.

**Интерфейс:** `ScrollHistory(ctx context.Context, h Handle, lines int) error`, `ExitHistory(ctx context.Context, h Handle) error`; такие же методы менеджера с `id string`.

- [ ] Добавить тест на реальном tmux: вывод 500 строк; `-10` входит в copy-mode и ставит `scroll_position=10`; `+10` выходит; `+5` вне режима ничего не делает; `ExitHistory` выходит и повторно безопасен.
- [ ] Запустить `go test ./internal/runtime -run TestTmux_Scroll` и проверить падение из-за отсутствующих методов.
- [ ] Реализовать команды tmux на конкретной панели; добавить интерфейс, обёртки менеджера и методы фейков.
- [ ] Запустить `go build ./...` и тест runtime; сделать коммит `feat(runtime): scroll tmux pane history via copy-mode`.

### Task 3: WebSocket-обработчик

**Файлы:** `internal/api/term.go`, `internal/api/term_test.go`.

**Интерфейс:** кадр `scroll` вызывает `Manager.ScrollHistory`; первый бинарный ввод после него вызывает `Manager.ExitHistory` до записи в PTY.

- [ ] Добавить WS-тесты для порядка операций после прокрутки, ввода из другой вкладки и после переподключения, обычного ввода, readonly, нулевого кадра, ограничения и ошибки runtime.
- [ ] Запустить `go test ./internal/api -run TestSessionTermScroll` и проверить падение из-за отсутствующих вызовов.
- [ ] Реализовать обработку с состоянием на сессию; ошибки runtime оставлять локальными и повторять неудачный выход при следующем вводе.
- [ ] Запустить тест и сделать коммит `feat(api): scroll frame scrolls tmux history; input exits it`.

### Task 4: Документация и проверка

**Файлы:** `docs/03-daemon-api.md`.

- [ ] Описать знак, предел, readonly, copy-mode и выход по первому вводу.
- [ ] Запустить `go vet ./...`, `go test ./...` и живой тест на временной tmux-сессии.
- [ ] Сделать коммит `docs: term scroll frame`, открыть PR в `main`.
