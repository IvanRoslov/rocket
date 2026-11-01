# Tmux Delivery — полный план первого этапа, v2

> **Для воркеров:** REQUIRED SUB-SKILL: используйте superpowers:executing-plans для выполнения своего брифа по шагам. Для продуктового кода обязательны superpowers:test-driven-development, systematic-debugging при сбоях и verification-before-completion; перед PR — requesting-code-review. Оркестратор координирует через Rocket, код реализуют воркеры.

**Цель:** Добавить Pi отдельный канал, который сам не меняет ручной ввод, с явными границами диалогов и последующих действий Pi; сохранить уже полученный исследовательский результат Codex/Kimi.

**Архитектура:** Сохранённый выбор транспорта отделяет новые Pi-сессии от legacy. Приватный UDS-мост расширения Pi принимает сообщения, а очередь Rocket подтверждает их по истории. Рабочие запуски Codex/Kimi на этом этапе не меняются.

**Стек:** Go 1.25, SQLite migrations, Unix sockets / JSONL, TypeScript Pi Extension API 1.4.0, существующий tmux runtime.

**Спецификация:** `docs/superpowers/specs/2026-10-31-tmux-delivary-design.md`.

## Общие ограничения

- Старый Go250 по v1 продолжает разрешать ядро5905 и завершённое исследование5907. Изменённый Pi scope ждёт нового Go Ивана по полной Spec/Plan и stable merged/content-verified5905, затем явного release оркестратора. Одна задача = одна ветка = один финальный PR; существующий draft163 сохраняется для задачи2.
- Сохраняем штатные терминалы; Claude и legacy продолжают существующую доставку.
- Новый транспорт: pi-uds-v1; протокол 1; кадр не больше 1 MiB; runtime-каталог 0700, сокет 0600.
- Новый путь Pi включается с версии 1.4.0 и успешного handshake; старые версии/сессии остаются legacy. Неуспешный старт нового канала не означает разрешение fallback для уже выбранной native-сессии.
- Режим Pi — followUp; нет собственного editor/setText/paste/submit, clear/Enter/SendKeys и раскрытия команд/шаблонов. Пользовательские extensions не удаляются и не переставляются; private fields, production monkeypatch и fork запрещены.
- Запрос содержит session ID, launch instance ID, message ID; unknown не переотправляется автоматически.
- Native accepted/unknown подтверждаются по истории; при завершении/restore либо QueueTimeout без подтверждения — native_delivery_unconfirmed. Отключённый таймаут означает ожидание до завершения/restore.
- Codex/Kimi — только изолированное исследование без изменения рабочего launch. Для следующего подключения нужны новая спецификация и Go.
- Проверки Pi используют только изолированные PTY/store/daemon/socket и локальный детерминированный provider. Не использовать платных провайдеров, реальные auth или сообщения рабочим участникам; не менять общий daemon/config/установку. В артефактах нет credentials и рабочих чатов.
- Known prompt/compaction guard до API даёт NotSent; скрытое/запаздывающее окно может не блокировать ход. После возможного вызова API — Accepted/Unknown до точного one-to-one доказательства в нужной истории, без ложного NotSent и blind resend.
- Escape/dequeue/custom close Pi могут самостоятельно перенести очередь в редактор/поставить курсор в конец. Мост не восстанавливает ввод скрытой записью; editor echo не delivered. Реальная потеря draft/синтетический выбор/неверная история вследствие доставки останавливают release.
- #5911 отдельно владеет Claude SocketSender; её product edits ждут stable5905 и совместного разбора queue/confirm/store/launch/hooks/wiring. NativeLaunch не означает Claude lifecycle identity.

## Фокус ревью

