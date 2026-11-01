# Claude-compatible delivery5911 Implementation Plan v4

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Follow superpowers:test-driven-development, superpowers:systematic-debugging and superpowers:verification-before-completion. No subagents or new workers; continue existing5914 after actual gate Go.

**Goal:** Завершить штатную доставку DeepSeek: автоматическая смена беседы проверенного root, отдельное подтверждение равных сообщений и безопасная граница старых неизвестных отправок.

**Architecture:** Сохраняем реализованный Claude socket path. Добавляем binding revision/event CAS, уникальный WireID в envelope и один migration cutoff. Старые snapshots immutable; native/Pi и прежние сроки не меняем.

**Tech Stack:** Go1.25, существующие SQLite/runtime/monitor/queue/socketmsg, без новых dependencies/services.

**Spec:** [spec.md](spec.md), v4. Historical core contract: [claude-contract-proposal-r5-final.md](claude-contract-proposal-r5-final.md), с overrides Spec3/4. Реализация до v4 — draft170/d0; итоговый main интегрируется перед финальными checks.

## Global Constraints

- Новые product edits только после actual gate Go по Spec4/Plan4. Прежний Go3 и разрешение merge не заменяют этот gate.
- Сроки ожидания остаются прежними; Claude-only30min fallback/Q6 API отсутствуют.
- No resend/tmux fallback после selected Claude claim/possible write; old target/proof/retention не перепривязываются.
- Только проверенный current root может автоматически сменить conversation UUID; отсутствие historical launch не исправляем enrollment.
- Не редактировать переданные5906 native functions/fixtures, Pi/nativeinput/heartbeat/0026 и exclusive pi_equal_text_correlation_test.go. Остальные shared hunks5914-first.
- Никаких live probes/sends/replay/daemon/config/registry/hook mutations, секретов, paid resources или новых workers. Rollout делает Иван после отдельного brief.
- Draft170 можно обновить и слить после реализации, local fallback всех обязательных checks и независимого review; появившаяся реальная required CI также обязана пройти.

## Review Focus

1. Delayed A activity/capture после перехода в B, где genuine blocked старше по timestamp: B не разблокируется (Task2).
2. Hook между чтением registry и transition CAS; observed A→B→A: stale revision/event не очищает новое waiting_input и не даёт claim (Task2).
3. Два одинаковых сообщения и сохранившийся writer другого Rocket session: один incoming record не подтверждает второе (Task1).
4. Feedback после upgrade, но до launch/Restore: global cutoff не превращает его в legacy_unproven (Task3).
5. Смена binding/Restore при paused sender и old accepted: sends<=1, old proof/saved-root death не заменяются current identity (Task2/4).

## Статус и разбиение

Core Tasks1–3 прежнего плана завершены и проверены на d0; whole-code review I-1/I-2/M-4 закрыт. Это continuation того же worker5914/branch/PR170, не переписывание core и не новый spawn. Новые Tasks1–3 ниже допускают отдельные локальные commits/проверку; whole integrated review и merge — Task4. Reviewer5916 уже независим от автора. Оркестратор пишет документы/координирует, не product code.

### Task1: Уникальный envelope ID

**Files:** Modify `internal/socketmsg/envelope.go`, `envelope_test.go`; `internal/claudeinstance/confirmation.go` и tests (Snapshot); `internal/queue/socket.go`, `socket_test.go`; `internal/store/claude_delivery.go`, `claude_delivery_test.go`. Дополнительные Claude-only queue tests допустимы в существующих claude_* test files. Generic/native Envelope callers не менять.

**Interfaces:** Produces `socketmsg.EnvelopeWithID(id, from, fromName, body string) (string, error)` и `Snapshot.EnvelopeVersion uint8`. New claim version1, legacy decode0. Consumes existing WireID generation/UNIQUE wire_id, immutable snapshot и full-envelope proof.

- [ ] Написать `TestClaudeEnvelopeWithID` table: canonical UUID даёт ровно один msg-id перед прежними attributes; Unwrap(body) прежний; invalid UUID ошибка. `TestClaudeNewClaimRequiresEnvelopeVersion1` отвергает v0/unknown version, missing/duplicate/malformed/mismatched tag без claim/I/O.
- [ ] Написать `TestClaudeEqualBodyDistinctWireProof`: M1/M2 одинаковый prepared text, разные WireID; только M1 record после M2 boundary не подтверждает M2. Варианты same-root A→B, Restore и другая Rocket session с B. Проверить обе настоящие записи подтверждают только свои сообщения; v0 pending/Recover не переписывается и не resend.
- [ ] Запустить выбранные новые tests `go test ./internal/socketmsg ./internal/queue ./internal/store ./internal/claudeinstance -run 'TestClaude(EnvelopeWithID|NewClaimRequiresEnvelopeVersion1|EqualBodyDistinctWireProof)' -count=1`, сохранить конкретные RED до fix.
- [ ] Реализовать pure helper, WireID-before-envelope, version1 consistency в Prepare/Claim/Send/stored validation. Body/inbox/JSONL/session_id/Raw не менять. Exact proof сравнивает полный сохранённый envelope, не отдельный parsed UUID.
- [ ] Запустить те же tests и affected packages, ожидается PASS; сохранить RED→GREEN. Commit `fix: identify each Claude envelope with its wire UUID`.

