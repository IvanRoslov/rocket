# Матрица проверок Pi native bridge

Задача: #5902, подзадача 2 (#5906). Дата: 1 ноября 2026.

Документ фиксирует evidence. Numbered Pi path реализован на ветке `feature/tmux-delivary/pi-bridge-cc` после shared-передачи и Go260; итоговая текущая матрица — в разделе «Итоговая текущая матрица numbered path» ниже. Ранние разделы — историческая хронология preflight/промежуточных стадий, не текущий PASS. Промежуточный commit `d0dffa1` не использовался как контракт.

## Воспроизводимый pre-flight harness

Файлы harness сохранены в репозитории:

- `scripts/probes/tmux-delivary/pty_preservation_probe.py`
  - SHA-256: `ab70099d4eb17328e855fa3d01ec2660ddf8ff5f99a1fdaf902dff6d3aa4b7a2`.
- `scripts/probes/tmux-delivary/preservation-extension.ts`
  - SHA-256: `c2581c2cabf085f77b7fc614b13a0ea370558f6899f54f67ec7ed867edab8866`.

Запуск из корня worktree:

```bash
python3 scripts/probes/tmux-delivary/pty_preservation_probe.py
```

Провайдер — **только детерминированный локальный тестовый backend** `probe/deterministic` внутри изолированного расширения. Он возвращает `Deterministic local response` после 100 ms в idle и 2500 ms в busy. Внешняя сеть и учётные данные не используются. Это не DeepSeek и не проверка production provider.

Каждый сценарий использует отдельные PTY, `PI_CODING_AGENT_DIR` и `--session-dir`. PTY непрерывно читается фоновым reader-потоком, чтобы вывод TUI не блокировал socket RPC. Проверяются фактические `pi --version`, состояние `CustomEditor`, JSONL-транскрипт и порядок сообщений.

## Уже подтверждено

Итоговый прогон по committed-path: 4/4 PASS за 13.751 секунды; `go test ./...` — все пакеты PASS.

| Проверка | idle single | idle multiline | busy single | busy multiline |
| --- | --- | --- | --- | --- |
| Русский черновик сохранён | PASS | PASS | PASS | PASS |
| Точный курсор сохранён | `(0,18)` | `(1,20)` | `(0,21)` | `(1,15)` |
| `sendUserMessage`, `followUp`, без expansion | PASS | PASS | PASS | PASS |
| Один injected user в транскрипте | 1 | 1 | 1 | 1 |
| Вызовы provider | 1 | 1 | 2 | 2 |
| Порядок user → assistant → user → assistant | н/д | н/д | PASS | PASS |

Маленькие sanitized-доказательства: `scripts/probes/tmux-delivary/results/<scenario>/result.json`. Полные `pty.raw`, `events.jsonl`, транскрипты и временные каталоги остаются git-ignored.

## Матрица после merged-contract release

### Go и контракт запуска

| Проверка | Ожидаемый результат |
| --- | --- |
| `TestPiNativeLaunchVersionGate` | Pi 1.4.0+ и successful handshake включают native; 1.0–1.3 и старые сессии остаются legacy; неуспешный запуск не даёт fallback |
| `TestPiBridgeRejectsWrongInstance` | Чужой session/launch не принимается и не попадает в историю |
| `TestPiBridgeDeduplicatesMessageID` | Повтор ID в живом запуске не вызывает второй API submit |
| `TestPiBridgeKeepsDistinctEqualTexts` | Равный текст с разными ID становится двумя сообщениями |
| `TestPiSwitchClosesOldEndpoint` | Switch/restore закрывает старый endpoint; новая история недоступна старому каналу |
| Native route vs tmux | Ноль `Inject`, `SendKeys`, `Capture` на native; нет silent fallback |
| Journal ownership | Accepted/Unknown подтверждаются только journal; Recover не переотправляет Unknown; terminal failure сначала проверяет историю |

### Node/Pi extension и UDS lifecycle