- Выбранный канал пропал после рестарта Rocket: сообщение не попадает в tmux.
- Одинаковый текст с разными message ID: второе сообщение не исчезает как ложный дубль.
- Возврат ответа моста не доказывает сохранение в истории: delayed/error сохраняет честное состояние.
- Restore или смена Pi-истории: старый endpoint не адресует новый контекст.
- Позднее событие окна, deferred input/compaction или Escape после followUp: нет ложного NotSent/delivered и автоматического дубля. Проверки входят в задачу2.
- Journal до inject_at, равные тексты, одновременное подтверждение, retention и restart: старое/чужое доказательство не подтверждает новое сообщение. Проверки входят в задачи1–2.

Каждый пункт закреплён проверками в задачах ниже.

## Размещение изменений и контракт между задачами

- `internal/nativeinput/protocol.go`, `client.go`: общий код Pi UDS, кадры и результаты; без зависимостей от queue/store/agent.
- `internal/queue/native.go`, `native_test.go`: выбор нового транспорта, отправка и переходы состояний; legacy-код не переписывается.
- `internal/queue/queue.go`, `confirm.go`: подключение нового маршрута и отдельные правила восстановления/подтверждения.
- `internal/store/sessions.go`, `messages.go`, очередная миграция: snapshot режима сессии и сообщения.
- `internal/agent/pi/delivery.go`, `delivery-extension.ts`, `pi.go`: поставка расширения и декларация native запуска.
- `internal/session/manager.go`: применение декларации до старта нового/восстановленного процесса.
- `internal/daemon/daemon.go`: подключение реального отправителя к очереди.
- `docs/features/tmux-delivary/*-live-recon.md`: факты из изолированных проверок.

Контракт задачи 1, которым пользуется задача 2:

- `nativeinput.Target{SessionID, InstanceID, SocketPath string}`.
- `nativeinput.Request{Version int, SessionID, InstanceID string, MessageID int64, Text string}`.
- `nativeinput.Outcome` = `NotSent`, `Accepted`, `Unknown`.
- `nativeinput.Send(ctx context.Context, target Target, messageID int64, text string) (Outcome, error)`.
- `queue.NativeSender` с методом `Send(ctx context.Context, target nativeinput.Target, messageID int64, text string) (nativeinput.Outcome, error)`; `(*Queue).SetNativeSender(sender NativeSender)`.
- Snapshot сессии: `DeliveryTransport`, `DeliveryInstance`, `DeliveryEndpoint` (string); snapshot сообщения: `DeliveryTransport` (string). Пустой/legacy — прежний путь; pi-uds-v1 — только NativeSender.
- `agent.NativeLaunch{Transport, InstanceID, Endpoint string}` и необязательный `agent.NativeLaunchProvider` с `PrepareNativeLaunch(spec LaunchSpec) (NativeLaunch, error)`. Обычный Agent не обязан реализовывать интерфейс. Manager применяет результат до SetupWorkspace/LaunchCommand через поле `LaunchSpec.NativeDelivery` и сохраняет snapshot до создания процесса.

Формат request JSON: `v`, `session_id`, `instance_id`, `message_id`, `text`; response повторяет идентичность запроса и содержит outcome accepted/not_sent. Клиент ограничивает весь обмен5s и кадр1MiB включая newline. Совместимость расширения/API и identity/version проверяются без синтетического user prompt для readiness; одного номера версии недостаточно. Служебная проверка готовности не разрешает подменять nativeinput контракт его расходящейся копией; необходимые общие изменения проходят root review после stable handoff5905.

## Задача 1: Контракт native-канала и безопасная очередь

**Статус:** #5905 in_progress, replacement review/fix loop; PR162 draft, не baseline. Продолжается по старому Go.

**Результат:** проверяемый новый маршрут с fake NativeSender, выключенный для существующих сессий. Самостоятельный PR, без расширения Pi.

**Файлы:** создать internal/nativeinput/{protocol.go,client.go,client_test.go}, internal/queue/{native.go,native_test.go}; изменить internal/agent/agent.go, internal/store/{sessions.go,messages.go}, очередную миграцию, internal/session/manager.go, internal/queue/{queue.go,confirm.go}, internal/daemon/daemon.go; добавить целевые store/session тесты. Номер миграции брать следующий свободный при реализации.

