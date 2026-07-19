# Фаза 5 «Мульти-агент» — план имплементации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Оркестратор и воркеры могут быть разными агентами: адаптер codex (launch + активность), выбор `--agent` per-spawn (есть с фазы 1), `default_agent` (есть), doctor проверяет всех. Критерий: воркер на codex проходит полный цикл (спавн → PR → merge → cleanup) под оркестратором на claude-code — на GitHub-стабе.

**Architecture:** Интерфейс Agent уже финален (Name/Available/LaunchCommand/Env/SetupWorkspace/Activity). Codex-адаптер — второй компилируемый адаптер в реестре; системный промпт — через `AGENTS.md` в worktree (codex-механика), секции Superpowers вырезаются адаптером по маркерам в шаблонах. Активность — сессионные JSONL codex (`~/.codex/sessions/`) по принципу claude-code-адаптера: mtime + последняя запись. Детали codex CLI 0.138.0 проверяются эмпирически до имплементации.

**Tech Stack:** без новых зависимостей.

## Global Constraints

- Сначала — три Important-фоллоу-апа финального ревью фазы 4 (Task 1), они блокируют надёжность цикла, который фаза 5 переиспользует.
- Codex-адаптер не должен менять поведение claude-code-адаптера; общие изменения prompts — только добавление маркеров секций (обратно совместимо: рендер без стрипа идентичен текущему).
- Маркеры skills-секций в шаблонах: строки-комментарии `<!-- skills:start -->` / `<!-- skills:end -->`; `prompts.StripSkills(text string) string` удаляет секции вместе с маркерами; рендер для агентов со skills оставляет текст, но маркеры ВСЕГДА удаляются из финального вывода (StripMarkers).
- Codex запускается неинтерактивно-безопасно: интерактивный TUI с авто-подтверждением (реальные флаги уточняются в Task 2 и фиксируются тестом; ожидаемо `--sandbox danger-full-access` или актуальный аналог; ложь в плане запрещена — Task 2 обновляет Task 3 бриф фактами).
- E2E не требует реального GitHub-токена (стаб как в фазе 4); live-проверка — пользовательская (гайд обновить).
- Прежние инварианты: тесты/vet/gofmt; сабагентам запрещены merge/rebase/reset; никакого shell вне задокументированных мест.

---

### Task 1: Фоллоу-апы фазы 4 (надёжность merge-cleanup)

**Files:** Modify `internal/ghpoller/poller.go`, `internal/ghpoller/reactions.go`, `internal/daemon/daemon.go`; tests.

