# Spec5911 v4: сообщения DeepSeek через Claude Code

Починим штатную отправку сообщений выбранному DeepSeek через Claude Code. Ложное завершение больше не должно терять feedback, а смена беседы в том же процессе будет подхватываться автоматически. Два одинаковых текста получат разные служебные номера, чтобы подтверждение одного нельзя было принять за подтверждение другого. После слияния Иван обновит и перезапустит Rocket; проверим новую DeepSeek-сессию. Перенос старых процессов убран. Старые сообщения, которые могли уже уйти, повторять не будем. Сроки ожидания остаются прежними.

## 1. Что уже сделано и что меняет эта версия

Проверенный код находится в draft [PR170](https://github.com/IvanRoslov/rocket/pull/170), head `d0e1968653753c85b5df1c2648ac0317e5d9f72f`, base `007387a7715c059fdb997a758efa004e532c7767`. Независимое review5916 закрыло I-1/I-2/M-4 на `9fc11c8f90ff133385d88f78ef974e7549ebf62c`; d0 добавляет только описание ограничений. Focused/full/vet/seven-package race прошли на этом коде. Это не доказательство исправления на живом Flash.

Версия4 завершает I-3/I-4 и T1: вместо переноса старых процессов используем новую сессию; разрешаем автоматическую смену UUID беседы только в уже проверенном процессе; различаем равные сообщения служебным UUID. Добавляем однократную границу обновления, чтобы не повторить старую неизвестную отправку. Это дополнение существующего Claude transport, не новый транспорт и не enrollment API. Технические решения — предложение оркестратора по проверенному source/review, а не приписанное человеку требование.

Предыдущие варианты ручного подтверждения каждой беседы, подключения старого root и границы по первому запуску отклонены. Последний вариант ошибочно исключал feedback, созданный после обновления, но до запуска worker. Граф родства процессов и глобальный запрет одинаковых текстов не нужны: различаем сами сообщения.

Эта Spec заменяет Spec3. Неизменённые гарантии frozen contract SHA256 `93a4202ec6c49f173891700b9d5914260443a0aaee5280fc0b896294e4429b68` сохраняются; прежние сроки уже уточнены Go3. Новые поля/методы и формат ниже имеют приоритет над старым запретом менять envelope. До нового gate Go — только документы и read-only review.

## 2. Неизменяемая основа доставки

- Optional Claude socket capability объявлена ClaudeCode и наследуется DeepSeek. Неподдерживаемые адаптеры не получают доступ по сходству имени. SocketDelivery автоматически не включается. Claude не переводится в NativeSender/NativeLaunch.
- Один resolver для sender/lifecycle проверяет launch generation, полную pane identity/PID-start, root PID/start и ancestry без nested Claude, UUID, protocol1, interactive kind, socket и канонический journal. Полный registry проверяется fail-closed, включая неизвестного альтернативного writer. Ошибка probe/metadata не означает смерть. Перед записью — повторная проверка. Wire session_id закреплён за UUID.
- Выбранный Claude route при недоступности не переходит в tmux. Active допускает штатную socket queue, genuine blocked сохраняется. Одной доступности сокета недостаточно для ready. Неквалифицированный/stale/nested SessionEnd не доказывает OS exit. Смерть устанавливается независимо для точного root.
- Hook читает только bounded metadata, не prompt/tool_input/секреты. Существующие ограничения: stdin16KiB, Python alarm1s, ps0.5s; ошибка/перегрузка может потерять событие. Это best effort, не повод принудительно менять состояние. Poll читает точный журнал, не newest transcript.
- До I/O устойчиво сохраняются marker, immutable snapshot, wire UUID, точный адресат, envelope, физический journal dev/inode и полная byte boundary. Ошибка сохранения запрещает send. После claim/possible write/held/accepted/unknown автоматического resend нет, включая crash/recover. Даже отсутствие записи не выводится из тишины.
- Receipt означает transport hint/acceptance, не delivered. Receipt без UUID остаётся UNKNOWN. Успех — полная точная incoming user запись из сохранённого UUID/file после границы, с устойчивым one-to-one consumption. Assistant/tool/screen echo не proof. Caller-derived общий бюджет scan+atomic re-read максимум5s, общий scan limit32MiB; отмена до commit не оставляет частичного proof/delivered. Уже состоявшийся commit не опровергается поздней отменой.
- Ownership/ambiguity/retention одного физического журнала не разделяются launch generation. Immutable side row либо main marker исключают unsafe generic recovery/expiry. Unresolved история сохраняется, в том числе после terminal unconfirmed; смерть root не доказывает закрытие всех writers и не разрешает GC. Повреждённая история fail-closed. Failed не открывается поздним proof.

Исторический producer ложного exit не доказан; это остаётся пределом вывода. Положительный пример79481 доказал получение/чтение в нужной instance, но не его socket route и не общий fix. Evidence хранится в [research.md](research.md).

## 3. Автоматическая смена беседы внутри проверенного root

Отсутствующий исторический launch не восстанавливаем по нынешнему процессу. Новый поддерживаемый spawn/restore сохраняет независимый launch как в проверенном коде. Первое binding появляется после полного resolver validation.

Для уже bound root можно автоматически сменить только Claude UUID и соответствующее каноническое имя journal. Rocket session/generation, full pane/PID-start, root PID/start, kind/protocol/socket и родительский каталог journal обязаны совпасть. Новый journal должен существовать и пройти прежние file/boundary проверки. Missing/lazy file, duplicate UUID в другом registry entry, неподтверждённый процесс или другая pane не разрешают переход. Никаких ручных команд после каждого clear/resume.

Добавляем JSON-поле `Launch.BindingRevision uint64` и `Snapshot.BindingRevision uint64`. Отсутствие декодируется как0. Это token свежести, не полномочие root и не новый proof domain. Увеличение с переполнением запрещено. Genuine новый launch имеет свою generation; stale pane completion не может откатить generation или binding revision.

Точные добавочные методы Store, прежние entry points сохраняют совместимость:

```go
GetClaudeBinding(ctx context.Context, sessionID string) (claudeinstance.Launch, *claudeinstance.Identity, int64, error)
AdvanceClaudeBinding(ctx context.Context, previous, next claudeinstance.Identity, expectedRevision uint64, observedEventAt, at int64) (bool, error)
AcceptClaudeHookForBinding(ctx context.Context, target claudeinstance.Identity, revision uint64, at int64, state string, activityTS int64) (bool, error)
ApplyClaudeActivityForBinding(ctx context.Context, target claudeinstance.Identity, revision uint64, observedEventAt int64, state string, activityTS int64) (bool, error)
```

Get возвращает вместе launch/binding/last_event_at из согласованного чтения. Event/barrier — UnixNano. Advance после повторной проверки candidate атомарно сравнивает полную прежнюю identity/generation/revision и `last_event_at==observedEventAt`, увеличивает revision, меняет binding, повышает barrier и очищает activity в unknown. Новый barrier положительный, больше прежнего и не из будущего. CAS loser ничего не очищает. Нельзя обнулять high-water при смене только беседы.

Qualified hooks сначала проходят прежнюю provenance/event validation, затем identity+revision CAS. Poll, prompt/quiz capture и miss counters используют identity+revision+observedEventAt; stale работа не меняет даже cache/counter другого token. Новое waiting_input notification инвалидирует прежний capture. Старое по timestamp, но настоящее blocked из текущего resumed journal остаётся blocked: journal timestamp не является binding epoch. Observed A→B→A отвергает старую работу A по revision; невидимая ABA не объявляется другим OS-процессом.

Queue сначала разрешает binding, затем читает persisted eligibility и повторно проверяет token. Нельзя использовать ready беседы A для первого claim беседы B. Prepare сохраняет revision; Claim атомарно проверяет current identity/revision и допустимое persisted activity; Send повторяет проверку перед единственным вызовом записи. Eligible остаются active/ready/idle/waiting_input. Unknown/exited ждут; genuine blocked даёт прежний отказ. Unversioned monitor cache сам по себе не разрешает send.

Переход не переписывает уже claimed сообщения: их старые root/UUID/file/boundary/proof остаются immutable. Текущая revision не требуется для подтверждения старого snapshot. Гонка записи с переключением беседы может остаться unconfirmed; нельзя обещать атомарность внутренней очереди Claude. Новая generation/current UUID сами по себе не доказывают смерть старого target.

## 4. Уникальный номер в служебной оболочке

Добавляем pure helper `socketmsg.EnvelopeWithID(id, from, fromName, body string) (string, error)`; существующий `Envelope` и его native/Pi callers не меняем. Новый Prepare сначала создаёт WireID, затем envelope:

```text
<cross-session-message msg-id="<wire-uuid>" from="..." from-name="...">
<прежний prepared text>
</cross-session-message>
```

Дополнительные существующие sender attributes сохраняют порядок после msg-id. Допускается ровно один canonical UUID в opening tag, равный WireID. Сохраняем `Snapshot.EnvelopeVersion uint8`: новые claims только version1, absent/version0 — старые сохранённые snapshots. New claim version0, отсутствующий/повторный/невалидный/несовпадающий msg-id и неизвестная версия запрещены. Stored version1 дополнительно сверяется с прежними SQL wire/hash/snapshot guards; Send сравнивает сохранённые version/revision.

UUIDv4 плюс существующий UNIQUE wire_id обеспечивают различие сообщений. Legacy version0 decoding/confirmation не меняется; старые envelopes не переписываются и не пересылаются. Unwrap возвращает прежний body. JSONL поля, session_id, Raw:true, исходный Message.Body, inbox file и prepared text не меняются. Меняется только видимая служебная metadata opening tag.

Две равные человеческие фразы имеют разные полные envelopes. Поздняя M1, попавшая в B после clear/restore или через сохранившегося writer другой Rocket session, не подтверждает M2. M1 в чужом журнале всё ещё не доказана; msg-id не разрешает GC и не заменяет точный journal proof. Никаких lineage tables или global equal-body блокировок.

## 5. Граница обновления для старых неподтверждённых строк

В исходном007 ErrHeld возвращал queued/attempts0 уже после socket write. Поэтому даже attempts0 не доказывает, что старое сообщение не отправляли. Границу старых строк фиксируем один раз при миграции этой версии, не при первом запуске recipient.

По журналу разрешённых операций0027 пока не применялась к non-fixture DB: PR draft, deployment не было. Добавляем в unreleased `0027_claude_delivery.sql` ровно одну таблицу и capture в той же migration transaction:

```sql
CREATE TABLE claude_legacy_cutoff (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 through_id INTEGER NOT NULL CHECK(through_id>=0)
);
INSERT INTO claude_legacy_cutoff VALUES(1,(SELECT COALESCE(MAX(id),0) FROM messages));
```

Если до выпуска установлено фактическое non-fixture применение0027, используем новую0028; самовольно читать/мигрировать live DB ради поиска не разрешается. `ClaudeLegacyCutoff() (int64, error)` требует ровно одну корректную singleton строку с целым неотрицательным значением; missing/corrupt не превращается в0. Fresh DB получает0. AUTOINCREMENT отделяет новые IDs. Reopen/spawn/restore/pane completion/UUID follow никогда не пересчитывают cutoff. Launch boundary поля и BeginClaudeLaunch API не добавляем.

Порядок только для Claude-selected socket first claim, unmarked queued без любой side ownership:

1. Уже истёкший configured age сохраняет timeout.
2. Attempts>0 сохраняет прежний delivery_unconfirmed независимо от cutoff.
3. При ошибке cutoff attempts0 ждёт unavailable с диагностикой, без claim/send.
4. Attempts0 и ID<=cutoff получает видимый legacy_unproven через существующие message.failed и sender notification, без новой попытки/side row/socket/tmux. Возможно, это было действительно неотправленное старое сообщение; отправитель сможет осознанно отправить его заново.
5. ID>cutoff/attempts0 — обычный первый claim после exact identity/eligibility. Cutoff/ownership/eligibility повторно проверяются в atomic Claim.

Feedback, созданный после миграции до готовности launch, остаётся допустимым после genuine Restore. Marked/any-side/accepted/delivering/failed/delivered строки, proof/recovery/retention не перезаписываем. Native/generic expiry/ResetDelivering, SocketDelivery=false и incompatible tmux policy не меняем.

## 6. Сроки остаются прежними

Решение Ивана5911/Q7 сохраняется полностью. Неуказанная настройка использует прежний Rocket default. Положительный QueueTimeout сохраняет age deadline. Explicit<=0 не получает Claude-only30min: generic unmarked queued housekeeping прежний, а possible-write/accepted хранит disabled age как zero time/SQL0, устойчиво после reopen/config change. Invalid nonzero encoding не становится disabled.

Для exact saved-root death или terminal/missing Rocket session, относящейся к тому же сохранённому launch, сначала общий bounded final proof scan; без proof — delivery_unconfirmed в обоих режимах возраста. Generation mismatch, legacy exit, probe error или отсутствие matching generation evidence не закрывают старый live/unknown target. Receipt не выдаёт lifecycle closure. No terminal reopen/no resend/retention сохраняются. Ожидание при живом/неизвестном target без age limit может длиться неопределённо; unresolved storage тоже может расти неопределённо.

## 7. Проверки, merge и установка

Реализацию продолжает существующий5914, review — независимый5916. Перед merge: RED→GREEN новых cases, focused affected packages, `go test ./...`, `go vet ./...` и семь race packages: queue/socketmsg/claudeinstance/monitor/api/store/session. Логи привязаны к точному final integrated head; изменение после проверки получает meaningful повтор, cache reuse раскрывается. Неизменённые historical PASS не подменяют final проверку.

В репозитории GitHub Actions workflows отсутствуют (fresh API проверка:0); `checks=[]` не является green CI. Эта версия разрешает обязательный local fallback: все перечисленные команды exit0 на конечном integrated head плюс независимое review без unresolved Critical/Important. Если к merge появилась реальная required CI, она тоже должна быть green. Новую workflow/paid runners/branch protection в этой задаче не создаём. Это явное уточнение прежнего условия CI, вступающее в силу только по Go этой Spec.

Иван5911/Q9 разрешил публикацию и слияние готовых проверенных исправлений. Draft170 уже опубликован. После Go, реализации, review/checks оркестратор обновляет PR обычным push, приводит описание к итоговому scope и сливает; затем `rocket verify-merge 5914` по content. Это не разрешение слить чужой WIP. Native/Pi владелец отдельно завершает свой PR и передаёт финальный diff на compatibility review.

Rollout отдельно: сначала точные merged SHA/build commands/rollback и текущий session inventory; затем Иван обновляет/перезапускает Rocket. Daemon restart не переавторизует старые Claude-процессы. Новая DeepSeek Flash-сессия запускается штатно с новой launch записью; согласованная проверка использует один новый rocket send без replay historical feedback. Сохраняем message ID, exact UUID/PID-start/pane, фактический transport/snapshot, incoming journal record и recipient Read/ACK. Connect-only, accepted или один CLI delivered не заменяют proof. До этого не заявляем live acceptance и не перезапускаем сессии/демон сами.

## 8. Ownership и вне scope

Stable5905 merged/content-verified; дальнейшая работа идёт поверх опубликованного main с сохранением upstream. Последняя сверка main `18276902eccabbb153aa568763551c6b2e301d9a`: после007 только prompts и версионирование task docs; их не откатываем. Конкретный final integration SHA фиксируется перед checks/review.

По finished partial handoff `9224eb882b7823dd4f35fe27998a2d45e435e067` native functions attemptNativeDelivery/failNativeAttempt/confirmNativeRecipient и native-only fixtures переданы5906.5914 их не редактирует; `internal/queue/pi_equal_text_correlation_test.go` исключительно5906. Остальные Claude/shared hunks5914-first. Если фактический новый diff пересекается с переданным — сначала exact hunk coordination с tmux-delivary-orch. Native API/0026, Pi/nativeinput/heartbeat production не меняем.

Вне scope: enrollment/подключение старых процессов, ручной transition endpoint/операторские команды, inference/provider/секреты/тарифы, смена модели, Variables code, переписывание остальных транспортов, глобальный config/symlink/registry/activity workaround, ручной socket/tmux injection и daemon deployment силами разработчика.