**Потребляет:** существующие prepareText, ClaimMessage, InjectAt, accepted/DeliveryConfirmer и очередь событий.

**Производит:** контракт выше, атомарное сохранение snapshots, выбор native без screen probing; безопасный client и восстановление состояний.

- [ ] Написать падающие тесты `TestNativeDeliveryDoesNotTouchTmux`, `TestNativeUnavailableNeverFallsBack`, `TestNativeUnknownDoesNotRetryAfterRecover`, `TestNativeDuplicateTextUsesDistinctMessageIDs`, `TestLegacySessionMigrationDefaults`, `TestNativeRestoreChangesInstance`. Проверки: ноль Inject/SendKeys/Capture на native; до send сохранены transport и inject_at; unknown остаётся под подтверждением; legacy fixtures проходят без native.
- [ ] Запустить целевые тесты и зафиксировать ожидаемые падения из-за отсутствующего контракта/поведения.
- [ ] Реализовать snapshot миграцию и NativeLaunchProvider: ошибка подготовки не запускает процесс с другим транспортом; legacy адаптеры не меняют argv/env.
- [ ] Реализовать `nativeinput.Send` и ограниченное чтение/проверку ответа. До начала передачи ошибка = NotSent; после возможной передачи без достоверного ответа = Unknown. Протокол чужой сессии/запуска не считается Accepted.
- [ ] Добавить маршрут в очередь перед визуальными gates. Повтор NotSent использует существующие лимиты; Accepted/Unknown не проходят через старое giveUpAccepted с переотправкой. Перед terminal failure сначала проверить историю, включая уже завершившийся/восстановленный получатель, чтобы не пропустить фактическую доставку.
- [ ] Добавить тесты разрыва до/после записи, oversized/malformed ответа, смены instance, delivering/recover, поздней истории, disabled QueueTimeout и terminal failure. Два равных текста требуют двух записей; сохранённое proof не переиспользуется после Recover.
- [ ] Регрессии review: queued с nonzero Attempts всё равно CAS-claim; окончательный scan из delivering реально settle/fail, ограничен context/deadline и использует сохранённый inject_at; запись до claim не доказывает доставку. Атомарное потребление proof ограничено получателем, чужая независимая история не конфликтует. Неизвестный transport/неполная launch declaration не падают в tmux; retention сохраняет pending native и нужные ему proofs. Restore marker должен обрабатываться для native и legacy одного получателя без взаимного поглощения.
- [ ] Запустить `go test ./internal/nativeinput ./internal/store ./internal/queue ./internal/session ./internal/agent/... ./internal/daemon`, затем `go test ./...` и `go test -race ./internal/nativeinput ./internal/queue ./internal/session`.
- [ ] Провести ревью, commit/PR, добиться зелёных доступных CI. Если CI в репозитории отсутствует, показать это явно и приложить локальные проверки.

## Задача 2: Мост Pi и рабочее подключение

**Зависит от:** нового Go Ивана по полной Spec/Plan v2, merged PR задачи1, rocket verify-merge5905 и явного release root с exact stable SHA. До этого — только сохранение/уточнение уже разрешённого preflight, без product edits/новых широких probes/копий контракта. Решение CTO5902/Q2 не является этим Go.

**Результат:** новые/restore Pi 1.4.0+ используют pi-uds-v1 в границах матрицы Spec v2; старые сессии работают как раньше. Draft PR163 пока содержит только preflight. Он не принимается за готовую задачу2.

**Файлы:** создать internal/agent/pi/delivery.go, delivery-extension.ts, delivery_test.go, delivery-extension.test.ts; изменить internal/agent/pi/pi.go и при необходимости confirm.go/confirm_test.go, целевые session/queue tests. Сохранить probes в scripts/probes/tmux-delivary; обновить docs/features/tmux-delivary/{pi-live-recon,pi-modal-safety,pi-test-matrix}.md и docs/{06-messaging,10-agents}.md.

**Потребляет:** NativeLaunchProvider и nativeinput контракт задачи 1, существующий Pi DeliveryConfirmer.

