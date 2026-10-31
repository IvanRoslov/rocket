# Tmux Delivery — план первого этапа

> **Для воркеров:** REQUIRED SUB-SKILL: используйте superpowers:executing-plans для выполнения своего брифа по шагам. Для продуктового кода обязательны superpowers:test-driven-development, systematic-debugging при сбоях и verification-before-completion; перед PR — requesting-code-review. Оркестратор координирует через Rocket, код реализуют воркеры.

**Цель:** Добавить Pi отдельный канал без изменения ручного ввода и получить воспроизводимый результат проверки каналов Codex/Kimi.

**Архитектура:** Сохранённый выбор транспорта отделяет новые Pi-сессии от legacy. Приватный UDS-мост расширения Pi принимает сообщения, а очередь Rocket подтверждает их по истории. Рабочие запуски Codex/Kimi на этом этапе не меняются.

**Стек:** Go 1.25, SQLite migrations, Unix sockets / JSONL, TypeScript Pi Extension API 1.4.0, существующий tmux runtime.

**Спецификация:** `docs/superpowers/specs/2026-10-31-tmux-delivary-design.md`.

## Общие ограничения

- До Go нет воркеров и продуктового кода. Одна задача = одна ветка = один PR.
- Сохраняем штатные терминалы; Claude и legacy продолжают существующую доставку.
- Новый транспорт: pi-uds-v1; протокол 1; кадр не больше 1 MiB; runtime-каталог 0700, сокет 0600.
- Новый путь Pi включается с версии 1.4.0 и успешного handshake; старые версии/сессии остаются legacy. Неуспешный старт нового канала не означает разрешение fallback для уже выбранной native-сессии.
- Режим Pi — followUp; без изменения редактора, без раскрытия команд/шаблонов.
- Запрос содержит session ID, launch instance ID, message ID; unknown не переотправляется автоматически.
- Native accepted/unknown подтверждаются по истории; при завершении/restore либо QueueTimeout без подтверждения — native_delivery_unconfirmed. Отключённый таймаут означает ожидание до завершения/restore.
- Codex/Kimi — только изолированное исследование без изменения рабочего launch. Для следующего подключения нужны новая спецификация и Go.
- Реальные тесты используют только изолированные сессии/каталоги, не сообщения существующим участникам. В артефактах нет credentials и содержимого рабочих чатов.

## Фокус ревью

- Выбранный канал пропал после рестарта Rocket: сообщение не попадает в tmux.
- Одинаковый текст с разными message ID: второе сообщение не исчезает как ложный дубль.
- Возврат ответа моста не доказывает сохранение в истории: delayed/error сохраняет честное состояние.
- Restore или смена Pi-истории: старый endpoint не адресует новый контекст.
- Codex server process теряет ROCKET_* или получает переменные другой сессии: такой рецепт не рекомендуется к рабочему подключению.

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

Формат request JSON: `v`, `session_id`, `instance_id`, `message_id`, `text`. Handshake/ответ используют те же v/session/instance/message идентификаторы и явный outcome. Точные служебные поля handshake оставлены задаче 1; после её PR фиксируются в protocol.go и не переизобретаются задачей 2.

## Задача 1: Контракт native-канала и безопасная очередь

**Результат:** проверяемый новый маршрут с fake NativeSender, выключенный для существующих сессий. Самостоятельный PR, без расширения Pi.

**Файлы:** создать internal/nativeinput/{protocol.go,client.go,client_test.go}, internal/queue/{native.go,native_test.go}; изменить internal/agent/agent.go, internal/store/{sessions.go,messages.go}, очередную миграцию, internal/session/manager.go, internal/queue/{queue.go,confirm.go}, internal/daemon/daemon.go; добавить целевые store/session тесты. Номер миграции брать следующий свободный при реализации.

**Потребляет:** существующие prepareText, ClaimMessage, InjectAt, accepted/DeliveryConfirmer и очередь событий.

**Производит:** контракт выше, атомарное сохранение snapshots, выбор native без screen probing; безопасный client и восстановление состояний.