### Task2: Automatic same-root following и qualified state

**Files:** Modify `internal/claudeinstance/identity.go`/tests, `internal/store/claude_launch.go`, `claude_activity.go`, `claude_delivery.go` и их tests; `internal/queue/socket.go`, `claude.go` и Claude-only tests; `internal/monitor/claude.go`, `claude_journal.go`, `claude_test.go`. Только если вызовы требуют — Claude-only hunks monitor.go/API qualified hook wiring; переданные native spans исключены.

**Interfaces:** Produces Launch/Snapshot `BindingRevision uint64`; consumes Task1 Snapshot version. Store methods ровно из Spec section3:

```go
GetClaudeBinding(ctx context.Context, sessionID string) (claudeinstance.Launch, *claudeinstance.Identity, int64, error)
AdvanceClaudeBinding(ctx context.Context, previous, next claudeinstance.Identity, expectedRevision uint64, observedEventAt, at int64) (bool, error)
AcceptClaudeHookForBinding(ctx context.Context, target claudeinstance.Identity, revision uint64, at int64, state string, activityTS int64) (bool, error)
ApplyClaudeActivityForBinding(ctx context.Context, target claudeinstance.Identity, revision uint64, observedEventAt int64, state string, activityTS int64) (bool, error)
```

Предыдущие методы остаются compatibility entry points; qualified production consumers используют guarded methods. ClaimClaudeMessage signature прежняя, дополнительно проверяет current token/persisted eligible activity в writer transaction. Internal token включает full Identity+revision+observedEventAt; имена private helpers выбирает автор.

- [ ] Написать `TestClaudeSameRootConversationFollow`: A→B и observed A→B→A автоматически bind, revision возрастает, barrier не сбрасывается, activity unknown до qualified signal. Wrong root/start/pane/socket/duplicate registry/missing journal/probe uncertainty переход не разрешают. Overflow не wrap.
- [ ] Написать `TestClaudeBindingCaptureRacesHook` с deterministic barrier: hook между Get и Advance меняет last_event_at, stale observedEventAt CAS=false и B/новый wait не очищен. Отдельно Restore race не позволяет stale pane completion/generation откат.
- [ ] Написать `TestClaudeBindingRejectsStaleActivityAndCapture`: delayed A ready против B blocked старше timestamp; cache/miss из старой revision или до Notification не применяются; genuine new prompt/quiz/wait сохраняется, stale prompt-free miss не участвует.
- [ ] Написать `TestClaudeEligibilityRecheckedAfterFollow`: A ready не разрешает B first claim; blocked не обходится monitor cache. Pause Prepare/Claim/Send, сменить binding/eligibility, получить zero writes до side effect; уже possible-write old snapshot не retarget, sends<=1 и old exact proof подтверждается без current revision.
- [ ] Запустить новые cases в claudeinstance/store/queue/monitor с `-count=1`, сохранить RED. Реализовать минимальные resolver transition + atomic methods и propagate token во все qualified caches/counters/eligibility. Сначала Resolve, потом eligibility; revalidate before CAS/write; no forced ready/death.
- [ ] Повторить focused cases/packages, ожидается PASS; проверить прежние API-error/SessionEnd/blocked/death/timeout/retention regressions. Commit `fix: follow Claude conversations within the verified root`.

### Task3: Однократная граница старых неизвестных отправок

**Files:** Modify unreleased `internal/store/migrations/0027_claude_delivery.sql`; `internal/store/claude_delivery.go` и tests; `internal/queue/claude.go`, `claude_route_test.go`/`claude_policy_test.go`. При необходимости новый Claude-only migration fixture test, без изменения0026/native tests.

**Interfaces:** Produces `(*Store).ClaudeLegacyCutoff() (int64, error)` и singleton table ровно по Spec section5. Consumes Task2 existing Claim method: cutoff/side ownership повторно проверяются атомарно. Никаких FirstClaimAfterID/BeginClaudeLaunch.