**Производит:** `(*Pi).PrepareNativeLaunch(spec agent.LaunchSpec) (agent.NativeLaunch, error)`, embedded расширение и явный launch flag; приватный UDS с правильным lifecycle.

### 2A. Привязка к merged контракту и запуск

- [ ] Получить root release с exact merge SHA и результатом verify-merge5905, импортировать main в своей ветке; прочитать полный финальный контракт, не переносить промежуточный dbcec3c как baseline.
- [ ] Написать RED `TestPiNativeLaunchVersionGate`, `TestPiNativeLaunchKeepsUserExtensions`, `TestPiNativeUnavailableNeverFallsBack`: Pi1.0–1.3/старый snapshot — legacy; native declaration только для поддержанного API1.4+; identity/version readiness проверяются без синтетического user prompt. Пользовательские extensions/их порядок не меняются; после выбора native недоступность не даёт tmux.
- [ ] Запустить `go test ./internal/agent/pi ./internal/session -run 'Native|Extension'` и зафиксировать RED.
- [ ] Реализовать `(*Pi).PrepareNativeLaunch(spec agent.LaunchSpec) (agent.NativeLaunch, error)` в delivery.go, embedded extension, короткий private endpoint и новый launch ID при restore; явный --extension только для выбранного native. Применить существующий NativeLaunchProvider, не вводить второй NativeLaunch/NativeSender.
- [ ] Запустить тот же Go набор до GREEN, commit `feat(pi): prepare native extension launches`.

### 2B. Сокет, outcomes и lifecycle

- [ ] Написать RED в delivery-extension.test.ts с fake ExtensionAPI, без модели: `rejectsWrongSessionOrInstance`, `boundsFramesByBytes`, `decodesSplitUTF8`, `deduplicatesMessageID`, `keepsDistinctEqualTexts`, `knownPromptAndCompactionRefuseBeforeAPI`, `possibleAPICallNeverReturnsNotSent`, `shutdownReloadAndSwitchCloseOldEndpoint`.
- [ ] Запустить `node --experimental-strip-types --test internal/agent/pi/delivery-extension.test.ts`; ожидается RED до bridge. Test adapter не должен импортировать CLI credentials или обращаться к сети.
- [ ] Реализовать bridge в delivery-extension.ts: один JSONL request/response по контракту задачи1; target/version validation до API, лимит1MiB в байтах включая newline, buffer для split UTF-8, permissions0700/0600, session_start/shutdown/switch/reload, dedupe живого launch. Неверный/закрытый target не вызывает API; старый endpoint не пересоздаётся для другой истории. Accepted ACK не служит history proof.
- [ ] Реализовать известные prompt/compaction guards через поддерживаемые события. Отказ до API — NotSent; после возможного вызова API — Accepted/Unknown, даже при асинхронной ошибке. Не обещать guard для скрытого/запаздывающего состояния и не изобретать гонку в синхронном участке без await. Обычный busy продолжает followUp.
- [ ] Запустить Node-набор до GREEN, commit `feat(pi): bridge native messages without editor mutation`.

### 2C. Подтверждение в истории и восстановление

- [ ] Написать RED `TestPiNativeProofRequiresTargetHistory`, `TestPiNativeEqualTextProofIsOneToOne`, `TestPiNativeUnknownSurvivesRecover`, `TestPiEditorEchoDoesNotConfirmDelivery`, `TestPiSwitchClosesOldEndpoint`. Точный user record после inject_at в нужной истории — proof; editor/queue/assistant echo и запись до отправки — нет. Два ID с равным текстом требуют двух входящих записей.
- [ ] Запустить `go test ./internal/agent/pi ./internal/queue ./internal/session -run 'Native|Confirm|Switch'` до RED; расширить настоящий queue/store путь, а не дублировать его в mock.
- [ ] Подключить существующий Pi DeliveryConfirmer и проверить stable Rocket session/launch binding. При необходимой технической корректировке confirm.go сохранять legacy и контракт ядра. После switch проверять прежнюю целевую историю; отсутствие доказательства заканчивается unconfirmed, а не чужим совпадением. О любой дополнительной общей schema/interface правке сначала сообщить root для координации с5911.
- [ ] Проверить temporary isolated daemon/store: потеря endpoint до/после possible write, crash между claim/result, Recover, restore/kill/switch/reload. Accepted/Unknown не переотправляются, NotSent ограниченно повторяется; startup не оставляет доверенный старый endpoint; завершённые ресурсы не накапливаются.
- [ ] Запустить Go-набор до GREEN, commit `feat(pi): confirm native delivery through the bound transcript`.