- (a) Restart-durable grace: при старте демона (после Recover, до Serve) — Reactions.RearmPending(): live-воркеры с pr_state=="merged" → scheduleGrace (событий/переходов подзадач не дублировать: только таймер).
- (b) Closed-unmerged re-discovery: в tickSession, если stored PRState=="closed" (не merged) — вернуться к discovery по ветке (новый PR на той же ветке подхватывается; старый номер перезаписывается; событие pr.opened для нового).
- (c) Persist-before-notify в updatePR: UpdateSessionPR ДО notify/events; при ошибке записи — не уведомлять.
- [ ] TDD: (a) сеанс merged+running, новый Reactions.RearmPending → после grace Complete; (b) стаб: closed PR затем новый open PR тем же branch → новый номер записан, события; (c) существующие тесты проходят с переставленным порядком (+тест: перезапись состояния до notify — проверить порядок через записанный store к моменту вызова notifier'а).
- [ ] Commit: `fix(ghpoller): durable merge grace, closed-PR rediscovery, persist-before-notify`.

### Task 2: Эмпирическая разведка codex CLI + маркеры skills-секций

**Files:** Modify `internal/prompts/templates/*.md` (маркеры вокруг Superpowers-секций: в orchestrator.md секция «## Process: Superpowers» + упоминания в kickoff (пункты про brainstorming/writing-plans — обернуть предложения-ссылки на skills) и worker.md «## Workflow (Superpowers is mandatory)» — вырезаемые блоки должны оставлять связный текст: для kickoff/worker добавить внутрь маркеров альтернативные формулировки НЕ нужно — вне маркеров оставить нейтральные шаги), `internal/prompts/prompts.go` (+StripSkills, StripMarkers — всегда в Render), tests.
- Разведка (в отчёт + правка брифа Task 3): `codex --help`, `codex exec --help`, поведение `codex` интерактивно в tmux (короткий live-запуск: подтвердить, что paste+Enter работает, какие флаги дают авто-подтверждение (`--sandbox`? `-a`/`--ask-for-approval`? config), где лежат session-JSONL (`ls ~/.codex/sessions/`), их формат (последняя запись, тип), поддерживает ли позиционный prompt первый ход, читает ли AGENTS.md из cwd.
- [ ] TDD prompts: StripSkills убирает секции целиком; рендер обоих режимов не содержит маркеров; шаблоны без skills-секций связны (нет висячих заголовков — ручная проверка текста в тесте: нет "Superpowers" в стрипнутом выводе).
- [ ] Commit: `feat(prompts): skills section markers and stripping` (+ отчёт разведки в .superpowers/sdd/).

### Task 3: Адаптер codex — launch/env/setup

**Files:** Create `internal/agent/codex/codex.go` (+тест); Modify daemon (blank-import), doctor (реестр сам подхватит).

- `Name()="codex"`; `Available()`: LookPath("codex"). `SetupWorkspace`: если SystemPrompt != "" — записать СТРИПНУТЫЙ (StripSkills) промпт в `<worktree>/AGENTS.md` (merge-политика: если AGENTS.md существует — наш блок в начало с маркерами `<!-- rocket:start -->…end -->`, идемпотентно); `.rocket`-хуков у codex нет (активность — только поллинг; push-канал недоступен — задокументировать).
- `LaunchCommand`: по фактам Task 2 (ожидаемо `codex` + флаги авто-подтверждения + позиционный FirstMessage; модель через `-m` если задана). `Env`: ROCKET_* + без CLAUDECODE.
- Activity(ctx, ref): `~/.codex/sessions/**.jsonl` — файл, соответствующий worktree (по разведке Task 2: как связать сессию с cwd — если в JSONL есть cwd-поле, фильтровать по нему; иначе — самый свежий файл, изменённый после старта сессии, с fallback ErrNoSignal; зафиксировать выбранную стратегию в отчёте); классификация mtime/последняя запись как в claude-code; нет сигнала → ErrNoSignal (монитор справится по TTY/pane).
- [ ] TDD: LaunchCommand/Env/SetupWorkspace (merge AGENTS.md, идемпотентность, стрип skills), Activity на фикстурах.
- [ ] Commit: `feat: codex agent adapter`.

### Task 4: Интеграционная проверка codex live (короткая)

- Ручная (в отчёт): spawn codex-сессии через rocket в scratch-репо (`rocket spawn --agent codex ...` из оркестраторской сессии ИЛИ временно через manager-тест), Inject «create PING.md, commit», проверить: агент получил AGENTS.md-промпт, выполнил, активность в `ls` меняется (poll-канал), сообщение через `rocket send` доставляется. Починить найденное (отдельные коммиты). Убить и вычистить.
- [ ] Commit(ы): фиксы по фактам.

### Task 5: E2E фазы — codex-воркер под claude-оркестратором на GitHub-стабе

- Сценарий по образцу P4 Task 8 (стаб, короткие интервалы): `rocket up` (claude-оркестратор) → оркестратору команда заспавнить воркера `--agent codex` с брифом «create PING.md, commit, report» → воркер-codex коммитит в ветку → стаб открывает PR → CI failing → codex-воркер получает `[rocket] CI failing…` и реагирует (достаточно: сообщение видно в его терминале) → стаб merged → grace → cleanup воркера, подзадача done, ветка жива. `rocket doctor` показывает оба агента ✅.
- Обновить docs/testing/phase-4-live-github.md → переименовать? НЕТ: добавить docs/testing/phase-5-live-multiagent.md (короткий: как повторить с реальным токеном и codex).
- [ ] Прогнать, чинить, транскрипт в отчёт; Commit: фиксы + `docs: phase 5 live multiagent guide`.

## Self-Review (выполнен)

- Роадмап фазы 5 покрыт: финализация интерфейса — уже финален (констатация), codex-адаптер T3, `--agent`/`defaults.agent` — существуют с фаз 1/3 (E2E проверяет), doctor — реестр подхватывает автоматически (E2E проверяет), критерий — T5.
- T1 не из роадмапа фазы 5, но обязателен (долг фазы 4, пайплайн переиспользуется T5).
- Риск: реальное поведение codex CLI может отличаться от ожиданий — управляется T2 (разведка до имплементации) и T4 (live-проверка до E2E).
