# Kimi Code: поиск поддерживаемого входа в живой stock TUI

Дата: 2026-11-01. Задача: #5907, задача 3 плана `tmux-delivary`. Исследовалась именно версия 2.4.0, без тестовых сообщений рабочим сессиям и без изменения Rocket.

## Короткий вывод

В проверенном объёме официальных API **не найден поддерживаемый способ для внешнего отправителя передать произвольное сообщение в тот же процесс уже работающего stock TUI Kimi Code 2.4.0**, минуя редактор и не создавая второго писателя истории.

Это отрицательный результат по поддерживаемому входу, а не утверждение, что в исходниках нет внутренних методов доставки. REST/WS и ACP существуют, но запускают/используют отдельный backend и не являются same-process stock-TUI каналом. Background completion действительно умеет запускать новый ход в 2.4.0, однако это agent-owned task notification, а не внешний API для произвольного сообщения.

Рабочая доставка Kimi должна остаться прежней до отдельного upstream/проектного решения.

## Точная версия и границы поиска

- Локальная команда `kimi --version` → `2.4.0`.
- Официальный тег: `@moonshot-ai/kimi-code@2.4.0`, commit `f26e4d91ef8d5d233f6c20b42a150f96924d4f5e`.
- Проверены локальный официальный source tag и те же файлы через официальные tag URLs: TUI reverse RPC, background task service, TUI event handler, REST prompts, hooks и Server API.
- `kimi session --help` даёт неинтерактивный `list`, а не вход в живой TUI. `kimi web --help` описывает отдельный foreground server/web UI.
- Не отбирались locks, не создавались вторые писатели, persistent schedules/watchers, тестовые сообщения участникам или изменения глобального config/auth.

Ссылки ниже ведут именно на tag 2.4.0, не на более новую документацию.

## Что проверено

### 1. REST/WebSocket: есть prompt API, но это другой процесс

