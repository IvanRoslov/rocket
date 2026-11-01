# Pi bridge: индекс evidence и остаточных ограничений

Дата: 1 ноября 2026. Документ только для чтения и ревью; новых проб не вводит.

## Текущий numbered bridge (pi-uds-v1)

SHA-256 проверенных blobs:

| Файл | SHA-256 |
| --- | --- |
| `internal/agent/pi/delivery-extension.ts` | `c593bb8df1da212c7e67fb0156ce864b22bc9a3c23db01dd3aaf271343043b4a` |
| `internal/agent/pi/delivery.go` | `153d63d05068c96979f91c04725d378f48be9957bfcddf6da39b562a3d2e4f0d` |
| `internal/agent/pi/confirm.go` | `a14aa3833a19a0bbbf574bc670f418da7b70324e3785e3d71e4bd5d043b8af73` |
| `internal/queue/native.go` | `f42bcf7e27613e18d6c15d600a56a01321bd8cb990d66d658bddb20962446bdb` |
| `internal/queue/confirm.go` | `086492a29306b5e0e2af71e2b6c864dacfc498d0e20b5bb79ae9018789783a4b` |
| `internal/queue/pi_envelope.go` | `374849eabc2cb2d4a01e9f2df81484553a34aae14d5fb801a1ea3d026e9edc52` |
| `internal/queue/pi_equal_text_correlation_test.go` | `461345fcad4f6d9b5568caec648f39245e3ac944a36fdd5c641a9012c6dfd25f` |
| `internal/queue/pi_envelope_test.go` | `5c3b7b228b17908b05e53a18c0abfffa431f38ecf78b7c47cf6c2c125fbfa34e` |
| `internal/queue/pi_stock_e2e_test.go` | `2a60d701a67cc5ed0bee1019a8adbbe3d29ba0639e76278c6c24b58e320e6b53` |
| `scripts/probes/tmux-delivary/pi_queue_e2e_fixture.py` | `4e14a783023befc8be3ec95f6d6642f1ce113a2b296b7699d6090be2cbd8f160` |
| `scripts/probes/tmux-delivary/native_bridge_probe.py` | `99de8bb1f4efcd7486b5567d3465a1fa83c81d46d5d62e73318ebf7ea0d99150` |
| `scripts/probes/tmux-delivary/input-handled-extension.ts` | `29227bf1ce0e1d3e8d0a2040d6d4ec4c3186174fbe0a5e6201211acbc4802ee4` |
| `scripts/probes/tmux-delivary/native-bridge-probe-extension.ts` | `7a497a2aa7fc6fb45f4fc957e60283082eafdca5af1b16666b463fe99df4a2b3` |
| `scripts/probes/tmux-delivary/slow-ui-listener-extension.ts` | `312a064888cbe98177e64fca903174b4080f8375eb509f4c781ff43955a966bb` |

## Точные команды harness

```bash
node --experimental-strip-types --test internal/agent/pi/delivery-extension.test.ts   # 31/31 PASS (compaction table included)
go test ./internal/agent/pi -count=1                                                  # PASS
go test ./internal/agent/pi ./internal/session ./internal/queue ./internal/nativeinput -count=1
go test -race ./internal/agent/pi ./internal/queue ./internal/nativeinput
go vet ./... && git diff --check

# Текущая preservation-матрица + handled-input (14 сценариев)
python3 scripts/probes/tmux-delivary/native_bridge_probe.py --all

# Сквозной queue -> stock Pi (opt-in; без флага обычный suite только SKIP этого теста)
ROCKET_PI_STOCK_E2E=1 go test -count=1 -timeout=90s ./internal/queue -run ^TestPiNativeQueueStockE2E$
ROCKET_PI_STOCK_E2E=1 go test -count=1 -timeout=90s ./internal/queue -run ^TestPiStockFixtureEarlyFailureCleansOwnedGroup$
```

## Матрица: current evidence

`--all` — 14/14 PASS, exit 0. Подпись `[rocket #<id>] ` ставится sender'ом (fixture её эмулирует), bridge остаётся текстово-агностичным. Старых pre-fix PASS в этой таблице нет.

| Сценарий | Evidence |
| --- | --- |
| `idle-single`, `idle-multiline`, `long-pointer` | русский draft и mid-line cursor сохранены до/после, injected 1, provider 1 |
| `empty` | пустое поле остаётся пустым, injected 1, provider 1 |
| `busy-single`, `busy-multiline` | draft/cursor сохранены во время хода, injected 1, provider 2 |
| `selector` | окно открыто, поиск/стрелки, Escape возвращает draft/cursor, model_selects 0 |
| `modal-guard` | все 5 видов: `not_sent` при открытом окне, `accepted` после закрытия |
| `delayed-modal` | delayed `accepted` при открытом окне, `ui_prompt_start` 1202 ms, slow listener 1201 ms; control 1 ms и `not_sent`, provider 0 |
| `target-frames` | wrong target `not_sent`/delta 0, oversized `not_sent`, split UTF-8 1, equal text 2 записи, duplicate 1 |
| `escape-dequeue` | queued подписанный текст в редакторе (действие Pi), cursor в конце, injected 0 |
| `reload` | тот же ID не переотправлен после `/reload`, injected 1 |
| `foreign-history` | endpoint отсутствует, 0 injected/0 provider, observed session B |
| `handled-input` (отдельно) | 1 handled + 1 continued, 1 user record, provider 1 |

Сквозной queue→Pi: PASS (normal и opt-in `-race`), A `failed/native_delivery_unconfirmed` после test-only +31m offset, B `delivered` своей записью; sender 1/ID, API 1/ID, records A0/B1, tmux 0. UDS count — derived: один dial/write на измеренный Sender invocation, не независимо наблюдаемый. Fixture и Pi в одной fixture-owned process group; pre-ready cleanup regression PASS (temp dir удалён, group gone, Python reaped); Queue.Run joined в cleanup (join confirm-loop публичный API не обещает). Evidence: `results/native-bridge/pi-stock-e2e/result.json`.

Compaction: deterministic FakeApi-таблица (before/after success/failure/cancellation/deferred+duplicate), exact outcomes/API counts, 31/31 Node. Stock live compaction на локальном provider не запускался — unknown/limit.

## Остаточные ограничения

**Lifecycle/restore**
- Отменённый `session_before_switch` закрывает приём до `/reload`.
- Повреждённое accepted-состояние — fail-closed до нового Rocket launch.
- После non-reload завершения launch ретирован; возврат в ту же историю требует Rocket restore.
- Старые неподписанные native-записи не подтверждаются; timeout/exit/restore → `native_delivery_unconfirmed`.
- nil-confirmer timeout policy не расширялась (восстановлено прежнее поведение non-nil legacy-only).

**Modal/deferred**
- Окно задержанного async `ui_prompt_start` допускается Spec2; guard best-effort.
- Login/OAuth и часть `showSelector`-веток — source-only; live только встроенный model selector.
- `{action:"handled"}` — доказанный no-record witness; брошенная ошибка input-handler'а Pi ловится и продолжается, отсутствие записи не доказывает.

**Compaction**
- Guard на `session_before_compact`/`session_compact`/`session_compact_failed`; deterministic FakeApi-таблица покрывает before/after success/failure/cancellation и deferred/duplicate (API не растёт). Stock live compaction не проверялся — unknown/limit.

**Операционные**
- GitHub Actions отсутствуют; пустой CI не считается зелёным.
- PR163 остаётся draft до принятия root; merge/verify — root.
- 5914/Claude shared hunks не входят; `confirm_test.go` не менялся.
