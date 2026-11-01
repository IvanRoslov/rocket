# Штатные модальные селекторы Pi 1.4.0 и внешняя доставка

Задача: #5902, подзадача 2 (#5906). Дата: 1 ноября 2026.
Статус: **только preflight-исследование**. Продуктовая интеграция, изменение запуска Rocket и production-хуки не выполнялись. Production остаётся на паузе до явного release финального merged контракта task 1.

Документ обновлён при восстановлении подзадачи после прерванного прогона: добавлен сохранённый результат timing-репродьюсера, уточнены ссылки на исходники Pi 1.4.0 и границы доказанного.

## Вопрос

Достаточно ли поддерживаемых событий Pi 1.4.0 и доказанно безопасных штатных модальных состояний, чтобы выполнить требование спецификации: «диалоги/compaction, где подача не подтверждена как безопасная, дают `not_sent` до вызова API», а черновик и курсор не повреждаются?

Отдельно проверялось утверждение из предыдущего отчёта, что `queueMicrotask` в `ui_prompt_start` сам по себе создаёт гонку с callback сокета.

## Среда

- Pi: `1.4.0` (`pi --version`); пакет `@earendil-works/pi-coding-agent` установлен через managed install Pi.
- Node.js: `v26.5.1`.
- Python: `3.14.4`.
- ОС: `Darwin 26.3.1 arm64` (macOS 26.3.1, kernel `Darwin 25.3.0`).
- Каждый прогон — отдельный PTY, отдельные `PI_CODING_AGENT_DIR` и `--session-dir`.
- Провайдер — только локальный детерминированный `probe`/`timing` внутри исследовательского расширения. Из окружения дочернего процесса удалены переменные вида `*_API_KEY`, `*_AUTH_TOKEN`, `*_ACCESS_TOKEN` и известные префиксы провайдеров, поэтому наследованные ключи (включая `DEEPSEEK_API_KEY`) в тест не попадают. `PI_OFFLINE=1`. Платных или внешних модельных вызовов нет. Это не проверка реального DeepSeek.
- Сокет исследования создаётся в `/tmp` с коротким именем: путь вида `.pi-probe-ui-prompt-timing.sock` в текущем worktree превышает лимит macOS на длину UDS (подтверждён `OSError: AF_UNIX path too long`). Это же ограничение важно для production-контракта «private short-path UDS».

## Fixtures и доказательства

| Файл | SHA-256 |
| --- | --- |
| `scripts/probes/tmux-delivary/selector_probe.py` | `69ae44b7b16b391986749e3914d9632f7ca51067b903d965fda3399737e899e3` |
| `scripts/probes/tmux-delivary/selector-probe-extension.ts` | `341c1d1011fd3b4583d9cb75e51d86757c877f685773382ae85812a21719fbc9` |
| `scripts/probes/tmux-delivary/ui_prompt_timing_probe.py` | `3b20e98cf343e581ddfcf614c0d0c19a13f2e7a909d9b003baafaa6c6c01f787` |
| `scripts/probes/tmux-delivary/slow-ui-listener-extension.ts` | `312a064888cbe98177e64fca903174b4080f8375eb509f4c781ff43955a966bb` |
| `scripts/probes/tmux-delivary/ui-timing-probe-extension.ts` | `a91ccde6995aefb72d5815f41d85492c840958b2dba7f07e7f70ae28ed5ebe06` |

Сохранённые sanitized-результаты:

- `scripts/probes/tmux-delivary/results/selector-model/result.json`
- `scripts/probes/tmux-delivary/results/ui-prompt-timing/result.json`
- `scripts/probes/tmux-delivary/results/ui-prompt-timing-control/result.json`

Полные `pty.raw`, `events.jsonl`, транскрипты, auth/config-файлы и временные каталоги остаются git-ignored. В `result.json` попадает только очищенное от ANSI и служебных путей доказательство: маркерные фрагменты экрана (ограничение 120–760 символов) плюс структурные поля состояния редактора, событий и транскрипта. Полный экран PTY и построчный дамп не публикуются.

Инструментация `CustomEditor.prototype.handleInput` — read-only обёртка: она только записывает вызов и делегирует оригиналу. Никаких `setText`, `pasteToEditor`, submit или изменения поведения. Расширение используется только исследованием и не является production-хуком.