[Server API 2.4.0](https://raw.githubusercontent.com/MoonshotAI/kimi-code/%40moonshot-ai/kimi-code%402.4.0/docs/en/reference/server-api.md) поддерживает:

- `POST /api/v1/sessions/{session_id}/prompts` с client-chosen `prompt_id` для идемпотентной подачи;
- очередь и `steer` в активный ход;
- список/чтение/cancel/detach background tasks;
- коды `40931` (session occupied) и `40932` (session updated).

Реализация [prompts.ts](https://raw.githubusercontent.com/MoonshotAI/kimi-code/%40moonshot-ai/kimi-code%402.4.0/packages/kap-server/src/routes/prompts.ts) подтверждает `resolved.prompt.submit` с origin `user` и ожидание accepted, не completion. Это полезный API для клиента `kimi web`, но он не превращается в вызов внутри существующего stock-TUI процесса.

Более того, [sessionLock.ts](https://raw.githubusercontent.com/MoonshotAI/kimi-code/%40moonshot-ai/kimi-code%402.4.0/packages/agent-core-v2/src/persistence/interface/sessionLock.ts) явно сообщает `This session is in use by another Kimi Code process`. В Server API `:takeover` разрешён только когда прежний процесс уже завершился; если он ещё работает, оба процесса пишут в сессию до её перехода в idle, и история может перемешаться. Поэтому REST/second writer и lock takeover исключены из допустимого решения, а не предложены как workaround.

### 2. ACP stdio: отдельный agent backend

[acp.ts](https://raw.githubusercontent.com/MoonshotAI/kimi-code/%40moonshot-ai/kimi-code%402.4.0/apps/kimi-code/src/cli/sub/acp.ts) запускает Agent Client Protocol server поверх `agent-core-v2` через stdio. Это самостоятельный backend для ACP-клиентов, не supported input в уже открытый stock TUI. Headless/ACP замена интерфейса выходит за рамки задачи сохранения штатного терминала.

### 3. Hooks: нет нужного внешнего trigger

[Hooks 2.4.0](https://raw.githubusercontent.com/MoonshotAI/kimi-code/%40moonshot-ai/kimi-code%402.4.0/docs/en/customization/hooks.md) показывает:

- `UserPromptSubmit`, `PreToolUse`, `Stop` могут влиять на основной поток;
- `SessionHeartbeat`, `Notification`, `TurnStarted` и другие observation-only события — нет;
- `Stop` может продолжить уже завершающийся ход, но не является внешним wake-up API для idle-сессии;
- `UserPromptSubmit` срабатывает на уже отправленное пользователем сообщение и не создаёт новое.

Следовательно, hook можно использовать для наблюдения/уведомлений, но не для гарантированной доставки нового Rocket-сообщения по внешнему событию.

### 4. TUI reverse RPC: только approval/question UI

[reverse-rpc/index.ts](https://raw.githubusercontent.com/MoonshotAI/kimi-code/%40moonshot-ai/kimi-code%402.4.0/apps/kimi-code/src/tui/reverse-rpc/index.ts) регистрирует только show/hide/dismiss для approval panel и question dialog. Общего `sendUserMessage`, prompt injection или editor-preserving followUp там нет. Внутренние методы вроде `sendNormalUserInput` относятся к собственному input flow TUI и не объявлены внешним стабильным API.

### 5. Background completion в 2.4.0: существует, но не решает внешнюю доставку

Узкая гипотеза проверена отдельно: механизм присутствует именно в 2.4.0, не только в более новых docs.

[taskService.ts](https://raw.githubusercontent.com/MoonshotAI/kimi-code/%40moonshot-ai/kimi-code%402.4.0/packages/agent-core-v2/src/agent/task/taskService.ts) при terminal effects вызывает `notifyAgentTask`, а затем `loop.notify` с:

- `role: 'user'`;
- `origin.kind: 'task'`;
- `turnScoped: false`;
- XML, построенным из notification/output конкретной задачи.

[TUI session-event-handler.ts](https://raw.githubusercontent.com/MoonshotAI/kimi-code/%40moonshot-ai/kimi-code%402.4.0/apps/kimi-code/src/tui/controllers/session-event-handler.ts) распознаёт `turn.started` с `origin.kind === 'task'` и обновляет background cards. Server API также умеет `detach` существующего foreground task, после чего completion приходит как task notification.

Почему это не подходит как Rocket-канал:

1. Уведомление принадлежит зарегистрированной agent-задаче и содержит её результат; произвольный внешний текст не является самостоятельным prompt API.
2. REST предоставляет list/read/cancel/detach, а не создание произвольной задачи с текстом Rocket.
3. Новый ход model-mediated: текст notification и дальнейшее поведение агента не равны точной вставке заданного сообщения в контекст один раз.
4. Использование служебной задачи как транспорта потребовало бы менять ownership/жизненный цикл агента и следить за task IDs, а не просто подключить внешний sender к живому TUI.

Итог по гипотезе: **механизм подтверждён, пригодность как поддерживаемый внешний канал — нет.** В исследовании не создавались фоновые задачи для имитации доставки.

## Матрица вывода

| Вопрос | Результат |
| --- | --- |
| Есть ли REST prompt API в 2.4.0? | PASS, но separate server/writer |
| Есть ли ACP? | PASS, но separate backend, не stock TUI |
| Можно ли безопасно отобрать lock у живой сессии? | NO: `:takeover` официально разрешён только для уже завершившегося владельца; у живого lock не отбирается, иначе возможна перемешанная история. Не использовалось |
| Могут ли hooks внешне разбудить idle TUI с произвольным текстом? | NOT FOUND в поддерживаемых событиях |
| Есть ли generic prompt input в TUI reverse RPC? | NO: только approval/question |
| Запускает ли background completion новый ход в 2.4.0? | YES, agent-owned `origin=task` |
| Является ли background completion поддерживаемым произвольным external input? | NOT FOUND / не подходит по ownership и семантике |
| Доказан ли same-process stock-TUI канал? | **NOT FOUND в проверенном официальном объёме** |

## Что предложить дальше

Предпочтительный upstream-запрос: явный supported input в stock TUI (например, extension/UDS) с `followUp`, message ID и подтверждением по истории, который не трогает editor/draft/cursor, не создаёт второго владельца и различает not_sent/accepted/unknown.

Альтернатива — использовать `kimi web` и собственный клиент Rocket поверх REST/WS. Это уже замена привычного интерфейса и отдельный проект, а не небольшой native-мост; в текущий этап не входит.

Пока отсутствует такой supported input, Kimi остаётся на существующей tmux-доставке. Документ не рекомендует lock takeover, ACP/headless или фоновые задачи как скрытый обход.
