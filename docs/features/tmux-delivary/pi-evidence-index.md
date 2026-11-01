# Pi bridge: индекс evidence и остаточных ограничений

Дата: 1 ноября 2026. Документ только для чтения и ревью; новых проб и production-изменений не вводит.

## Текущий bridge

SHA-256 на этом коммите (head цепочки — `be01768`, плюс docs/test-comment правка):

| Файл | SHA-256 |
| --- | --- |
| `internal/agent/pi/delivery-extension.ts` | `c593bb8df1da212c7e67fb0156ce864b22bc9a3c23db01dd3aaf271343043b4a` |
| `internal/agent/pi/delivery-extension.test.ts` | `e64334259977399dcb780df07b26f284d86a05bd35dbf7103577c857880ba239` |
| `internal/agent/pi/delivery.go` | `153d63d05068c96979f91c04725d378f48be9957bfcddf6da39b562a3d2e4f0d` |
| `internal/queue/pi_equal_text_correlation_test.go` | `14867db43fb4422d94b03e2e659c523fc38101b55918ec015317452a8340ee17` |
| `scripts/probes/tmux-delivary/native_bridge_probe.py` | `ace4f8d457fcdbddcc0ea1fcc52ba5bccc0956853642d96f73dbe52516b29764` |
| `scripts/probes/tmux-delivary/input-handled-extension.ts` | `f2409c10efaa504e0cd6688b8f386e1722dcdc7bf3ea05f48602d40f2a35b2b6` |

`pi_equal_text_correlation_test.go` — осознанно RED (Q5); все остальные перечисленные checks по Pi-модулю зелёные.

## Точные команды harness

```bash
node --experimental-strip-types --test internal/agent/pi/delivery-extension.test.ts   # 29/29 PASS
go test ./internal/agent/pi -count=1                                                  # PASS
go test ./internal/agent/pi ./internal/session ./internal/queue ./internal/nativeinput -count=1  # Pi/session/nativeinput PASS; queue RED только Q5-тестом
go test -race ./internal/agent/pi -count=1                                            # PASS
go vet ./...                                                                          # clean
git diff --check                                                                      # clean

# bounded live, current bridge
python3 scripts/probes/tmux-delivary/native_bridge_probe.py \
  --scenario reload --scenario target-frames --scenario foreign-history
python3 scripts/probes/tmux-delivary/native_bridge_probe.py --scenario handled-input

# Q5 counterexample (intentional RED)
go test ./internal/queue -run 'TestPiEqualTextCorrelation' -count=1 -v
```

## Матрица сохранения: pre-fix vs current

Всего 13 preservation-сценариев (12 исходных + `foreign-history`) плюс новый Q5-witness `handled-input`.

| Сценарий | Состояние evidence |
| --- | --- |
| `reload`, `target-frames`, `foreign-history` | current: перезапущены после review-исправлений, PASS |
| `handled-input` (Q5 witness, вне preservation-матрицы) | current: PASS, 1 matching record, 2 accepted |
| `idle-single`, `idle-multiline`, `empty`, `long-pointer`, `busy-single`, `busy-multiline`, `selector`, `escape-dequeue` | pre-fix JSON (17:12–17:14); на текущем bridge не перезапускались |
| `modal-guard`, `delayed-modal` | pre-fix; у `delayed-modal` в файле `ui_prompt_starts = 0`, задержка slow listener этим артефактом не доказана; сводка `/tmp/pi-live-all.out` устарела относительно per-scenario JSON |

## Остаточные ограничения

**Lifecycle/restore**
- Отменённый `session_before_switch` оставляет endpoint закрытым до `/reload`.
- Повреждённый accepted-state переводит launch в fail-closed до нового Rocket launch.
- Политика очистки старого runtime предполагает штатный restore, который сначала гасит старый process.
- После не-reload завершения launch ретирован; возврат в ту же историю требует Rocket restore, а не переиспользования старого моста.
- Нет единого end-to-end queue→Pi теста: Q5 собран из двух дополнительных проб (queue counterexample + реальный handled witness).

**Modal/deferred**
- Окно задержанного async `ui_prompt_start` допускается Spec2; guard best-effort, без изменения порядка handlers.
- Login/OAuth и прочие ветки `showSelector` — source-only; live-доказательство только для встроенного model selector (pre-fix).
- `modal-guard`/`delayed-modal` на текущем коде не перезапускались.

**Compaction**
- Guard построен на `session_before_compact` / `session_compact` / `session_compact_failed`; current-code live evidence компакции нет (pre-fix/plan-level).

**Операционные**
- Q5 RED открыт; shared `internal/queue/confirm.go` held до отдельного ruling.
- Полная live-матрица остановлена до triage.
- GitHub Actions отсутствуют; пустой CI не считается зелёным.
- PR163 остаётся draft; root владеет интеграцией/merge.