### 2D. Матрица настоящего stock TUI на локальном provider

Каждый сценарий — изолированный PTY, непрерывный reader, отдельные PI_CODING_AGENT_DIR/session-dir/store/socket и только собственные child groups. Повторяется нужная production-ветка bridge, а не только старый preflight. Полные логи/auth/config ignored; публикуются небольшие sanitized JSON с версиями, before/after, outcome, счётчиками истории и cleanup. Никаких платных/внешних провайдеров и live workers.

- [ ] Idle/busy × русский single/multiline mid-line cursor, пустое поле, длинный inbox pointer: точные text/cursor до API, после и после обработки. Injected user ровно1, busy ordering правильный; одновременно подтвердить ноль tmux Inject/SendKeys/Capture на queue native path.
- [ ] Model selector и control timing: селектор не закрыт, выбор не синтезируется, ввод идёт в selector search, Escape возвращает прежний editor. Source-only вывод о других showSelector ветках оставить именно source-only, если отдельной live проверки нет.
- [ ] Изолированные extension select/confirm/input/editor/custom и более ранний async listener: known guard отказывает до API; delayed window может принять, history/outcome честные. В custom close отдельно записать собственный setText Pi и изменения непосредственно при доставке; без скрытого восстановления editor.
- [ ] Compaction before/success/failure/cancel, deferred prompt/input-handler error: known refusal — NotSent с0API calls; после possible API — Accepted/Unknown и journal proof либо unconfirmed без повтора. Проверка на fake API покрывает детерминированные интервалы, stock runtime — доступные реальные события; точные границы обеих проверок указать.
- [ ] Busy followUp затем Escape/dequeue: показать отдельно изменения из-за доставки и последующего штатного действия Pi. Текст в editor не delivered; незаписанный user record остаётся unconfirmed, автоматического повтора нет. Нельзя объявлять порчу исходного draft нормой.
- [ ] Reload/switch/kill настоящего тестового Pi: старый endpoint закрыт, прежний request не попадает в новую историю. Login/OAuth не выполнять с реальными auth; непроверенные UI branches явно перечислить, никакого общего обещания всех extensions.
- [ ] Если обнаружены потеря draft, синтетический выбор или неверная история вследствие доставки — прекратить release, сохранить evidence и сообщить root для пересмотра. Не подменять невозможную универсальную гарантию количеством PASS.
- [ ] Обновить pi-live-recon/pi-modal-safety/pi-test-matrix и эксплуатационные docs простым описанием: возможен ответ при открытом окне, Escape/dequeue может вернуть очередь и сдвинуть курсор; только history означает delivered. Commit `docs(pi): record native delivery guarantees and runtime evidence`.

### 2E. Финальная проверка и PR

- [ ] Запустить `go test ./internal/agent/pi ./internal/session ./internal/queue ./internal/nativeinput`, `go test ./...`, `go test -race ./internal/agent/pi ./internal/queue ./internal/nativeinput`, `go vet ./...`, Node bridge suite и `git diff --check`. Для зависаний задавать bounded timeout и читать stack, не повторять900s вслепую.
- [ ] Свежее независимое ревью полного final diff; исправить blockers и повторить затронутые проверки. Обновить существующий draft PR163 вокруг законченного результата; указать exact head, команды/exit, матрицу unknown, отсутствие CI если оно сохраняется. Root делает merge/verify, воркер не merge самостоятельно.