| Проверка | Ожидаемый результат |
| --- | --- |
| `session_start` | Создаются private short-path UDS и launch identity |
| `session_shutdown` | Endpoint и ресурсы закрываются, завершённые сессии не копятся |
| Switch/restore | Старый endpoint закрыт, новый launch получает новый instance ID |
| Target validation | Неверная версия/session/instance отклоняется до API |
| Frames | JSONL protocol 1; кадр больше 1 MiB и malformed input отклоняются |
| IDs | Один ID дедуплицируется; два ID с равным текстом сохраняются |
| API options | `followUp`, `expandPromptTemplates: false`; нет `setText`, editor submit, tmux paste/Enter |
| Unsafe state | Dialog/compaction возвращает refusal до вызова API |
| Ack honesty | Возврат socket не означает delivered; Accepted/Unknown ждут journal confirmation |
| Connection loss | До записи — NotSent; после возможной записи — Unknown, без слепого retry |

Примечание по unsafe state: см. `pi-modal-safety.md`. Встроенный model selector live-доказан как safe-to-accept, общий `showSelector` — source-level. Публичного признака открытого встроенного селектора/login/reload нет, поэтому строгое «любое небезопасное встроенное модальное состояние ⇒ not_sent» текущим Extension API не реализуемо; нужен продуктовый выбор (принять safe-to-accept, upstream accessor или изоляция расширений).

### Live matrix на изолированных сессиях

| Область | Сценарии и доказательства |
| --- | --- |
| Пустой ввод | Одно и несколько сообщений появляются один раз в правильной истории |
| Черновики | Русский одно-/многострочный текст и mid-line cursor сохраняются в idle/busy |
| Длинное сообщение | Существующая подготовка inbox/ссылки сохраняется; кадр ограничен |
| Дубликаты | Повтор ID не дублируется; равный текст с новым ID не исчезает |
| Потеря сокета | До/после принятия различаются; unknown не вызывает автоматический повтор |
| Rocket restart | Только изолированные daemon/store/socket; recovery подтверждает через journal, не через tmux |
| Pi restore/kill/switch | Старый endpoint не адресует новую или соседнюю историю |
| Legacy | Pi 1.0–1.3 и ранее запущенные сессии сохраняют прежний транспорт |
| Отказ native | После выбора native нет молчаливого возврата к tmux |

### Команды верификации

```bash
go test ./internal/agent/pi ./internal/session ./internal/queue ./internal/nativeinput
go test ./...
go test -race ./internal/agent/pi ./internal/queue ./internal/nativeinput
node --experimental-strip-types --test internal/agent/pi/delivery-extension.test.ts
```

Отсутствие GitHub Actions workflows не выдаётся за зелёный CI.

## Review-исправления 1 ноября 2026

После раннего review закрыты пять блокеров в Pi-owned файлах; каждый шаг RED→GREEN.

| Блокер | Исправление | RED-проверка |
| --- | --- | --- |
| Привязка к реальной истории после switch+reload | `ctx.sessionManager.getSessionId()` проверяется при открытии endpoint и перед каждым submit; чужой/недоступный context закрывает канал с нулём API-вызовов | `refusesStartInAForeignHistory`, `reloadAfterSwitchNeverRoutesIntoNewHistory`, `submissionRequiresBoundHistory`, `historyGetterFailureFailsClosed` |
| Уникальность endpoint | Ключ сессии расширен до 128 бит; endpoint содержит launch instance (`s-<instance>`); поздняя очистка старого запуска не трогает новый endpoint | Go `TestPiNativeCollisionIDsProduceDistinctTargets` (`rocket-native-collision-37639`/`133628`), `TestPiNativeLateCleanupKeepsNewEndpoint`, `TestPiNativeRestoreChangesInstance` |
| Долговечная дедупликация | Запись accepted ID синхронно до API; ошибка записи даёт `not_sent` с 0 вызовов; нечитаемое/повреждённое состояние не становится пустым набором | `reservesAcceptedIDBeforeAPI`, `unreadableAcceptedStateFailsClosed`, `corruptAcceptedStateFailsClosed`, `persistenceFailureReturnsNotSentWithoutAPI` |
| Ограничение старта и прав | Ошибка chmod сокета закрывает и удаляет endpoint; rejection `start()` не выходит за handler-границу | `socketPermissionFailureFailsClosed` (RED), `endpointDirectoryBindFailureIsContained` и `danglingAcceptedSymlinkFailsClosed` (regression-покрытие существующего поведения) |
| Один обмен на соединение | Обрабатывается только первый JSONL-кадр; кадры в том же или следующем chunk не вызывают API | `oneExchangePerConnection`, `trailingChunkCannotSubmit` |
| Возврат A→B→A после switch | Нерелоад-завершение пишет durable `retired-<instance>.marker`; возврат в ту же историю не переоткрывает launch; accepted state сохраняется; новый Rocket launch очищает marker | `retiredLaunchDoesNotReopenAfterSwitchAndReturn`, `shutdownReloadAndSwitchCloseOldEndpoint`, Go `TestPiNativeNewLaunchClearsRetirement` |
| Частичная/неканоническая запись accepted | Файл обязан заканчиваться newline, каждая запись строго `/^[1-9][0-9]*$/`; иначе старт отклоняется с 0 вызовов | `truncatedAcceptedTailFailsClosed`, `nonCanonicalAcceptedRecordsFailClosed` |

