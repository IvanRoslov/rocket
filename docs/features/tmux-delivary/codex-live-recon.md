# Codex: изолированная проверка отдельного канала доставки

Дата: 2026-11-01. Задача: #5907, задача 3 плана `tmux-delivary`. Проверялась только изолированная конфигурация Codex 0.160.0; рабочий запуск Rocket не менялся.

## Короткий вывод

Отдельный `codex app-server` на тестовую сессию, штатный TUI с `--remote` и внешний `codex queue` на том же приватном endpoint работают с одним thread UUID. В сохранённом ручном эксперименте подтверждены доставка в историю/TUI при idle и busy, follow-up после активного хода, различные `ROCKET_*` в shell-tools двух серверов, отказ offline queue и восстановление после restart.

Черновик на экране сохранился, включая reconnect. **Сохранение курсора в середине строки не доказано**: BEFORE-кадра в исходном эксперименте нет, а финальный курсор находится в конце ввода. Повторная ограниченная автоматическая проверка остановилась до cursor baseline из-за пропавшего observer-уведомления. Поэтому production-интеграцию нельзя считать полностью проверенной.

## Границы и версии

- `codex --version` → `codex-cli 0.160.0`.
- `codex queue --help` подтверждает `--thread <THREAD>` и `--remote <ADDR>`; поддерживаются `unix://PATH`, `ws://` и `wss://`.
- `codex app-server --help` подтверждает `--listen unix://PATH` и другие транспорты.
- Текущий адаптер намеренно добавляет `--no-daemon`: `internal/agent/codex/codex.go:230-238` ссылается на потерю `ROCKET_*` у общего managed app-server. Этот флаг не снимался.
- Глобальные config/auth/model/daemon не менялись, чужие сессии не получали сообщений. Использовались только синтетические `rocket-5907-a`, `rocket-5907-b` и `rocket-5907-cursor` с их приватными socket/work-каталогами.