- [ ] Написать падающие тесты `TestNativeDeliveryDoesNotTouchTmux`, `TestNativeUnavailableNeverFallsBack`, `TestNativeUnknownDoesNotRetryAfterRecover`, `TestNativeDuplicateTextUsesDistinctMessageIDs`, `TestLegacySessionMigrationDefaults`, `TestNativeRestoreChangesInstance`. Проверки: ноль Inject/SendKeys/Capture на native; до send сохранены transport и inject_at; unknown остаётся под подтверждением; legacy fixtures проходят без native.
- [ ] Запустить целевые тесты и зафиксировать ожидаемые падения из-за отсутствующего контракта/поведения.
- [ ] Реализовать snapshot миграцию и NativeLaunchProvider: ошибка подготовки не запускает процесс с другим транспортом; legacy адаптеры не меняют argv/env.
- [ ] Реализовать `nativeinput.Send` и ограниченное чтение/проверку ответа. До начала передачи ошибка = NotSent; после возможной передачи без достоверного ответа = Unknown. Протокол чужой сессии/запуска не считается Accepted.
- [ ] Добавить маршрут в очередь перед визуальными gates. Повтор NotSent использует существующие лимиты; Accepted/Unknown не проходят через старое giveUpAccepted с переотправкой. Перед terminal failure сначала проверить историю, включая уже завершившийся/восстановленный получатель, чтобы не пропустить фактическую доставку.
- [ ] Добавить тесты разрыва до/после записи, oversized/malformed ответа, смены instance, перезапуска в delivering, отсутствующего transcript, позднего подтверждения, disabled QueueTimeout и terminal failure. Отдельно подтвердить две одинаковые строки в разных записях.
- [ ] Запустить `go test ./internal/nativeinput ./internal/store ./internal/queue ./internal/session ./internal/agent/... ./internal/daemon`, затем `go test ./...` и `go test -race ./internal/nativeinput ./internal/queue ./internal/session`.
- [ ] Провести ревью, commit/PR, добиться зелёных доступных CI. Если CI в репозитории отсутствует, показать это явно и приложить локальные проверки.

## Задача 2: Мост Pi и рабочее подключение

**Зависит от:** merged PR задачи 1; до этого допускается только чтение контракта/изолированный ресерч, без расходящихся копий API.

**Результат:** новые/restore Pi 1.4.0+ используют pi-uds-v1; старые сессии работают как раньше.

**Файлы:** создать internal/agent/pi/delivery.go, delivery-extension.ts, delivery_test.go; изменить internal/agent/pi/pi.go и целевые session/queue tests; добавить docs/features/tmux-delivary/pi-live-recon.md и документацию эксплуатации в docs/06-messaging.md, docs/10-agents.md.

**Потребляет:** NativeLaunchProvider и nativeinput контракт задачи 1, существующий Pi DeliveryConfirmer.

**Производит:** `(*Pi).PrepareNativeLaunch(spec agent.LaunchSpec) (agent.NativeLaunch, error)`, embedded расширение и явный launch flag; приватный UDS с правильным lifecycle.

- [ ] В изолированной Pi-сессии проверить поддерживаемый способ sendUserMessage при непустом редакторе и busy. Это исследовательская проверка, не рабочая интеграция. Сохранить версии, команды, доказательства и отдельно неизвестное. При нарушении гарантии остановиться и сообщить оркестратору.
- [ ] Написать падающие Go/bridge тесты `TestPiNativeLaunchVersionGate`, `TestPiExtensionPreservesDraftAndCursor`, `TestPiBridgeRejectsWrongInstance`, `TestPiBridgeDeduplicatesMessageID`, `TestPiBridgeKeepsDistinctEqualTexts`, `TestPiSwitchClosesOldEndpoint`. Использовать тестовый runtime/ExtensionAPI без платных модельных запросов там, где проверяется собственная логика.
- [ ] Запустить их и получить ожидаемое падение до реализации.
- [ ] Реализовать подготовку extension-файла, короткого endpoint и instance ID; явный --extension только для native запуска. Перехватить асинхронные ошибки настолько, насколько позволяет Pi; не считать вызов API подтверждением delivered.
- [ ] Реализовать session_start/session_shutdown и смену истории, ограничение кадра, проверку target, дедупликацию по ID живого запуска и followUp без template/command expansion. При неподтверждённо безопасном состоянии возвращать NotSent до API-вызова.
- [ ] Проверить crash/recover вместе с задачей 1: accepted/unknown не становятся повторной отправкой; отсутствующий сокет не возвращает Insert в строку. Не сохранять бесконечно память завершённых/несуществующих сессий.
- [ ] Выполнить live-матрицу: пустой ввод, русский одно-/многострочный черновик, курсор в середине, idle/busy, длинное сообщение, два одинаковых текста, потеря сокета, restart Rocket, restore/kill/switch Pi. Приложить редактированные свидетельства. Автоматизацию live-проверок согласовать с оркестратором через rocket send; не писать в рабочие сессии других участников.
- [ ] Запустить `go test ./internal/agent/pi ./internal/session ./internal/queue ./internal/nativeinput`, `go test ./...`, `go test -race ./internal/agent/pi ./internal/queue ./internal/nativeinput` и bridge-тесты выбранным штатным runtime. В отчёте указать реальную команду bridge-тестов.
- [ ] Ревью, commit/PR, зелёные доступные CI; документировать включение только для новых/restore сессий и ограничение unconfirmed.