Результат: `node --experimental-strip-types --test internal/agent/pi/delivery-extension.test.ts` — 28/28 PASS; `go test ./internal/agent/pi -count=1` — PASS; `go test -race ./internal/agent/pi` — PASS; `go vet ./...` — чисто.

Полный `go test ./...`: все пакеты PASS, кроме одного intermittent падения `internal/api` `TestParticipantFanOut_SkipsAuthorAndHuman` (`TempDir RemoveAll cleanup: directory not empty`). Целевой повтор теста и повторный прогон пакета — PASS. Это известный TempDir-cleanup flake, не связанный с изменёнными Pi-файлами; зелёным полный прогон не объявляется.

## Повторная live-проверка (ограниченная)

Прогон только затронутых сценариев на stock Pi 1.4.0 с локальным детерминированным provider:

```bash
python3 scripts/probes/tmux-delivary/native_bridge_probe.py \
  --scenario reload --scenario target-frames --scenario foreign-history
```

Fixture теперь запускает Pi с `--session-id` равным ROCKET target и проверяет `sessionManager.getSessionId()` наблюдаемого процесса. Результаты: `reload`, `target-frames`, `foreign-history` — PASS. В `foreign-history` фактическая история `live-foreign-history-b` при target `live-foreign-history`: endpoint не создан, подключение отсутствует (`ENOENT`), 0 injected и 0 вызовов provider.

Этот ограниченный прогон заменён итоговой текущей матрицей ниже: все 13 preservation-сценариев и `handled-input` перезапущены на numbered bridge, старые pre-fix JSON перезаписаны.

## Q5 correlation witness (supported public API)

Дата: 1 ноября 2026. Отдельная проверка equal-text correlation, не входит в preservation-матрицу.

- Сценарий `handled-input` (stock Pi 1.4.0, локальный детерминированный provider, изолированные PTY/dirs/socket): два одинаковых extension-сообщения 1001/1002; публичный `input` handler вернул `{action:"handled"}` для первого `source:"extension"` вызова и `continue` для второго идентичного.
- Результат PASS: оба bridge-ответа `accepted`; sources `["extension","extension"]`; одна `handled`, одна `continued`; в транскрипте ровно одна совпадающая user-запись; 1 вызов provider; наблюдаемая Pi-сессия равна ROCKET target. Sanitized: `scripts/probes/tmux-delivary/results/native-bridge/handled-input/result.json`.
- Источник Pi 1.4.0: `agent-session.js` `prompt` → `_runInputHandlers`; `{action:"handled"}` возвращается до записи user record. `runner.js` `emitInput` перехватывает исключения handler'а, вызывает `emitError` и продолжает к следующему handler'у (итог — continue/transform). Поэтому брошенная ошибка input-handler'а сама по себе **не** доказывает отсутствие записи.
- Корректная формулировка: реальный witness — `action:"handled"`. Node-фикстура `mockAcceptedCallMayLeaveNoRecord` — только модель accepted/no-record, не live-доказательство.
- Counterexample из `internal/queue/pi_equal_text_correlation_test.go` сохранён как историческое RED-доказательство (Q5). Текущая версия теста проверяет уже исправленное поведение numbered path: A не подтверждается записью B, B подтверждается только своей подписанной записью.

## Строгий native proof (Pi-owned, additive)

Дата: 1 ноября 2026. Ранняя независимая часть Task1 по RELEASE-5906-V3-PI-PROOF-ONLY-GO260; в очередь/маршрут не подключено, shared hunks не тронуты.

