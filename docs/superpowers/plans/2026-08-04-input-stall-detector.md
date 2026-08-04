# Input-stall detector (feature tui-2-3-1, subtask #917)

## Задача

Демон должен замечать оркестратора, который завис на интерактивном вводе, и
эскалировать это постоянному агенту `cto`, потому что сам оркестратор в этом
состоянии ничего сделать не может (он ждёт нажатия клавиши), а heartbeat-сводка
адресована ему же и потому бесполезна.

## Определение стойла

Сессия застряла на интерактивном вводе, если

- `PendingQuiz != ""` (открыт виджет AskUserQuestion), либо
- `activity == waiting_input`,

и это длится дольше нового порога `input_stall_threshold` (по умолчанию 10m).

Отсчёт: для квиза — `asked_at` из JSON `pending_quiz` (как в
`internal/monitor.pollQuiz`), с откатом на `ActivityTS`, если `asked_at`
отсутствует; для `waiting_input` — `ActivityTS`. Если ни одной метки времени
нет, стойло не объявляется (нельзя измерить длительность).

## Эскалация

В `heartbeat.tickOne`, отдельной веткой от сводки воркеров/вопросов:

1. Строка в inbox агента `cto` (`store.AddInboxMessage`, from = id оркестратора)
   — inbox, а не очередь сообщений, потому что `cto` может быть не запущен, а
   эскалация не должна теряться.
2. Событие шины `orchestrator.input_stalled` с `{task_id, reason, minutes}`,
   где `reason` = `pending_quiz` | `waiting_input`.
3. Анти-спам: не чаще одной эскалации на оркестратора за `HeartbeatInterval`
   (отдельная карта `lastEscalated`, чтобы обычная heartbeat-сводка не съедала
   окно эскалации и наоборот).

Проверка «оркестратор активен» тут не нужна: сессия, ждущая ввода, по
определению не active. Ограничение «корневая задача in_progress» сохраняется —
оно уже стоит в начале `tickOne`.

## Шаги (TDD)

1. `config`: поле `InputStallThreshold` + дефолт 10m + разбор yaml. Тесты в
   `internal/config/config_test.go`.
2. `heartbeat`: `inputStallReason(sess, now, threshold)` — чистая функция,
   таблица тестов (квиз/waiting_input/свежий/без меток/другая активность).
3. `heartbeat.tickOne`: эскалация в inbox + событие + анти-спам. Тесты через
   `st.ListInboxMessages("cto")`.
4. Документация: `docs/05-state.md` (конфиг), `docs/08-orchestrators.md`
   (поведение), `docs/03-daemon-api.md` (тип события).

## Вне объёма

Воркеры (их простой уже покрыт `worker_stall_threshold`), UI, доставка
эскалации в живую tmux-сессию `cto`.