## Задача 3: Изолированная проверка Codex/Kimi — сохранённый результат

**Статус:** #5907 done, PR164 merged в af9189f832e40b10d145bc67347aabf787e3a13a; rocket verify-merge5907 подтвердил полное совпадение remote деревьев. Воркер убран. Не запускать исследование повторно.

**Файлы:** docs/features/tmux-delivary/{codex-live-recon,kimi-native-recon}.md, evidence/*.sanitized.json, scripts/probes/tmux-delivary/{codex_native_probe,render_tui,ws_request}.py.

**Интерфейсы:** Потребляет установленные CLI/первичные источники, не вводит product API. Производит scoped evidence для следующей спецификации, не разрешение менять рабочие Codex/Kimi.

- [x] Версии/help/env checked; Codex private app-server + stock TUI + внешний sender на одном UUID, idle/busy/followUp и окружение двух серверов подтверждены.
- [x] Черновик, offline queue refusal и server restart проверены; mid-line cursor и отдельный TUI restart явно непроверены. Неудачный observer probe не превращён в PASS; неисполненные success-path assertions удалены.
- [x] Kimi2.4.0 checked official surface: supported same-process input не найден; takeover/second writer исключены; background completion отделён от generic inbox.
- [x] Синтаксис Python/JSON/diff, renderer и Go suite проверены; независимый review закрыт, CI отсутствует и не назван green. Ни production, ни глобальные auth/config/daemon не менялись.

## Порядок выполнения и завершение

1. Подготовить полные Spec v2/Plan v2, сравнительную delta и короткий plain brief; получить CTO review, затем сохранить документы в5902 и запросить один новый человеческий gate. Ответ в Q2 не Go. До него Pi product edits остановлены; ядро5905 завершает прежний review/fix loop.
2. #5905: закрыть blockers, final required checks, разрешённый merge, rocket verify-merge5905. Передать exact stable SHA/schema/interfaces/hunk map5911 для совместного review. Никакого чужого spawn/product edits до её зависимости.
3. Только после нового Go + verified merge5905 root выпускает существующего worker5906 с полным английским brief. Перед любым новым spawn — rocket models ls, разрешённый профиль по задаче, запись решения. Не использовать сломанный OAuth как повод менять registry/allowlist.
4. Worker вопросы root решает сам; новые продуктовые компромиссы — отдельный thread, новая полная spec/gate. Изолированные синтетические проверки из этого плана не означают разрешение deployment, общего restart или реального send участникам.
5. Для merged PR — rocket verify-merge, subtask done и kill --cleanup без прощального сообщения. #5907 уже прошла этот шаг; draft163 ещё нет.
6. Финальная проверка объединённого результата: mandatory Go/Node checks и конкретные stock-TUI evidence, legacy/Claude regressions. Показать точные версии/heads, границы исходных и новых проб; не выдавать source-only/непроверенное за live PASS.
7. Сохранить report5902, проверить отсутствие workers, перевести родителя в review. Человек принимает и ставит done. Codex/Kimi остаются на прежней доставке; #5911 принимается отдельно.

## Delta и самопроверка плана

По сравнению с v1 добавлена честная матрица modal/deferred/abort состояний и проверка последующих действий Pi без editor patch. Не изменены протокол/outcomes/no-resend, штатные TUI/плагины и границы Claude/Codex/Kimi. Завершённое исследование не перезапускается. Новая зависимость — человеческий Go по изменённому Pi scope.

Покрытие Spec: ядро и lifecycle snapshots — задача1; stock input/API/UDS/modal/abort/history/recovery —2A–2D; final checks/review —2E; исследовательские выводы — завершённая3. Имена NativeLaunch/NativeSender/nativeinput совпадают с контрактом1; финальный stable SHA pin берётся только из handoff после merge/verify, а не из промежуточного head. Для каждого Review Focus есть регрессия1 или сценарий2. Общей гарантии произвольных extensions план не обещает.