## Задача 3: Изолированная проверка Codex и оставшегося пути Kimi

**Независима от:** задач 1–2. Может выполняться параллельно после Go отдельным Rocket-воркером. Одна исследовательская задача, один PR с доказательствами и выводами.

**Файлы:** docs/features/tmux-delivary/codex-live-recon.md, kimi-native-recon.md; при необходимости воспроизводимые probe-скрипты в scripts/probes/tmux-delivary/ без подключения к продукту.

**Потребляет:** research.md, текущие launch/env адаптеров, установленные CLI, первичную документацию.

**Производит:** однозначные выводы по каждой проверке: подтверждено, опровергнуто либо не проверено с причиной. Продуктового API для других задач не вводит.

- [ ] Проверить точные версии/--help, документацию и текущее происхождение --no-daemon. Не читать/копировать credentials и не менять глобальный конфиг.
- [ ] Запустить изолированный Codex app-server с окружением тестовой сессии и TUI --remote на приватном endpoint; не использовать общий daemon текущего человека. Подключить внешнего отправителя к тому же thread UUID.
- [ ] Проверить idle/busy, черновик/курсор, очередь, видимость сообщения в том же TUI/истории, правильные ROCKET_SESSION_ID/ROCKET_SOCKET из shell-tool; две параллельные тестовые сессии не видят окружение друг друга. Если для живого модельного шага нужна недоступная авторизация, зафиксировать конкретно непроверенный шаг и сообщить оркестратору, не придумывать успех.
- [ ] Проверить разрыв/завершение сервера/TUI и восстановление. Сформулировать минимальную будущую интеграцию либо конкретный блокер.
- [ ] Для Kimi найти поддерживаемый вход в живой TUI через текущие API/SDK/hooks. Проверить ownership; не отнимать lock у живой сессии и не создавать второго писателя. При отсутствии входа сохранить отрицательный результат с источниками и границами поиска.
- [ ] Оформить команды, ожидаемые/фактические результаты и редактированные свидетельства. Для throwaway probes достаточно минимальных проверок исполнения; production TDD здесь не имитировать тестами Markdown.
- [ ] Проверить `git diff --check`; если добавлены probe-скрипты, проверить их синтаксис и выполнить изолированный сценарий. Ревью, commit/PR, доступные CI.

## Порядок выполнения и завершение

1. После Go создать подзадачи, выбрать профили из актуального rocket models ls и записать обоснование в карточках. Для ограниченных кодовых задач подходит разрешённый kimi-k3-1-pro; исследовательский профиль выбрать по актуальному описанию и возможностям, не менять allowlist.
2. Запустить задачи 1 и 3 параллельно через rocket spawn; полные английские брифы через rocket send --file. Задачу 2 — после merge задачи 1.
3. Worker вопросы решает оркестратор; продуктовые изменения — новым thread и новой spec/gate. Выполнение не начинается по одному ответу в чате.
4. Для каждого merged PR выполнить rocket verify-merge, затем подзадачу done и kill --cleanup, без прощальных сообщений.
5. Финальная проверка: целевые тесты и live-доказательства Pi, регрессии legacy/Claude, проверенный результат Codex/Kimi. Не называть неиспытанный канал рабочим.
6. Сохранить report, проверить отсутствие воркеров, перевести #5902 в review. В отчёте явно обозначить, что Codex/Kimi пока используют прежнюю доставку, и предложить следующую спецификацию по фактам. Done ставит человек.

## Самопроверка плана

Проверено покрытие спецификации: неизменный TUI/legacy — задачи 1–2; lifecycle/неоднозначный приём/перезапуск — 1–2; реальные черновики — 2; окружение Codex и ограничение Kimi — 3. Имена контракта между 1 и 2 совпадают. Продуктовые неизвестные требуют нового решения; технические способы реализовать заданные гарантии оставлены исполнителям.
