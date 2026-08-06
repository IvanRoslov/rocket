# Реплика в закрытый тред не переоткрывает его без `--dispute`

Задача #1181 (фича papercuts-rocket-1006-1050).

## Проблема

`internal/api/questions.go` (и симметрично `internal/api/agent_questions.go`)
безусловно ставит `reopen = true` для ЛЮБОЙ реплики агента в resolved-тред.
Обычное «принял, работаю» переоткрывает вопрос, поднимает бейдж человеку и
возвращает тред в attention. Оспаривание — узкий инструмент, ack — нет.

## Решение

1. `postQuestionReplyRequest` / `postAgentQuestionReplyRequest` получают
   `dispute bool`.
2. Реплика в resolved-тред переоткрывает его ТОЛЬКО при `dispute: true`.
   Без флага: сообщение ложится в историю, статус остаётся `resolved`,
   `AttentionOnEntry` не вызывается (attention не трогаем), событие
   `question_reopened` не публикуется. Fan-out участникам остаётся —
   иначе ack никто не увидит.
3. Человеку resolved-тред по-прежнему финален (`409`), кроме `fyi`; правило
   `--dispute` для fyi единое: без флага реплика человека в fyi остаётся
   заметкой в истории и тред не становится decision.
4. CLI: флаг `--dispute` у `rocket task reply` и `rocket agent reply`;
   после реплики без него в закрытый тред печатается подсказка.
5. Документация: `docs/12-tasks.md`, `docs/03-daemon-api.md`,
   `docs/prompts/orchestrator.md`, `internal/prompts/templates/orchestrator.md`.

## Порядок (TDD)

- [ ] API-тесты: ack не переоткрывает; `dispute: true` переоткрывает (обе поверхности)
- [ ] Реализация в обоих хендлерах
- [ ] CLI-тесты на флаг и подсказку → реализация
- [ ] Документация и промпты
- [ ] `go test ./...`