Ограничение сохранённых доказательств: selector-прогон ранее выполнялся 5 раз (5/5 PASS), сохранён один sanitized `result.json`. Timing-репродьюсер ранее проходил 3 раза, но перед остановкой воркера каталог сценария был пересоздан новым прогоном; тот прогон был прерван до записи `result.json`, поэтому в snapshot остались только `events.jsonl`/`slow-events.jsonl` без результата. При восстановлении выполнен **один** ограниченный прогон, его `result.json` сохранён ниже. Control-прогон сохранён (один `result.json`, ранее 2/2 PASS).

## 1. Штатный встроенный селектор под внешним followUp

Команда:

```bash
python3 scripts/probes/tmux-delivary/selector_probe.py
```

Сценарий: многострочный русский черновик (две строки: `Первая строка черновика`, `Вторая строка с курсором`), курсор внутри второй строки (`line=1, col=21`), затем `Ctrl+L` открывает штатный **model selector** (`app.model.select` → `showModelSelector` → общий `showSelector`). Стрелка вниз переводит подсветку на вторую модель (`secondary`), чтобы синтетический Enter был заметен. Затем через probe-сокет отправляется один `sendUserMessage(..., { deliverAs: "followUp", expandPromptTemplates: false })`.

Наблюдения сохранённого result.json (один PASS; ранее 5/5 PASS):

| Проверка | Результат |
| --- | --- |
| Черновик до/после открытия селектора | идентичен |
| Курсор до/после | `(1, 21)` без изменений |
| Селектор после доставки | остаётся открытым, `→ secondary [probe]` на месте |
| Ввод `zz` после доставки | ушёл в поиск селектора (`No matching models`), `editorInputCount` не изменился |
| `Escape` | закрыл селектор, черновик и курсор не изменились |
| Ввод `!` после `Escape` | попал в редактор в позицию курсора: `Вторая строка с курсо!ром` |
| Смена модели | нет: `model_select` events = 0, модель осталась `probe/deterministic` |
| `ui_prompt_start/end` | 0 событий: встроенные селекторы их не эмитят |
| Транскрипт | ровно 1 injected user + 1 assistant, порядок `user → assistant` |
| Вызовы provider | 1 |

Сохранённые маркерные фрагменты экрана подтверждают одновременно открытый селектор (`Only showing models from configured providers ... → secondary [probe]`), фильтрацию поиска (`No matching models`) и возврат к редактору (`Вторая строка с курсо!ром`). То есть доставка не закрывает селектор, не синтезирует Enter и не трогает редактор.

### Почему это работает (source audit)

Проверено по установленному пакету `@earendil-works/pi-coding-agent/dist` версии 1.4.0.

- Все встроенные селекторы, проходящие через общий `showSelector` (`dist/modes/interactive/interactive-mode.js:3979-4000`), заменяют редактор в `editorContainer`: `editorContainer.clear()`, затем в контейнер кладётся компонент селектора, фокус переводится на него. Объект редактора при этом сохраняется в `this.editor`.
- Закрытие (`done()`) возвращает прежний экземпляр редактора в контейнер и фокус на него (`:3982-3991`). В этом пути нет `setText`, поэтому текст и позиция курсора сохраняются по построению.
- Через `showSelector` идут как минимум: settings (`:4003`), thinking (`:4267`), trust (`:4395`), model (`:4414`), scoped-models (`:4476`), user-message/fork (`:4559`), tree (`:4608`), session (`:4725`), login auth-type/provider (`:4908`, `:4946`, `:4983`), MCP-offer (`:5304`).
- Приватное состояние селектора — `activeSelectorToken` / `activeSelectorDispose` (`:254-255`) — не имеет публичного события или accessor'а. `disposeActiveSelector` (`:3969-3974`) используется при reload/switch редактора.
- Отрисовка хода и очереди не трогает редактор: `turn_start` (`:2719-2732`) обновляет только progress/working indicator; `queue_update` (`:2733-2736`) обновляет только `pendingMessagesContainer` (`updatePendingMessagesDisplay`, `:3834-3851`); `message_start` (`:2786-2803`) добавляет сообщение в `chatContainer`. Поэтому followUp во время открытого селектора не меняет фокус и не вызывает `done()`.
- Пользовательские callback'и выбора могут менять редактор уже после подтверждения (например, fork `:4568`, clone `:4593`, tree navigate `:4687`), но это действие пользователя, а не внешняя доставка. `/settings` очищает редактор после открытия селектора (`:2507-2510`), `/scoped-models` — до (`:2512-2514`); это собственная обработка команды, а не следствие внешней доставки.
- Extension-диалоги (`ctx.ui.select/confirm/input/editor`) используют стек `pushExtensionDialog`/`removeExtensionDialog`/`showTopExtensionDialog` (`:2110-2132`), который также возвращает прежний экземпляр редактора, без `setText`. А `ctx.ui.custom` в non-overlay режиме при закрытии делает `this.editor.setText(savedText)` (`:2295-2300`, строка `:2298`), то есть схлопывает курсор в конец последней строки **независимо от моста**. Значит, обещание «курсор сохраняется» нельзя распространять на сценарий, где пользователь закрывает custom-диалог расширения: это поведение самого Pi. `Editor.setText()` в `@earendil-works/pi-tui` ставит курсор в `end` (`dist/components/editor.js:921-932`, `setTextInternal` `:371-383`).