- Добавлен `(*Pi).ConfirmNativeDelivered(ctx, ref, text, since)`: доказательством служит ровно одна user-запись, у которой **всё объединённое text content** после существующей нормализации пробелов равно ожидаемому тексту. Не подстрока, не `holdsLines`, не отдельные text parts.
- Существующий `ConfirmDelivered`/`holdsLines` для legacy не изменён; это проверено контрастным тестом.
- RED→GREEN: `go test ./internal/agent/pi -run TestPiNativeProof -count=1` (было RED со stub, затем GREEN). Покрыты: собственная полная подпись; цитата чужой полной подписи в большей записи; подпись в одном part при наличии другого текста; multiline/Unicode с нормализацией; чужой ID; assistant/custom/editor echo; запись на boundary; number-only для непустого Body; пустой Body с полным service header; earliest match; cancelled context/ErrNoSignal/чужая сессия; неизменность legacy.
- Полный `go test ./internal/agent/pi`, `-race`, `go vet ./internal/agent/pi`, `git diff --check` — PASS/clean.
- Метод подключён в Task2 через приватный адаптер: `attemptNativeDelivery` подписывает отправку, accepted-native и финальный delivering scan используют ту же read-only реконструкцию; при отсутствии strict capability native остаётся unconfirmed без legacy-fallback.

## Итоговая текущая матрица numbered path (1 ноября 2026)

Прогон: `python3 scripts/probes/tmux-delivary/native_bridge_probe.py --all` — 14/14 PASS, exit 0 (13 preservation + `handled-input`). Fixture эмулирует numbered NativeSender: подпись `[rocket #<id>] ` ставится на wire/transcript, сам bridge остаётся текстово-агностичным; проверки транскрипта используют точный подписанный текст. Все 13 — current evidence, старые pre-fix JSON заменены этим прогоном.

| Сценарий | Ключевое evidence |
| --- | --- |
| idle-single | русский draft/cursor сохранены, injected 1, provider 1 |
| idle-multiline | многострочный draft/cursor сохранены, injected 1, provider 1 |
| empty | пустое поле остаётся пустым, injected 1, provider 1 |
| long-pointer | draft/cursor сохранены на inbox-pointer, injected 1, provider 1 |
| busy-single | draft/cursor сохранены во время хода, injected 1, provider 2 |
| busy-multiline | то же для многострочного draft, injected 1, provider 2 |
| selector | окно открыто, поиск/стрелки работают, Escape возвращает draft/cursor, injected 1, model_selects 0 |
| modal-guard | select/confirm/input/editor/custom: отказ `not_sent` при открытом окне, `accepted` после закрытия, provider 5 |
| delayed-modal | delayed: `accepted` при открытом окне, `ui_prompt_start` через 1202 ms, slow listener 1201 ms; control без slow listener: `ui_prompt_start` через 1 ms, `not_sent`, provider 0 |
| target-frames | неверные session/instance/version/id — `not_sent`, delta транскрипта 0; oversized `not_sent`; split UTF-8 1; два разных ID с равным текстом 2; повтор ID 1 |
| escape-dequeue | подписанный queued текст оказался в редакторе, курсор в конце, injected 0 |
| reload | тот же ID после `/reload` не отправлен повторно, injected 1, provider не вырос |
| foreign-history | endpoint отсутствует, 0 injected, 0 provider, наблюдаемая история `live-foreign-history-b` |

Сквозной queue→Pi (отдельно, opt-in): `ROCKET_PI_STOCK_E2E=1 go test -count=1 -timeout=90s ./internal/queue -run ^TestPiNativeQueueStockE2E$` — PASS (3.82 s): A `failed/native_delivery_unconfirmed` после timeout, B `delivered` своей записью; sender 1 вызов/ID, bridge API 1/ID, records A0/B1, tmux 0, Body не изменён. Evidence: `scripts/probes/tmux-delivary/results/native-bridge/pi-stock-e2e/result.json`.

Остаточные ограничения: login/OAuth и часть `showSelector`-веток — source-only; отложенный async `ui_prompt_start` — принятое исключение Spec2; отменённый switch оставляет канал закрытым до `/reload`; повреждённое accepted-состояние fail-closed до нового Rocket launch; возврат в старую историю после non-reload завершения требует restore; `queue_timeout`-политика для nil-confirmer не расширялась. GitHub workflows отсутствуют — пустой CI не считается зелёным.