- [ ] По release record убедиться, что0027 не deployed вне fixtures. Не читать live DB. При установленном non-fixture применении использовать0028, зафиксировав причину; не переписывать уже применённую миграцию.
- [ ] Написать `TestClaudeLegacyCutoffMigration`: pre-upgrade DB fixture c несколькими messages во всех statuses, migration capture=MAX(id); fresh DB0; поздний insert получает ID>cutoff; reopen/spawn/restore/follow cutoff не меняют. Missing/noninteger/corrupt singleton — error, не0.
- [ ] Написать `TestClaudeLegacyUnprovenNeverReplayed`: pre-cutoff held-style attempts0 => failed/legacy_unproven + notification, sends/Inject/attempt delta0; attempts>0 => прежний delivery_unconfirmed; expired => timeout. Missing/corrupt cutoff attempts0 ждёт без I/O. Проверить этот порядок.
- [ ] Написать `TestClaudePostUpgradeFeedbackBeforeLaunch`: F после migration ждёт, затем genuine launch становится eligible и единожды sends; cutoff не захватывается при launch. Native/incompatible/socket-disabled/marked/any-side/accepted/failed rows без изменений.
- [ ] Запустить новые store/queue cases с `-count=1`, сохранить RED. Реализовать table/getter и ordered preclaim rule, затем atomic Claim guard. Positive/disabled age и generic housekeeping не менять.
- [ ] Повторить новые cases и store/queue packages, ожидается PASS; сохранить RED→GREEN. Commit `fix: reject ambiguous pre-upgrade Claude sends without replay`.

### Task4: Интеграция, независимый review и merge

**Files/artifacts:** final diff, проверочные логи, review5916, существующий Claude delivery runbook/report; описание PR170. Нет новой workflow.

- [ ] Нормально интегрировать опубликованный main, сохранить upstream prompts/doc-versioning; не force-push и не импортировать непроверенный Pi WIP. При пересечении с переданными native hunks остановить только пересечение и согласовать exact diff с tmux-delivary-orch. Проверить frozen manifest и actual changed paths.
- [ ] На settled final integrated head выполнить focused packages: claudeinstance/socketmsg/queue/store/monitor/api/session/agent/claudecode/agent/deepseek; затем `go test ./...`, `go vet ./...`, `go test -race ./internal/queue ./internal/socketmsg ./internal/claudeinstance ./internal/monitor ./internal/api ./internal/store ./internal/session`. Все exit0; логи/head/tree/cache provenance сохранить. Не запускать дубли suites пока предыдущая работает.
- [ ] Передать exact final head reviewer5916 для независимого review Tasks1–3/T1/I3/I4 и проверки whole-code regressions; получить отсутствие unresolved Critical/Important. Новые finding fixes получают meaningful RED→GREEN/checks. Получить native compatibility verdict на реально изменённые shared hunks, не переоткрывая неизменённый контракт.
- [ ] Обновить runbook: новая сессия после обновления, автоматический follow, legacy_unproven и сознательный новый resend только отправителем, wire metadata, прежние limits/indefinite unresolved retention. PR body переписать под final scope, раскрыть checks[]/обязательный local fallback; если actual required CI появилась — дождаться green.
- [ ] После всех checks/review обычный push в PR170, снять draft, merge в пределах разрешения5911/Q9. Проверить `rocket verify-merge 5914` по content, потом task5914 done и kill implementation cleanup без farewell. Review worker/task завершить только когда не нужна новая работа.
- [ ] Согласовать с владельцем5902 его final reviewed Pi merge/compatibility отдельно; не сливать за него WIP. Не считать частичный handoff разрешением трогать оставшиеся Claude hunks.

### Task5: Установка человеком и доказательство новой доставки

**Artifacts:** короткий rollout brief и итоговый report5911; product code не добавляется.

- [ ] Зафиксировать точные merged SHA, команды build/update для Ивана, session inventory, влияние на другие живые сессии и rollback. Перезапуск делает Иван; root не меняет daemon/binary/global config. Даём команду «можно пересобирать» только после готовности кода.
- [ ] После подтверждения установки запустить/согласовать новую deepseek-flash-1m Claude-сессию штатно с доказанным launch, без restore старого guard ради обхода. Согласовать одно новое сообщение и проверить отсутствие уже выполненного side effect перед повторением команд.
- [ ] Один normal rocket send; сохранить message ID/native Claude route/snapshot и точную incoming journal запись/Read/ACK адресата. При uncertainty не повторять; investigated reason/retained ownership вместо ложного success. Только этот proof закрывает live acceptance.
- [ ] Upload report: почему не доходило, что изменено, tests/review/local fallback/merge/live evidence и ограничения. После завершения всех subtasks и cleanup — task5911 review; done ставит человек.

## Self-review

Spec sections2/6 сохраняются core regression suite и Tasks2–4; sections3/4/5 реализуются Tasks2/1/3; checks/ownership/rollout — Tasks4/5. Все пять Review Focus имеют отдельные cases. API signatures совпадают со Spec, cutoff не зависит от launch, old snapshots immutable. Этот план не выдаёт historical PASS за итоговый head и не разрешает implementation до нового gate.