### Чего этот аудит не покрывает

Утверждение «все встроенные селекторы безопасны» было бы сильнее доказательств. Ниже — состояния, которые **не** проходят через `showSelector` и не имеют публичного события:

- OAuth auth-select внутри LoginDialog: `showAuthSelect` (`:5164-5188`) кладёт `ExtensionSelectorComponent` прямо в `editorContainer`.
- Login-диалоги: `showAmbientAuthDialog` (`:5106-5122`), `showApiKeyLoginDialog` (`:5123-5136`), provider login (`:5250-5256`).
- Reload-бокс `/reload` (`:5338-5356`) и подмена редактора `setCustomEditorComponent` (`:2206-2270`).

Статически путь доставки (`AgentSession.prompt` → события TUI) не обращается к `editorContainer` и не вызывает `done()`/`setText`; поэтому доставка во время этих состояний, вероятно, не повреждает ввод. Но это source-level вывод, а не живая проверка, и UX-вопрос (сообщение и ход агента во время login/reload) не исследован. Reload при этом штатно присылает `session_shutdown` с `reason: "reload"` (`types.d.ts:636-640`), то есть production-мост должен закрыть endpoint по этому событию, а не принимать сообщения в перезагруженный контекст.

## 2. Тайминг `ui_prompt_start`: что именно является гонкой

Source audit:

- `withUIPrompt` оборачивает extension UI-вызовы и эмитит `ui_prompt_start`/`ui_prompt_end` (`dist/core/extensions/runner.js:369-395`), причём только для внешнего prompt (`uiPromptDepth === 0`, `:370-373`).
- `emitUIPromptEvent` действительно использует `queueMicrotask` (`:396-400`).
- Но `Runner.emit` — `async` и ждёт обработчики **последовательно в порядке загрузки расширений** (`:813-840`, `await handler(event, ctx)` в `:816-819`); снимок обработчиков берётся в `snapshotEventHandlers` (`:87-89`).
- Порядок CLI-расширений сохраняется: `--extension`/`-e` пушится в `result.extensions` в порядке аргументов (`dist/cli/args.js:156-158`), затем `resolveCliPaths` (`dist/main.js:586`) передаётся в `additionalExtensionPaths` (`:632`), а загрузчик сохраняет порядок путей в `orderedExtensions` (`dist/core/resource-loader.js:568-571`).
- Callback сокета — это macrotask. Между `queueMicrotask` и опустошением microtask-очереди он выполниться не может. Поэтому `queueMicrotask` сам по себе **не** создаёт гонку. Контрольный прогон это подтверждает.
- Встроенные расширения Pi не регистрируют `ui_prompt_start`/`ui_prompt_end`: единственный эмиттер в `dist` — `runner.js` (та же логика продублирована в bundled chunk); в `dist/extensions/*` обработчиков нет. В чистом Pi окна, создаваемого async-обработчиком, нет.

Контроль (`--control`, только расширение с синхронным обработчиком): сохранённый `result.json` показывает `flag=true`, `promptOpen=true`, `idle=true` в окне; `prompt_requested` и `flag_set` в одну миллисекунду, задержка `0 ms` до любого socket callback.

Репродьюсер (`slow-ui-listener-extension.ts` загружен первым, его `ui_prompt_start` handler ждёт 1200 мс; `ui-timing-probe-extension.ts` вторым выставляет флаг). Восстановительный прогон 1 ноября 2026:

```bash
python3 scripts/probes/tmux-delivary/ui_prompt_timing_probe.py
```

Результат: **PASS**, сохранён в `scripts/probes/tmux-delivary/results/ui-prompt-timing/result.json`.

| Момент | Значение |
| --- | --- |
| `prompt_requested` | t+0 мс, модальный диалог уже показан |
| socket `state` | `flag=false`, `promptOpen=true`, `idle=true` — guard пропустил бы отправку |
| socket `send` | принят при `flag=false`, `promptOpen=true` (t+57 мс) |
| `provider_start` | t+61 мс, то есть **внутри** окна с открытым диалогом |
| `slow_start_end` / `flag_set` | t+1202 мс (после возврата первого async handler'а) |
| закрытие диалога | флаг снимается ещё через ~1200 мс (безопасная сторона: лишние отказы) |
| Транскрипт | ровно 1 injected user, порядок `user → assistant` |

Вывод: точная формулировка гонки — не «queueMicrotask», а **последовательный `await` обработчиков в порядке загрузки**: если другое, ранее загруженное расширение имеет асинхронный `ui_prompt_start`, флаг моста выставляется с произвольной задержкой, пока диалог уже открыт и `isIdle()` возвращает `true`. Это воспроизведено принудительно; в stock Pi таких обработчиков нет, но в запуске с пользовательскими расширениями окно реально.

## 3. Очередь followUp и пользовательский abort

Отдельный аудит показал ещё одно ограничение, не связанное с модальными селекторами. Внешний followUp во время streaming попадает в очередь Pi; если пользователь затем нажимает `Escape`:

- `defaultEditor.onEscape` при `session.isStreaming` вызывает `restoreQueuedMessagesToEditor({ abort: true })` (`interactive-mode.js:2377-2380`); тот же путь доступен через `ctx.abort()` (`:1732-1734`) и `abortHandler` (`:1479-1481`), а также через dequeue (`handleDequeue`, `:3645-3653`).
- `restoreQueuedMessagesToEditor` (`:3852-3871`) склеивает текст очереди с текущим текстом редактора и вызывает `this.editor.setText(combinedText)` (`:3865`).
- `setText` ставит курсор в конец последней строки (`@earendil-works/pi-tui/dist/components/editor.js:921-932`, `371-383`).

Следствие: сама доставка редактор не меняет, но если внешнее сообщение уже стоит в очереди и пользователь прерывает ход, Pi по своей штатной логике переносит это сообщение в редактор и схлопывает курсор. Текст черновика при этом не теряется, но обещание «курсор сохраняется» на это пользовательское действие не распространяется. Это поведение Pi, а не моста; для production нужен явный продуктовый ответ, если такой сценарий считается частью гарантии.

## 4. Compaction

Поддерживаемые сигналы остаются прежними:

- `session_before_compact` — до compaction; поля `reason: "manual" | "threshold" | "overflow"`, `willRetry`, `signal` (`types.d.ts:600-611`; регистрация `:1193`).
- `session_compact` — после успеха (`types.d.ts:612-621`; регистрация `:1194`).
- `session_compact_failed` — после ошибки/отмены, включая отмену расширением (`types.d.ts:622-635`; регистрация `:1195`).
- `ctx.isIdle()` ложен для agent run и compaction/branch summary (`agent-session.js:1067-1074`, `1223-1228`); `prompt()` независимо отказывает при активном `_compactionAbortController` (`agent-session.js:1648-1650`).

Это позволяет вернуть `not_sent` до вызова API, если признак активной compaction проверен синхронно перед вызовом. **Гонки guard→call здесь нет**: синхронная проверка в расширении и непосредственный вызов `pi.sendUserMessage(...)` выполняются в одной задаче JavaScript; без `await` между ними compaction не может вклиниться. Прежняя формулировка «compaction между проверкой и вызовом» была бы неверной.

Остающаяся граница — отложенные и асинхронные шаги внутри `prompt()`, а не интервал перед вызовом API:

- при `_isEmittingAgentSettled` вызов не выполняется сразу, а кладётся в `_deferredSettledActions` (`agent-session.js:1632-1635`) и выполнится позже, на settled-границе; compaction может стать активной к этому моменту;
- после синхронной проверки `_compactionAbortController` (`agent-session.js:1648-1650`) идут `await this._runInputHandlers(...)` (`:1652`), затем `await this._queueFollowUp(...)`/`_queueSteer` (`:1670`, `:1673`) для streaming или `await this._startRun(...)` (`:1693`) для idle. На этих await-границах compaction может начаться уже после того, как мост вызвал API;
- при `expandPromptTemplates: false` (контракт моста) ветка `await this._tryExecuteExtensionCommand(...)` (`:1640-1647`) не выполняется, поэтому при обычном вызове синхронная проверка действительно достигается в той же задаче, что и вызов.

Отдельная source-level граница — лаг собственного флага моста: `session_before_compact` эмитится через тот же последовательный `Runner.emit`, поэтому при чужом async-обработчике обновление флага моста может произойти позже. Core выставляет `_compactionAbortController` **до** эмита события (manual `:2231-2232`, auto `:2537-2538`; эмит `:2253-2262`, `:2543-2552`), поэтому `prompt()` увидит активную compaction на синхронной проверке `:1648-1650` и отклонит вызов. Итог: запуск хода во время compaction предотвращается core-проверкой, но мост может вернуть accepted без per-message ошибки (fire-and-forget). Этот лаг не воспроизводился живым сценарием; это source-level вывод, а не доказанная гонка.

Если ошибка возникает на await-границе после проверки (`_runInputHandlers`, `_queueFollowUp`/`_queueSteer`, `_startRun`), промис `sendUserMessage` отклоняется, и его `.catch` в `bindCore` (`agent-session.js:2774-2782`) отправляет ошибку в error-listener раннера (`runner.js:493-501`) без идентификатора сообщения — мост не получает delivered-подтверждения. Если же вызов попал в `_deferredSettledActions` (`_isEmittingAgentSettled`), исходный `prompt()` уже вернулся успешно; отложенное действие выполняется позже в `_emitAgentSettled` (`:695-704`), и его ошибка не проходит через catch исходного `sendUserMessage`, то есть мост тем более не получает per-message ошибки. Конкретный исход и частота этих границ живым сценарием **не продемонстрированы**; это неизвестная граница, требующая отдельного воспроизводимого теста, а не доказанная гонка.

Прямого доступа к редактору в пути `prompt()` нет. В ветке синхронной проверки `:1648-1650` отказ происходит до `_runInputHandlers` и запуска хода, поэтому черновик не затрагивается; для отложенных границ повреждение ввода не проверялось отдельно.

## 5. Ответ по спецификации

Что можно утверждать на доказательствах:

1. **Встроенный model selector (общий путь `showSelector`)** — безопасное состояние для внешнего followUp: селектор остаётся открытым и управляемым, выбор не синтезируется, черновик и mid-line курсор сохраняются, сообщение появляется один раз в правильной истории. Это проверено на реальном Pi 1.4.0 и сохранено в `result.json`.
2. **Остальные встроенные селекторы через `showSelector`** — source-level вывод из общей ветки: доставка не вызывает `done()` и не трогает `editorContainer`; живых прогонов для settings/trust/session/tree/login/thinking/scoped-models/MCP нет. Это сильнее, чем «неизвестно», но слабее живого доказательства.
3. **Compaction** — отказ реализуем на поддерживаемых событиях, если признак активной compaction проверяется синхронно перед вызовом API; между такой проверкой и вызовом без `await` compaction вклиниться не может. Остаётся недемонстрированная отложенная граница внутри `prompt()` (`_deferredSettledActions`, `await _runInputHandlers`/`_queueFollowUp`/`_startRun`); её исход нужно проверять отдельным сценарием, а не называть доказанной гонкой. Редактор Pi в этом пути не затрагивается.
4. **Extension UI prompts** — `ui_prompt_start/end` покрывают их, но только с точностью до описанной ordering-гонки; при отсутствии чужих async-обработчиков флаг выставляется до socket callback (control).

Что **невозможно** гарантировать только поддерживаемым API:

- Атомарное «любое небезопасное встроенное модальное состояние ⇒ `not_sent`»: публичного признака открытого селектора/диалога нет (`activeSelectorToken`/`extensionDialogs` — приватные; у `ExtensionContext` есть `hasUI` и `isIdle(): boolean`, у `ExtensionUIContext` — `getEditorText`/`setEditorText`, но нет modal-depth/признака открытого селектора). Встроенные login-диалоги и reload-бокс также не эмитят `ui_prompt_start`.
- Атомарное «extension-диалог открыт ⇒ `not_sent`» при наличии другого расширения с асинхронным `ui_prompt_start`: событие приходит с задержкой, синхронного `ctx.isModal()` нет. Это остаточный риск, а не доказанная безопасность; в stock Pi окна нет.
- Сохранение позиции курсора после закрытия `ctx.ui.custom` в non-overlay режиме: Pi сам делает `setText(savedText)`.
- Сохранение курсора при пользовательском `Escape`/dequeue, если внешнее сообщение уже в очереди: Pi сам склеивает очередь с редактором.

Практический ответ: **строгое требование «отказывать во всех небезопасных встроенных состояниях» текущим публичным API не выполнимо**. Выполнимое приближение — объявить проверенные встроенные состояния safe-to-accept (model selector — живое доказательство; общий `showSelector` — source-level), отказывать по compaction, а для extension-диалогов использовать best-effort флаг `ui_prompt_start` с документированным остаточным окном. Это продуктовое решение, а не молчаливое ослабление гарантии.

## Варианты для решения (нужен orchestrator/root, не код)

1. **Принять текущее поведение**: встроенные селекторы через `showSelector` — safe-to-accept (живое доказательство для model selector, source-level для общей ветки); extension-диалоги — best-effort флаг `ui_prompt_start`; compaction — поддерживаемый отказ. Остаточную гонку с чужими async-расширениями и непроверенные login/reload-состояния задокументировать как ограничение.
2. **Ужесточить**: запросить upstream поддерживаемый синхронный modal-depth/accessor (покрывающий `showSelector`, overlays, login-диалоги и extension-диалоги), затем сделать `not_sent` атомарным.
3. **Ограничить запуск**: production Pi запускать с изолированным набором расширений (только Rocket), чтобы исключить чужие async `ui_prompt_start`; это сужает гонку, но не даёт публичного признака встроенных селекторов и login/reload-состояний.

Production не начинается до явного release финального merged контракта task 1. Следующие шаги после release: embedded extension, explicit launch opt-in, private short-path UDS, session/launch/message binding, dedupe, honest ack, lifecycle/switch protection, UTF-8 split across chunks, byte frame limits и реальный extension runtime. Этот документ не является разрешением на production и не подтверждает delivered-семантику моста.

## Что осталось непроверенным

- Живые прогоны остальных встроенных селекторов (`settings`, `trust`, `session`, `tree`, `login`, `thinking`, `scoped-models`, MCP) — только source-level общая ветка.
- Extension-диалоги под внешней доставкой (select/confirm/input/editor/custom) — не прогонялись; проверен только тайминг их события.
- Встроенные login-диалоги, OAuth auth-select и reload-бокс — source-level; живых прогонов нет.
- Поведение при пользовательском `Escape`/dequeue с уже стоящим в очереди внешним сообщением (см. раздел 3) — source-level.
- Отложенная граница compaction внутри `prompt()` (`_deferredSettledActions`, `await _runInputHandlers`/`_queueFollowUp`/`_startRun`) и лаг флага `session_before_compact` при чужом async-обработчике — source-level; конкретный исход не воспроизведён отдельным сценарием.
- Реальный DeepSeek, платные/внешние модельные ответы, production UDS lifecycle, restore/kill/switch, дедупликация ID, oversized frames, разрыв соединения — после merged task-1 contract и явного release.

## Команды проверки

```bash
python3 scripts/probes/tmux-delivary/selector_probe.py
python3 scripts/probes/tmux-delivary/ui_prompt_timing_probe.py
python3 scripts/probes/tmux-delivary/ui_prompt_timing_probe.py --control
```

Повторяемость на момент обновления: selector — сохранён один PASS `result.json` (ранее 5/5 PASS); timing reproducer — сохранён один PASS `result.json` (восстановительный прогон, задержка 1202 мс; ранее сообщалось 3/3 PASS без сохранённого результата); timing control — сохранён один PASS `result.json` (ранее 2/2 PASS). Четыре ранее доказанных сценария сохранения черновика (`pty_preservation_probe.py`) прошли 4/4 в исходном worktree до остановки воркера; при восстановлении они не перезапускались (no broad repetition), их committed `result.json` оставлены без изменений, чтобы совпадать с `pi-manifest.json`.
