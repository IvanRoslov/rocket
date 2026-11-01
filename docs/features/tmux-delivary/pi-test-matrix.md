# Матрица проверок Pi native bridge

Задача: #5902, подзадача 2 (#5906). Дата: 1 ноября 2026.

Документ подготовлен только для чтения и планирования. Продукционная реализация остановлена до явного разрешения оркестратора и получения merged task-1 contract. Промежуточный commit `d0dffa1` не использовался как контракт.

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
```

Реальная команда Node/Pi bridge-тестов будет зафиксирована после выбора штатного runtime. Отдельно будут приложены версии, RED/GREEN результаты, live-логи, результаты restart/switch/connection-loss и очистка изолированных ресурсов. Отсутствие GitHub Actions workflows не будет выдаваться за зелёный CI.

## Ограничение текущего результата

Preflight подтверждает только сохранение ввода и базовое поведение `sendUserMessage` в реальном Pi 1.4.0. Production integration, delivered-семантика моста, реальный DeepSeek, dialogs/compaction и lifecycle ещё не проверены и не запускаются без merged contract и явного release оркестратора.