Документация, ранее проверенная в `research.md`: [developer commands](https://learn.chatgpt.com/docs/developer-commands), [app-server](https://learn.chatgpt.com/docs/app-server).

## Воспроизводимый рецепт

Все процессы запускаются с environment конкретной тестовой сессии и отдельным UDS. Пример показывает форму команд; выполнять его нужно в новом пустом каталоге, а не поверх сохранённых доказательств.

```bash
ROOT=/tmp/codex-native-recon
mkdir -p "$ROOT/work"
cd "$ROOT/work"

ROCKET_SESSION_ID=rocket-recon-a \
ROCKET_SOCKET=/tmp/rocket-recon-a.sock \
codex app-server --listen "unix://$ROOT/app.sock"
```

Во втором терминале `ROOT` нужно задать заново: shell-переменная не переносится между процессами. `expect` запускает TUI на PTY и записывает поток ввода-вывода в `$ROOT/tui.log`; размер терминала — 120×40.

```bash
ROOT=/tmp/codex-native-recon
cd "$ROOT/work" && ROOT="$ROOT" expect -c '
set timeout -1
log_file -noappend $env(ROOT)/tui.log
set stty_init "rows 40 cols 120"
spawn env TERM=xterm-256color COLUMNS=120 LINES=40 \
  ROCKET_SESSION_ID=rocket-recon-a \
  ROCKET_SOCKET=/tmp/rocket-recon-a.sock \
  codex --remote "unix://$env(ROOT)/app.sock" --no-alt-screen \
  -c check_for_update_on_startup=false
expect eof
'
```

Найти thread, поставить сообщение и прочитать историю можно сохранёнными probe-скриптами:

```bash
# из корня репозитория
ROOT=/tmp/codex-native-recon
PROBE_PYTHON=/path/to/isolated-python-with-pyte-and-websockets

"$PROBE_PYTHON" scripts/probes/tmux-delivary/ws_request.py \
  "$ROOT/app.sock" thread/loaded/list '{}'

codex queue --thread '<thread-uuid>' \
  --message 'Reply with exactly RECON_OK' \
  --remote "unix://$ROOT/app.sock"

"$PROBE_PYTHON" scripts/probes/tmux-delivary/ws_request.py \
  "$ROOT/app.sock" thread/turns/list \
  '{"threadId":"<thread-uuid>","itemsView":"full"}'

"$PROBE_PYTHON" scripts/probes/tmux-delivary/render_tui.py "$ROOT/tui.log"
```

`queue` лишь принимает сообщение. Для вывода о доставке нужны последующие `thread/turns/list` и/или видимый ответ в том же TUI, а не один ответ CLI. `render_tui.py` не обязателен для доставки: он только разбирает уже сохранённый PTY-поток (например, `$ROOT/tui.log`) в экран и позицию курсора.

Завершать проверку следует только сигналами своим child process groups. Нельзя убивать общий app-server, менять глобальную установку или удалять чужие каталоги.

## Что подтверждено

Санированные выдержки: [основные ходы A](evidence/codex-a-turns.sanitized.json), [environment B](evidence/codex-b-environment.sanitized.json), [финальный TUI](evidence/codex-tui-final.sanitized.json). В них нет credentials, account data или рабочих чатов; оставлены только синтетические UUID, команды и маркеры.

| Проверка | Результат | Доказательство |
| --- | --- | --- |
| Общий thread TUI и внешнего отправителя | PASS | Один UUID `01a19403-6bbc-7f53-ad7d-16c45c63a357` в `thread/loaded/list`, queue и `thread/turns/list` |
| Idle delivery | PASS | Прямой `turn/start` завершился с `TUI_THREAD_H`; текст виден в TUI и полной истории |
| Busy follow-up | PASS | Длинный ход: started `1793484627`, completed `1793484651`; follow-up: started `1793484651`, completed `1793484653` с `BUSY_G_OK` |
| Shell environment A | PASS | Shell-tool напечатал `ROCKET_SESSION_ID=rocket-5907-a`, `ROCKET_SOCKET=/tmp/rocket-5907-a.sock` |
| Shell environment B / изоляция | PASS | Другой сервер напечатал `rocket-5907-b` и `/tmp/rocket-5907-b.sock`, не значения A |
| Черновик после busy/reconnect | PASS | `busy3-черновик-ε` остался на финальном экране после `RESTART_I_OK` |
| Offline queue | PASS (ожидаемый отказ) | После остановки сервера `codex queue` завершился с exit 1 и `No such file or directory`; слепого retry не было |
| Restart server | PASS | Тот же thread принял новый queue message; TUI переподключился и показал `RESTART_I_OK` |
| Курсор в середине строки | **UNVERIFIED** | Исходный скрипт отправил два Left, но BEFORE-кадра нет. Финальный `x=18,y=37`: 16 символов черновика + 2 клетки prompt, то есть конец ввода |
| Отдельный restart именно TUI | NOT TESTED | Проверен stop/restart app-server и автоматический reconnect TUI; отдельное завершение/запуск TUI не проверялось |
| Exactly-once при дублях/сбоях | NOT CLAIMED | Повторные одинаковые маркеры в экранном логе не являются проверкой дедупликации или exactly-once |

Граница результата: mid-line cursor и отдельный lifecycle TUI остаются непроверенными; exactly-once и дедупликацию повторных маркеров этот эксперимент не проверял. Полный интеграционный gate поэтому не пройден, хотя доставка idle/busy, окружение и восстановление подтверждены.

## Ограниченная автоматическая проверка: отрицательный результат

Был сделан один новый запуск `codex_native_probe.py` с непрерывным draining PTY и BEFORE/AFTER snapshots. Он не дошёл до черновика: ожидание initial `turn/completed` закончилось через 120 секунд с ошибкой. Это повторило исходный сбой старого probe, а не стало зелёной проверкой.

Ключевой факт для диагностики: rendered TUI содержит `PROBE_READY` и `Worked for 2s`. Значит, модельный ход завершился; нельзя описывать случившееся как зависший turn. Observer не получил `turn/completed`. Непрерывный draining не устранил проблему, поэтому PTY backpressure остаётся лишь ранней гипотезой, а не установленной причиной. Точная причина потери уведомления не установлена, дополнительных попыток не было.

Санированная выдержка: [codex-automation-failure.sanitized.json](evidence/codex-automation-failure.sanitized.json).

После ревью скрипт сокращён до воспроизведения именно этого сбоя: неисполненный success-path с cursor/busy/restart удалён, чтобы непроверенная логика не могла дать ложный PASS. Полный cursor-сценарий остаётся задачей следующего gate.

Повторить ограниченный сценарий можно так, только в новом пустом каталоге:

```bash
# из корня репозитория
PROBE_PYTHON=/path/to/isolated-python-with-pyte-and-websockets

"$PROBE_PYTHON" scripts/probes/tmux-delivary/codex_native_probe.py \
  --root /tmp/codex-cursor-recon \
  --session-id rocket-cursor-recon \
  --rocket-socket /tmp/rocket-cursor-recon.sock
```

Ожидаемый результат на сохранённой конфигурации — `status=failed` через 120 секунд, а не PASS. Скрипт оставлен как минимальный воспроизводимый probe именно этого сбоя, не как рабочая интеграция и не как cursor-тест. `render_tui.py` и `ws_request.py` — более узкие полезные примитивы для ручного рецепта. При cleanup `codex_native_probe.py` завершает только собственные child process groups (`start_new_session=True` и `killpg`); общий app-server daemon и чужие каталоги не затрагиваются.

## Предложение для следующей спецификации

1. На каждую новую Codex-сессию запускать отдельный app-server с её `ROCKET_*` и приватным UDS; общий managed daemon не использовать.
2. Штатный TUI подключать через `--remote`, внешний отправитель — к тому же endpoint и конкретному thread UUID.
3. После `queue` подтверждать по истории нужного thread, различать accepted/unknown/not_sent и не повторять неоднозначную отправку вслепую.
4. Не делать скрытого fallback в tmux. Offline/restart обрабатывать как отдельные состояния, а не как разрешение вставить текст в редактор.
5. До production gate отдельно доказать BEFORE/AFTER mid-line cursor, TUI restart и надёжное observer-завершение. Для cursor-теста сначала записать rendered BEFORE с доказанным положением внутри строки, затем busy/reconnect и AFTER.

Пока Rocket Codex остаётся на прежней доставке. Этот документ не вводит production API и не объявляет канал готовым.
